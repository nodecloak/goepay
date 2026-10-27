package mailaccount195

import (
	"context"
	"encoding/hex"
	"goepay/internal/mailer"
	"goepay/internal/siteinfo"
)

// BoundSMTP keeps the authenticated account revision in the original notice
// claim's scope, even if the console account changes before network I/O.
type BoundSMTP struct {
	*mailer.SMTP
	scope string
}

func (s *BoundSMTP) NoticeScope() string {
	if s == nil {
		return ""
	}
	return s.scope
}
func (s *BoundSMTP) ChannelNoticeScope() string { return s.NoticeScope() }
func (s *Store) ResolveSMTP(ctx context.Context) (*BoundSMTP, bool, error) {
	if s == nil || s.pool == nil {
		return nil, false, ErrSource
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, false, ErrSource
	}
	defer tx.Rollback(ctx)
	account, e := s.load(ctx, tx, false)
	if e != nil {
		return nil, true, ErrSource
	}
	if !account.managed {
		return nil, false, tx.Commit(ctx)
	}
	if account.facts.Status != 1 {
		return nil, true, ErrSource
	}
	config, e := SMTPConfig(account.config)
	if e != nil {
		return nil, true, ErrSource
	}
	site, e := siteinfo.Load(ctx, tx)
	if e != nil {
		return nil, true, ErrSource
	}
	config.FromName = site.Name
	config.ReplyTo = config.From
	sender, e := mailer.NewSMTP(config)
	if e != nil {
		return nil, true, ErrSource
	}
	scope, e := s.digest("notice-scope", []any{account.facts.Revision, account.proof, sender.ChannelNoticeScope()})
	if e != nil {
		return nil, true, ErrSource
	}
	bound := &BoundSMTP{SMTP: sender, scope: hex.EncodeToString(scope)}
	if e = tx.Commit(ctx); e != nil {
		return nil, true, ErrSource
	}
	return bound, true, nil
}
