package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// MembershipReferralPurchaseSnapshot is for the private funds authorization
// digest. HTTP handlers must publish an explicit safe summary, never this value.
type MembershipReferralPurchaseSnapshot struct {
	MerchantID       int64    `json:"merchant_id,string"`
	GroupID          int64    `json:"group_id,string"`
	PromoterID       int64    `json:"promoter_id,string"`
	RelationID       int64    `json:"relation_id,string"`
	Price            int64    `json:"price,string"`
	Reward           int64    `json:"reward,string"`
	Reason           string   `json:"reason"`
	PolicyIDs        [3]int64 `json:"policy_ids"`
	PolicyRevisions  [3]int64 `json:"policy_revisions"`
	PayerGroupID     int64    `json:"payer_group_id,string"`
	PromoterGroupID  int64    `json:"promoter_group_id,string"`
	ProofFingerprint string   `json:"proof_fingerprint"`
}

type membershipReferralSource struct {
	MembershipReferralPurchaseSnapshot
	SourceVersion                            int
	PolicyHashes                             [3]string
	RelationHash                             []byte
	BuyerRevision, PromoterRevision, RateBPS int64
	PromoterEnabled, Enabled                 bool
	IncomeAccountID, PurchaseJournalID       int64
	TransactionID                            string
	ProofHash                                []byte
}

func membershipReferralReward(price, rate int64) (int64, error) {
	if price < 0 {
		return 0, ErrInvalid
	}
	return calculateReferralReward(price, rate)
}

func membershipReferralReason(s membershipReferralSource) (string, int64, error) {
	switch {
	case s.RelationID == 0:
		return "no_relation", 0, nil
	case !s.PromoterEnabled:
		return "promoter_disabled", 0, nil
	case !s.Enabled:
		return "disabled", 0, nil
	case s.Price == 0:
		return "zero_price", 0, nil
	case s.RateBPS == 0:
		return "zero_rate", 0, nil
	}
	r, e := membershipReferralReward(s.Price, s.RateBPS)
	if e != nil {
		return "", 0, e
	}
	if r == 0 {
		return "rounds_zero", 0, nil
	}
	return "eligible", r, nil
}

func membershipEffectiveGroup(ctx context.Context, q referralReader, mid int64, at time.Time) (int64, int64, bool, error) {
	var group, revision int64
	var enabled, unresolved bool
	e := q.QueryRow(ctx, "SELECT base_group_id,config_revision,enabled,group_baseline_unresolved FROM merchants WHERE id=$1", mid).Scan(&group, &revision, &enabled, &unresolved)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, 0, false, ErrNotFound
	}
	if e != nil {
		return 0, 0, false, e
	}
	if unresolved {
		return 0, 0, false, ErrState
	}
	var purchased int64
	e = q.QueryRow(ctx, "SELECT group_id FROM memberships WHERE merchant_id=$1 AND expires_at>$2", mid, at).Scan(&purchased)
	if e == nil {
		group = purchased
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return 0, 0, false, e
	}
	return group, revision, enabled, nil
}

func (s *Store) membershipReferralDecision(ctx context.Context, q referralReader, mid, group int64, externalNo string) (membershipReferralSource, error) {
	out := membershipReferralSource{MembershipReferralPurchaseSnapshot: MembershipReferralPurchaseSnapshot{MerchantID: mid, GroupID: group}, SourceVersion: 1}
	if q == nil || mid <= 0 || group <= 0 || externalNo == "" || len(externalNo) > 128 {
		return out, ErrInvalid
	}
	var at time.Time
	if e := q.QueryRow(ctx, "SELECT pg_catalog.clock_timestamp()").Scan(&at); e != nil {
		return out, e
	}
	var enabled bool
	var e error
	out.PayerGroupID, out.BuyerRevision, enabled, e = membershipEffectiveGroup(ctx, q, mid, at)
	if e != nil {
		return out, e
	}
	if !enabled {
		return out, ErrState
	}
	var priceRaw string
	if e = q.QueryRow(ctx, "SELECT COALESCE(body->>'price_cents','0') FROM backoffice_records WHERE resource='groups' AND id=$1 AND body->>'enabled'='true'", group).Scan(&priceRaw); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			e = ErrNotFound
		}
		return out, e
	}
	out.Price, e = strconv.ParseInt(priceRaw, 10, 64)
	if e != nil || out.Price < 0 {
		return out, ErrInvalid
	}
	relation, e := s.ReferralRelationTx(ctx, q, mid)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return out, e
	}
	if e == nil {
		out.PromoterID, out.RelationID, out.RelationHash = relation.PromoterID, relation.ID, relation.ProofHash
		out.PromoterGroupID, out.PromoterRevision, out.PromoterEnabled, e = membershipEffectiveGroup(ctx, q, relation.PromoterID, at)
		if e != nil {
			return out, e
		}
		for i, scope := range []struct {
			kind string
			id   int64
		}{{"global", 0}, {"group", out.PayerGroupID}, {"group", out.PromoterGroupID}} {
			if scope.kind == "group" && scope.id == 0 {
				continue
			}
			p, e := s.readMembershipReferralPolicy(ctx, q, scope.kind, scope.id)
			if errors.Is(e, ErrNotFound) {
				continue
			}
			if e != nil {
				return out, e
			}
			out.PolicyIDs[i], out.PolicyRevisions[i], out.PolicyHashes[i] = p.ID, p.Revision, hex.EncodeToString(p.ProofHash)
			if p.Enabled != nil {
				out.Enabled = *p.Enabled
			}
			if p.RateBPS != nil {
				out.RateBPS = *p.RateBPS
			}
		}
	}
	out.Reason, out.Reward, e = membershipReferralReason(out)
	if e != nil {
		return out, e
	}
	proof, e := s.membershipReferralDigest("purchase-decision", []any{mid, group, externalNo, out.PromoterID, out.RelationID, hex.EncodeToString(out.RelationHash), out.PayerGroupID, out.PromoterGroupID, out.PolicyIDs, out.PolicyRevisions, out.PolicyHashes, out.BuyerRevision, out.PromoterRevision, out.PromoterEnabled, out.Enabled, out.RateBPS, out.Price, out.Reward, out.Reason})
	if e == nil {
		out.ProofFingerprint = hex.EncodeToString(proof)
	}
	return out, e
}

func (s *Store) FundsMembershipReferralPurchaseSnapshot(ctx context.Context, q referralReader, mid, group int64, externalNo string) (MembershipReferralPurchaseSnapshot, error) {
	v, e := s.membershipReferralDecision(ctx, q, mid, group, externalNo)
	return v.MembershipReferralPurchaseSnapshot, e
}

// Read the immutable relation before taking the complete sorted merchant set;
// never take the buyer lock first and then discover an unlocked promoter.
func (s *Store) membershipLockParties(ctx context.Context, tx pgx.Tx, mid int64) error {
	r, e := s.ReferralRelationTx(ctx, tx, mid)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return e
	}
	ids := []int64{mid}
	if e == nil {
		ids = append(ids, r.PromoterID)
	}
	rows, e := tx.Query(ctx, "SELECT id FROM merchants WHERE id=ANY($1::bigint[]) ORDER BY id FOR UPDATE", ids)
	if e != nil {
		return e
	}
	n := 0
	for rows.Next() {
		n++
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if n != len(ids) {
		return ErrNotFound
	}
	after, e := s.ReferralRelationTx(ctx, tx, mid)
	if r.ID == 0 && errors.Is(e, ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	if after.ID != r.ID || after.PromoterID != r.PromoterID || !hmac.Equal(after.ProofHash, r.ProofHash) {
		return ErrConflict
	}
	return nil
}

func (s *Store) membershipReferralSourceProof(p MembershipPurchase, v membershipReferralSource) ([]byte, error) {
	return s.membershipReferralDigest("purchase-source", []any{p.ID, p.MerchantID, p.GroupID, p.ExternalNo, p.Price, p.GroupName, p.DurationDays, p.ExpiresAt.UTC().Format(time.RFC3339Nano), p.Actor, p.CreatedAt.UTC().Format(time.RFC3339Nano), p.SourceVersion, v.SourceVersion, v.PromoterID, v.RelationID, hex.EncodeToString(v.RelationHash), v.PayerGroupID, v.PromoterGroupID, v.PolicyIDs, v.PolicyRevisions, v.PolicyHashes, v.BuyerRevision, v.PromoterRevision, v.PromoterEnabled, v.Enabled, v.RateBPS, v.Reward, v.Reason, v.IncomeAccountID, v.PurchaseJournalID, v.TransactionID})
}

func (s *Store) membershipReferralEvent(ctx context.Context, tx pgx.Tx, p MembershipPurchase, operation string, subject []byte) error {
	key := fmt.Sprintf("membership-referral:%d:%s", p.ID, operation)
	proof, e := s.membershipReferralDigest("event", []any{p.ID, operation, key, p.Actor, hex.EncodeToString(subject)})
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO membership_referral_events(purchase_id,operation,event_key,actor,subject_hash,proof_hash) VALUES($1,$2,$3,$4,$5,$6)", p.ID, operation, key, p.Actor, subject, proof)
	return e
}

func (s *Store) membershipReferralRewardProof(purchase, promoter, amount, journal int64, source []byte) ([]byte, error) {
	return s.membershipReferralDigest("award", []any{purchase, promoter, amount, journal, hex.EncodeToString(source)})
}

func (s *Store) prepareMembershipReferralFunding(ctx context.Context, tx pgx.Tx, purchase int64, v membershipReferralSource) (membershipReferralSource, error) {
	// Writers use the same independent policy keys exclusively. Shared locks
	// also protect a genuinely missing head against a concurrent first policy.
	if v.RelationID > 0 {
		keys := []string{"goepay/membership-referral-policy/global/0"}
		for _, id := range []int64{v.PayerGroupID, v.PromoterGroupID} {
			if id > 0 {
				keys = append(keys, fmt.Sprintf("goepay/membership-referral-policy/group/%d", id))
			}
		}
		// The global key first is fixed; group locks are shared and compatible.
		for _, key := range keys {
			if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock_shared(hashtextextended($1,0))", key); e != nil {
				return v, e
			}
		}
	}
	ids := []int64{}
	if v.Price > 0 {
		var buyer int64
		if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", v.MerchantID).Scan(&buyer); e != nil {
			return v, e
		}
		if e := tx.QueryRow(ctx, "INSERT INTO accounts(owner_id,kind,scope_id) VALUES(0,'membership_income',$1) RETURNING id", purchase).Scan(&v.IncomeAccountID); e != nil {
			return v, e
		}
		ids = append(ids, buyer, v.IncomeAccountID)
	}
	if v.Reward > 0 {
		var promoter int64
		if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", v.PromoterID).Scan(&promoter); e != nil {
			return v, e
		}
		ids = append(ids, promoter)
	}
	return v, lockLedgerAccountSet(ctx, tx, ids)
}

func (s *Store) fundMembershipReferral(ctx context.Context, tx pgx.Tx, p MembershipPurchase, v membershipReferralSource) error {
	var buyerAccount, promoterAccount, awardJournal int64
	ids := []int64{}
	if p.Price > 0 {
		if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", p.MerchantID).Scan(&buyerAccount); e != nil {
			return e
		}
		if v.IncomeAccountID <= 0 {
			return ErrConflict
		}
		ids = append(ids, buyerAccount, v.IncomeAccountID)
	}
	if v.Reward > 0 {
		if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", v.PromoterID).Scan(&promoterAccount); e != nil {
			return e
		}
		ids = append(ids, promoterAccount)
	}
	if e := lockLedgerAccountSet(ctx, tx, ids); e != nil {
		return e
	}
	if p.Price > 0 {
		key := fmt.Sprintf("membership:%d", p.ID)
		if e := postByAccountIDs(ctx, tx, key, "original membership purchase income", buyerAccount, v.IncomeAccountID, p.Price); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&v.PurchaseJournalID); e != nil {
			return e
		}
	}
	if v.Reward > 0 {
		key := fmt.Sprintf("membership-referral:%d:award", p.ID)
		if e := postByAccountIDs(ctx, tx, key, "original membership purchase referral reward", v.IncomeAccountID, promoterAccount, v.Reward); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&awardJournal); e != nil {
			return e
		}
	}
	if e := tx.QueryRow(ctx, "SELECT pg_current_xact_id()::text").Scan(&v.TransactionID); e != nil {
		return e
	}
	var e error
	v.ProofHash, e = s.membershipReferralSourceProof(p, v)
	if e != nil {
		return e
	}
	pids, _ := json.Marshal(v.PolicyIDs)
	revisions, _ := json.Marshal(v.PolicyRevisions)
	hashes, _ := json.Marshal(v.PolicyHashes)
	_, e = tx.Exec(ctx, `INSERT INTO membership_referral_sources(purchase_id,source_version,merchant_id,promoter_id,relation_id,relation_hash,payer_group_id,promoter_group_id,policy_ids,policy_revisions,policy_hashes,buyer_revision,promoter_revision,promoter_enabled,enabled,rate_bps,price,reward_amount,reason,income_account_id,purchase_journal_id,transaction_id,proof_hash) VALUES($1,1,$2,NULLIF($3::bigint,0),NULLIF($4::bigint,0),$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,NULLIF($19::bigint,0),NULLIF($20::bigint,0),$21::xid8,$22)`, p.ID, p.MerchantID, v.PromoterID, v.RelationID, v.RelationHash, v.PayerGroupID, v.PromoterGroupID, pids, revisions, hashes, v.BuyerRevision, v.PromoterRevision, v.PromoterEnabled, v.Enabled, v.RateBPS, p.Price, v.Reward, v.Reason, v.IncomeAccountID, v.PurchaseJournalID, v.TransactionID, v.ProofHash)
	if e != nil {
		return e
	}
	if e = s.membershipReferralEvent(ctx, tx, p, "source", v.ProofHash); e != nil {
		return e
	}
	if v.Reward > 0 {
		proof, e := s.membershipReferralRewardProof(p.ID, v.PromoterID, v.Reward, awardJournal, v.ProofHash)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO membership_referral_rewards(purchase_id,promoter_id,amount,source_hash,journal_id,proof_hash) VALUES($1,$2,$3,$4,$5,$6)", p.ID, v.PromoterID, v.Reward, v.ProofHash, awardJournal, proof); e != nil {
			return e
		}
		return s.membershipReferralEvent(ctx, tx, p, "award", proof)
	}
	return nil
}
