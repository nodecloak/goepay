package adminnotice

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	notice "goepay/internal/merchantnotice"
)

type WechatMerchantConfig struct {
	AppID, Secret string
	Template      notice.WechatTemplateConfig
	Input         notice.Input
}
type WechatMerchant struct {
	appID, secret, scope string
	payload              notice.WechatTemplate
	messageHash          [32]byte
	client               *http.Client
}

func (*WechatMerchant) String() string                    { return "merchantnotice.Wechat(configured)" }
func (*WechatMerchant) GoString() string                  { return "merchantnotice.Wechat(configured)" }
func (*WechatMerchant) NoticeTransport() notice.Transport { return notice.Wechat }
func (w *WechatMerchant) NoticeAccountID() string {
	if w == nil {
		return ""
	}
	return w.appID
}
func (w *WechatMerchant) NoticeScope() string {
	if w == nil {
		return ""
	}
	return w.scope
}
func (w *WechatMerchant) ValidateNoticeRecipient(to string) error {
	if w == nil || !qqIdentity.MatchString(to) {
		return ErrMerchantDelivery
	}
	return nil
}
func wechatMessageHash(m notice.Message) [32]byte {
	raw, _ := json.Marshal([]string{m.Title, m.HTML, m.Text})
	return sha256.Sum256(raw)
}

// Construction is pure. The exact input, mappings and account are frozen here;
// token acquisition starts only in Send after the domain's durable claim.
func NewWechatMerchant(c WechatMerchantConfig, client *http.Client) (*WechatMerchant, error) {
	if !qqIdentity.MatchString(c.AppID) || !validCredential(c.Secret, 1024) {
		return nil, ErrMerchantDelivery
	}
	payload, err := notice.BuildWechatTemplate(c.Input, c.Template)
	if err != nil {
		return nil, ErrMerchantDelivery
	}
	message, err := notice.RenderWechat(c.Input)
	if err != nil {
		return nil, ErrMerchantDelivery
	}
	configured, err := strictMerchantHTTPClient(client)
	if err != nil {
		return nil, ErrMerchantDelivery
	}
	cfgRaw, _ := json.Marshal(c.Template)
	return &WechatMerchant{appID: c.AppID, secret: c.Secret, scope: routeScope("wechat-template80/v1", c.AppID, c.Secret, string(cfgRaw)), payload: payload, messageHash: wechatMessageHash(message), client: configured}, nil
}
func (w *WechatMerchant) SendMerchantNotice(ctx context.Context, to string, m notice.Message) error {
	if w == nil || ctx == nil || ctx.Err() != nil || !m.Valid() || wechatMessageHash(m) != w.messageHash || w.ValidateNoticeRecipient(to) != nil {
		return ErrMerchantDelivery
	}
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	defer w.client.CloseIdleConnections()
	started := time.Now()
	fields, err := postJSON(work, w.client, "https://api.weixin.qq.com/cgi-bin/stable_token", map[string]any{"grant_type": "client_credential", "appid": w.appID, "secret": w.secret, "force_refresh": false})
	if err != nil {
		return ErrMerchantDelivery
	}
	if code, present := fields["errcode"]; present && string(code) != "0" {
		return ErrMerchantDelivery
	}
	var token string
	var seconds int64
	if json.Unmarshal(fields["access_token"], &token) != nil || !validCredential(token, 1024) || json.Unmarshal(fields["expires_in"], &seconds) != nil || seconds <= 0 || seconds > 7200 || !time.Now().Before(started.Add(time.Duration(seconds)*time.Second)) {
		return ErrMerchantDelivery
	}
	payload := struct {
		To string `json:"touser"`
		notice.WechatTemplate
	}{To: to, WechatTemplate: w.payload}
	result, err := postJSON(work, w.client, "https://api.weixin.qq.com/cgi-bin/message/template/send?access_token="+url.QueryEscape(token), payload)
	if err != nil || string(result["errcode"]) != "0" {
		return ErrMerchantDelivery
	}
	var messageID int64
	if json.Unmarshal(result["msgid"], &messageID) != nil || messageID <= 0 {
		return ErrMerchantDelivery
	}
	return nil
}
