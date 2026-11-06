package main

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/enrollment"
	"goepay/internal/mailaccount"
)

func configuredManagedVerification195(pool *pgxpool.Pool, master []byte, getenv func(string) string) (enrollment.Sender, error) {
	if getenv == nil {
		return nil, errVerificationSMTPConfiguration
	}
	gate := getenv("GOEPAY_ENABLE_EMAIL_VERIFICATION")
	if gate == "" || gate == "false" {
		return nil, nil
	}
	if gate != "true" || pool == nil || len(master) != 32 {
		return nil, errVerificationSMTPConfiguration
	}
	configured := false
	for _, key := range []string{"GOEPAY_SMTP_HOST", "GOEPAY_SMTP_PORT", "GOEPAY_SMTP_SECURITY", "GOEPAY_SMTP_USERNAME", "GOEPAY_SMTP_PASSWORD", "GOEPAY_SMTP_FROM"} {
		if getenv(key) != "" {
			configured = true
		}
	}
	var fallback enrollment.Sender
	if configured {
		var e error
		fallback, e = configuredVerificationSender(getenv)
		if e != nil {
			return nil, e
		}
	}
	return &mailaccount.Verification{Store: mailaccount.FromMaster(pool, master), Fallback: fallback}, nil
}
