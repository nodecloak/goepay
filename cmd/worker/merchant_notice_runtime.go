package main

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/adapters/adminnotice"
	"goepay/internal/mailaccount195"
	"goepay/internal/mailer"
	notice "goepay/internal/merchantnotice"
	"goepay/internal/voiceconfig230"
)

var errMerchantNoticeConfiguration = errors.New("merchant notice delivery configuration unavailable")

func (r *merchantNoticeRuntime) ResolveMerchantNoticeTx231(ctx context.Context, tx pgx.Tx, kind notice.Transport, a notice.Account) (notice.Sender, error) {
	copy := *r
	copy.mailAccounts = bindMailTx231(r.mailAccounts, tx)
	return copy.ResolveMerchantNotice(ctx, kind, a)
}

type merchantNoticeRuntime struct {
	mailAccounts mailAccountResolver195
	voiceConfig  interface {
		Resolve(context.Context) (voiceconfig230.View, voiceconfig230.Credential, bool, error)
	}
	store  *notice.Store
	getenv func(string) string
}

func newMerchantNoticeRuntime(pool *pgxpool.Pool, getenv func(string) string) (*merchantNoticeRuntime, error) {
	if getenv == nil {
		return nil, errMerchantNoticeConfiguration
	}
	switch getenv("GOEPAY_ENABLE_MERCHANT_NOTICES") {
	case "", "false":
		return nil, nil
	case "true":
	default:
		return nil, errMerchantNoticeConfiguration
	}
	key, err := hex.DecodeString(getenv("GOEPAY_MASTER_KEY"))
	if err != nil {
		return nil, errMerchantNoticeConfiguration
	}
	store, err := notice.NewStore(pool, key)
	if err != nil {
		return nil, errMerchantNoticeConfiguration
	}
	voice, e := voiceconfig230.FromMaster(pool, key)
	if e != nil {
		return nil, e
	}
	return &merchantNoticeRuntime{store: store, getenv: getenv, mailAccounts: mailaccount195.FromMaster(pool, key), voiceConfig: voice}, nil
}
func (r *merchantNoticeRuntime) ResolveMerchantNotice(ctx context.Context, kind notice.Transport, a notice.Account) (notice.Sender, error) {
	if r == nil || r.getenv == nil || ctx == nil || ctx.Err() != nil {
		return nil, errMerchantNoticeConfiguration
	}
	switch kind {
	case notice.SMS:
		sender, err := adminnotice.NewSMSMerchant231(a.SMS231, a.Input, nil)
		if err != nil {
			return nil, errMerchantNoticeConfiguration
		}
		return sender, nil
	case notice.Voice:
		agent, secret := r.getenv("GOEPAY_MERCHANT_VOICE_AGENT_ID"), r.getenv("GOEPAY_MERCHANT_VOICE_SECRET")
		revision := int64(0)
		if a.VoiceResolved && a.VoiceManaged {
			agent, secret = a.VoiceCredential.AgentID, a.VoiceCredential.Secret
			revision = a.VoiceRevision
		} else if !a.VoiceResolved && r.voiceConfig != nil {
			v, c, managed, e := r.voiceConfig.Resolve(ctx)
			if e != nil || managed && !v.Enabled {
				return nil, errMerchantNoticeConfiguration
			}
			if managed {
				agent, secret = c.AgentID, c.Secret
				revision = v.Revision
			}
		}
		sender, err := adminnotice.NewVoiceMerchant(adminnotice.VoiceMerchantConfig{Endpoint: r.getenv("GOEPAY_MERCHANT_VOICE_HTTPS_ENDPOINT"), AgentID: agent, Secret: secret, Input: a.Input, Revision: revision}, nil)
		if err != nil {
			return nil, errMerchantNoticeConfiguration
		}
		return sender, nil
	case notice.Wechat:
		appID := r.getenv("GOEPAY_MERCHANT_WECHAT_APPID")
		if appID == "" || a.WXAppID != appID {
			return nil, errMerchantNoticeConfiguration
		}
		cfg := a.Template
		cfg.ConsoleURL = r.getenv("GOEPAY_CONSOLE_ORIGIN")
		sender, err := adminnotice.NewWechatMerchant(adminnotice.WechatMerchantConfig{AppID: appID, Secret: r.getenv("GOEPAY_MERCHANT_WECHAT_APPSECRET"), Template: cfg, Input: a.Input}, nil)
		if err != nil {
			return nil, errMerchantNoticeConfiguration
		}
		return sender, nil
	case notice.SMTP:
		if r.mailAccounts != nil {
			sender, managed, e := r.mailAccounts.ResolveSMTP(ctx)
			if e != nil {
				return nil, errMerchantNoticeConfiguration
			}
			if managed {
				if sender == nil {
					return nil, errMerchantNoticeConfiguration
				}
				return sender, nil
			}
		}
		raw := r.getenv("GOEPAY_SMTP_PORT")
		port, err := strconv.Atoi(raw)
		if err != nil || strconv.Itoa(port) != raw {
			return nil, errMerchantNoticeConfiguration
		}
		sender, err := mailer.NewSMTP(mailer.SMTPConfig{Host: r.getenv("GOEPAY_SMTP_HOST"), Port: port, Security: r.getenv("GOEPAY_SMTP_SECURITY"), Username: r.getenv("GOEPAY_SMTP_USERNAME"), Password: r.getenv("GOEPAY_SMTP_PASSWORD"), From: r.getenv("GOEPAY_SMTP_FROM")})
		if err != nil {
			return nil, errMerchantNoticeConfiguration
		}
		return sender, nil
	case notice.Robot:
		sender, err := adminnotice.NewWebhook(a.WebhookURL, nil)
		if err != nil {
			return nil, errMerchantNoticeConfiguration
		}
		return sender, nil
	case notice.Telegram:
		token := r.getenv("GOEPAY_MERCHANT_TELEGRAM_TOKEN")
		if a.TelegramResolved {
			token = a.TelegramToken
		}
		sender, err := adminnotice.NewTelegram(token, nil)
		if err != nil {
			return nil, errMerchantNoticeConfiguration
		}
		return sender, nil
	case notice.QQBot:
		sender, err := adminnotice.NewQQBot(a.QQBotAppID, a.QQBotAppSecret, nil)
		if err != nil {
			return nil, errMerchantNoticeConfiguration
		}
		return sender, nil
	default:
		return nil, errMerchantNoticeConfiguration
	}
}
func (r *merchantNoticeRuntime) Handle(ctx context.Context, job int64, lease string) error {
	if r == nil || r.store == nil || r.getenv == nil || r.getenv("GOEPAY_ENABLE_MERCHANT_NOTICES") != "true" {
		return errMerchantNoticeConfiguration
	}
	return r.store.HandleJob(ctx, job, lease, r)
}
