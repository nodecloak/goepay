package adminnotice

import (
	"context"
	"goepay/internal/ledger"
	notice "goepay/internal/merchantnotice"
	"net/http"
	"net/url"
	"time"
)

type VoiceMerchantConfig struct {
	Endpoint, AgentID, Secret string
	Input                     notice.Input
}
type VoiceMerchant struct {
	endpoint, agent, secret, text, scope string
	messageHash                          [32]byte
	client                               *http.Client
}

func (*VoiceMerchant) String() string                    { return "merchantnotice.Voice(configured)" }
func (*VoiceMerchant) GoString() string                  { return "merchantnotice.Voice(configured)" }
func (*VoiceMerchant) NoticeTransport() notice.Transport { return notice.Voice }
func (v *VoiceMerchant) NoticeScope() string {
	if v == nil {
		return ""
	}
	return v.scope
}
func (v *VoiceMerchant) ValidateNoticeRecipient(to string) error {
	if v == nil || !qqIdentity.MatchString(to) {
		return ErrMerchantDelivery
	}
	return nil
}
func NewVoiceMerchant(c VoiceMerchantConfig, client *http.Client) (*VoiceMerchant, error) {
	endpoint, err := url.Parse(c.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() != "iot.solomo-info.com" || endpoint.Port() != "" && endpoint.Port() != "443" || endpoint.Path != "/admin/common/msgpush" || endpoint.RawPath != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.User != nil || endpoint.Opaque != "" || !validCredential(c.AgentID, 128) || !validCredential(c.Secret, 1024) || c.Input.Scene != "order" || c.Input.Params["name"] == "付款码收款" {
		return nil, ErrMerchantDelivery
	}
	message, err := notice.Render(c.Input)
	if err != nil {
		return nil, ErrMerchantDelivery
	}
	configured, err := strictMerchantHTTPClient(client)
	if err != nil {
		return nil, ErrMerchantDelivery
	}
	// Render validates canonical fen-compatible source values; format again so
	// a harmless input such as 12.3 has the original two-decimal spoken amount.
	amount, err := ledger.Parse(c.Input.Params["money"])
	if err != nil {
		return nil, ErrMerchantDelivery
	}
	return &VoiceMerchant{endpoint: c.Endpoint, agent: c.AgentID, secret: c.Secret, text: c.Input.Params["type"] + "收款" + ledger.Format(amount) + "元", scope: routeScope("voice80/v1", c.Endpoint, c.AgentID, c.Secret), messageHash: wechatMessageHash(message), client: configured}, nil
}
func (v *VoiceMerchant) SendMerchantNotice(ctx context.Context, to string, m notice.Message) error {
	if v == nil || ctx == nil || ctx.Err() != nil || !m.Valid() || wechatMessageHash(m) != v.messageHash || v.ValidateNoticeRecipient(to) != nil {
		return ErrMerchantDelivery
	}
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	defer v.client.CloseIdleConnections()
	out, err := postJSON(work, v.client, v.endpoint, map[string]any{"agent_id": v.agent, "agent_secret": v.secret, "sbx_id": to, "msg": v.text, "debug": 0})
	if err != nil || string(out["result"]) != `"success"` {
		return ErrMerchantDelivery
	}
	return nil
}
