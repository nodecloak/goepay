package mailaccount

import (
	"context"
	"crypto/x509"
	"github.com/jackc/pgx/v5"
	"goepay/internal/enrollment"
	"goepay/internal/mailer"
	"goepay/internal/siteinfo"
)

// Verification selects a sealed console account at the original admission.
// Fallback is eligible only while this installation has never taken ownership.
type Verification struct {
	Store    *Store
	Fallback enrollment.Sender
	// Explicit private-PKI trust, never populated from the console or account.
	RootCAs *x509.CertPool
}

func (v *Verification) PrepareVerification(ctx context.Context, tx pgx.Tx) (enrollment.Sender, error) {
	if v == nil || v.Store == nil || tx == nil {
		return nil, ErrSource
	}
	account, e := v.Store.load(ctx, tx, false)
	if e != nil {
		return nil, ErrSource
	}
	if !account.managed {
		if v.Fallback == nil {
			return nil, ErrSource
		}
		return v.Fallback, nil
	}
	if account.facts.Status != 1 {
		return nil, ErrSource
	}
	config, e := SMTPConfig(account.config)
	if e != nil {
		return nil, ErrSource
	}
	config.RootCAs = v.RootCAs
	site, e := siteinfo.Load(ctx, tx)
	if e != nil {
		return nil, ErrSource
	}
	config.FromName = site.Name
	config.ReplyTo = config.From
	// NewSMTP copies credentials into this one immutable delivery instance;
	// neither current account state nor the environment is consulted after admission.
	sender, e := mailer.NewSMTP(config)
	if e != nil {
		return nil, ErrSource
	}
	return sender, nil
}

// The mutable resolver must never be used as a post-commit sender.
func (v *Verification) SendVerification(context.Context, string, string, string) error {
	return ErrSource
}
