package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
)

type referralDailyTerms struct {
	OrderID, MerchantID, PromoterID, Amount, PlatformFee, Reward, PendingAccountID int64
	Basis                                                                          string
	Versions                                                                       [5]int64
	TermsHash, Proof                                                               []byte
}

func (s *Store) dailyDigest(domain string, fact any) ([]byte, error) {
	if len(s.indexKey) != 32 {
		return nil, ErrInvalid
	}
	raw, e := json.Marshal(fact)
	if e != nil || len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	m := hmac.New(sha256.New, s.indexKey)
	m.Write([]byte("goepay/referral-daily/" + domain + "/v2\x00"))
	m.Write(raw)
	return m.Sum(nil), nil
}
func (s *Store) dailyTermsProof(d referralDailyTerms) ([]byte, error) {
	return s.dailyDigest("original-order", []any{2, "daily", "Asia/Shanghai-UTC+08", d.OrderID, d.MerchantID, d.PromoterID, d.Amount, d.PlatformFee, d.Reward, d.Basis, d.Versions, d.PendingAccountID, d.TermsHash})
}

// Recover mode from the original immutable keyed policy IDs. Losing a daily
// marker must never downgrade a deferred entitlement to immediate credit.
func (s *Store) referralOriginalMode(ctx context.Context, q referralReader, t referralOrderTerms) (string, string, error) {
	mode, basis := "immediate", "fee"
	for i, id := range t.PolicyVersions[:3] {
		if id == 0 {
			continue
		}
		if id < 0 {
			return "", "", ErrConflict
		}
		var r referralPolicyRecord
		var audited bool
		e := q.QueryRow(ctx, `SELECT p.id,p.scope_kind,p.scope_id,p.revision,p.enabled,p.rate_bps,p.mode,p.basis,p.refund_policy,p.actor_role,p.actor_id,p.actor_name,p.proof_hash,p.created_at,EXISTS(SELECT 1 FROM referral_policy_audits a WHERE a.policy_id=p.id AND (a.actor_role,a.actor_id,a.actor_name)=(p.actor_role,p.actor_id,p.actor_name)) FROM referral_policies p WHERE p.id=$1`, id).Scan(&r.ID, &r.Scope, &r.ScopeID, &r.Revision, &r.Enabled, &r.RateBPS, &r.Mode, &r.Basis, &r.RefundPolicy, &r.Actor.Role, &r.Actor.ID, &r.Actor.Name, &r.ProofHash, &r.CreatedAt, &audited)
		if e != nil {
			return "", "", ErrConflict
		}
		scope, scopeID := "global", int64(0)
		if i > 0 {
			scope = "group"
			scopeID = t.PolicyVersions[i+2]
		}
		proof, e := s.referralPolicyProof(r)
		if e != nil || !audited || !hmac.Equal(proof, r.ProofHash) || r.Scope != scope || r.ScopeID != scopeID || validateReferralPolicy(r.Scope, r.ReferralPolicyInput) != nil {
			return "", "", ErrConflict
		}
		mode, basis = r.Mode, r.Basis
	}
	return mode, basis, nil
}
func (s *Store) readReferralDailyTerms(ctx context.Context, q referralReader, o Order, t referralOrderTerms) (*referralDailyTerms, error) {
	mode, basis, e := s.referralOriginalMode(ctx, q, t)
	if e != nil {
		return nil, e
	}
	var d referralDailyTerms
	var raw []byte
	e = q.QueryRow(ctx, `SELECT order_id,merchant_id,promoter_id,amount,platform_fee,reward_amount,basis,policy_versions,pending_account_id,terms_hash,source_hash FROM referral_daily_terms WHERE order_id=$1 AND source_version=2 AND mode='daily' AND timezone='Asia/Shanghai-UTC+08'`, o.ID).Scan(&d.OrderID, &d.MerchantID, &d.PromoterID, &d.Amount, &d.PlatformFee, &d.Reward, &d.Basis, &raw, &d.PendingAccountID, &d.TermsHash, &d.Proof)
	if errors.Is(e, pgx.ErrNoRows) {
		if mode == "daily" && t.Enabled {
			return nil, ErrConflict
		}
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	charge104, _, chargeError104 := s.readBuyerSource104(ctx, q, o)
	if chargeError104 != nil {
		return nil, chargeError104
	}
	if mode != "daily" || !t.Enabled || json.Unmarshal(raw, &d.Versions) != nil || d.OrderID != o.ID || d.MerchantID != o.MerchantID || d.PromoterID != t.PromoterID || d.Amount != charge104 || d.PlatformFee != o.PlatformFee || d.Reward != t.RewardAmount || d.Basis != basis || d.Versions != t.PolicyVersions || !hmac.Equal(d.TermsHash, t.ProofHash) {
		return nil, ErrConflict
	}
	proof, e := s.dailyTermsProof(d)
	if e != nil || !hmac.Equal(proof, d.Proof) {
		return nil, ErrConflict
	}
	var valid bool
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1 AND owner_id=0 AND kind='referral_daily_pending' AND scope_id=$2 AND balance>=0)`, d.PendingAccountID, o.ID).Scan(&valid)
	if e != nil || !valid {
		return nil, ErrConflict
	}
	if e = s.verifyDailyEvent(ctx, q, d.OrderID, "birth", 0, "payment-engine", d.Proof); e != nil {
		return nil, e
	}
	return &d, nil
}
func (s *Store) insertReferralDailyTerms(ctx context.Context, tx pgx.Tx, o Order, t referralOrderTerms, basis string) error {
	charge104, _, chargeError104 := s.readBuyerSource104(ctx, tx, o)
	if chargeError104 != nil {
		return chargeError104
	}
	d := referralDailyTerms{OrderID: o.ID, MerchantID: o.MerchantID, PromoterID: t.PromoterID, Amount: charge104, PlatformFee: o.PlatformFee, Reward: t.RewardAmount, Basis: basis, Versions: t.PolicyVersions, TermsHash: t.ProofHash}
	if e := tx.QueryRow(ctx, `INSERT INTO accounts(owner_id,kind,scope_id)VALUES(0,'referral_daily_pending',$1)RETURNING id`, o.ID).Scan(&d.PendingAccountID); e != nil {
		return e
	}
	var e error
	d.Proof, e = s.dailyTermsProof(d)
	if e != nil {
		return e
	}
	raw, _ := json.Marshal(d.Versions)
	_, e = tx.Exec(ctx, `INSERT INTO referral_daily_terms(order_id,merchant_id,promoter_id,amount,platform_fee,reward_amount,basis,policy_versions,pending_account_id,terms_hash,source_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, d.OrderID, d.MerchantID, d.PromoterID, d.Amount, d.PlatformFee, d.Reward, d.Basis, raw, d.PendingAccountID, d.TermsHash, d.Proof)
	if e != nil {
		return e
	}
	return s.insertDailyEvent(ctx, tx, d.OrderID, "birth", 0, "payment-engine", d.Proof)
}
func (s *Store) dailyEventProof(oid int64, op string, ref int64, actor string, proof []byte) ([]byte, error) {
	return s.dailyDigest("audit", []any{oid, op, ref, "system", actor, proof})
}
func dailyEventKey(oid int64, op string, ref int64) string {
	return fmt.Sprintf("referral-daily:%d:%s:%d", oid, op, ref)
}
func (s *Store) insertDailyEvent(ctx context.Context, tx pgx.Tx, oid int64, op string, ref int64, actor string, proof []byte) error {
	h, e := s.dailyEventProof(oid, op, ref, actor, proof)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO referral_daily_events(order_id,operation,reference_id,event_key,actor_name,subject_hash,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7)`, oid, op, ref, dailyEventKey(oid, op, ref), actor, proof, h)
	return e
}
func (s *Store) verifyDailyEvent(ctx context.Context, q referralReader, oid int64, op string, ref int64, actor string, proof []byte) error {
	expected, e := s.dailyEventProof(oid, op, ref, actor, proof)
	if e != nil {
		return e
	}
	var actual, subject []byte
	e = q.QueryRow(ctx, `SELECT subject_hash,proof_hash FROM referral_daily_events WHERE order_id=$1 AND operation=$2 AND reference_id=$3 AND event_key=$4 AND actor_role='system' AND actor_name=$5`, oid, op, ref, dailyEventKey(oid, op, ref), actor).Scan(&subject, &actual)
	if e != nil || !hmac.Equal(expected, actual) || !hmac.Equal(subject, proof) {
		return ErrConflict
	}
	return nil
}
