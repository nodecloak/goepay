package main

import (
	"errors"
	"goepay/internal/adapters/adminnotice"
	"goepay/internal/mailer"
	"goepay/internal/payment"
	"net/mail"
	"strconv"
)

var errChannelNoticeConfiguration = errors.New("invalid explicitly configured administrator channel notice delivery")

func configuredChannelNotices(getenv func(string) string) (payment.ChannelNoticeSender, string, error) {
	return configuredChannelNoticesWithSMTP195(getenv, nil)
}
func configuredChannelNoticesWithSMTP195(getenv func(string) string, resolve func() (payment.ChannelNoticeSender, error)) (payment.ChannelNoticeSender, string, error) {
	flag := getenv("GOEPAY_ENABLE_ADMIN_CHANNEL_NOTICES")
	if flag == "" || flag == "false" {
		return nil, "", nil
	}
	if flag != "true" {
		return nil, "", errChannelNoticeConfiguration
	}
	// Exact legacy field names are mapped to protected process configuration.
	// Constructors and route validation are pure: startup never tests delivery.
	risk := getenv("GOEPAY_ADMIN_MSGCONFIG_RISK")
	if risk != "" && risk != "0" && risk != "1" && risk != "2" && risk != "3" {
		return nil, "", errChannelNoticeConfiguration
	}
	var sender payment.ChannelNoticeSender
	var target string
	var e error
	switch risk {
	case "1":
		target = getenv("GOEPAY_ADMIN_MSGROBOT_URL")
		if target != "" {
			sender, e = adminnotice.NewWebhook(target, nil)
		}
	case "2":
		target = getenv("GOEPAY_ADMIN_MSGTELEGRAM_CHATID")
		if target != "" {
			sender, e = adminnotice.NewTelegram(getenv("GOEPAY_ADMIN_LOGIN_TELEGRAM_TOKEN"), nil)
		}
	case "3":
		target = getenv("GOEPAY_ADMIN_QQBOT_OPENID")
		if target != "" {
			sender, e = adminnotice.NewQQBot(getenv("GOEPAY_ADMIN_QQBOT_APPID"), getenv("GOEPAY_ADMIN_QQBOT_APPSECRET"), nil)
		}
	}
	if target != "" {
		if e != nil || sender == nil {
			return nil, "", errChannelNoticeConfiguration
		}
		route, ok := sender.(payment.ChannelNoticeRoute)
		if !ok || route.ValidateChannelNoticeRecipient(target) != nil {
			return nil, "", errChannelNoticeConfiguration
		}
		return sender, target, nil
	}
	to := getenv("GOEPAY_ADMIN_NOTICE_RECIPIENT")
	if original := getenv("GOEPAY_ADMIN_MAIL_RECV"); original != "" {
		to = original
	} else if original = getenv("GOEPAY_ADMIN_MAIL_NAME"); original != "" {
		to = original
	}
	address, e := mail.ParseAddress(to)
	if e != nil || address.Address != to || address.Name != "" || len(to) > 254 {
		return nil, "", errChannelNoticeConfiguration
	}
	for _, r := range to {
		if r < 33 || r > 126 {
			return nil, "", errChannelNoticeConfiguration
		}
	}
	if resolve != nil {
		sender, e = resolve()
		if e != nil {
			return nil, "", errChannelNoticeConfiguration
		}
		if sender != nil {
			return sender, to, nil
		}
	}
	raw := getenv("GOEPAY_SMTP_PORT")
	port, e := strconv.Atoi(raw)
	if e != nil || strconv.Itoa(port) != raw {
		return nil, "", errChannelNoticeConfiguration
	}
	sender, e = mailer.NewSMTP(mailer.SMTPConfig{Host: getenv("GOEPAY_SMTP_HOST"), Port: port, Security: getenv("GOEPAY_SMTP_SECURITY"), Username: getenv("GOEPAY_SMTP_USERNAME"), Password: getenv("GOEPAY_SMTP_PASSWORD"), From: getenv("GOEPAY_SMTP_FROM")})
	if e != nil {
		return nil, "", errChannelNoticeConfiguration
	}
	return sender, to, nil
}
