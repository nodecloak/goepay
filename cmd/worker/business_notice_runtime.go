package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/adapters/adminnotice"
	"goepay/internal/businessnotice"
	"goepay/internal/merchantnotice"
)

var errBusinessNoticeConfiguration = errors.New("administrator business notice configuration unavailable")

func (r *businessNoticeRuntime) ResolveBusinessNoticeTx231(ctx context.Context, tx pgx.Tx, kind merchantnotice.Transport, a businessnotice.Account) (businessnotice.Route, error) {
	copy := *r
	copy.mailAccounts = bindMailTx231(r.mailAccounts, tx)
	return copy.ResolveBusinessNotice(ctx, kind, a)
}

type businessNoticeRuntime struct {
	mailAccounts mailAccountResolver195
	store        *businessnotice.Store
	getenv       func(string) string
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
	case merchantnotice.SMS:
		if a.SMS231 == nil {
			return out, errBusinessNoticeConfiguration
		}
		out.Recipient = a.SMS231.Snapshot.Phone
		out.Sender, err = adminnotice.NewSMSMerchant231(a.SMS231, a.Input, nil)
	case merchantnotice.SMTP:
		if _, present := a.Settings["mail_recv"]; present {
			out.Recipient = a.Settings["mail_recv"]
			if out.Recipient == "" {
				out.Recipient = a.Settings["mail_name"]
			}
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
		out.Sender, err = (&merchantNoticeRuntime{getenv: r.getenv, mailAccounts: r.mailAccounts}).ResolveMerchantNotice(ctx, kind, merchantnotice.Account{})
	case merchantnotice.Robot:
		out.Recipient = r.getenv("GOEPAY_ADMIN_MSGROBOT_URL")
		if a.Managed231 {
			out.Recipient = a.Credential231.AppID
		}
		out.Sender, err = adminnotice.NewWebhook(out.Recipient, nil)
		if err == nil && a.Managed231 && a.Credential231.Phone != "" {
			out.Sender, err = adminnotice.NewWebhookMention(out.Recipient, a.Credential231.Phone, nil)
		}
	case merchantnotice.Telegram:
		out.Recipient = get("msgtelegram_chatid", "GOEPAY_ADMIN_MSGTELEGRAM_CHATID")
		token := r.getenv("GOEPAY_ADMIN_LOGIN_TELEGRAM_TOKEN")
		if a.Managed231 {
			out.Recipient = a.Credential231.AppID
			token = a.Credential231.Secret
		}
		out.Sender, err = adminnotice.NewTelegram(token, nil)
	case merchantnotice.QQBot:
		out.Recipient = get("qqbot_openid", "GOEPAY_ADMIN_QQBOT_OPENID")
		app, secret := r.getenv("GOEPAY_ADMIN_QQBOT_APPID"), r.getenv("GOEPAY_ADMIN_QQBOT_APPSECRET")
		if a.Managed231 {
			app, secret, out.Recipient = a.Credential231.AppID, a.Credential231.Secret, a.Credential231.Recipient
		}
		out.Sender, err = adminnotice.NewQQBot(app, secret, nil)
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
