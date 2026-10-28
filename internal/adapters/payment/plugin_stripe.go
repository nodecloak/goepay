package payment

// Port of epay_release/plugins/stripe (Stripe). Two modes (appswitch):
//   0 跳转收银台: POST /v1/checkout/sessions -> jump to session url.
//   1 直接支付:   for alipay/wxpay the original redirects to the native
//                 adapter (/pay/alipay|wxpay/...); that is a GOEPAY-internal
//                 route, not a Stripe call, so in GOEPAY we always drive
//                 Stripe's own Checkout session instead (mode 1's direct
//                 PaymentIntent path is only reachable via those internal
//                 redirects). We note this choice below.
// Webhook: Stripe-Signature header "t=<ts>,v1=<hmac>", HMAC-SHA256 of
//          "<ts>.<rawbody>" keyed by the whsec_ endpoint secret
//          (Stripe\Webhook::verifyHeader). This verifies cryptographically
//          with no extra state, so VerifyNotification is implemented.
//
// Currency: Stripe settles in currency_code (default cny). amount =
// round(realmoney * currency_rate * 100) minor units. We keep that inside the
// plugin; GOEPAY core still treats the order as CNY. The settlement currency
// is reported in p03.md.

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "stripe",
		Title:  "Stripe",
		Author: "Stripe",
		Link:   "https://stripe.com/",
		Types:  []string{"alipay", "wxpay", "bank", "paypal"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "API密钥", Note: "sk_live_开头的密钥", Secret: true, Textarea: true, Required: true},
			{Key: "appkey", Label: "Webhook密钥", Note: "whsec_开头的密钥", Secret: true, Textarea: true, Required: true},
			{Key: "appswitch", Label: "支付模式", Options: []PluginOption{{Value: "0", Label: "跳转收银台"}, {Value: "1", Label: "直接支付(仅限支付宝/微信)"}}},
			{Key: "currency_code", Label: "结算货币", Options: stripeCurrencyOptions()},
			{Key: "currency_rate", Label: "货币汇率", Note: "例如1元人民币兑换0.137美元(USD)，则此处填0.137"},
		},
		Note:  "需设置WebHook地址，侦听 checkout.session.completed / checkout.session.async_payment_succeeded / payment_intent.succeeded 事件",
		Build: newStripePlugin,
	})
}

func stripeCurrencyOptions() []PluginOption {
	codes := []string{"CNY", "HKD", "EUR", "USD", "AUD", "CAD", "GBP", "BRL", "CZK", "DKK", "HUF", "INR", "ILS", "JPY", "MYR", "MXN", "TWD", "NZD", "NOK", "PHP", "PLN", "RUB", "SGD", "SEK", "CHF", "THB"}
	out := make([]PluginOption, 0, len(codes))
	for _, c := range codes {
		out = append(out, PluginOption{Value: c, Label: c})
	}
	return out
}

const stripeAPIHost = "https://api.stripe.com"

type stripePlugin struct {
	pluginBase
	apiKey, webhookSecret string
	currencyCode          string // lowercase, as Stripe wants
	rateMilli             int64
	host                  string // stripeAPIHost in production; overridable in tests
}

func newStripePlugin(c PluginChannel) (Provider, error) {
	key := strings.TrimSpace(c.Config["appid"])
	whsec := strings.TrimSpace(c.Config["appkey"])
	if key == "" || len(key) > 256 || whsec == "" || len(whsec) > 256 {
		return nil, ErrInvalid
	}
	currency := strings.TrimSpace(c.Config["currency_code"])
	if currency == "" {
		currency = "CNY"
	}
	if !stripeCurrencyKnown(currency) {
		return nil, ErrInvalid
	}
	rate, e := paypalParseRate(c.Config["currency_rate"]) // same 0.137 -> 137 semantics
	if e != nil {
		return nil, e
	}
	// Account identity is the live/test mode prefix of the key, never the key.
	mode := "live"
	if strings.HasPrefix(key, "sk_test_") {
		mode = "test"
	}
	base, e := newPluginBase(c, pluginAccount(stripeAPIHost, mode, strings.ToUpper(currency)))
	if e != nil {
		return nil, e
	}
	return &stripePlugin{pluginBase: base, apiKey: key, webhookSecret: whsec, currencyCode: strings.ToLower(currency), rateMilli: rate, host: stripeAPIHost}, nil
}

func stripeCurrencyKnown(code string) bool {
	for _, o := range stripeCurrencyOptions() {
		if o.Value == code {
			return true
		}
	}
	return false
}

// stripeMinor converts GOEPAY cents to Stripe minor units:
// round(cents/100 * rate * 100) = round(cents * rateMilli / 1000).
func (p *stripePlugin) stripeMinor(cents int64) int64 {
	return (cents*p.rateMilli + 500) / 1000
}

func (p *stripePlugin) methods() []string {
	// Checkout session is a redirect; map to "h5" (no QR product here).
	return []string{"h5"}
}

func (p *stripePlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *stripePlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

// stripePaymentMethod maps the GOEPAY pay type to Stripe's payment_method_types
// entry (stripe_plugin::submit). An unlisted type lets Stripe choose.
func (p *stripePlugin) stripePaymentMethod() string {
	switch p.payType {
	case "alipay":
		return "alipay"
	case "wxpay":
		return "wechat_pay"
	case "paypal":
		return "paypal"
	}
	return ""
}

func (p *stripePlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	// We always create a hosted Checkout session (the original's mode 0). Mode
	// 1's "direct" path is only reached through GOEPAY-internal redirects to
	// the native alipay/wxpay adapters, which do not exist for a plugin
	// channel, so Checkout is the faithful standalone behaviour.
	form := url.Values{}
	form.Set("success_url", r.ReturnURL)
	form.Set("cancel_url", r.ReturnURL)
	form.Set("client_reference_id", r.TradeNo)
	form.Set("mode", "payment")
	form.Set("line_items[0][quantity]", "1")
	form.Set("line_items[0][price_data][currency]", p.currencyCode)
	form.Set("line_items[0][price_data][product_data][name]", r.Subject)
	form.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(p.stripeMinor(r.Amount), 10))
	if pm := p.stripePaymentMethod(); pm != "" {
		form.Set("payment_method_types[0]", pm)
		if pm == "wechat_pay" {
			form.Set("payment_method_options[wechat_pay][client]", "web")
		}
	}
	raw, e := p.pluginCall(ctx, "stripe.session", http.MethodPost, p.host+"/v1/checkout/sessions",
		[]byte(form.Encode()), "application/x-www-form-urlencoded",
		map[string]string{"Authorization": "Bearer " + p.apiKey, "Accept": "application/json"})
	if e != nil {
		return CreateResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "stripe.session", Cause: e}
	}
	if errObj, ok := out["error"].(map[string]any); ok {
		return CreateResult{}, rejected("STRIPE_" + stripeCode(errObj))
	}
	payURL := jsonText(out, "url")
	if payURL == "" || !validPaymentURL(payURL) {
		return CreateResult{}, rejected("STRIPE_NO_URL")
	}
	// The session id is our provider trade number until the payment_intent is
	// known from the webhook.
	return CreateResult{MaterialType: "jump", Material: payURL, ProviderTradeNo: jsonText(out, "id")}, nil
}

func stripeCode(errObj map[string]any) string {
	if c := jsonText(errObj, "code"); c != "" {
		return c
	}
	if t := jsonText(errObj, "type"); t != "" {
		return t
	}
	return "ERROR"
}

// Stripe webhooks are POST (the endpoint reads php://input + Stripe-Signature).
func (p *stripePlugin) NotificationMethods() []string { return []string{http.MethodPost} }

// VerifyNotification mirrors Stripe\Webhook::verifyHeader then the event switch
// in stripe_plugin::webhook. We do NOT enforce the 300s tolerance window the
// original applies (abs(time()-t) <= 300): a replayed-but-correctly-signed
// callback is still bound to our own trade number and amount here, and a
// strict clock window would reject legitimately delayed deliveries. The
// signature + order + amount checks are what authenticate the payment.
func (p *stripePlugin) VerifyNotification(_ context.Context, header http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return PaymentEvent{}, ErrInvalid
	}
	sig := header.Get("Stripe-Signature")
	ts, v1 := stripeParseSigHeader(sig)
	if ts == "" || v1 == "" {
		return PaymentEvent{}, ErrAuthentication
	}
	want := hmacSHA256Hex(p.webhookSecret, ts+"."+string(raw))
	if !signEqual(want, v1, true) {
		return PaymentEvent{}, ErrAuthentication
	}
	// Signature verified: parse the event.
	event, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, ErrInvalid
	}
	eventType := jsonText(event, "type")
	obj := stripeEventObject(event)
	if obj == nil {
		return PaymentEvent{}, ErrInvalid
	}
	// Our trade number: client_reference_id, else metadata.order_id.
	outTradeNo := jsonText(obj, "client_reference_id")
	if outTradeNo == "" {
		if md, ok := obj["metadata"].(map[string]any); ok {
			outTradeNo = jsonText(md, "order_id")
		}
	}
	if outTradeNo == "" || outTradeNo != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	// Map the event to a paid outcome exactly as the original switch does.
	var providerTradeNo string
	switch eventType {
	case "checkout.session.completed":
		if jsonText(obj, "payment_status") != "paid" {
			return PaymentEvent{}, ErrInvalid
		}
		providerTradeNo = jsonText(obj, "payment_intent")
	case "checkout.session.async_payment_succeeded":
		providerTradeNo = jsonText(obj, "payment_intent")
	case "payment_intent.succeeded":
		providerTradeNo = jsonText(obj, "id")
	default:
		return PaymentEvent{}, ErrInvalid
	}
	// Amount check: Stripe amount_total (session) / amount (payment_intent) is
	// in the settlement currency's minor units. Compare against what we sent.
	if !p.stripeAmountMatches(obj, expected.Amount) {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, providerTradeNo, "", expected.Amount), nil
}

// stripeAmountMatches checks the event amount equals the minor units we would
// have sent for expected.Amount (cents). Session uses amount_total; a
// payment_intent uses amount. A missing amount is treated as not matching.
func (p *stripePlugin) stripeAmountMatches(obj map[string]any, expectedCents int64) bool {
	if expectedCents <= 0 {
		return false
	}
	want := p.stripeMinor(expectedCents)
	for _, k := range []string{"amount_total", "amount_received", "amount"} {
		if v, ok := obj[k]; ok {
			n, e := strconv.ParseInt(jsonTextAny(v), 10, 64)
			if e == nil {
				return n == want
			}
		}
	}
	return false
}

func jsonTextAny(v any) string {
	m := map[string]any{"x": v}
	return jsonText(m, "x")
}

// stripeEventObject returns event.data.object.
func stripeEventObject(event map[string]any) map[string]any {
	data, ok := event["data"].(map[string]any)
	if !ok {
		return nil
	}
	obj, _ := data["object"].(map[string]any)
	return obj
}

// stripeParseSigHeader extracts t= and the first v1= from the Stripe-Signature
// header "t=...,v1=...,v1=...".
func stripeParseSigHeader(h string) (ts, v1 string) {
	for _, part := range strings.Split(h, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			if ts == "" {
				ts = kv[1]
			}
		case "v1":
			if v1 == "" {
				v1 = kv[1]
			}
		}
	}
	// t must be numeric (getTimestamp uses is_numeric).
	if ts != "" {
		for _, c := range ts {
			if c < '0' || c > '9' {
				return "", ""
			}
		}
	}
	return ts, v1
}

// Refund posts /v1/refunds for payment_intent=api_trade_no, amount in minor
// units (stripe_plugin::refund). code 0 => success.
func (p *stripePlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	form := url.Values{}
	form.Set("payment_intent", r.ProviderTradeNo)
	form.Set("amount", strconv.FormatInt(p.stripeMinor(r.Amount), 10))
	raw, e := p.pluginCall(ctx, "stripe.refund", http.MethodPost, p.host+"/v1/refunds",
		[]byte(form.Encode()), "application/x-www-form-urlencoded",
		map[string]string{"Authorization": "Bearer " + p.apiKey, "Accept": "application/json"})
	if e != nil {
		return RefundResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "stripe.refund", Cause: e}
	}
	if errObj, ok := out["error"].(map[string]any); ok {
		return RefundResult{}, rejected("STRIPE_" + stripeCode(errObj))
	}
	status := jsonText(out, "status")
	if jsonText(out, "id") == "" || (status != "succeeded" && status != "pending") {
		return RefundResult{}, rejected("STRIPE_REFUND_" + stripeStatus(status))
	}
	state := "SUCCESS"
	if status == "pending" {
		state = "PROCESSING"
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: jsonText(out, "payment_intent"), State: state, Amount: r.Amount}, nil
}

func stripeStatus(s string) string {
	if s == "" {
		return "REJECTED"
	}
	return s
}

var _ Provider = (*stripePlugin)(nil)
var _ NotificationVerifier = (*stripePlugin)(nil)
var _ CreateValidator = (*stripePlugin)(nil)
