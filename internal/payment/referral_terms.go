package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

const referralRefundPolicy = "any_successful_refund_voids_reward"

// The same authenticated reads serve both a read-only funds preview and the
// business transaction that consumes its authorization.
type referralReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func calculateReferralReward(fee, rate int64) (int64, error) {
	if fee < 0 || rate < 0 || rate > 10000 {
		return 0, ErrInvalid
	}
	return fee/10000*rate + (fee%10000*rate+5000)/10000, nil
}

func (s *Store) referralDigest(domain string, fact any) ([]byte, error) {
	if len(s.indexKey) != 32 {
		return nil, ErrInvalid
	}
	raw, err := json.Marshal(fact)
	if err != nil || len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	m := hmac.New(sha256.New, s.indexKey)
	m.Write([]byte("goepay/referral/" + domain + "/v1\x00"))
	m.Write(raw)
	return m.Sum(nil), nil
}

type referralOrderTerms struct {
	OrderID, MerchantID, RelationID, PromoterID int64
	Purpose, Reason                             string
	Enabled                                     bool
	RateBPS, PlatformFee, RewardAmount          int64
	RefundPolicy                                string
	PolicyVersions                              [5]int64 // global, payer, promoter policy IDs; payer/promoter group IDs
	ProofHash                                   []byte
	daily                                       *referralDailyTerms
	profit                                      *referralProfitCost
}

func (s *Store) referralTermsProof(o Order, t referralOrderTerms) ([]byte, error) {
	return s.referralDigest("order-terms", withRuntimeFeeBirthMarker(o, []any{o.ID, o.TradeNo, o.MerchantID, o.OutTradeNo,
		o.Amount, o.MerchantAmount, o.PlatformFee, o.FeeRateBPS, o.FeeGroupID, o.FeePolicy, o.RefundFeePayer,
		t.RelationID, t.PromoterID, t.Purpose, t.Enabled, t.Reason, t.RateBPS, t.PlatformFee, t.RewardAmount,
		t.RefundPolicy, t.PolicyVersions}))
}

func (s *Store) readReferralTermsBase(ctx context.Context, tx referralReader, o Order) (referralOrderTerms, error) {
	var t referralOrderTerms
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT order_id,merchant_id,COALESCE(relation_id,0),COALESCE(promoter_id,0),purpose,enabled,reason,rate_bps,platform_fee,reward_amount,refund_policy,policy_versions,proof_hash FROM referral_order_terms WHERE order_id=$1`, o.ID).Scan(
		&t.OrderID, &t.MerchantID, &t.RelationID, &t.PromoterID, &t.Purpose, &t.Enabled, &t.Reason,
		&t.RateBPS, &t.PlatformFee, &t.RewardAmount, &t.RefundPolicy, &raw, &t.ProofHash)
	if errors.Is(err, pgx.ErrNoRows) {
		if e := s.readReferralOrderSource(ctx, tx, o, nil); e != nil {
			return t, e
		}
		var orphan bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM referral_profit_costs WHERE order_id=$1) OR EXISTS(SELECT 1 FROM referral_profit_cost_events WHERE order_id=$1)`, o.ID).Scan(&orphan); e != nil {
			return t, e
		}
		if orphan {
			return t, ErrConflict
		}
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	if json.Unmarshal(raw, &t.PolicyVersions) != nil || t.MerchantID != o.MerchantID || t.PlatformFee != o.PlatformFee || t.RefundPolicy != referralRefundPolicy {
		return t, ErrConflict
	}
	hash, err := s.referralTermsProof(o, t)
	if err != nil || !hmac.Equal(hash, t.ProofHash) {
		return t, ErrConflict
	}
	if err = s.readReferralOrderSource(ctx, tx, o, t.ProofHash); err != nil {
		return t, err
	}
	var recharge bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM recharge_intents WHERE order_id=$1)", o.ID).Scan(&recharge); err != nil {
		return t, err
	}
	if (t.Purpose == "recharge") != recharge {
		return t, ErrConflict
	}
	if t.RelationID > 0 {
		r, err := s.ReferralRelationTx(ctx, tx, o.MerchantID)
		if err != nil {
			return t, err
		}
		if r.ID != t.RelationID || r.PromoterID != t.PromoterID {
			return t, ErrConflict
		}
	}
	return t, nil
}

func (s *Store) readReferralTerms(ctx context.Context, tx referralReader, o Order) (referralOrderTerms, error) {
	t, err := s.readReferralTermsBase(ctx, tx, o)
	if err != nil {
		return t, err
	}
	t.daily, err = s.readReferralDailyTerms(ctx, tx, o, t)
	if err == nil {
		t.profit, err = s.readReferralProfit(ctx, tx, o, t)
	}
	return t, err
}

func (s *Store) snapshotReferralTerms(ctx context.Context, tx pgx.Tx, o Order, purpose string, cost *PaymentCostQuote) error {
	if old, err := s.readReferralTerms(ctx, tx, o); err == nil {
		if old.Purpose != purpose {
			return ErrConflict
		}
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	charge104, _, chargeError104 := s.readBuyerSource104(ctx, tx, o)
	if chargeError104 != nil {
		return chargeError104
	}
	t := referralOrderTerms{OrderID: o.ID, MerchantID: o.MerchantID, Purpose: purpose, PlatformFee: o.PlatformFee, RefundPolicy: referralRefundPolicy, Reason: "no_relation"}
	dailyBasis := ""
	var profit *referralProfitCost
	if purpose == "recharge" {
		t.Reason = "recharge"
	} else {
		r, err := s.ReferralRelationTx(ctx, tx, o.MerchantID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err == nil {
			t.RelationID, t.PromoterID = r.ID, r.PromoterID
			policy, refs, enabled, err := s.effectiveReferralPolicy(ctx, tx, o.FeeGroupID, r.PromoterID)
			if err != nil {
				return err
			}
			t.PolicyVersions, t.RateBPS = refs, policy.RateBPS
			if enabled && policy.Enabled && policy.Basis == "profit" {
				profit, err = s.prepareReferralProfit(o, charge104, policy.RateBPS, cost)
				if err != nil {
					return err
				}
			}
			switch {
			case !enabled:
				t.Reason = "promoter_disabled"
			case !policy.Enabled:
				t.Reason = "disabled"
			case o.PlatformFee == 0:
				t.Reason = "zero_fee"
			case policy.RateBPS == 0:
				t.Reason = "zero_rate"
			default:
				if profit != nil {
					t.RewardAmount = profit.Reward
				} else {
					t.RewardAmount, err = calculateReferralPolicyReward(charge104, o.PlatformFee, policy.RateBPS, policy.Basis)
				}
				if err != nil {
					return err
				}
				t.Enabled = t.RewardAmount > 0
				t.Reason = "eligible"
				if !t.Enabled {
					t.Reason = "zero_rate"
				}
				if t.Enabled && policy.Mode == "daily" {
					dailyBasis = policy.Basis
				}
			}
		}
	}
	hash, err := s.referralTermsProof(o, t)
	if err != nil {
		return err
	}
	refs, _ := json.Marshal(t.PolicyVersions)
	_, err = tx.Exec(ctx, `INSERT INTO referral_order_terms(order_id,merchant_id,relation_id,promoter_id,purpose,enabled,reason,rate_bps,platform_fee,reward_amount,refund_policy,policy_versions,proof_hash) VALUES($1,$2,NULLIF($3::bigint,0),NULLIF($4::bigint,0),$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		t.OrderID, t.MerchantID, t.RelationID, t.PromoterID, t.Purpose, t.Enabled, t.Reason, t.RateBPS, t.PlatformFee, t.RewardAmount, t.RefundPolicy, refs, hash)
	t.ProofHash = hash
	if err == nil && profit != nil {
		err = s.insertReferralProfit(ctx, tx, t, profit)
	}
	if err == nil && dailyBasis != "" {
		err = s.insertReferralDailyTerms(ctx, tx, o, t, dailyBasis)
	}
	if err == nil {
		err = s.insertReferralOrderSource(ctx, tx, o, t, profit)
	}
	return err
}

type ReferralRefundSource struct {
	RewardID, OrderID, MerchantID, PromoterID, Amount, PlatformFee, AwardJournalID int64
	RequestHash                                                                    []byte
}

type referralRewardRecord struct {
	ID, OrderID, MerchantID, PromoterID, Amount, JournalID, ChannelID int64
	ProviderTradeNo                                                   string
	TermsHash, ProofHash                                              []byte
}

func (s *Store) referralRewardProof(r referralRewardRecord) ([]byte, error) {
	return s.referralDigest("original-fee-award", []any{r.ID, r.OrderID, r.MerchantID, r.PromoterID, r.Amount, r.JournalID, r.ChannelID, r.ProviderTradeNo, hex.EncodeToString(r.TermsHash)})
}

func (s *Store) readReferralRewardTx(ctx context.Context, tx referralReader, orderID int64) (referralRewardRecord, error) {
	var r referralRewardRecord
	err := tx.QueryRow(ctx, `SELECT id,order_id,merchant_id,promoter_id,amount,journal_id,channel_id,provider_trade_no,terms_hash,proof_hash FROM referral_rewards WHERE order_id=$1`, orderID).Scan(
		&r.ID, &r.OrderID, &r.MerchantID, &r.PromoterID, &r.Amount, &r.JournalID, &r.ChannelID, &r.ProviderTradeNo, &r.TermsHash, &r.ProofHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	o, err := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", orderID))
	if err != nil {
		return r, err
	}
	t, err := s.readReferralTerms(ctx, tx, o)
	if err != nil {
		return r, err
	}
	hash, err := s.referralRewardProof(r)
	if err != nil || !hmac.Equal(hash, r.ProofHash) || !hmac.Equal(t.ProofHash, r.TermsHash) || !t.Enabled || t.PromoterID != r.PromoterID || t.RewardAmount != r.Amount {
		return r, ErrConflict
	}
	var valid bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM referral_reward_invariants WHERE reward_id=$1 AND valid IS TRUE)", r.ID).Scan(&valid); err != nil {
		return r, err
	}
	if !valid {
		return r, ErrConflict
	}
	if t.daily != nil {
		if err = s.verifyReferralDailyAwardMirror(ctx, tx, o, t, r); err != nil {
			return r, err
		}
	}
	return r, nil
}

func (s *Store) referralRefundSource(ctx context.Context, tx referralReader, orderID int64) (*ReferralRefundSource, error) {
	o, err := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", orderID))
	if err != nil {
		return nil, err
	}
	t, err := s.readReferralTerms(ctx, tx, o)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if errors.Is(err, ErrNotFound) || !t.Enabled {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM referral_rewards WHERE order_id=$1)", orderID).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrConflict
		}
		return nil, nil
	}
	if t.daily != nil {
		_, state, _, dailyErr := s.readReferralDailyState(ctx, tx, o, t)
		if dailyErr != nil {
			return nil, dailyErr
		}
		if state != "awarded" {
			return nil, nil
		}
	}
	r, err := s.readReferralRewardTx(ctx, tx, orderID)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return &ReferralRefundSource{r.ID, r.OrderID, r.MerchantID, r.PromoterID, r.Amount, t.PlatformFee, r.JournalID, r.ProofHash}, nil
}
