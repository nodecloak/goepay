package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"goepay/internal/backoffice"
	"goepay/internal/captcha203"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"goepay/internal/publictestpay203"
)

func NewPublicTestPayService203(s *Server, verifier captcha203.Verifier) (*publictestpay203.Service, error) {
	if s == nil {
		return nil, publictestpay203.ErrUnavailable
	}
	origin := ""
	if s.ProviderFlow != nil && s.ProviderFlow.CallbackBaseURL != "" {
		origin = strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/")
	}
	return NewPublicTestPayService203ForMaster(s, verifier, s.CheckoutKey, origin)
}
func NewPublicTestPayService203ForMaster(s *Server, verifier captcha203.Verifier, master []byte, origin string) (*publictestpay203.Service, error) {
	if s == nil {
		return nil, publictestpay203.ErrUnavailable
	}
	origin = strings.TrimRight(origin, "/")
	return publictestpay203.New(s.Pool, s.Store, master, origin, publicTestEngine203{s: s, origin: origin}, verifier, func(remote, purpose string) bool {
		return s.allowCollectionAttempt(remote, "public-testpay203:"+purpose, time.Now())
	})
}
func (s *Server) testPayService203() (*publictestpay203.Service, error) {
	if s.PublicTestPay203 != nil {
		return s.PublicTestPay203, nil
	}
	return NewPublicTestPayService203(s, nil)
}
func (s *Server) publicTestPayRoutes203(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/console/v1/public/testpay/") && r.URL.Path != "/console/v1/public/captcha" {
		return false
	}
	service, e := s.testPayService203()
	if e != nil {
		response(w, 503, -1, "public test payment unavailable", nil)
		return true
	}
	service.ServeHTTP(w, r)
	return true
}

type publicTestEngine203 struct {
	s      *Server
	origin string
}

func (e publicTestEngine203) Types(ctx context.Context, mid int64) ([]publictestpay203.PayType, error) {
	view, err := e.s.withParentRuntime(ctx, mid, "")
	if err != nil || view.ProviderFlow == nil || !view.ProviderFlow.AllowLive || !view.paymentExecutionWriteAllowed() && !view.ProviderFlow.LocalSimulation {
		return nil, publictestpay203.ErrUnavailable
	}
	allowed := map[string]bool{}
	for _, id := range view.MerchantChannels[mid] {
		p := view.ProviderFlow.Providers[id]
		if p != nil && (p.ID() == "alipay" || p.ID() == "wxpay") && p.Capabilities().Query && hasProviderMethod(p.Capabilities().Methods, "native") {
			allowed[p.ID()] = true
		}
	}
	rows, err := e.s.Pool.Query(ctx, `SELECT id,body FROM backoffice_records WHERE resource='pay-types' AND body->'enabled'='true'::jsonb ORDER BY id LIMIT 101`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []publictestpay203.PayType{}
	for rows.Next() {
		var id int64
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var body struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &body) != nil {
			return nil, publictestpay203.ErrUnavailable
		}
		if allowed[body.Type] && body.Name != "" && len(body.Name) <= 100 {
			out = append(out, publictestpay203.PayType{ID: strconv.FormatInt(id, 10), Name: body.Type, ShowName: body.Name})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// A type that was never saved in 支付方式 follows the original catalog
	// default, as the payment router does (payTypeCatalogOpen): alipay and
	// wxpay start open, so a fresh install offers them like the original
	// pre_type rows. Such a type has no record id, so it is offered under
	// testPayCatalogTypeID; typeid stays a positive integer.
	ids := map[string]bool{}
	for _, t := range out {
		ids[t.ID] = true
	}
	for i, t := range originalPayTypes {
		if !t.Open || !allowed[t.Name] {
			continue
		}
		var saved bool
		if err = e.s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_records WHERE resource='pay-types' AND body->>'type'=$1)`, t.Name).Scan(&saved); err != nil {
			return nil, err
		}
		id := strconv.FormatInt(testPayCatalogTypeID+int64(i), 10)
		if !saved && !ids[id] {
			out = append(out, publictestpay203.PayType{ID: id, Name: t.Name, ShowName: t.ShowName})
		}
	}
	// Catalog order, like the original's pre_type id order (支付宝 first);
	// records of one type keep their id order.
	sort.SliceStable(out, func(i, j int) bool { return payTypeCatalogIndex(out[i].Name) < payTypeCatalogIndex(out[j].Name) })
	if len(out) > 100 {
		return nil, publictestpay203.ErrUnavailable
	}
	return out, nil
}

// testPayCatalogTypeID is the typeid base of a catalog 支付方式 that has no
// stored record (originalPayTypes index added). Record ids come from a
// sequence and never reach this range.
const testPayCatalogTypeID int64 = 9_000_000_000_000_000_000

func payTypeCatalogIndex(payType string) int {
	for i, t := range originalPayTypes {
		if t.Name == payType {
			return i
		}
	}
	return len(originalPayTypes)
}
func (e publicTestEngine203) Create(ctx context.Context, in publictestpay203.Intent, source string) (string, string, error) {
	ctx = payment.WithCoreRiskCustomerIP(ctx, source, "direct_http")
	if err := backoffice.New(e.s.Pool, e.s.Store).CheckPayment(ctx, backoffice.PaymentContext{MerchantID: in.MerchantID, ClientIP: source}); err != nil {
		return "", "", err
	}
	view, err := e.s.withParentRuntime(ctx, in.MerchantID, in.OutTradeNo)
	if err != nil {
		return "", "", err
	}
	target := e.origin + "/test?ok=1&trade_no=" + url.QueryEscape(in.OutTradeNo)
	if !validURL(target) {
		if view.ProviderFlow == nil || !view.ProviderFlow.LocalSimulation {
			return "", "", publictestpay203.ErrUnavailable
		}
		target = ""
	}
	params := compat.Params{"type": in.PayType, "out_trade_no": in.OutTradeNo, "name": "支付测试", "money": ledger.Format(in.Amount), "clientip": source, "return_url": target, "param": "public-testpay203:" + in.ID}
	channel, method, err := view.providerRoutedBrowserRequest(ctx, in.MerchantID, params)
	if err != nil {
		return "", "", err
	}
	if method != "native" {
		return "", "", publictestpay203.ErrUnavailable
	}
	view, err = view.paymentIntentServer(ctx, in.MerchantID, in.OutTradeNo, channel)
	if err != nil {
		return "", "", err
	}
	cost, err := view.ProviderFlow.PaymentCostQuote(channel, method)
	if err != nil {
		return "", "", err
	}
	order, err := view.Store.CreateOrder(ctx, payment.CreateOrderInput{PaymentCost: cost, Version: "v1", MerchantID: in.MerchantID, OutTradeNo: in.OutTradeNo, Name: "支付测试", Type: in.PayType, Amount: in.Amount, ReturnURL: target, Param: params["param"]})
	if err != nil {
		return "", "", err
	}
	if err = view.Store.SetOrderClientIP(ctx, order.ID, source); err != nil {
		return order.TradeNo, "", err
	}
	attempt, err := view.ProviderFlow.Submit(ctx, order.TradeNo, channel, method, source, "")
	if err != nil || attempt.State != "pending" || attempt.Material == "" {
		return order.TradeNo, "", publictestpay203.ErrConflict
	}
	checkout, err := e.Checkout(ctx, order)
	return order.TradeNo, checkout, err
}
func (e publicTestEngine203) Checkout(ctx context.Context, order payment.Order) (string, error) {
	attempt, err := e.s.Store.ProviderAttempt(ctx, order.TradeNo)
	if err != nil || attempt.Material == "" || attempt.State != "pending" && order.Status != "paid" {
		return "", publictestpay203.ErrConflict
	}
	target, err := url.Parse(e.s.checkoutURL(order.TradeNo, "checkout"))
	if err != nil {
		return "", err
	}
	return target.EscapedPath() + "?" + target.RawQuery, nil
}
func (s *Server) captchaSettingsConsole203(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "settings" || parts[2] != "captcha" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.Issuer != nil || session.User.AdminClass != "super" {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	service, err := s.testPayService203()
	if err != nil {
		response(w, 503, -1, "CAPTCHA key settings unavailable", nil)
		return true
	}
	actor := captcha203.Actor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	var out captcha203.KeyView
	switch r.Method {
	case "GET":
		out, err = service.Credentials().Read(r.Context(), actor)
	case "PATCH":
		var fields map[string]json.RawMessage
		if !decodeFundsJSON(w, r, &fields) {
			return true
		}
		var in captcha203.KeyUpdate
		if json.Unmarshal(fields["action"], &in.Action) != nil || json.Unmarshal(fields["expected_fingerprint"], &in.Expected) != nil || len(fields) != 2 && len(fields) != 3 {
			response(w, 400, -1, "exact CAPTCHA key operation required", nil)
			return true
		}
		for field, value := range fields {
			if field != "action" && field != "expected_fingerprint" && field != "key" || string(value) == "null" {
				response(w, 400, -1, "unsupported CAPTCHA key field", nil)
				return true
			}
		}
		if value, ok := fields["key"]; ok {
			if json.Unmarshal(value, &in.Key) != nil {
				response(w, 400, -1, "string CAPTCHA key required", nil)
				return true
			}
		}
		if in.Action == "replace" && len(fields) != 3 || in.Action == "clear" && len(fields) != 2 {
			response(w, 400, -1, "exact CAPTCHA key operation required", nil)
			return true
		}
		out, err = service.Credentials().Update(r.Context(), actor, in)
	default:
		w.Header().Set("Allow", "GET, PATCH")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if err != nil {
		status := 503
		if errors.Is(err, identity.ErrAdminSession) {
			status = 403
		} else if errors.Is(err, captcha203.ErrConflict) {
			status = 409
		} else if errors.Is(err, captcha203.ErrInvalid) {
			status = 422
		}
		response(w, status, -1, "CAPTCHA key operation could not be confirmed; reload before retrying", nil)
	} else {
		response(w, 200, 0, "", out)
	}
	return true
}
