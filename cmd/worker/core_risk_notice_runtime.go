package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantnotice"
	"goepay/internal/payment"
)

var errCoreRiskNoticeConfiguration = errors.New("core risk notice delivery configuration unavailable")

type coreRiskNoticeRuntime struct {
	mailAccounts mailAccountResolver195
	store        *payment.Store
	getenv       func(string) string
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
		return &coreRiskNoticeRuntime{store: store, getenv: getenv}, nil
	default:
		return nil, errCoreRiskNoticeConfiguration
	}
}
func (r *coreRiskNoticeRuntime) ResolveCoreRiskNotice(ctx context.Context, kind string) (merchantnotice.Sender, string, error) {
	if r == nil || r.getenv == nil || ctx == nil || ctx.Err() != nil || r.getenv("GOEPAY_ENABLE_CORE_RISK_NOTICES") != "true" {
		return nil, "", errCoreRiskNoticeConfiguration
	}
	if kind == "merchant" {
		sender, e := (&merchantNoticeRuntime{getenv: r.getenv, mailAccounts: r.mailAccounts}).ResolveMerchantNotice(ctx, merchantnotice.SMTP, merchantnotice.Account{})
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
	runtime := &channelNoticeRuntime{getenv: get, settings: r.store, mailAccounts: r.mailAccounts}
	sender, to, e := runtime.Resolve(ctx)
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

// Bot accounts are resolved on the original claim connection; no nested pool
// lease or network work is permitted while it holds the source/job locks.
func (r *coreRiskNoticeRuntime) ResolveCoreRiskNoticeTx231(ctx context.Context, tx pgx.Tx, kind string) (merchantnotice.Sender, string, error) {
	if r == nil || r.store == nil || r.getenv == nil || r.getenv("GOEPAY_ENABLE_CORE_RISK_NOTICES") != "true" {
		return nil, "", errCoreRiskNoticeConfiguration
	}
	if kind == "merchant" {
		if r == nil {
			return nil, "", errCoreRiskNoticeConfiguration
		}
		sender, e := (&merchantNoticeRuntime{getenv: r.getenv, mailAccounts: bindMailTx231(r.mailAccounts, tx)}).ResolveMerchantNotice(ctx, merchantnotice.SMTP, merchantnotice.Account{})
		return sender, "", e
	}
	if r == nil || r.store == nil || r.getenv == nil || r.getenv("GOEPAY_ENABLE_CORE_RISK_NOTICES") != "true" || (kind != "channel" && kind != "subchannel") {
		return nil, "", errCoreRiskNoticeConfiguration
	}
	rows, e := tx.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1::text[]) ORDER BY id FOR SHARE`, []string{"msgconfig_risk", "msgtelegram_chatid", "qqbot_openid", "mail_recv", "mail_name"})
	if e != nil {
		return nil, "", e
	}
	values := map[string]string{}
	for rows.Next() {
		var k, v string
		var raw []byte
		if e = rows.Scan(&k, &raw); e != nil {
			break
		}
		if _, ok := values[k]; ok {
			e = errCoreRiskNoticeConfiguration
			break
		}
		if json.Unmarshal(raw, &v) != nil || payment.ValidateAdminNoticeSetting(k, v) != nil {
			e = errCoreRiskNoticeConfiguration
			break
		}
		values[k] = v
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return nil, "", e
	}
	mode := r.getenv("GOEPAY_ADMIN_MSGCONFIG_RISK")
	if v, ok := values["msgconfig_risk"]; ok {
		mode = v
	}
	if k := riskKind231(mode); k != "" {
		config, e := r.store.NoticeAccounts231()
		if e != nil {
			return nil, "", e
		}
		v, c, managed, stamp, e := config.EffectiveTx(ctx, tx, k)
		if e != nil {
			return nil, "", e
		}
		if managed {
			sender, to, e := managedFactory231(config, k, v, c, stamp)
			if e != nil {
				return nil, "", e
			}
			return sender.(merchantnotice.Sender), to, nil
		}
	}
	get := func(key string) string {
		if key == "GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES" {
			return "true"
		}
		keys := map[string]string{"GOEPAY_ADMIN_MSGCONFIG_RISK": "msgconfig_risk", "GOEPAY_ADMIN_MSGTELEGRAM_CHATID": "msgtelegram_chatid", "GOEPAY_ADMIN_QQBOT_OPENID": "qqbot_openid", "GOEPAY_ADMIN_MAIL_RECV": "mail_recv", "GOEPAY_ADMIN_MAIL_NAME": "mail_name"}
		if k := keys[key]; k != "" {
			if v, ok := values[k]; ok {
				return v
			}
		}
		return r.getenv(key)
	}
	runtime := &channelNoticeRuntime{getenv: get, settings: frozenChannelSettings231(values), mailAccounts: bindMailTx231(r.mailAccounts, tx)}
	sender, to, e := runtime.Resolve(ctx)
	if e != nil || sender == nil {
		return nil, "", errCoreRiskNoticeConfiguration
	}
	out, ok := sender.(merchantnotice.Sender)
	if !ok {
		return nil, "", errCoreRiskNoticeConfiguration
	}
	return out, to, nil
}

type frozenChannelSettings231 map[string]string

func (f frozenChannelSettings231) AdminChannelNoticeSettings(context.Context) (map[string]string, error) {
	return f, nil
}
