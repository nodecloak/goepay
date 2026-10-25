package main

import (
	"errors"
	"goepay/internal/enrollment"
	"goepay/internal/mailer"
	"strconv"
)

var errVerificationSMTPConfiguration = errors.New("invalid explicitly configured verification email delivery")

// Disabled means an actual nil interface, preserving enrollment's503/default-off
// behavior. Reading explicit settings performs no connection or credential I/O.
func configuredVerificationSender(getenv func(string) string) (enrollment.Sender, error) {
	enabled := getenv("GOEPAY_ENABLE_EMAIL_VERIFICATION")
	if enabled == "" || enabled == "false" {
		return nil, nil
	}
	if enabled != "true" {
		return nil, errVerificationSMTPConfiguration
	}
	raw := getenv("GOEPAY_SMTP_PORT")
	port, e := strconv.Atoi(raw)
	if e != nil || strconv.Itoa(port) != raw {
		return nil, errVerificationSMTPConfiguration
	}
	sender, e := mailer.NewSMTP(mailer.SMTPConfig{Host: getenv("GOEPAY_SMTP_HOST"), Port: port, Security: getenv("GOEPAY_SMTP_SECURITY"), Username: getenv("GOEPAY_SMTP_USERNAME"), Password: getenv("GOEPAY_SMTP_PASSWORD"), From: getenv("GOEPAY_SMTP_FROM")})
	if e != nil {
		return nil, errVerificationSMTPConfiguration
	}
	return sender, nil
}
