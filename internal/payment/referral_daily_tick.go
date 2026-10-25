package payment

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type ReferralDailyTickResult struct {
	Examined int      `json:"examined"`
	Awarded  int      `json:"awarded"`
	Skipped  int      `json:"skipped"`
	OrderIDs []string `json:"order_ids"`
}
type referralDailyAward struct {
	OrderID, RewardID, ReleaseJournalID int64
	ExecutedAt                          time.Time
	Proof                               []byte
}
type dailyClock70 func(context.Context, pgx.Tx) (time.Time, error)

func dailyDBClock70(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var at time.Time
	e := tx.QueryRow(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&at)
	return at, e
}

// TickReferralDaily performs only bounded local, funded ledger transfers. The
// public entry point always uses the database clock; no caller clock is exposed.
func (s *Store) TickReferralDaily(ctx context.Context, through string, limit int) (ReferralDailyTickResult, error) {
	return s.tickReferralDailyAt(ctx, through, limit, dailyDBClock70)
}
func (s *Store) tickReferralDailyAt(ctx context.Context, through string, limit int, clock dailyClock70) (ReferralDailyTickResult, error) {
	result := ReferralDailyTickResult{OrderIDs: []string{}}
	day, e := time.Parse("2006-01-02", through)
	if e != nil || day.Format("2006-01-02") != through || limit < 1 || limit > 100 || clock == nil {
		return result, ErrInvalid
	}
	probe, e := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		return result, e
	}
	now, e := clock(ctx, probe)
	probe.Rollback(ctx)
	if e != nil {
		return result, e
	}
	if through > now.In(referralBeijing70).Format("2006-01-02") {
		return result, ErrInvalid
	}
	rows, e := s.pool.Query(ctx, `SELECT l.order_id FROM referral_daily_liabilities l JOIN referral_daily_terms d ON d.order_id=l.order_id JOIN merchants m ON m.id=d.merchant_id JOIN merchants p ON p.id=d.promoter_id WHERE l.eligible_day<=$1::date AND m.enabled AND p.enabled AND NOT EXISTS(SELECT 1 FROM referral_daily_awards a WHERE a.order_id=l.order_id) AND NOT EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.order_id=l.order_id AND c.state<>'failed') ORDER BY l.eligible_day,l.order_id LIMIT $2`, through, limit)
	if e != nil {
		return result, e
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return result, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	for _, id := range ids {
		result.Examined++
		paid, e := s.tickReferralDailyItem(ctx, id, through, clock)
		if e != nil {
			return result, e
		}
		if paid {
			result.Awarded++
			result.OrderIDs = append(result.OrderIDs, fmt.Sprint(id))
		} else {
			result.Skipped++
		}
	}
	return result, nil
}
func (s *Store) dailyAwardProof(d referralDailyTerms, a referralDailyActivation, r referralRewardRecord, x referralDailyAward) ([]byte, error) {
	return s.dailyDigest("disbursement", []any{d.Proof, a.Proof, r.ProofHash, x.OrderID, x.RewardID, x.ReleaseJournalID, x.ExecutedAt.UTC().Format(time.RFC3339Nano)})
}
func (s *Store) verifyReferralDailyAwardMirror(ctx context.Context, q referralReader, o Order, t referralOrderTerms, r referralRewardRecord) error {
	if t.daily == nil {
		return ErrConflict
	}
	a, e := s.readReferralDailyActivation(ctx, q, o, *t.daily)
	if e != nil {
		return e
	}
	var x referralDailyAward
	e = q.QueryRow(ctx, `SELECT order_id,reward_id,release_journal_id,executed_at,source_hash FROM referral_daily_awards WHERE order_id=$1`, o.ID).Scan(&x.OrderID, &x.RewardID, &x.ReleaseJournalID, &x.ExecutedAt, &x.Proof)
	if e != nil || x.OrderID != o.ID || x.RewardID != r.ID {
		return ErrConflict
	}
	proof, e := s.dailyAwardProof(*t.daily, a, r, x)
	if e != nil || !hmac.Equal(proof, x.Proof) {
		return ErrConflict
	}
	var valid bool
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM referral_daily_award_invariants WHERE order_id=$1 AND valid IS TRUE)`, o.ID).Scan(&valid)
	if e != nil || !valid {
		return ErrConflict
	}
	return s.verifyDailyEvent(ctx, q, o.ID, "awarded", r.ID, "daily-referral-engine", x.Proof)
}
func (s *Store) readReferralDailyState(ctx context.Context, q referralReader, o Order, t referralOrderTerms) (referralDailyActivation, string, int64, error) {
	return s.readReferralDailyStateForRefund(ctx, q, o, t, 0)
}
func (s *Store) readReferralDailyStateForRefund(ctx context.Context, q referralReader, o Order, t referralOrderTerms, currentRefund int64) (referralDailyActivation, string, int64, error) {
	var a referralDailyActivation
	if t.daily == nil {
		return a, "", 0, ErrConflict
	}
	a, e := s.readReferralDailyActivation(ctx, q, o, *t.daily)
	if e != nil {
		return a, "", 0, e
	}
	history, e := s.readDailyRefundHistory(ctx, q, *t.daily, a)
	if e != nil {
		return a, "", 0, e
	}
	var mirrored, rewarded bool
	var balance int64
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM referral_daily_awards WHERE order_id=$1),EXISTS(SELECT 1 FROM referral_rewards WHERE order_id=$1),(SELECT balance FROM accounts WHERE id=$2)`, o.ID, t.daily.PendingAccountID).Scan(&mirrored, &rewarded, &balance)
	if e != nil {
		return a, "", 0, e
	}
	if mirrored {
		if !rewarded || balance != 0 || history.Unresolved != nil || history.Success != nil {
			return a, "", 0, ErrConflict
		}
		if _, e = s.readReferralRewardTx(ctx, q, o.ID); e != nil {
			return a, "", 0, e
		}
		return a, "awarded", 0, nil
	}
	if rewarded {
		return a, "", 0, ErrConflict
	}
	// Every pre-award refund must have its own keyed pending-family fact.
	// The explicit current ID is skipped only while the original locked
	// ReserveRefund transaction is constructing that very fact.
	rows, e := q.Query(ctx, `SELECT id FROM refunds WHERE order_id=$1 ORDER BY id`, o.ID)
	if e != nil {
		return a, "", 0, e
	}
	var refundIDs []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return a, "", 0, e
		}
		refundIDs = append(refundIDs, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return a, "", 0, e
	}
	for _, id := range refundIDs {
		known := false
		for _, c := range history.Items {
			known = known || c.RefundID == id
		}
		if !known && id != currentRefund && (history.Success == nil || id < history.Success.RefundID) {
			return a, "", 0, ErrConflict
		}
	}
	if history.Success != nil {
		if balance != 0 {
			return a, "", 0, ErrConflict
		}
		return a, "voided", 0, nil
	}
	if history.Unresolved != nil {
		if balance != 0 {
			return a, "", 0, ErrConflict
		}
		return a, "blocked", 0, nil
	}
	if balance != t.RewardAmount {
		return a, "", 0, ErrConflict
	}
	return a, "pending", balance, nil
}
func (s *Store) tickReferralDailyItem(ctx context.Context, id int64, through string, clock dailyClock70) (bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1 FOR UPDATE SKIP LOCKED", id))
	if errors.Is(e, ErrNotFound) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	t, e := s.readReferralTerms(ctx, tx, o)
	if e != nil {
		return false, e
	}
	a, state, _, e := s.readReferralDailyState(ctx, tx, o, t)
	if e != nil {
		return false, e
	}
	if state != "pending" {
		return false, nil
	}
	now, e := clock(ctx, tx)
	if e != nil {
		return false, e
	}
	// PostgreSQL timestamps retain microseconds. Seal the exact persisted value
	// even when a package-private test clock supplies nanosecond precision.
	now = now.UTC().Truncate(time.Microsecond)
	if through > now.In(referralBeijing70).Format("2006-01-02") || through < a.EligibleDay {
		return false, ErrInvalid
	}
	rows, e := tx.Query(ctx, `SELECT enabled FROM merchants WHERE id=ANY($1::bigint[]) ORDER BY id FOR SHARE`, []int64{o.MerchantID, t.PromoterID})
	if e != nil {
		return false, e
	}
	count := 0
	enabled := true
	for rows.Next() {
		var v bool
		if e = rows.Scan(&v); e != nil {
			rows.Close()
			return false, e
		}
		count++
		enabled = enabled && v
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return false, e
	}
	if count != 2 || !enabled {
		return false, nil
	}
	if e = lockPaymentAccountSet(ctx, tx, o, t); e != nil {
		return false, e
	}
	key := fmt.Sprintf("referral-daily-release:%d", o.ID)
	if e = postByAccountIDs(ctx, tx, key, "release this order funded daily referral liability", t.daily.PendingAccountID, a.FeesAccountID, t.RewardAmount); e != nil {
		return false, e
	}
	x := referralDailyAward{OrderID: o.ID, ExecutedAt: now}
	if e = tx.QueryRow(ctx, `SELECT id FROM journals WHERE event_key=$1`, key).Scan(&x.ReleaseJournalID); e != nil {
		return false, e
	}
	// Original v1 award writer runs only after this exact original liability
	// funded its fees debit in this same transaction. It is a real award now.
	immediate := t
	immediate.daily = nil
	if e = s.awardReferralPayment(ctx, tx, o, immediate); e != nil {
		return false, e
	}
	r := referralRewardRecord{OrderID: o.ID, MerchantID: o.MerchantID, PromoterID: t.PromoterID, Amount: t.RewardAmount, ChannelID: a.ChannelID, ProviderTradeNo: a.ProviderTradeNo, TermsHash: t.ProofHash}
	if e = tx.QueryRow(ctx, `SELECT id,journal_id,proof_hash FROM referral_rewards WHERE order_id=$1`, o.ID).Scan(&r.ID, &r.JournalID, &r.ProofHash); e != nil {
		return false, e
	}
	proof, e := s.referralRewardProof(r)
	if e != nil || !hmac.Equal(proof, r.ProofHash) {
		return false, ErrConflict
	}
	x.RewardID = r.ID
	x.Proof, e = s.dailyAwardProof(*t.daily, a, r, x)
	if e != nil {
		return false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO referral_daily_awards(order_id,reward_id,release_journal_id,executed_at,source_hash)VALUES($1,$2,$3,$4,$5)`, x.OrderID, x.RewardID, x.ReleaseJournalID, x.ExecutedAt, x.Proof); e != nil {
		return false, e
	}
	if e = s.insertDailyEvent(ctx, tx, o.ID, "awarded", r.ID, "daily-referral-engine", x.Proof); e != nil {
		return false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return false, e
	}
	return true, nil
}
