package main

import (
	"context"
	"goepay/internal/payment"
	"sync"
)

type channelNoticeSettingsReader interface {
	AdminChannelNoticeSettings(context.Context) (map[string]string, error)
}
type channelNoticeRuntime struct {
	getenv   func(string) string
	settings channelNoticeSettingsReader
	mu       sync.Mutex
	sender   payment.ChannelNoticeSender
}

func newChannelNoticeRuntime(ctx context.Context, r channelNoticeSettingsReader, getenv func(string) string) (*channelNoticeRuntime, error) {
	if getenv == nil {
		return nil, errChannelNoticeConfiguration
	}
	if getenv("GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES") != "true" {
		_, _, e := configuredChannelNotices(getenv)
		return nil, e
	}
	out := &channelNoticeRuntime{getenv: getenv, settings: r}
	if _, _, e := out.Resolve(ctx); e != nil {
		return nil, e
	}
	return out, nil
}
func (r *channelNoticeRuntime) Resolve(ctx context.Context) (payment.ChannelNoticeSender, string, error) {
	if r == nil || ctx == nil || ctx.Err() != nil || r.getenv == nil || r.settings == nil {
		return nil, "", errChannelNoticeConfiguration
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getenv("GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES") != "true" {
		return configuredChannelNotices(r.getenv)
	}
	values, e := r.settings.AdminChannelNoticeSettings(ctx)
	if e != nil {
		return nil, "", errChannelNoticeConfiguration
	}
	keys := map[string]string{"GOEPAY_ADMIN_MSGCONFIG_RISK": "msgconfig_risk", "GOEPAY_ADMIN_MSGTELEGRAM_CHATID": "msgtelegram_chatid", "GOEPAY_ADMIN_QQBOT_OPENID": "qqbot_openid", "GOEPAY_ADMIN_MAIL_RECV": "mail_recv", "GOEPAY_ADMIN_MAIL_NAME": "mail_name"}
	for key, value := range values {
		if payment.ValidateAdminNoticeSetting(key, value) != nil {
			return nil, "", errChannelNoticeConfiguration
		}
	}
	getenv := func(key string) string {
		if original, ok := keys[key]; ok {
			if value, present := values[original]; present {
				return value
			}
		}
		return r.getenv(key)
	}
	sender, to, e := configuredChannelNotices(getenv)
	if e != nil || sender == nil {
		return nil, "", errChannelNoticeConfiguration
	}
	profile, ok := sender.(payment.ChannelNoticeRoute)
	if !ok {
		return nil, "", errChannelNoticeConfiguration
	}
	if old, ok := r.sender.(payment.ChannelNoticeRoute); ok && old.ChannelNoticeMode() == profile.ChannelNoticeMode() && old.ChannelNoticeScope() == profile.ChannelNoticeScope() && old.ValidateChannelNoticeRecipient(to) == nil {
		return r.sender, to, nil
	}
	r.sender = sender
	return sender, to, nil
}
