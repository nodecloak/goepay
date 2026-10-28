// Package notification dispatches durable merchant payment notifications.
package notification

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"goepay/internal/compat"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"goepay/internal/platform/httpclient"
	"goepay/internal/platform/jobs"
)

type Repository interface {
	OrderByTradeNo(context.Context, string) (payment.Order, error)
	Merchant(context.Context, int64) (payment.Merchant, error)
}
type Handler struct {
	Store       Repository
	Client      *http.Client
	PlatformKey *rsa.PrivateKey
	Now         func() time.Time
	// Planner selects how a notification leaves the platform (中转代理). When
	// nil, delivery is always direct. When set, a planning error fails the
	// delivery closed — a configured proxy is never silently bypassed.
	Planner DeliveryPlanner
}

// Delivery is how one notification is sent. Mode 0 = direct, 1 = through a
// proxy server (Client is a proxied client), 2 = through a proxy-API relay.
type Delivery struct {
	Mode           int
	Client         *http.Client
	API            ProxyAPIDeliverer
	APIURL, APIKey string
}

// ProxyAPIDeliverer posts the target URL to a mode-2 relay and returns its
// response. httpclient.ProxyAPIClient satisfies it.
type ProxyAPIDeliverer interface {
	Deliver(ctx context.Context, apiURL, apikey, target string) (int, string, error)
}

// DeliveryPlanner resolves the current 中转代理 plan at send time.
type DeliveryPlanner interface {
	DeliveryPlan(ctx context.Context) (Delivery, error)
}

// URL constructs both notify and return URLs with the current timestamp on each retry.
// Base query parameters are signed too, preventing unsigned parameter collisions.
func URL(raw string, o payment.Order, m payment.Merchant, key *rsa.PrivateKey, now time.Time) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("invalid notification URL")
	}
	p, e := compat.Parse(u.Query())
	if e != nil {
		return "", e
	}
	for k, v := range (compat.Params{"pid": strconv.FormatInt(o.MerchantID, 10), "trade_no": o.TradeNo, "out_trade_no": o.OutTradeNo, "type": o.Type, "name": o.Name, "money": ledger.Format(o.Amount), "trade_status": "TRADE_SUCCESS"}) {
		p[k] = v
	}
	delete(p, "sign")
	delete(p, "sign_type")
	delete(p, "timestamp")
	if o.Param != "" {
		p["param"] = o.Param
	} else {
		delete(p, "param")
	}
	switch o.Version {
	case "v2":
		p["timestamp"] = strconv.FormatInt(now.Unix(), 10)
		p["sign_type"] = "RSA"
		p["sign"], e = compat.SignRSAWithProfile(p, key, compat.ProfileSDKV2)
		if e != nil {
			return "", e
		}
	case "", "v1":
		p["sign_type"] = "MD5"
		p["sign"] = compat.SignMD5(p, m.APIKey)
	default:
		return "", errors.New("unsupported payment version")
	}
	query := url.Values{}
	for k, v := range p {
		query.Set(k, v)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}
func (h Handler) Handle(ctx context.Context, j jobs.Job) error {
	if j.Kind != "payment.notify" {
		return errors.New("unsupported job kind")
	}
	if gate, ok := h.Store.(interface {
		VerifyScheduledJobClaim(context.Context, jobs.Job) error
	}); ok {
		if e := gate.VerifyScheduledJobClaim(ctx, j); e != nil {
			return e
		}
	}
	var payload struct {
		TradeNo string `json:"trade_no"`
	}
	if e := json.Unmarshal(j.Payload, &payload); e != nil || payload.TradeNo == "" {
		return errors.New("invalid job payload")
	}
	o, e := h.Store.OrderByTradeNo(ctx, payload.TradeNo)
	if e != nil {
		return e
	}
	if o.Status != "paid" {
		return errors.New("cannot notify unpaid order")
	}
	if o.NotifyURL == "" {
		return errors.New("empty notification URL")
	}
	m, e := h.Store.Merchant(ctx, o.MerchantID)
	if e != nil {
		return e
	}
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	target, e := URL(o.NotifyURL, o, m, h.PlatformKey, now)
	if e != nil {
		return e
	}
	delivery := Delivery{Mode: 0}
	if h.Planner != nil {
		delivery, e = h.Planner.DeliveryPlan(ctx)
		if e != nil {
			if ctx.Err() != nil {
				return errors.New("notification delivery interrupted")
			}
			// A configured proxy that cannot be resolved fails the delivery;
			// it is never silently sent direct.
			return observedDeliveryFailure(j, "proxy", "notification proxy unavailable")
		}
	}
	if delivery.Mode == 2 {
		if delivery.API == nil {
			return observedDeliveryFailure(j, "proxy", "notification proxy unavailable")
		}
		status, body, de := delivery.API.Deliver(ctx, delivery.APIURL, delivery.APIKey, target)
		if de != nil {
			if ctx.Err() != nil {
				return errors.New("notification delivery interrupted")
			}
			return observedDeliveryFailure(j, "transport", "notification transport failed")
		}
		if status != http.StatusOK || !httpclient.ProxyAPIAcknowledged(body) {
			return observedDeliveryFailure(j, "ack", fmt.Sprintf("notification not acknowledged (HTTP %d)", status))
		}
		return nil
	}
	client := h.Client
	if delivery.Mode == 1 {
		if delivery.Client == nil {
			return observedDeliveryFailure(j, "proxy", "notification proxy unavailable")
		}
		client = delivery.Client
	} else if client == nil {
		client = httpclient.New()
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if e != nil {
		return errors.New("invalid notification request")
	}
	req.Header.Set("User-Agent", "GOEPAY-Notify/1.0")
	response, e := client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return errors.New("notification delivery interrupted")
		}
		return observedDeliveryFailure(j, "transport", "notification transport failed")
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, 4097))
	if e != nil {
		if ctx.Err() != nil {
			return errors.New("notification delivery interrupted")
		}
		return observedDeliveryFailure(j, "read", "notification response read failed")
	}
	if len(raw) > 4096 || response.StatusCode != http.StatusOK || string(raw) != "success" {
		return observedDeliveryFailure(j, "ack", fmt.Sprintf("notification not acknowledged (HTTP %d)", response.StatusCode))
	}
	return nil
}
