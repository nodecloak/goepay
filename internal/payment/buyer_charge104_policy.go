package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

type buyerPolicyFacts104 struct {
	Version           int
	Kind              string
	OwnerID, Revision int64
	MerchantMode      int
	BuyerRate         *string
	Threshold, Cost   *int64
	ChangedAt         time.Time
}
type buyerPolicyRecord104 struct {
	Facts      buyerPolicyFacts104
	Proof      []byte
	Configured bool
}

func buyerPolicyValid104(f buyerPolicyFacts104) bool {
	if f.Version != 1 || f.Revision <= 0 || f.ChangedAt.IsZero() {
		return false
	}
	if f.Kind == "merchant" {
		return f.OwnerID > 0 && (f.MerchantMode == 0 || f.MerchantMode == 1) && (f.BuyerRate == nil || f.MerchantMode == 1 && validBuyerRate104(f.BuyerRate)) && f.Threshold == nil && f.Cost == nil
	}
	if f.Kind != "global" && f.Kind != "parent" || f.Kind == "global" && f.OwnerID != 0 || f.Kind == "parent" && f.OwnerID <= 0 || f.MerchantMode != 0 || f.BuyerRate != nil {
		return false
	}
	return (f.Threshold == nil || *f.Threshold >= 0) && (f.Cost == nil || *f.Cost >= 0) && (f.Kind != "global" || f.Threshold != nil && f.Cost != nil)
}
func decodeBuyerPolicy104(raw []byte) (buyerPolicyFacts104, error) {
	var f buyerPolicyFacts104
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || len(keys) != 9 {
		return f, ErrConflict
	}
	for _, key := range []string{"Version", "Kind", "OwnerID", "Revision", "MerchantMode", "BuyerRate", "Threshold", "Cost", "ChangedAt"} {
		if _, ok := keys[key]; !ok {
			return f, ErrConflict
		}
	}
	if json.Unmarshal(raw, &f) != nil || !buyerPolicyValid104(f) {
		return f, ErrConflict
	}
	f.ChangedAt = f.ChangedAt.UTC()
	return f, nil
}
func (s *Store) buyerPolicyProof104(f buyerPolicyFacts104) ([]byte, error) {
	return s.referralDigest("customer-charge104/policy", f)
}
func buyerPolicyCommitment104(proof []byte) string {
	sum := sha256.Sum256(proof)
	return hex.EncodeToString(sum[:])
}
func lockBuyerPolicy104(ctx context.Context, tx pgx.Tx, kind string, owner int64, write bool) error {
	if kind != "merchant" && kind != "global" && kind != "parent" || kind == "global" && owner != 0 || kind != "global" && owner <= 0 {
		return ErrInvalid
	}
	lock := "pg_advisory_xact_lock_shared"
	if write {
		lock = "pg_advisory_xact_lock"
	}
	_, e := tx.Exec(ctx, "SELECT "+lock+"(hashtextextended($1,0))", "goepay/customer-charge104/"+kind+"/"+strconv.FormatInt(owner, 10))
	return e
}
func (s *Store) buyerPolicyTx104(ctx context.Context, tx pgx.Tx, kind string, owner int64, write bool) (buyerPolicyRecord104, error) {
	var out buyerPolicyRecord104
	if e := lockBuyerPolicy104(ctx, tx, kind, owner, write); e != nil {
		return out, e
	}
	var raw, proof []byte
	var revision int64
	e := tx.QueryRow(ctx, `SELECT revision,facts,proof_hash FROM customer_charge_policies104 WHERE kind=$1 AND owner_id=$2`, kind, owner).Scan(&revision, &raw, &proof)
	found := e == nil
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	var count int64
	var latestRevision, commitment string
	if e = tx.QueryRow(ctx, `WITH entries AS MATERIALIZED(SELECT id,changes FROM backoffice_audit WHERE resource='customer-charge-policies104' AND resource_id=$1 AND changes->>'kind'=$2 AND operation='update') SELECT count(*),COALESCE((SELECT changes->>'revision' FROM entries ORDER BY id DESC LIMIT 1),'0'),COALESCE((SELECT changes->>'commitment' FROM entries ORDER BY id DESC LIMIT 1),'') FROM entries`, owner, kind).Scan(&count, &latestRevision, &commitment); e != nil {
		return out, e
	}
	var witnessCommitment string
	var witnessProof []byte
	var witnessCount, witnessRevision int64
	e = tx.QueryRow(ctx, `SELECT count(*),COALESCE(max(revision),0),COALESCE((SELECT commitment FROM customer_charge_policy_witnesses104 WHERE kind=$1 AND owner_id=$2 ORDER BY revision DESC LIMIT 1),''),(SELECT proof_hash FROM customer_charge_policy_witnesses104 WHERE kind=$1 AND owner_id=$2 ORDER BY revision DESC LIMIT 1) FROM customer_charge_policy_witnesses104 WHERE kind=$1 AND owner_id=$2`, kind, owner).Scan(&witnessCount, &witnessRevision, &witnessCommitment, &witnessProof)
	if e != nil {
		return out, e
	}
	if !found {
		if count != 0 || witnessCount != 0 {
			return out, ErrConflict
		}
		return out, nil
	}
	witnessExpected, e := s.referralDigest("customer-charge104/policy-revision-witness", []any{kind, owner, witnessRevision, witnessCommitment})
	if e != nil || witnessCount != revision || witnessRevision != revision || witnessCommitment != buyerPolicyCommitment104(proof) || !hmac.Equal(witnessExpected, witnessProof) {
		return out, ErrConflict
	}
	f, e := decodeBuyerPolicy104(raw)
	if e != nil {
		return out, e
	}
	expected, e := s.buyerPolicyProof104(f)
	if e != nil {
		return out, e
	}
	if f.Kind != kind || f.OwnerID != owner || f.Revision != revision || count != revision || latestRevision != strconv.FormatInt(revision, 10) || !hmac.Equal(expected, proof) || commitment != buyerPolicyCommitment104(proof) {
		return out, ErrConflict
	}
	return buyerPolicyRecord104{Facts: f, Proof: proof, Configured: true}, nil
}
func (s *Store) writeBuyerPolicy104(ctx context.Context, tx pgx.Tx, previous buyerPolicyRecord104, next buyerPolicyFacts104, actor MerchantRSAActor) (buyerPolicyRecord104, error) {
	var out buyerPolicyRecord104
	next.Version = 1
	next.Revision = previous.Facts.Revision + 1
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&next.ChangedAt); e != nil {
		return out, e
	}
	next.ChangedAt = next.ChangedAt.UTC()
	if !buyerPolicyValid104(next) {
		return out, ErrInvalid
	}
	raw, e := json.Marshal(next)
	if e != nil {
		return out, e
	}
	proof, e := s.buyerPolicyProof104(next)
	if e != nil {
		return out, e
	}
	var merchant any
	if next.Kind == "merchant" {
		merchant = next.OwnerID
	}
	if previous.Configured {
		tag, e := tx.Exec(ctx, `UPDATE customer_charge_policies104 SET revision=$3,facts=$4,proof_hash=$5 WHERE kind=$1 AND owner_id=$2 AND revision=$6`, next.Kind, next.OwnerID, next.Revision, raw, proof, previous.Facts.Revision)
		if e != nil {
			return out, e
		}
		if tag.RowsAffected() != 1 {
			return out, ErrConflict
		}
	} else if _, e = tx.Exec(ctx, `INSERT INTO customer_charge_policies104(kind,owner_id,merchant_id,revision,facts,proof_hash) VALUES($1,$2,$3,$4,$5,$6)`, next.Kind, next.OwnerID, merchant, next.Revision, raw, proof); e != nil {
		return out, e
	}
	commitment := buyerPolicyCommitment104(proof)
	witness, e := s.referralDigest("customer-charge104/policy-revision-witness", []any{next.Kind, next.OwnerID, next.Revision, commitment})
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO customer_charge_policy_witnesses104(kind,owner_id,revision,commitment,proof_hash)VALUES($1,$2,$3,$4,$5)`, next.Kind, next.OwnerID, next.Revision, commitment, witness); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'customer-charge-policies104',$4,'update',jsonb_build_object('kind',$5::text,'revision',$6::bigint::text,'commitment',$7::text))`, actor.User.Role, actor.User.ID, actor.User.Username, next.OwnerID, next.Kind, next.Revision, buyerPolicyCommitment104(proof)); e != nil {
		return out, e
	}
	return buyerPolicyRecord104{Facts: next, Proof: proof, Configured: true}, nil
}

type MerchantBuyerFeeSettings104 struct {
	MerchantID       int64  `json:"merchant_id,string"`
	Mode             int    `json:"mode"`
	Rate             string `json:"mode_feerate"`
	Configured       bool   `json:"configured"`
	Revision         string `json:"revision"`
	Fingerprint      string `json:"fingerprint"`
	CanUpdate        bool   `json:"can_update"`
	PartialAvailable bool   `json:"partial_available"`
}

func buyerRateText104(rate *string) string {
	if rate == nil {
		return ""
	}
	return *rate
}
func (s *Store) merchantBuyerFeeState104(ctx context.Context, tx pgx.Tx, a MerchantRSAActor, write bool) (MerchantBuyerFeeSettings104, buyerPolicyRecord104, error) {
	var out MerchantBuyerFeeSettings104
	var record buyerPolicyRecord104
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	var revision int64
	var enabled bool
	e := tx.QueryRow(ctx, `SELECT id,config_revision,enabled FROM merchants WHERE id=$1`+lock, a.User.ID).Scan(&out.MerchantID, &revision, &enabled)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, record, identity.ErrOriginalMerchantSession
	}
	if e != nil {
		return out, record, e
	}
	if !enabled || revision <= 0 {
		return out, record, identity.ErrOriginalMerchantSession
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return out, record, e
	}
	record, e = s.buyerPolicyTx104(ctx, tx, "merchant", a.User.ID, write)
	if e != nil {
		return out, record, e
	}
	out.Mode = record.Facts.MerchantMode
	out.Rate = buyerRateText104(record.Facts.BuyerRate)
	out.Configured = record.Configured
	out.Revision = strconv.FormatInt(record.Facts.Revision, 10)
	out.CanUpdate = true
	out.PartialAvailable = true
	digest, e := s.referralDigest("customer-charge104/merchant-cas", []any{out.MerchantID, revision, enabled, record.Configured, record.Facts, record.Proof})
	if e != nil {
		return out, record, e
	}
	out.Fingerprint = hex.EncodeToString(digest)
	return out, record, nil
}
func (s *Store) MerchantBuyerFeeSettings104(ctx context.Context, a MerchantRSAActor) (MerchantBuyerFeeSettings104, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return MerchantBuyerFeeSettings104{}, e
	}
	defer tx.Rollback(ctx)
	out, _, e := s.merchantBuyerFeeState104(ctx, tx, a, false)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) UpdateMerchantBuyerFeeSettings104(ctx context.Context, a MerchantRSAActor, mode int, rate, expected string) (MerchantBuyerFeeSettings104, error) {
	var empty MerchantBuyerFeeSettings104
	if (mode != 0 && mode != 1) || rate != strings.TrimSpace(rate) || mode == 0 && rate != "" || len(expected) != 64 {
		return empty, ErrInvalid
	}
	var bps *string
	if rate != "" {
		v, _, _, e := parseBuyerRate104(rate)
		if e != nil {
			return empty, ErrInvalid
		}
		bps = &v
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	state, record, e := s.merchantBuyerFeeState104(ctx, tx, a, true)
	if e != nil {
		return empty, e
	}
	if !hmac.Equal([]byte(state.Fingerprint), []byte(expected)) {
		return empty, ErrConflict
	}
	_, e = s.writeBuyerPolicy104(ctx, tx, record, buyerPolicyFacts104{Kind: "merchant", OwnerID: a.User.ID, MerchantMode: mode, BuyerRate: bps}, a)
	if e != nil {
		return empty, e
	}
	if _, e = tx.Exec(ctx, `UPDATE merchants SET config_revision=config_revision+1 WHERE id=$1`, a.User.ID); e != nil {
		return empty, e
	}
	state, _, e = s.merchantBuyerFeeState104(ctx, tx, a, true)
	if e != nil {
		return empty, e
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return empty, e
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, e
	}
	return state, nil
}
