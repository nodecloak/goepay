package payment

import (
	"context"
	"crypto/hmac"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

var referralBeijing70 = time.FixedZone("Beijing-UTC+08", 8*60*60)

type referralDailyActivation struct {
	OrderID, ChannelID, FeesAccountID, PendingAccountID, FundingJournalID int64
	ActivatedAt                                                           time.Time
	BusinessDay, EligibleDay, ProviderTradeNo                             string
	Proof                                                                 []byte
}

func (s *Store) dailyActivationProof(d referralDailyTerms, a referralDailyActivation) ([]byte, error) {
	return s.dailyDigest("original-paid-funding", []any{d.Proof, a.OrderID, a.ChannelID, a.ProviderTradeNo, a.FeesAccountID, a.PendingAccountID, a.FundingJournalID, a.ActivatedAt.UTC().Format(time.RFC3339Nano), a.BusinessDay, a.EligibleDay})
}
func (s *Store) activateReferralDaily(ctx context.Context, tx pgx.Tx, o Order, t referralOrderTerms) error {
	d := t.daily
	if d == nil || !t.Enabled {
		return ErrConflict
	}
	a := referralDailyActivation{OrderID: o.ID, PendingAccountID: d.PendingAccountID}
	if e := tx.QueryRow(ctx, `SELECT channel_id,provider_trade_no FROM verified_payment_receipts WHERE order_id=$1`, o.ID).Scan(&a.ChannelID, &a.ProviderTradeNo); e != nil {
		return e
	}
	if e := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE owner_id=0 AND kind='fees' AND scope_id=0`).Scan(&a.FeesAccountID); e != nil {
		return e
	}
	if e := tx.QueryRow(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&a.ActivatedAt); e != nil {
		return e
	}
	a.BusinessDay = a.ActivatedAt.In(referralBeijing70).Format("2006-01-02")
	a.EligibleDay = a.ActivatedAt.In(referralBeijing70).AddDate(0, 0, 1).Format("2006-01-02")
	key := fmt.Sprintf("referral-daily-funding:%d", o.ID)
	if e := postByAccountIDs(ctx, tx, key, "original payment fee funds deferred referral liability", a.FeesAccountID, a.PendingAccountID, d.Reward); e != nil {
		return e
	}
	if e := tx.QueryRow(ctx, `SELECT id FROM journals WHERE event_key=$1`, key).Scan(&a.FundingJournalID); e != nil {
		return e
	}
	var e error
	a.Proof, e = s.dailyActivationProof(*d, a)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO referral_daily_liabilities(order_id,activated_at,business_day,eligible_day,channel_id,provider_trade_no,fees_account_id,pending_account_id,funding_journal_id,source_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, a.OrderID, a.ActivatedAt, a.BusinessDay, a.EligibleDay, a.ChannelID, a.ProviderTradeNo, a.FeesAccountID, a.PendingAccountID, a.FundingJournalID, a.Proof)
	if e != nil {
		return e
	}
	return s.insertDailyEvent(ctx, tx, o.ID, "funded", 0, "payment-engine", a.Proof)
}
func (s *Store) readReferralDailyActivation(ctx context.Context, q referralReader, o Order, d referralDailyTerms) (referralDailyActivation, error) {
	var a referralDailyActivation
	e := q.QueryRow(ctx, `SELECT order_id,activated_at,business_day::text,eligible_day::text,channel_id,provider_trade_no,fees_account_id,pending_account_id,funding_journal_id,source_hash FROM referral_daily_liabilities WHERE order_id=$1`, o.ID).Scan(&a.OrderID, &a.ActivatedAt, &a.BusinessDay, &a.EligibleDay, &a.ChannelID, &a.ProviderTradeNo, &a.FeesAccountID, &a.PendingAccountID, &a.FundingJournalID, &a.Proof)
	if e != nil {
		return a, ErrConflict
	}
	proof, e := s.dailyActivationProof(d, a)
	if e != nil || !hmac.Equal(proof, a.Proof) || a.OrderID != o.ID || a.PendingAccountID != d.PendingAccountID {
		return a, ErrConflict
	}
	var valid bool
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM referral_daily_liability_sources WHERE order_id=$1 AND valid IS TRUE)`, o.ID).Scan(&valid)
	if e != nil || !valid {
		return a, ErrConflict
	}
	if e = s.verifyDailyEvent(ctx, q, o.ID, "funded", 0, "payment-engine", a.Proof); e != nil {
		return a, e
	}
	return a, nil
}
func (s *Store) verifyReferralDailyPaid(ctx context.Context, q referralReader, o Order, t referralOrderTerms) error {
	_, _, _, e := s.readReferralDailyState(ctx, q, o, t)
	return e
}
