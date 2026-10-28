package payment

// Port of epay_release/plugins/zhangyishou (掌易收聚合支付). JSON POST to the
// fixed host https://apipay.zhangyishou.com. The create sign is a POSITIONAL
// MD5: the request field VALUES concatenated in a fixed order, then the key
// (md5(MerchantId.DownstreamOrderNo.OrderTime.PayChannelId.AsynPath.OrderMoney.
// IPPath.key)). AddOrder returns Code "1009" with Info = the pay URL. The
// async callback signs md5(MerchantId.DownstreamOrderNo.key) as Signature and
// OrderState 1 is success; ack is "OK"/"ERROR". Refund posts a positional sign
// over MerchantId.MerchantOrder.RefundAmount.key.
//
// Config: appid=登录账号(MerchantId), appurl=商户编号(MerchantNo),
// appkey=商户密钥(key), appmchid=通道ID(PayChannelId; a "a|b" pair selects a
// scan id and a mini-program id).
//
// Scope: payments + refund. OUT OF SCOPE: transfer / transfernotify /
// balance_query (transfers), and the WeChat 公众号 JSAPI flow (wxjspay), which
// needs a multi-step oauth/openid round trip the framework does not drive here.

import (
	"context"
	"net/http"
	"strings"
	"time"
)

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "zhangyishou",
		Title:  "掌易收聚合支付",
		Author: "掌易收",
		Link:   "http://www.zhangyishou.com/",
		Types:  []string{"alipay", "qqpay", "wxpay", "bank"},
		// transtypes alipay/bank are transfers — out of scope.
		Inputs: []PluginInput{
			{Key: "appid", Label: "登录账号", Required: true},
			{Key: "appkey", Label: "商户密钥", Secret: true, Required: true},
			{Key: "appurl", Label: "商户编号", Required: true},
			{Key: "appmchid", Label: "通道ID", Note: "微信有扫码和小程序2种时填写2个ID，用|隔开", Required: true},
		},
		Note:  "如果微信通道有扫码和小程序2种，直接在通道ID填写2个ID，用|隔开",
		Build: newZhangyishouPlugin,
	})
}

const zhangyishouHost = "https://apipay.zhangyishou.com"

type zhangyishouPlugin struct {
	pluginBase
	merchantID, merchantNo, key string
	channelQR, channelMini      string
}

var zhangyishouNow = func() time.Time { return time.Now() }

func newZhangyishouPlugin(c PluginChannel) (Provider, error) {
	merchantID := strings.TrimSpace(c.Config["appid"])
	merchantNo := strings.TrimSpace(c.Config["appurl"])
	key := c.Config["appkey"]
	chans := strings.TrimSpace(c.Config["appmchid"])
	if merchantID == "" || len(merchantID) > 64 || merchantNo == "" || len(merchantNo) > 64 || key == "" || len(key) > 256 || chans == "" || len(chans) > 128 {
		return nil, ErrInvalid
	}
	qr, mini := chans, ""
	if i := strings.IndexByte(chans, '|'); i >= 0 {
		qr = strings.TrimSpace(chans[:i])
		mini = strings.TrimSpace(chans[i+1:])
	}
	if qr == "" {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount(zhangyishouHost, merchantID, merchantNo))
	if e != nil {
		return nil, e
	}
	return &zhangyishouPlugin{pluginBase: base, merchantID: merchantID, merchantNo: merchantNo, key: key, channelQR: qr, channelMini: mini}, nil
}

func (p *zhangyishouPlugin) methods() []string {
	// wxpay with a mini-program id can also produce an app URL scheme.
	if p.payType == "wxpay" && p.channelMini != "" {
		return []string{"native", "jsapi"}
	}
	return []string{"native"}
}

func (p *zhangyishouPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

// zhangyishouSign concatenates the ordered values then the key and md5s them.
func zhangyishouSign(values []string, key string) string {
	return md5Hex(strings.Join(values, "") + key)
}

func (p *zhangyishouPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *zhangyishouPlugin) Create(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	payChannelID := p.channelQR
	scheme := false
	if r.Method == "jsapi" && p.channelMini != "" {
		payChannelID = p.channelMini
		scheme = true
	}
	orderTime := zhangyishouNow().Format("2006-01-02 15:04:05")
	money := yuan(r.Amount)
	// Field order mirrors the PHP $params array exactly (sign is over values).
	sign := zhangyishouSign([]string{p.merchantID, r.TradeNo, orderTime, payChannelID, r.NotifyURL, money, r.ClientIP}, p.key)
	payload := map[string]string{
		"MerchantId":        p.merchantID,
		"DownstreamOrderNo": r.TradeNo,
		"OrderTime":         orderTime,
		"PayChannelId":      payChannelID,
		"AsynPath":          r.NotifyURL,
		"OrderMoney":        money,
		"IPPath":            r.ClientIP,
		"MD5Sign":           sign,
		"MerchantNo":        p.merchantNo,
		"Mproductdesc":      r.Subject,
	}
	if r.ReturnURL != "" {
		payload["ReturnUrl"] = r.ReturnURL
	}
	raw, e := p.postJSON(ctx, "zhangyishou.create", zhangyishouHost+"/api/Order/AddOrder", payload, nil)
	if e != nil {
		return CreateResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return CreateResult{}, &UnknownOutcome{Operation: "zhangyishou.create", Cause: e}
	}
	if jsonText(out, "Code") != "1009" {
		return CreateResult{}, rejected("ZYS_" + jsonText(out, "Code"))
	}
	payURL := jsonText(out, "Info")
	if payURL == "" || len(payURL) > 4096 || strings.ContainsAny(payURL, "\x00\r\n") {
		return CreateResult{}, rejected("ZYS_NO_PAY_URL")
	}
	if scheme {
		if !strings.Contains(payURL, "://") {
			return CreateResult{}, rejected("ZYS_NO_PAY_URL")
		}
		return CreateResult{MaterialType: "urlscheme", Material: payURL}, nil
	}
	return CreateResult{MaterialType: "qrcode", Material: payURL}, nil
}

// Callbacks post a JSON body; the original reads php://input.
func (p *zhangyishouPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	data, e := callbackJSON(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	merchantID, downstream := jsonText(data, "MerchantId"), jsonText(data, "DownstreamOrderNo")
	sign := zhangyishouSign([]string{merchantID, downstream, p.key}, "")
	if !signEqual(sign, jsonText(data, "Signature"), false) {
		return PaymentEvent{}, ErrAuthentication
	}
	if merchantID != p.merchantID {
		return PaymentEvent{}, ErrAuthentication
	}
	if jsonText(data, "OrderState") != "1" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseYuan(jsonText(data, "OrderMoney"))
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || downstream != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, jsonText(data, "OrderNo"), "", amount), nil
}

// The original echoes "OK"/"ERROR".
func (p *zhangyishouPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusBadRequest, "text/plain; charset=utf-8", "ERROR"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "OK"
}

func (p *zhangyishouPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.TradeNo == "" || r.RefundNo == "" || r.Amount <= 0 || r.TotalAmount < r.Amount {
		return RefundResult{}, ErrInvalid
	}
	money := yuan(r.Amount)
	sign := zhangyishouSign([]string{p.merchantID, r.TradeNo, money}, p.key)
	payload := map[string]string{
		"MerchantId":    p.merchantID,
		"MerchantOrder": r.TradeNo,
		"RefundAmount":  money,
		"MD5Sign":       sign,
	}
	raw, e := p.postJSON(ctx, "zhangyishou.refund", zhangyishouHost+"/api/OrderRefund/Refund", payload, nil)
	if e != nil {
		return RefundResult{}, e
	}
	out, e := callbackJSON(raw)
	if e != nil {
		return RefundResult{}, &UnknownOutcome{Operation: "zhangyishou.refund", Cause: e}
	}
	if jsonText(out, "Code") != "1009" {
		return RefundResult{}, rejected("ZYS_" + jsonText(out, "Code"))
	}
	return RefundResult{RefundNo: r.RefundNo, State: "SUCCESS", Amount: r.Amount}, nil
}

var _ Provider = (*zhangyishouPlugin)(nil)
var _ NotificationVerifier = (*zhangyishouPlugin)(nil)
var _ CreateValidator = (*zhangyishouPlugin)(nil)
