package payment

// Port of epay_release/plugins/heepay (汇付宝): the 汇付宝 cashier
// (https://pay.heepay.com). Payment is a browser redirect to
// /Payment/Index.aspx with the order as a signed query string (a "jump"), so
// Create returns the jump URL rather than calling the API. The pay signature
// is a FIXED-ORDER MD5 over version/agent_id/agent_bill_id/agent_bill_time/
// pay_type/pay_amt/notify_url/return_url/user_ip(/bank_card_type)(/ref_agent_id)
// + "&key="+appkey. Callbacks and the sync return are GET with the same kind
// of fixed-order MD5 (result/agent_id/jnet_bill_no/agent_bill_id/pay_type/
// pay_amt/remark + key). result=="1" is paid; the original echoes "ok".
// Refund posts to PaymentRefund.aspx (lower-cased, fixed-order MD5 over the
// refund key appsecret) and parses a GBK XML reply (ret_code 0000 = success).
//
// Out of scope (this wave): transfers and their 3DES detail_data
// (transfer/transfer_query/transfer_proof/balance_query/transfernotify), the
// 进件/提现 (apply/cash) callbacks, and refundnotify.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:          "heepay",
		Title:         "汇付宝",
		Author:        "汇付宝",
		Link:          "https://www.heepay.com/",
		Types:         []string{"alipay", "wxpay", "bank"},
		TransferTypes: []string{"alipay", "wxpay", "bank"}, // declared by original; not implemented this wave
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户编号", Required: true},
			{Key: "appmchid", Label: "二级商户号", Note: "可留空，集团模式传参"},
			{Key: "bank_id", Label: "上游商户BankId", Note: "可留空，多个用,隔开"},
			{Key: "appkey", Label: "支付密钥", Secret: true, Required: true},
			{Key: "appsecret", Label: "退款密钥", Secret: true},
			{Key: "transfer_key", Label: "付款密钥", Secret: true},
			{Key: "transfer_des_key", Label: "付款3DES加密密钥", Secret: true},
			{Key: "mch_private_key", Label: "常规业务RSA私钥", Textarea: true, Secret: true},
		},
		// select_bank: 1=网银支付(20), 2=银联支付(64), 3=云闪付H5(34).
		AppTypes: []PluginOption{{Value: "1", Label: "网银支付"}, {Value: "2", Label: "银联支付"}, {Value: "3", Label: "云闪付H5"}},
		Build:    newHeepayPlugin,
	})
}

const (
	heepayPayURL    = "https://pay.heepay.com/Payment/Index.aspx"
	heepayRefundURL = "https://pay.heepay.com/API/Payment/PaymentRefund.aspx"
)

type heepayPlugin struct {
	pluginBase
	agentID, refAgentID, bankID string
	appkey, refundkey           string
	refundTarget                string
	appTypes                    []string
}

func newHeepayPlugin(c PluginChannel) (Provider, error) {
	agentID := strings.TrimSpace(c.Config["appid"])
	appkey := c.Config["appkey"]
	if agentID == "" || len(agentID) > 32 || strings.ContainsAny(agentID, "&=?# ") || appkey == "" || len(appkey) > 128 {
		return nil, ErrInvalid
	}
	refAgent := strings.TrimSpace(c.Config["appmchid"])
	bankID := strings.TrimSpace(c.Config["bank_id"])
	if len(refAgent) > 32 || len(bankID) > 128 {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount("https://pay.heepay.com", agentID, refAgent))
	if e != nil {
		return nil, e
	}
	return &heepayPlugin{pluginBase: base, agentID: agentID, refAgentID: refAgent, bankID: bankID, appkey: appkey, refundkey: c.Config["appsecret"], refundTarget: heepayRefundURL, appTypes: append([]string(nil), c.AppTypes...)}, nil
}

func (p *heepayPlugin) heepayHasApp(v string) bool {
	for _, a := range p.appTypes {
		if a == v {
			return true
		}
	}
	return false
}

func (p *heepayPlugin) methods() []string {
	if p.payType == "alipay" {
		return []string{"page", "wap"}
	}
	return []string{"h5"}
}

func (p *heepayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// heepayPayType resolves the upstream pay_type for the channel + method.
// pay_type 20 (网银) uses a different fixed-order sign (getBankPayParam).
func (p *heepayPlugin) heepayPayType(method string) (payType string, bank bool) {
	switch p.payType {
	case "alipay":
		return "22", false
	case "wxpay":
		return "30", false
	default: // bank
		switch {
		case p.heepayHasApp("3"):
			return "34", false
		case p.heepayHasApp("1"):
			return "20", true
		default:
			if method == "wap" || method == "h5" {
				return "34", false
			}
			return "64", false
		}
	}
}

// heepaySign joins the ordered "k=v" pairs with "&" and MD5s the result;
// lower controls the strtolower(...) the refund/return variants apply.
func heepaySign(pairs [][2]string, key string, lower bool) string {
	var sb strings.Builder
	for _, kv := range pairs {
		sb.WriteString(kv[0])
		sb.WriteByte('=')
		sb.WriteString(kv[1])
		sb.WriteByte('&')
	}
	sb.WriteString("key=")
	sb.WriteString(key)
	s := sb.String()
	if lower {
		s = strings.ToLower(s)
	}
	return md5Hex(s)
}

func (p *heepayPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *heepayPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	payType, bank := p.heepayPayType(r.Method)
	ts := time.Now().UTC().Format("20060102150405")
	amt := yuan(r.Amount)
	userIP := strings.ReplaceAll(r.ClientIP, ".", "_")
	q := url.Values{}
	q.Set("agent_id", p.agentID)
	q.Set("agent_bill_id", r.TradeNo)
	q.Set("agent_bill_time", ts)
	q.Set("pay_amt", amt)
	q.Set("notify_url", r.NotifyURL)
	q.Set("return_url", r.ReturnURL)
	q.Set("user_ip", userIP)
	q.Set("goods_name", r.Subject)
	q.Set("sign_type", "MD5")

	var sign string
	if bank {
		q.Set("version", "3")
		q.Set("pay_type", "20")
		q.Set("pay_code", "0")
		q.Set("bank_card_type", "-1")
		pairs := [][2]string{
			{"version", "3"}, {"agent_id", p.agentID}, {"agent_bill_id", r.TradeNo},
			{"agent_bill_time", ts}, {"pay_type", "20"}, {"pay_amt", amt},
			{"notify_url", r.NotifyURL}, {"return_url", r.ReturnURL}, {"user_ip", userIP},
			{"bank_card_type", "-1"},
		}
		if p.refAgentID != "" {
			q.Set("ref_agent_id", p.refAgentID)
			pairs = append(pairs, [2]string{"ref_agent_id", p.refAgentID})
		}
		sign = heepaySign(pairs, p.appkey, false)
	} else {
		q.Set("version", "1")
		q.Set("pay_type", payType)
		pairs := [][2]string{
			{"version", "1"}, {"agent_id", p.agentID}, {"agent_bill_id", r.TradeNo},
			{"agent_bill_time", ts}, {"pay_type", payType}, {"pay_amt", amt},
			{"notify_url", r.NotifyURL}, {"return_url", r.ReturnURL}, {"user_ip", userIP},
		}
		if p.refAgentID != "" {
			q.Set("ref_agent_id", p.refAgentID)
			pairs = append(pairs, [2]string{"ref_agent_id", p.refAgentID})
		}
		if p.bankID != "" {
			q.Set("bank_id", p.heepayBankID())
		}
		sign = heepaySign(pairs, p.appkey, false)
	}
	q.Set("sign", sign)
	return CreateResult{MaterialType: "jump", Material: heepayPayURL + "?" + q.Encode()}, nil
}

// heepayBankID picks one upstream bank id when several are configured, as the
// original does (array_rand); a single value is returned as-is.
func (p *heepayPlugin) heepayBankID() string {
	if !strings.Contains(p.bankID, ",") {
		return p.bankID
	}
	parts := strings.Split(p.bankID, ",")
	// Deterministic choice avoids a global RNG; the upstream accepts any.
	return strings.TrimSpace(parts[0])
}

// Callbacks are GET: the original reads $_GET and verifies a fixed-order sign.
func (p *heepayPlugin) NotificationMethods() []string {
	return []string{http.MethodGet, http.MethodPost}
}

func (p *heepayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	g, e := callbackParams(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	pairs := [][2]string{
		{"result", g["result"]}, {"agent_id", g["agent_id"]}, {"jnet_bill_no", g["jnet_bill_no"]},
		{"agent_bill_id", g["agent_bill_id"]}, {"pay_type", g["pay_type"]}, {"pay_amt", g["pay_amt"]},
		{"remark", g["remark"]},
	}
	if !signEqual(heepaySign(pairs, p.appkey, false), g["sign"], false) {
		return PaymentEvent{}, ErrAuthentication
	}
	if g["agent_id"] != "" && g["agent_id"] != p.agentID {
		return PaymentEvent{}, ErrAuthentication
	}
	if g["result"] != "1" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(g["pay_amt"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || g["agent_bill_id"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, g["jnet_bill_no"], g["pay_user"], amount), nil
}

// The original notify() echoes plain "ok" on success, "error" on sign failure.
func (p *heepayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "error"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "ok"
}

// Refund posts to PaymentRefund.aspx with a lower-cased fixed-order MD5 over
// the refund key; full-amount and partial use different field sets. The reply
// is a GBK XML document with ret_code 0000 on success.
func (p *heepayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	if p.refundkey == "" {
		return RefundResult{}, ErrUnsupported
	}
	form := url.Values{"version": {"1"}, "agent_id": {p.agentID}, "sign_type": {"MD5"}}
	if r.Amount == r.TotalAmount {
		form.Set("agent_bill_id", r.TradeNo)
		// full refund sign order: agent_bill_id & agent_id & key & version
		form.Set("sign", md5Hex(strings.ToLower("agent_bill_id="+r.TradeNo+"&agent_id="+p.agentID+"&key="+p.refundkey+"&version=1")))
	} else {
		details := r.TradeNo + "," + yuan(r.Amount) + "," + r.RefundNo
		form.Set("refund_details", details)
		// partial sign order: agent_id & key & refund_details & version
		form.Set("sign", md5Hex(strings.ToLower("agent_id="+p.agentID+"&key="+p.refundkey+"&refund_details="+details+"&version=1")))
	}
	return p.heepayDoRefund(ctx, form, r)
}

func (p *heepayPlugin) heepayDoRefund(ctx context.Context, form url.Values, r RefundRequest) (RefundResult, error) {
	raw, e := p.postForm(ctx, "heepay.refund", p.refundTarget, form)
	if e != nil {
		return RefundResult{}, e
	}
	utf, e := simplifiedchinese.GBK.NewDecoder().Bytes(raw)
	if e != nil {
		utf = raw
	}
	flat, e := xmlFlat(utf)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "heepay.refund", Cause: e}
	}
	if flat["ret_code"] != "0000" {
		code := flat["ret_code"]
		if code == "" {
			return RefundResult{}, &UnknownOutcome{Operation: "heepay.refund", Cause: errHeepayBadReply}
		}
		return RefundResult{}, rejected("HEEPAY_" + code)
	}
	return RefundResult{RefundNo: r.RefundNo, State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*heepayPlugin)(nil)
var _ NotificationVerifier = (*heepayPlugin)(nil)
var _ CreateValidator = (*heepayPlugin)(nil)
var _ NotificationAcker = (*heepayPlugin)(nil)

var errHeepayBadReply = newHeepayError("heepay: unexpected upstream reply")

func newHeepayError(s string) error { return &heepayError{s} }

type heepayError struct{ s string }

func (e *heepayError) Error() string { return e.s }
