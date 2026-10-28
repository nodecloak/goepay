package payment

// Port of epay_release/plugins/alipaycode (支付宝免签约码支付). This is a
// SIGNATURE-FREE, DAEMON-CONFIRMED product: the payer transfers money to the
// merchant's Alipay UID with the trade number in the remark, and the original
// plugin's server.php daemon polls the account bill (AlipayBillService.
// accountlogQuery) to match trans_memo==trade_no and mark the order paid.
//
// Consequently there is NO upstream create API for the plain-transfer mode and
// NO signed asynchronous callback at all. What CAN be produced at create time
// is the 转账确认单 (appswitch==1) deep link to render.alipay.com, which is an
// unsigned URL built entirely from config + order data, exactly as the PHP
// pay() method does.
//
// SCOPE of this port:
//   - native (qrcode): the 转账确认单 render.alipay.com transfer URL, carrying
//     the payee UID and the trade number as the transfer remark. Served only
//     when appswitch==1.
// NOT PORTED (documented in the report):
//   - appswitch==0 普通转账: the original shows an internal pay page and relies
//     on the server.php polling daemon; there is no code-constructable,
//     self-contained checkout material, so this mode returns ErrUnsupported.
//   - payment confirmation: it is the external accountlog-polling daemon, a
//     business process, not a signed notify. VerifyNotification is therefore
//     unsupported (ErrUnsupported via the kit default is NOT applicable to a
//     verifier; we explicitly reject with ErrUnsupported).
//   - query / close / refund: the original plugin implements none.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

const alipaycodeRenderBase = "https://render.alipay.com/p/yuyan/180020010001206672/rent-index.html"

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "alipaycode",
		Title:  "支付宝免签约码支付",
		Author: "支付宝",
		Link:   "https://open.alipay.com/",
		Types:  []string{"alipay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用APPID", Required: true},
			{Key: "appkey", Label: "支付宝公钥", Note: "供对账守护进程使用", Textarea: true},
			{Key: "appsecret", Label: "应用私钥", Secret: true, Textarea: true, Required: true},
			{Key: "appmchid", Label: "支付宝UID", Note: "2088开头的16位纯数字，收款方UID", Required: true},
			{Key: "apptoken", Label: "商户授权token", Note: "只有第三方应用需要填写"},
			{Key: "appswitch", Label: "支付类型", Options: []PluginOption{{Value: "0", Label: "普通转账"}, {Value: "1", Label: "转账确认单"}}},
		},
		Note:  "免签约码支付，靠对账守护进程(server.php)轮询确认到账。本移植仅生成「转账确认单」(appswitch=1) 的 render.alipay.com 收款链接；普通转账模式、对账确认守护进程、退款/查询均不在范围内，且无签名回调。",
		Build: newAlipaycodePlugin,
	})
}

type alipaycodePlugin struct {
	pluginBase
	payeeUID string
	confirm  bool // appswitch == 1 (转账确认单)
}

func newAlipaycodePlugin(c PluginChannel) (Provider, error) {
	uid := strings.TrimSpace(c.Config["appmchid"])
	// 2088 + 12 digits = 16-digit numeric UID.
	if !alipaycodeValidUID(uid) {
		return nil, ErrInvalid
	}
	appID := strings.TrimSpace(c.Config["appid"])
	if appID == "" || len(appID) > 32 {
		return nil, ErrInvalid
	}
	// The private key is validated for parity with the bill-query daemon even
	// though the create path does not sign anything.
	if _, e := pluginPrivateKey(c.Config["appsecret"]); e != nil {
		return nil, e
	}
	base, e := newPluginBase(c, pluginAccount("alipaycode", appID, uid))
	if e != nil {
		return nil, e
	}
	return &alipaycodePlugin{pluginBase: base, payeeUID: uid, confirm: c.Config["appswitch"] == "1"}, nil
}

func alipaycodeValidUID(uid string) bool {
	if len(uid) != 16 || !strings.HasPrefix(uid, "2088") {
		return false
	}
	for _, r := range uid {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (p *alipaycodePlugin) methods() []string {
	if p.confirm {
		return []string{"native"}
	}
	// Plain-transfer mode has no code-constructable material in this port.
	return nil
}

func (p *alipaycodePlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods()}
}

func (p *alipaycodePlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

// Create builds the 转账确认单 render.alipay.com link (appswitch==1). The trade
// number is carried as the transfer remark, which the daemon matches to
// confirm payment. No signature, no network call.
func (p *alipaycodePlugin) Create(_ context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	if !p.confirm || r.Method != "native" {
		return CreateResult{}, ErrUnsupported
	}
	params := map[string]any{
		"productCode": "TRANSFER_TO_ALIPAY_ACCOUNT",
		"bizScene":    "YUEBAO",
		"transAmount": yuan(r.Amount),
		"remark":      r.TradeNo,
		"businessParams": map[string]string{
			"returnUrl": "alipays://platformapi/startapp?appId=2021001167654035&nbupdate=syncforce",
		},
		"payeeInfo": map[string]string{
			"identity":     p.payeeUID,
			"identityType": "ALIPAY_USER_ID",
		},
	}
	formData, e := json.Marshal(params)
	if e != nil {
		return CreateResult{}, ErrInvalid
	}
	target := alipaycodeRenderBase + "?formData=" + url.QueryEscape(string(formData))
	return CreateResult{MaterialType: "qrcode", Material: target}, nil
}

// VerifyNotification: the original has no signed callback (confirmation is the
// accountlog-polling daemon), so verification is not supported.
func (p *alipaycodePlugin) VerifyNotification(context.Context, http.Header, []byte, ExpectedPayment) (PaymentEvent, error) {
	return PaymentEvent{}, ErrUnsupported
}

var _ Provider = (*alipaycodePlugin)(nil)
var _ NotificationVerifier = (*alipaycodePlugin)(nil)
var _ CreateValidator = (*alipaycodePlugin)(nil)
