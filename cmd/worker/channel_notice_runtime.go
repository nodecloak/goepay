package main

import (
	"context"
	"goepay/internal/noticeconfig231"
	"goepay/internal/payment"
	"sync"
)

type channelNoticeSettingsReader interface {
	AdminChannelNoticeSettings(context.Context) (map[string]string, error)
}
type channelNoticeRuntime struct {
	mailAccounts mailAccountResolver195
	getenv       func(string) string
	settings     channelNoticeSettingsReader
	mu           sync.Mutex
	sender       payment.ChannelNoticeSender
}

func newChannelNoticeRuntime(ctx context.Context, r channelNoticeSettingsReader, getenv func(string) string) (*channelNoticeRuntime, error) {
	return newManagedChannelNoticeRuntime195(ctx, r, getenv, nil)
}
func newManagedChannelNoticeRuntime195(ctx context.Context, r channelNoticeSettingsReader, getenv func(string) string, mailAccounts mailAccountResolver195) (*channelNoticeRuntime, error) {
	if getenv == nil {
		return nil, errChannelNoticeConfiguration
	}
	if getenv("GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES") != "true" {
		_, _, e := configuredChannelNotices(getenv)
		return nil, e
	}
	out := &channelNoticeRuntime{getenv: getenv, settings: r, mailAccounts: mailAccounts}
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
	if source, ok := r.settings.(interface {
		NoticeAccounts231() (*noticeconfig231.Store, error)
	}); ok {
		if kind := riskKind231(getenv("GOEPAY_ADMIN_MSGCONFIG_RISK")); kind != "" {
			config, e := source.NoticeAccounts231()
			if e != nil {
				return nil, "", e
			}
			v, c, managed, stamp, e := config.Effective(ctx, kind)
			if e != nil {
				return nil, "", e
			}
			if managed {
				return managedFactory231(config, kind, v, c, stamp)
			}
		}
	}
	sender, to, e := configuredChannelNoticesWithSMTP195(getenv, func() (payment.ChannelNoticeSender, error) {
		if r.mailAccounts == nil {
			return nil, nil
		}
		sender, managed, e := r.mailAccounts.ResolveSMTP(ctx)
		if e != nil {
			return nil, errChannelNoticeConfiguration
		}
		if !managed {
			return nil, nil
		}
		if sender == nil {
			return nil, errChannelNoticeConfiguration
		}
		return sender, nil
	})
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
