package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"goepay/internal/adapters/adminnotice"
	"goepay/internal/merchantnotice"
	"goepay/internal/noticeconfig231"
	"goepay/internal/payment"
	"goepay/internal/templatechange"
	"time"
)

type managedNotice231 struct {
	merchantnotice.Sender
	config      *noticeconfig231.Store
	kind, stamp string
}

func (m *managedNotice231) NoticeScope() string {
	raw, _ := json.Marshal([]string{m.Sender.NoticeScope(), m.stamp})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (m *managedNotice231) ChannelNoticeScope() string { return m.NoticeScope() }
func (m *managedNotice231) ChannelNoticeMode() string  { return string(m.NoticeTransport()) }
func (m *managedNotice231) ValidateChannelNoticeRecipient(to string) error {
	return m.ValidateNoticeRecipient(to)
}
func (m *managedNotice231) SendChannelStopped(ctx context.Context, to string, c, e int64, code string) error {
	return m.Sender.(payment.ChannelNoticeSender).SendChannelStopped(ctx, to, c, e, code)
}
func (m *managedNotice231) SendPageTemplateNotice(ctx context.Context, to string, n templatechange.Notice) error {
	return m.Sender.(interface {
		SendPageTemplateNotice(context.Context, string, templatechange.Notice) error
	}).SendPageTemplateNotice(ctx, to, n)
}
func (m *managedNotice231) ValidateNoticeConfig231(ctx context.Context, tx pgx.Tx, born time.Time) error {
	v, _, managed, stamp, e := m.config.EffectiveTx(ctx, tx, m.kind)
	if e != nil {
		return e
	}
	if !managed || !v.Enabled || stamp != m.stamp {
		return errChannelNoticeConfiguration
	}
	changed, e := m.config.ChangedAtTx(ctx, tx, m.kind, v.Revision)
	if e != nil {
		return e
	}
	if born.Before(changed) {
		return errChannelNoticeConfiguration
	}
	return nil
}
func managedFactory231(config *noticeconfig231.Store, k string, v noticeconfig231.View, c noticeconfig231.Credential, stamp string) (payment.ChannelNoticeSender, string, error) {
	if !v.Enabled {
		return nil, "", errChannelNoticeConfiguration
	}
	var sender merchantnotice.Sender
	var e error
	to := c.AppID
	switch k {
	case "robot":
		sender, e = adminnotice.NewWebhookMention(c.AppID, c.Phone, nil)
	case "telegram":
		sender, e = adminnotice.NewTelegram(c.Secret, nil)
	case "qqbot":
		to = c.Recipient
		sender, e = adminnotice.NewQQBot(c.AppID, c.Secret, nil)
	default:
		return nil, "", errChannelNoticeConfiguration
	}
	if e != nil || sender == nil || sender.ValidateNoticeRecipient(to) != nil {
		return nil, "", errChannelNoticeConfiguration
	}
	return &managedNotice231{Sender: sender, config: config, kind: k, stamp: stamp}, to, nil
}
func riskKind231(mode string) string {
	switch mode {
	case "1":
		return "robot"
	case "2":
		return "telegram"
	case "3":
		return "qqbot"
	}
	return ""
}
