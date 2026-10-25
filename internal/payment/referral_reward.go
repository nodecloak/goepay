package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Called under the original order lock, before the first payment journal. Old
// orders have no terms and are deliberately not backfilled with new rewards.
func (s *Store) referralPaymentTerms(ctx context.Context, tx pgx.Tx, o Order) (referralOrderTerms, error) {
	t, err := s.readReferralTerms(ctx, tx, o)
	if errors.Is(err, ErrNotFound) {
		return referralOrderTerms{}, nil
	}
	return t, err
}

func lockPaymentAccountSet(ctx context.Context, tx pgx.Tx, o Order, t referralOrderTerms) error {
	owners := []int64{o.MerchantID}
	if t.Enabled {
		owners = append(owners, t.PromoterID)
	}
	if err := lockMerchantForeignKeySet(ctx, tx, owners); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM accounts WHERE scope_id=0 AND
 ((owner_id=0 AND kind='clearing') OR (owner_id=0 AND kind='fees' AND $1)
 OR (owner_id=$2 AND kind='available' AND $3)
 OR (owner_id=$4 AND kind='available' AND $5))`, o.PlatformFee > 0, o.MerchantID, o.MerchantAmount > 0, t.PromoterID, t.Enabled)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	want := 1
	if o.PlatformFee > 0 {
		want++
	}
	if o.MerchantAmount > 0 {
		want++
	}
	if t.Enabled {
		want++
	}
	if t.daily != nil {
		ids = append(ids, t.daily.PendingAccountID)
		want++
	}
	if len(ids) != want {
		return ErrNotFound
	}
	return lockLedgerAccountSet(ctx, tx, ids)
}

func (s *Store) awardReferralPayment(ctx context.Context, tx pgx.Tx, o Order, t referralOrderTerms) error {
	if !t.Enabled {
		return nil
	}
	if t.daily != nil {
		return s.activateReferralDaily(ctx, tx, o, t)
	}
	var channel int64
	var trade string
	err := tx.QueryRow(ctx, "SELECT channel_id,provider_trade_no FROM verified_payment_receipts WHERE order_id=$1", o.ID).Scan(&channel, &trade)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrState
	}
	if err != nil {
		return err
	}
	key := fmt.Sprintf("referral-award:%d", o.ID)
	if err = post(ctx, tx, key, "original payment fee referral reward", 0, "fees", t.PromoterID, "available", t.RewardAmount); err != nil {
		return err
	}
	r := referralRewardRecord{OrderID: o.ID, MerchantID: o.MerchantID, PromoterID: t.PromoterID, Amount: t.RewardAmount, ChannelID: channel, ProviderTradeNo: trade, TermsHash: t.ProofHash}
	if err = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('referral_rewards','id'))").Scan(&r.ID); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&r.JournalID); err != nil {
		return err
	}
	proof, err := s.referralRewardProof(r)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO referral_rewards(id,order_id,merchant_id,promoter_id,amount,terms_hash,journal_id,channel_id,provider_trade_no,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.ID, r.OrderID, r.MerchantID, r.PromoterID, r.Amount, r.TermsHash, r.JournalID, r.ChannelID, r.ProviderTradeNo, proof); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO referral_reward_audits(reward_id,event_key) VALUES($1,$2)", r.ID, key)
	return err
}

func (s *Store) verifyReferralPaidIntegrity(ctx context.Context, tx pgx.Tx, o Order) error {
	t, err := s.referralPaymentTerms(ctx, tx, o)
	if err != nil {
		return err
	}
	if t.Enabled {
		if t.daily != nil {
			return s.verifyReferralDailyPaid(ctx, tx, o, t)
		}
		_, err = s.readReferralRewardTx(ctx, tx, o.ID)
		if errors.Is(err, ErrNotFound) {
			return ErrConflict
		}
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM referral_rewards WHERE order_id=$1)", o.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrConflict
	}
	return nil
}
