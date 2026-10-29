package payment

// Port of epay_release/plugins/paypal (PayPal). The original flow:
//   submit():  OAuth2 client-credentials -> POST /v2/checkout/orders -> show
//              the "approve" link (type=jump). Payer approves on PayPal.
//   return():  GET with ?token&PayerID -> POST .../capture -> processReturn.
//   webhook(): PAYMENT.CAPTURE.COMPLETED event, verified by fetching the cert
//              named in the attacker-controlled PAYPAL-CERT-URL request header.
//   refund():  POST /v2/payments/captures/{id}/refund.
//
// Currency: PayPal settles in the channel's currency_code (default behaviour:
// amount = round(realmoney * currency_rate, 2) in that currency), NOT CNY. We
// keep that currency math inside the plugin and report the settlement currency
// in CreateResult only as the amount we send upstream; the GOEPAY core still
// treats the order as CNY. See p03.md for the owner decision.
//
// VerifyNotification is deliberately NOT implemented. The original webhook
// verifies the signature by downloading a certificate from the URL given in
// the PAYPAL-CERT-URL request header (openssl_pkey_get_public(get_curl(...))).
// Trusting a caller-named URL for the verifying key is an SSRF and a key-
// substitution hole, and reproducing it faithfully needs extra state (the
// webhook id as appsecret, a fetched+cached cert). Per the brief we implement
// the parts that verify without extra state (Create, Refund) and leave the
// webhook to confirmation-by-capture on return / an operator-driven query.

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "paypal",
		Title:  "PayPal",
		Author: "PayPal",
		Link:   "https://www.paypal.com/",
		Types:  []string{"paypal"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "ClientId", Required: true},
			{Key: "appkey", Label: "ClientSecret", Secret: true, Required: true},
			{Key: "appswitch", Label: "模式选择", Options: []PluginOption{{Value: "0", Label: "线上模式"}, {Value: "1", Label: "沙盒模式"}}},
			{Key: "currency_code", Label: "结算货币", Options: paypalCurrencyOptions()},
			{Key: "currency_rate", Label: "货币汇率", Note: "例如1元人民币兑换0.137美元(USD)，则此处填0.137"},
		},
		Build: newPaypalPlugin,
	})
}

func paypalCurrencyOptions() []PluginOption {
	// Order matches the original 'options' map.
	codes := []string{"USD", "AUD", "BRL", "CAD", "CNY", "CZK", "DKK", "EUR", "HKD", "HUF", "INR", "ILS", "JPY", "MYR", "MXN", "TWD", "NZD", "NOK", "PHP", "PLN", "GBP", "RUB", "SGD", "SEK", "CHF", "THB"}
	out := make([]PluginOption, 0, len(codes))
	for _, c := range codes {
		out = append(out, PluginOption{Value: c, Label: c})
	}
	return out
}

// paypalAPIHosts: mode 0 live, 1 sandbox (PayPalClient::$api_url).
var paypalAPIHosts = map[string]string{"0": "https://api-m.paypal.com", "1": "https://api-m.sandbox.paypal.com"}

type paypalPlugin struct {
	pluginBase
	clientID, clientSecret string
	apiHost                string
	currencyCode           string
	// rateMilli is currency_rate * 1000 kept as an integer so we avoid float
	// drift; PayPal amounts are formatted from it (see paypalAmount).
	rateMilli int64
}

func newPaypalPlugin(c PluginChannel) (Provider, error) {
	id := strings.TrimSpace(c.Config["appid"])
	secret := c.Config["appkey"]
	if id == "" || len(id) > 128 || secret == "" || len(secret) > 256 {
		return nil, ErrInvalid
	}
	mode := c.Config["appswitch"]
	if mode == "" {
		mode = "0"
	}
	host, ok := paypalAPIHosts[mode]
	if !ok {
		return nil, ErrInvalid
	}
	currency := strings.TrimSpace(c.Config["currency_code"])
	if currency == "" {
		currency = "USD"
	}
	if !paypalCurrencyKnown(currency) {
		return nil, ErrInvalid
	}
	rate, e := paypalParseRate(c.Config["currency_rate"])
	if e != nil {
		return nil, e
	}
	base, e := newPluginBase(c, pluginAccount(host, id, currency))
	if e != nil {
		return nil, e
	}
	return &paypalPlugin{pluginBase: base, clientID: id, clientSecret: secret, apiHost: host, currencyCode: currency, rateMilli: rate}, nil
}

func paypalCurrencyKnown(code string) bool {
	for _, o := range paypalCurrencyOptions() {
		if o.Value == code {
			return true
		}
	}
	return false
}

// paypalParseRate reads currency_rate like PHP: empty -> 1 (the original's
// "if(!$channel['currency_rate']) $channel['currency_rate'] = 1"). Stored as
// milli-units (0.137 -> 137) with at most 3 decimal places.
func paypalParseRate(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 1000, nil
	}
	whole, frac := raw, ""
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		whole, frac = raw[:i], raw[i+1:]
	}
	if len(frac) > 3 {
		return 0, ErrInvalid
	}
	for len(frac) < 3 {
		frac += "0"
	}
	if whole == "" {
		whole = "0"
	}
	w, e := strconv.ParseInt(whole, 10, 64)
	if e != nil || w < 0 || w > 1000000 {
		return 0, ErrInvalid
	}
	f, e := strconv.ParseInt(frac, 10, 64)
	if e != nil || f < 0 {
		return 0, ErrInvalid
	}
	rate := w*1000 + f
	if rate <= 0 {
		return 0, ErrInvalid
	}
	return rate, nil
}

// paypalAmount converts GOEPAY cents to the settlement currency string:
// round(cents/100 * rate, 2). We compute in integers: cents * rateMilli gives
// hundred-thousandths of a currency unit; round to 2 decimals.
func (p *paypalPlugin) paypalAmount(cents int64) string {
	// cents (1/100 unit of CNY) * rateMilli (1/1000) = value in 1/100000 of
	// the target currency. Round to 1/100 -> divide by 1000 with rounding.
	hundredThousandths := cents * p.rateMilli
	cents2 := (hundredThousandths + 500) / 1000
	return amountString(cents2)
}

func (p *paypalPlugin) methods() []string {
	// Only a redirect-to-approval flow; map to "h5" for non-alipay pay types.
	return []string{"h5"}
}

func (p *paypalPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Query: true, Refund: true}
}

func (p *paypalPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

// paypalToken fetches an OAuth2 access token (client-credentials, HTTP Basic).
func (p *paypalPlugin) paypalToken(ctx context.Context) (string, error) {
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(p.clientID+":"+p.clientSecret))
	raw, e := p.pluginCall(ctx, "paypal.token", http.MethodPost, p.apiHost+"/v1/oauth2/token",
		[]byte("grant_type=client_credentials"), "application/x-www-form-urlencoded",
		map[string]string{"Authorization": auth, "Accept": "application/json"})
	if e != nil {
		return "", e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return "", &UnknownOutcome{Operation: "paypal.token", Cause: e}
	}
	tok := jsonText(out, "access_token")
	if tok == "" || strings.ContainsAny(tok, " \r\n") {
		return "", rejected("PAYPAL_NO_TOKEN")
	}
	return tok, nil
}

func (p *paypalPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	tok, e := p.paypalToken(ctx)
	if e != nil {
		return CreateResult{}, e
	}
	// POST /v2/checkout/orders (PayPalClient::createOrder). custom_id and
	// invoice_id carry our trade number so the capture ties back to us.
	body := map[string]any{
		"intent": "CAPTURE",
		"purchase_units": []map[string]any{{
			"amount":      map[string]string{"currency_code": p.currencyCode, "value": p.paypalAmount(r.Amount)},
			"description": r.Subject,
			"custom_id":   r.TradeNo,
			"invoice_id":  r.TradeNo,
		}},
		"application_context": map[string]string{
			"cancel_url": r.ReturnURL,
			"return_url": r.ReturnURL,
		},
	}
	raw, e := p.postJSON(ctx, "paypal.create", p.apiHost+"/v2/checkout/orders", body,
		map[string]string{"Authorization": "Bearer " + tok, "Accept": "application/json"})
	if e != nil {
		return CreateResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		// Order may have been created; the reply is unreadable.
		return CreateResult{}, &UnknownOutcome{Operation: "paypal.create", Cause: e}
	}
	orderID := jsonText(out, "id")
	approval := paypalApprovalLink(out)
	if approval == "" || !validPaymentURL(approval) {
		return CreateResult{}, rejected("PAYPAL_NO_APPROVAL_URL")
	}
	return CreateResult{MaterialType: "jump", Material: approval, ProviderTradeNo: orderID}, nil
}

// paypalApprovalLink finds the links[] entry with rel == "approve".
func paypalApprovalLink(out map[string]any) string {
	links, ok := out["links"].([]any)
	if !ok {
		return ""
	}
	for _, l := range links {
		m, ok := l.(map[string]any)
		if !ok {
			continue
		}
		if jsonText(m, "rel") == "approve" {
			return jsonText(m, "href")
		}
	}
	return ""
}

// Refund posts to /v2/payments/captures/{capture_id}/refund. The original
// refunds api_trade_no (the capture id) for refundmoney * currency_rate in the
// channel currency. We require the caller to pass the capture id as
// ProviderTradeNo. code 0 on an id in the reply => success.
func (p *paypalPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	tok, e := p.paypalToken(ctx)
	if e != nil {
		return RefundResult{}, e
	}
	body := map[string]any{
		"amount": map[string]string{"currency_code": p.currencyCode, "value": p.paypalAmount(r.Amount)},
	}
	raw, e := p.postJSON(ctx, "paypal.refund", p.apiHost+"/v2/payments/captures/"+paypalPathSafe(r.ProviderTradeNo)+"/refund", body,
		map[string]string{"Authorization": "Bearer " + tok, "Accept": "application/json"})
	if e != nil {
		return RefundResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "paypal.refund", Cause: e}
	}
	refundID := jsonText(out, "id")
	status := jsonText(out, "status")
	if refundID == "" || (status != "COMPLETED" && status != "PENDING") {
		return RefundResult{}, rejected("PAYPAL_REFUND_" + paypalStatusCode(status))
	}
	state := "SUCCESS"
	if status == "PENDING" {
		state = "PROCESSING"
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: refundID, State: state, Amount: r.Amount}, nil
}

func paypalStatusCode(s string) string {
	if s == "" {
		return "REJECTED"
	}
	return s
}

// Query confirms an order, mirroring the original return()'s capture-on-return
// (there is no notify we can trust). QueryRequest.ProviderTradeNo carries the
// PayPal order id we returned from Create; PayPal has no public "find order by
// invoice_id" endpoint without extra state, so the order id is required.
//
//	GET /v2/checkout/orders/{id}:
//	  CREATED / PAYER_ACTION_REQUIRED -> NOTPAY (payer has not approved yet)
//	  VOIDED                          -> CLOSED
//	  COMPLETED                       -> already captured; verify the capture
//	  APPROVED                        -> POST .../capture (idempotent via the
//	                                     PayPal-Request-Id = our trade number)
//	then SUCCESS only when a capture is COMPLETED with the expected amount and
//	currency. A capture business refusal (e.g. INSTRUMENT_DECLINED) is
//	definitive; transport/5xx/unreadable stay UnknownOutcome.
func (p *paypalPlugin) Query(ctx context.Context, r QueryRequest) (QueryResult, error) {
	orderID := strings.TrimSpace(r.ProviderTradeNo)
	if r.TradeNo == "" || orderID == "" {
		return QueryResult{}, ErrInvalid
	}
	tok, e := p.paypalToken(ctx)
	if e != nil {
		return QueryResult{}, e
	}
	headers := map[string]string{"Authorization": "Bearer " + tok, "Accept": "application/json"}
	raw, e := p.getURLWithHeaders(ctx, "paypal.order", p.apiHost+"/v2/checkout/orders/"+paypalPathSafe(orderID), headers)
	if e != nil {
		return QueryResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return QueryResult{}, &UnknownOutcome{Operation: "paypal.order", Cause: e}
	}
	switch jsonText(out, "status") {
	case "CREATED", "PAYER_ACTION_REQUIRED", "SAVED":
		return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: orderID, State: "NOTPAY", Currency: "CNY"}, nil
	case "VOIDED":
		return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: orderID, State: "CLOSED", Currency: "CNY"}, nil
	case "COMPLETED":
		return p.paypalCaptureResult(r.TradeNo, orderID, out)
	case "APPROVED":
		body := []byte("{}")
		raw, e := p.pluginCall(ctx, "paypal.capture", http.MethodPost,
			p.apiHost+"/v2/checkout/orders/"+paypalPathSafe(orderID)+"/capture", body,
			"application/json; charset=utf-8",
			map[string]string{"Authorization": "Bearer " + tok, "Accept": "application/json", "PayPal-Request-Id": r.TradeNo})
		if e != nil {
			return QueryResult{}, e
		}
		cap, e := callbackJSON(raw)
		if e != nil {
			return QueryResult{}, &UnknownOutcome{Operation: "paypal.capture", Cause: e}
		}
		return p.paypalCaptureResult(r.TradeNo, orderID, cap)
	default:
		return QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: orderID, State: "NOTPAY", Currency: "CNY"}, nil
	}
}

// paypalCaptureResult reads purchase_units[0].payments.captures[0] of an order
// (the shape returned by both GET orders and the capture call) and maps it to a
// QueryResult, verifying the capture is COMPLETED for the expected amount and
// settlement currency. A capture whose own status names a decline is a
// definitive refusal.
func (p *paypalPlugin) paypalCaptureResult(tradeNo, orderID string, out map[string]any) (QueryResult, error) {
	capture := paypalFirstCapture(out)
	if capture == nil {
		// No capture recorded yet under a COMPLETED/APPROVED order: unknown,
		// query again rather than declaring either outcome.
		return QueryResult{}, &UnknownOutcome{Operation: "paypal.capture", Cause: errPaypalNoCapture}
	}
	status := jsonText(capture, "status")
	if status != "COMPLETED" {
		if status == "PENDING" {
			return QueryResult{TradeNo: tradeNo, ProviderTradeNo: orderID, State: "NOTPAY", Currency: "CNY"}, nil
		}
		// DECLINED / FAILED / etc. — a definitive refusal carrying the code.
		return QueryResult{}, rejected("PAYPAL_CAPTURE_" + paypalStatusCode(status))
	}
	amt, _ := capture["amount"].(map[string]any)
	if amt == nil {
		return QueryResult{}, &UnknownOutcome{Operation: "paypal.capture", Cause: errPaypalNoCapture}
	}
	if jsonText(amt, "currency_code") != p.currencyCode {
		return QueryResult{}, rejected("PAYPAL_CAPTURE_CURRENCY")
	}
	captureID := jsonText(capture, "id")
	return QueryResult{TradeNo: tradeNo, ProviderTradeNo: captureID, State: "SUCCESS", Currency: "CNY"}, nil
}

var errPaypalNoCapture = &paypalStaticError{"paypal: no capture in order"}

type paypalStaticError struct{ s string }

func (e *paypalStaticError) Error() string { return e.s }

// paypalFirstCapture digs out purchase_units[0].payments.captures[0].
func paypalFirstCapture(out map[string]any) map[string]any {
	units, ok := out["purchase_units"].([]any)
	if !ok || len(units) == 0 {
		return nil
	}
	u0, ok := units[0].(map[string]any)
	if !ok {
		return nil
	}
	payments, ok := u0["payments"].(map[string]any)
	if !ok {
		return nil
	}
	caps, ok := payments["captures"].([]any)
	if !ok || len(caps) == 0 {
		return nil
	}
	c0, _ := caps[0].(map[string]any)
	return c0
}

// getURLWithHeaders is a GET carrying auth headers (the kit's getURL sends
// none); kept local so plugin_kit stays untouched.
func (p *paypalPlugin) getURLWithHeaders(ctx context.Context, operation, target string, headers map[string]string) ([]byte, error) {
	return p.pluginCall(ctx, operation, http.MethodGet, target, nil, "", headers)
}

// paypalPathSafe rejects a provider id that could escape the URL path; PayPal
// capture ids are alphanumeric with dashes.
func paypalPathSafe(id string) string {
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '-' || c == '_') {
			return "invalid"
		}
	}
	if len(id) == 0 || len(id) > 64 {
		return "invalid"
	}
	return id
}

var _ Provider = (*paypalPlugin)(nil)
var _ CreateValidator = (*paypalPlugin)(nil)
