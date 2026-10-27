package main

import (
	"context"
	"errors"
	"goepay/internal/merchantnotice"
	"goepay/internal/payment"
)

var errCoreRiskNoticeConfiguration = errors.New("core risk notice delivery configuration unavailable")

type coreRiskNoticeRuntime struct {
	store  *payment.Store
	getenv func(string) string
}

func newCoreRiskNoticeRuntime(store *payment.Store, getenv func(string) string) (*coreRiskNoticeRuntime, error) {
	if getenv == nil {
		return nil, errCoreRiskNoticeConfiguration
	}
	switch getenv("GOEPAY_ENABLE_CORE_RISK_NOTICES") {
	case "", "false":
		return nil, nil
	case "true":
		if store == nil {
			return nil, errCoreRiskNoticeConfiguration
		}
		return &coreRiskNoticeRuntime{store, getenv}, nil
	default:
		return nil, errCoreRiskNoticeConfiguration
	}
}
func (r *coreRiskNoticeRuntime) ResolveCoreRiskNotice(ctx context.Context, kind string) (merchantnotice.Sender, string, error) {
	if r == nil || r.getenv == nil || ctx == nil || ctx.Err() != nil || r.getenv("GOEPAY_ENABLE_CORE_RISK_NOTICES") != "true" {
		return nil, "", errCoreRiskNoticeConfiguration
	}
	if kind == "merchant" {
		sender, e := (&merchantNoticeRuntime{getenv: r.getenv}).ResolveMerchantNotice(ctx, merchantnotice.SMTP, merchantnotice.Account{})
		return sender, "", e
	}
	if kind != "channel" && kind != "subchannel" {
		return nil, "", errCoreRiskNoticeConfiguration
	}
	values, e := r.store.AdminChannelNoticeSettings(ctx)
	if e != nil {
		return nil, "", errCoreRiskNoticeConfiguration
	}
	get := func(k string) string {
		if k == "GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES" {
			return "true"
		}
		keys := map[string]string{"GOEPAY_ADMIN_MSGCONFIG_RISK": "msgconfig_risk", "GOEPAY_ADMIN_MSGTELEGRAM_CHATID": "msgtelegram_chatid", "GOEPAY_ADMIN_QQBOT_OPENID": "qqbot_openid", "GOEPAY_ADMIN_MAIL_RECV": "mail_recv", "GOEPAY_ADMIN_MAIL_NAME": "mail_name"}
		if original := keys[k]; original != "" {
			if v, ok := values[original]; ok {
				return v
			}
		}
		return r.getenv(k)
	}
	sender, to, e := configuredChannelNotices(get)
	if e != nil || sender == nil {
		return nil, "", errCoreRiskNoticeConfiguration
	}
	out, ok := sender.(merchantnotice.Sender)
	if !ok {
		return nil, "", errCoreRiskNoticeConfiguration
	}
	return out, to, nil
}
func (r *coreRiskNoticeRuntime) Handle(ctx context.Context, id int64, token string) error {
	if r == nil || r.store == nil || r.getenv == nil || r.getenv("GOEPAY_ENABLE_CORE_RISK_NOTICES") != "true" {
		return errCoreRiskNoticeConfiguration
	}
	return r.store.DeliverCoreRiskNotice(ctx, id, token, r)
}
