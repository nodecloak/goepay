package cryptopay

import (
	"context"
	"crypto/rsa"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/compat"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"goepay/internal/platform/httpclient"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type MerchantReader interface {
	Merchant(context.Context, int64) (payment.Merchant, error)
}
type Notifier struct {
	Service     *Service
	Merchants   MerchantReader
	Client      *http.Client
	PlatformKey *rsa.PrivateKey
	HideName    bool
}

func NotificationURL(raw string, v Invoice, m payment.Merchant, key *rsa.PrivateKey, now time.Time, hide bool) (string, error) {
	if v.State != "paid" && v.State != "overpaid" || v.MerchantID != m.ID || !validURL(raw) {
		return "", ErrInvalid
	}
	u, e := url.Parse(raw)
	if e != nil {
		return "", ErrInvalid
	}
	p, e := compat.Parse(u.Query())
	if e != nil {
		return "", e
	}
	name := v.Name
	if hide {
		name = "product"
	}
	for k, val := range (compat.Params{"pid": strconv.FormatInt(v.MerchantID, 10), "trade_no": "USDT" + v.ID, "out_trade_no": v.OutTradeNo, "type": "usdt", "name": name, "trade_status": "TRADE_SUCCESS", "asset": "USDT", "network": v.Network, "crypto_amount": v.Amount}) {
		p[k] = val
	}
	delete(p, "sign")
	delete(p, "sign_type")
	delete(p, "timestamp")
	delete(p, "money")
	delete(p, "amount")
	delete(p, "param")
	if v.Param != "" {
		p["param"] = v.Param
	}
	if v.Version == "v1" || v.Version == "v2" {
		if v.CNYAmount <= 0 {
			return "", ErrInvalid
		}
		p["money"] = ledger.Format(v.CNYAmount)
	} else {
		p["amount"] = v.Amount
	}
	if v.Version == "v2" {
		p["timestamp"] = strconv.FormatInt(now.Unix(), 10)
		p["sign_type"] = "RSA"
		p["sign"], e = compat.SignRSAWithProfile(p, key, compat.ProfileSDKV2)
		if e != nil {
			return "", e
		}
	} else {
		if m.APIKey == "" {
			return "", ErrUnavailable
		}
		p["sign_type"] = "MD5"
		p["sign"] = compat.SignMD5(p, m.APIKey)
	}
	q := url.Values{}
	for k, val := range p {
		q.Set(k, val)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// RunOne retains its durable row lock until the bounded webhook attempt is
// recorded. A lost HTTP acknowledgement may be retried with the same trade_no;
// merchants must deduplicate payment notifications, as in the existing API.
func (n *Notifier) RunOne(ctx context.Context) (bool, error) {
	if n.Service == nil || n.Service.Pool == nil || n.Merchants == nil {
		return false, ErrUnavailable
	}
	tx, e := n.Service.Pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	var id string
	var attempts int
	e = tx.QueryRow(ctx, "SELECT invoice_id,attempts FROM crypto_notifications WHERE NOT delivered AND next_attempt<=clock_timestamp() ORDER BY next_attempt FOR UPDATE SKIP LOCKED LIMIT 1").Scan(&id, &attempts)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return false, nil
		}
		return false, e
	}
	v, e := readInvoice(ctx, tx, "id=$1", id)
	if e != nil {
		return true, e
	}
	delivered := v.NotifyURL == ""
	var deliveryErr error
	if !delivered {
		delivered, deliveryErr = n.deliver(ctx, v)
	}
	delay := int64(1) << min(attempts+1, 16)
	_, e = tx.Exec(ctx, "UPDATE crypto_notifications SET attempts=attempts+1,delivered=$2,next_attempt=clock_timestamp()+$3*interval '1 second' WHERE invoice_id=$1", id, delivered, delay)
	if e != nil {
		return true, e
	}
	if e = tx.Commit(ctx); e != nil {
		return true, e
	}
	return true, deliveryErr
}

func (n *Notifier) deliver(ctx context.Context, v Invoice) (bool, error) {
	m, e := n.Merchants.Merchant(ctx, v.MerchantID)
	if e != nil {
		return false, e
	}
	target, e := NotificationURL(v.NotifyURL, v, m, n.PlatformKey, time.Now(), n.HideName)
	if e != nil {
		return false, e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", target, nil)
	if e != nil {
		return false, e
	}
	client := n.Client
	if client == nil {
		client = httpclient.New()
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copyClient.Timeout <= 0 || copyClient.Timeout > 15*time.Second {
		copyClient.Timeout = 15 * time.Second
	}
	res, e := copyClient.Do(req)
	if e != nil {
		return false, errors.New("crypto notification delivery failed")
	}
	defer res.Body.Close()
	body, e := io.ReadAll(io.LimitReader(res.Body, 65))
	if e != nil || res.StatusCode != 200 || strings.TrimSpace(string(body)) != "success" {
		return false, errors.New("crypto notification not acknowledged")
	}
	return true, nil
}
