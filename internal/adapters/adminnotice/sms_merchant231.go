package adminnotice

import (
	"context"
	smsadapter "goepay/internal/adapters/sms"
	"goepay/internal/businesssms"
	notice "goepay/internal/merchantnotice"
	"net/http"
	"regexp"
	"strconv"
)

type SMSMerchant231 struct {
	sender                      smsadapter.Sender
	recipient, parameter, scope string
	messageHash                 [32]byte
}

func (*SMSMerchant231) String() string                    { return "merchantnotice.SMS(configured)" }
func (*SMSMerchant231) GoString() string                  { return "merchantnotice.SMS(configured)" }
func (*SMSMerchant231) NoticeTransport() notice.Transport { return notice.SMS }
func (s *SMSMerchant231) NoticeScope() string {
	if s == nil {
		return ""
	}
	return s.scope
}

var noticePhone231 = regexp.MustCompile(`^1[3-9][0-9]{9}$`)

func (s *SMSMerchant231) ValidateNoticeRecipient(to string) error {
	if s == nil || to != s.recipient || !noticePhone231.MatchString(to) {
		return ErrMerchantDelivery
	}
	return nil
}

// The constructor is pure. The resolved credential belongs to the source and
// its permanent claim; Send never reloads a gateway, template or recipient.
func NewSMSMerchant231(resolved *businesssms.Resolved, in notice.Input, client *http.Client) (*SMSMerchant231, error) {
	if resolved == nil || !resolved.Snapshot.Valid() {
		return nil, ErrMerchantDelivery
	}
	message, e := notice.Render(in)
	if e != nil {
		return nil, ErrMerchantDelivery
	}
	parameter := ""
	switch in.Scene {
	case "balance":
		parameter = in.Params["msgmoney"]
	case "complain", "complain_all":
		parameter = in.Params["trade_no"]
	case "group":
		parameter = in.Params["group"]
	default:
		return nil, ErrMerchantDelivery
	}
	if parameter == "" {
		return nil, ErrMerchantDelivery
	}
	configured, e := strictMerchantHTTPClient(client)
	if e != nil {
		return nil, e
	}
	c, snap := resolved.Credentials, resolved.Snapshot
	var sender smsadapter.Sender
	switch snap.Provider {
	case "aliyun":
		sender = smsadapter.Aliyun{AccessKeyID: c.AccessKeyID, AccessKeySecret: c.AccessKeySecret, SignName: c.Sign, TemplateCode: snap.Template, Client: configured}
	case "qcloud":
		sender = smsadapter.Qcloud{AppID: c.AppID, AppKey: c.AppKey, Sign: c.Sign, TplID: snap.Template, Client: configured}
	case "smsbao":
		sender = smsadapter.SmsBao{User: c.User, Pass: c.Pass, Sign: c.Sign, Template: snap.Template, BaseURL: "https://api.smsbao.com", Client: configured}
	default:
		return nil, ErrMerchantDelivery
	}
	return &SMSMerchant231{sender: sender, recipient: snap.Phone, parameter: parameter, scope: routeScope("sms231/v1", snap.Provider, snap.Fingerprint, strconv.FormatInt(snap.Revision, 10)), messageHash: wechatMessageHash(message)}, nil
}
func (s *SMSMerchant231) SendMerchantNotice(ctx context.Context, to string, m notice.Message) error {
	if ctx == nil || ctx.Err() != nil || s.ValidateNoticeRecipient(to) != nil || !m.Valid() || wechatMessageHash(m) != s.messageHash {
		return ErrMerchantDelivery
	}
	if e := s.sender.Send(ctx, to, s.parameter); e != nil {
		return ErrMerchantDelivery
	}
	return nil
}
