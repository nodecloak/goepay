package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *Store) readMembershipReferralSource(ctx context.Context, q referralReader, p MembershipPurchase) (membershipReferralSource, error) {
	var v membershipReferralSource
	v.MerchantID, v.GroupID, v.Price = p.MerchantID, p.GroupID, p.Price
	var pids, revisions, hashes []byte
	e := q.QueryRow(ctx, `SELECT source_version,merchant_id,COALESCE(promoter_id,0),COALESCE(relation_id,0),relation_hash,payer_group_id,promoter_group_id,policy_ids,policy_revisions,policy_hashes,buyer_revision,promoter_revision,promoter_enabled,enabled,rate_bps,price,reward_amount,reason,COALESCE(income_account_id,0),COALESCE(purchase_journal_id,0),transaction_id::text,proof_hash FROM membership_referral_sources WHERE purchase_id=$1`, p.ID).Scan(&v.SourceVersion, &v.MerchantID, &v.PromoterID, &v.RelationID, &v.RelationHash, &v.PayerGroupID, &v.PromoterGroupID, &pids, &revisions, &hashes, &v.BuyerRevision, &v.PromoterRevision, &v.PromoterEnabled, &v.Enabled, &v.RateBPS, &v.Price, &v.Reward, &v.Reason, &v.IncomeAccountID, &v.PurchaseJournalID, &v.TransactionID, &v.ProofHash)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, ErrConflict
	}
	if e != nil {
		return v, e
	}
	if p.SourceVersion != 1 || v.SourceVersion != 1 || p.MerchantID != v.MerchantID || p.Price != v.Price || json.Unmarshal(pids, &v.PolicyIDs) != nil || json.Unmarshal(revisions, &v.PolicyRevisions) != nil || json.Unmarshal(hashes, &v.PolicyHashes) != nil {
		return v, ErrConflict
	}
	proof, e := s.membershipReferralSourceProof(p, v)
	if e != nil || !hmac.Equal(proof, v.ProofHash) {
		return v, ErrConflict
	}
	var enabled bool
	var rate int64
	for i, scope := range []struct {
		kind string
		id   int64
	}{{"global", 0}, {"group", v.PayerGroupID}, {"group", v.PromoterGroupID}} {
		if v.PolicyIDs[i] == 0 {
			if v.PolicyRevisions[i] != 0 || v.PolicyHashes[i] != "" {
				return v, ErrConflict
			}
			continue
		}
		pol, e := s.membershipReferralPolicyByID(ctx, q, v.PolicyIDs[i])
		if e != nil {
			return v, e
		}
		if pol.Scope != scope.kind || pol.ScopeID != scope.id || pol.Revision != v.PolicyRevisions[i] || hex.EncodeToString(pol.ProofHash) != v.PolicyHashes[i] {
			return v, ErrConflict
		}
		if pol.Enabled != nil {
			enabled = *pol.Enabled
		}
		if pol.RateBPS != nil {
			rate = *pol.RateBPS
		}
	}
	if v.Enabled != enabled || v.RateBPS != rate {
		return v, ErrConflict
	}
	if v.RelationID > 0 {
		r, e := s.ReferralRelationTx(ctx, q, p.MerchantID)
		if e != nil {
			return v, e
		}
		if r.ID != v.RelationID || r.PromoterID != v.PromoterID || !hmac.Equal(r.ProofHash, v.RelationHash) {
			return v, ErrConflict
		}
	}
	reason, reward, e := membershipReferralReason(v)
	if e != nil || reason != v.Reason || reward != v.Reward {
		return v, ErrConflict
	}
	var valid bool
	if e = q.QueryRow(ctx, "SELECT valid FROM membership_referral_source_invariants WHERE purchase_id=$1", p.ID).Scan(&valid); e != nil {
		return v, e
	}
	if !valid {
		return v, ErrConflict
	}
	if e = s.verifyMembershipReferralEvent(ctx, q, p, "source", v.ProofHash); e != nil {
		return v, e
	}
	if v.Reward > 0 {
		var promoter, amount, journal int64
		var original, hash []byte
		if e = q.QueryRow(ctx, "SELECT promoter_id,amount,journal_id,source_hash,proof_hash FROM membership_referral_rewards WHERE purchase_id=$1", p.ID).Scan(&promoter, &amount, &journal, &original, &hash); e != nil {
			return v, e
		}
		proof, e := s.membershipReferralRewardProof(p.ID, promoter, amount, journal, original)
		if e != nil || promoter != v.PromoterID || amount != v.Reward || !hmac.Equal(original, v.ProofHash) || !hmac.Equal(hash, proof) {
			return v, ErrConflict
		}
		if e = s.verifyMembershipReferralEvent(ctx, q, p, "award", hash); e != nil {
			return v, e
		}
	}
	var count int64
	if e = q.QueryRow(ctx, "SELECT count(*) FROM membership_referral_events WHERE purchase_id=$1", p.ID).Scan(&count); e != nil {
		return v, e
	}
	expected := int64(1)
	if v.Reward > 0 {
		expected++
	}
	if count != expected {
		return v, ErrConflict
	}
	return v, nil
}

func (s *Store) verifyMembershipReferralEvent(ctx context.Context, q referralReader, p MembershipPurchase, operation string, subject []byte) error {
	var key, actor string
	var got, hash []byte
	e := q.QueryRow(ctx, "SELECT event_key,actor,subject_hash,proof_hash FROM membership_referral_events WHERE purchase_id=$1 AND operation=$2", p.ID, operation).Scan(&key, &actor, &got, &hash)
	if e != nil {
		return e
	}
	expected := fmt.Sprintf("membership-referral:%d:%s", p.ID, operation)
	proof, e := s.membershipReferralDigest("event", []any{p.ID, operation, expected, p.Actor, hex.EncodeToString(subject)})
	if e != nil || key != expected || actor != p.Actor || !hmac.Equal(got, subject) || !hmac.Equal(hash, proof) {
		return ErrConflict
	}
	return nil
}

func (s *Store) verifyMembershipPurchase(ctx context.Context, q referralReader, p MembershipPurchase) error {
	if p.SourceVersion == 1 {
		_, e := s.readMembershipReferralSource(ctx, q, p)
		return e
	}
	if p.SourceVersion != 0 {
		return ErrConflict
	}
	var valid bool
	e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM membership_referral_legacy_purchases l JOIN membership_purchases p ON p.id=l.purchase_id WHERE p.id=$1 AND p.source_version=0 AND l.row_snapshot=membership_referral_legacy_snapshot(p) AND l.row_hash=sha256(convert_to(membership_referral_legacy_snapshot(p)::text,'UTF8')))`, p.ID).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return ErrConflict
	}
	return nil
}
