package main

import (
	"context"
	"errors"
	"goepay/internal/adapters/adminnotice"
	"goepay/internal/businessnotice"
	"goepay/internal/merchantnotice"
)

var errBusinessNoticeConfiguration = errors.New("administrator business notice configuration unavailable")

type businessNoticeRuntime struct {
	store  *businessnotice.Store
	getenv func(string) string
}

func newBusinessNoticeRuntime(store *businessnotice.Store, getenv func(string) string) (*businessNoticeRuntime, error) {
	if getenv == nil {
		return nil, errBusinessNoticeConfiguration
	}
	switch getenv("GOEPAY_ENABLE_ADMIN_BUSINESS_NOTICES") {
	case "", "false":
		return nil, nil
	case "true":
	default:
		return nil, errBusinessNoticeConfiguration
	}
	if store == nil {
		return nil, errBusinessNoticeConfiguration
	}
	return &businessNoticeRuntime{store: store, getenv: getenv}, nil
}
func (r *businessNoticeRuntime) ResolveBusinessNotice(ctx context.Context, kind merchantnotice.Transport, a businessnotice.Account) (businessnotice.Route, error) {
	var out businessnotice.Route
	if r == nil || r.getenv == nil || ctx == nil || ctx.Err() != nil {
		return out, errBusinessNoticeConfiguration
	}
	get := func(original, env string) string {
		if v, present := a.Settings[original]; present {
			return v
		}
		return r.getenv(env)
	}
	var err error
	switch kind {
	case merchantnotice.SMTP:
		if _, present := a.Settings["mail_recv"]; present {
			out.Recipient = a.Settings["mail_recv"]
		} else if _, present := a.Settings["mail_name"]; present {
			out.Recipient = a.Settings["mail_name"]
		} else {
			out.Recipient = r.getenv("GOEPAY_ADMIN_MAIL_RECV")
			if out.Recipient == "" {
				out.Recipient = r.getenv("GOEPAY_ADMIN_MAIL_NAME")
			}
			if out.Recipient == "" {
				out.Recipient = r.getenv("GOEPAY_ADMIN_NOTICE_RECIPIENT")
			}
		}
		out.Sender, err = (&merchantNoticeRuntime{getenv: r.getenv}).ResolveMerchantNotice(ctx, kind, merchantnotice.Account{})
	case merchantnotice.Robot:
		out.Recipient = r.getenv("GOEPAY_ADMIN_MSGROBOT_URL")
		out.Sender, err = adminnotice.NewWebhook(out.Recipient, nil)
	case merchantnotice.Telegram:
		out.Recipient = get("msgtelegram_chatid", "GOEPAY_ADMIN_MSGTELEGRAM_CHATID")
		out.Sender, err = adminnotice.NewTelegram(r.getenv("GOEPAY_ADMIN_LOGIN_TELEGRAM_TOKEN"), nil)
	case merchantnotice.QQBot:
		out.Recipient = get("qqbot_openid", "GOEPAY_ADMIN_QQBOT_OPENID")
		out.Sender, err = adminnotice.NewQQBot(r.getenv("GOEPAY_ADMIN_QQBOT_APPID"), r.getenv("GOEPAY_ADMIN_QQBOT_APPSECRET"), nil)
	default:
		return businessnotice.Route{}, errBusinessNoticeConfiguration
	}
	if err != nil || out.Sender == nil || out.Sender.ValidateNoticeRecipient(out.Recipient) != nil {
		return businessnotice.Route{}, errBusinessNoticeConfiguration
	}
	return out, nil
}
func (r *businessNoticeRuntime) Handle(ctx context.Context, job int64, lease string) error {
	if r == nil || r.store == nil || r.getenv == nil || r.getenv("GOEPAY_ENABLE_ADMIN_BUSINESS_NOTICES") != "true" {
		return errBusinessNoticeConfiguration
	}
	return r.store.HandleJob(ctx, job, lease, r)
}

func (r *businessNoticeRuntime) BusinessNoticeAccountID(ctx context.Context, kind merchantnotice.Transport) (string, error) {
	if r == nil || r.getenv == nil || ctx == nil || ctx.Err() != nil || kind != merchantnotice.QQBot {
		return "", errBusinessNoticeConfiguration
	}
	app := r.getenv("GOEPAY_ADMIN_QQBOT_APPID")
	if app == "" {
		return "", errBusinessNoticeConfiguration
	}
	return app, nil
}
