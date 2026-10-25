package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"math"
	"regexp"
	"time"
)

type ReferralRelation struct {
	ID           int64     `json:"id,string"`
	InvitedID    int64     `json:"invited_id,string"`
	PromoterID   int64     `json:"promoter_id,string"`
	TokenID      int64     `json:"token_id,string"`
	TokenVersion int64     `json:"token_version,string"`
	ChallengeID  string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	ProofHash    []byte    `json:"-"`
}
type ReferralTokenView struct {
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	ID         int64      `json:"id,string"`
	MerchantID int64      `json:"merchant_id,string"`
	Version    int64      `json:"version,string"`
	Enabled    bool       `json:"enabled"`
	Masked     string     `json:"masked"`
	CreatedAt  time.Time  `json:"created_at"`
}
type IssuedReferralToken struct {
	ReferralTokenView
	Token string `json:"token,omitempty"`
}

var referralTokenPattern = regexp.MustCompile(`^rp_[A-Za-z0-9_-]{43}$`)

func referralOwnerValid(mid int64, actor TransferBatchActor) bool {
	return actor.Role == "merchant" && actor.ID == mid && actor.valid(mid)
}
func (s *Store) referralRelationDigest(purpose string, v any) ([]byte, error) {
	raw, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	m := hmac.New(sha256.New, s.indexKey)
	m.Write([]byte("goepay/referral/v1/" + purpose + "\x00"))
	m.Write(raw)
	return m.Sum(nil), nil
}

type referralTokenRecord struct {
	ReferralTokenView
	Hash, Cipher, Proof []byte
	ActorName           string
}

const referralTokenColumns = "t.id,t.merchant_id,t.version,t.enabled,t.token_masked,t.created_at,t.expires_at,t.token_hash,t.token_cipher,t.actor_name,t.proof_hash"

func referralTokenAAD(r referralTokenRecord) []byte {
	raw, _ := json.Marshal([]any{r.ID, r.MerchantID, r.Version})
	return raw
}
func referralTokenExpiry(r referralTokenRecord) string {
	if r.ExpiresAt == nil {
		return ""
	}
	return r.ExpiresAt.UTC().Format(time.RFC3339Nano)
}
func (s *Store) referralTokenFingerprint(r referralTokenRecord) ([]byte, error) {
	return s.referralRelationDigest("token-record", []any{r.ID, r.MerchantID, r.Version, r.Enabled, hex.EncodeToString(r.Hash), hex.EncodeToString(r.Cipher), r.Masked, r.ActorName, r.CreatedAt.UTC().Format(time.RFC3339Nano), referralTokenExpiry(r)})
}
func (s *Store) scanReferralToken(row pgx.Row) (referralTokenRecord, error) {
	var r referralTokenRecord
	e := row.Scan(&r.ID, &r.MerchantID, &r.Version, &r.Enabled, &r.Masked, &r.CreatedAt, &r.ExpiresAt, &r.Hash, &r.Cipher, &r.ActorName, &r.Proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	proof, e := s.referralTokenFingerprint(r)
	if e != nil || !hmac.Equal(proof, r.Proof) {
		return r, ErrConflict
	}
	if r.Enabled {
		if r.ExpiresAt == nil || !r.ExpiresAt.After(r.CreatedAt) {
			return r, ErrConflict
		}
		n := s.aead.NonceSize()
		if len(r.Cipher) < n {
			return r, ErrConflict
		}
		raw, e := s.aead.Open(nil, r.Cipher[:n], r.Cipher[n:], referralTokenAAD(r))
		if e != nil {
			return r, ErrConflict
		}
		token := string(raw)
		hash, e := s.referralRelationDigest("token-index", token)
		if e != nil || !referralTokenPattern.MatchString(token) || !hmac.Equal(hash, r.Hash) || r.Masked != "rp_****"+token[len(token)-4:] {
			return r, ErrConflict
		}
	} else if len(r.Hash) != 0 || len(r.Cipher) != 0 || r.Masked != "" || r.ExpiresAt != nil {
		return r, ErrConflict
	}
	return r, nil
}
func (s *Store) RotateReferralToken(ctx context.Context, mid, expected int64, actor TransferBatchActor) (IssuedReferralToken, error) {
	return s.changeReferralToken(ctx, mid, expected, actor, true)
}
func (s *Store) RevokeReferralToken(ctx context.Context, mid, expected int64, actor TransferBatchActor) (ReferralTokenView, error) {
	r, e := s.changeReferralToken(ctx, mid, expected, actor, false)
	return r.ReferralTokenView, e
}
func (s *Store) changeReferralToken(ctx context.Context, mid, expected int64, actor TransferBatchActor, enabled bool, lifetime ...time.Duration) (IssuedReferralToken, error) {
	var out IssuedReferralToken
	ttl := 90 * 24 * time.Hour
	if len(lifetime) > 1 || !enabled && len(lifetime) > 0 {
		return out, ErrInvalid
	}
	if len(lifetime) == 1 {
		ttl = lifetime[0]
	}
	if enabled && (ttl <= 0 || ttl > 365*24*time.Hour) {
		return out, ErrInvalid
	}
	if !referralOwnerValid(mid, actor) || expected < 0 || expected == math.MaxInt64 || s.aead == nil {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,56))", "referral-token-owner:"+fmt.Sprint(mid)); e != nil {
		return out, e
	}
	var merchantEnabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR SHARE", mid).Scan(&merchantEnabled); errors.Is(e, pgx.ErrNoRows) {
		return out, ErrNotFound
	} else if e != nil {
		return out, e
	}
	if !merchantEnabled {
		return out, ErrState
	}
	var current int64
	e = tx.QueryRow(ctx, "SELECT version FROM referral_token_current WHERE merchant_id=$1 FOR UPDATE", mid).Scan(&current)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if current != expected {
		return out, ErrConflict
	}
	if current > 0 {
		if _, e = s.scanReferralToken(tx.QueryRow(ctx, "SELECT "+referralTokenColumns+" FROM referral_tokens t JOIN referral_token_current c ON c.token_id=t.id AND c.merchant_id=t.merchant_id AND c.version=t.version WHERE c.merchant_id=$1", mid)); e != nil {
			return out, e
		}
	}
	r := referralTokenRecord{ReferralTokenView: ReferralTokenView{MerchantID: mid, Version: current + 1, Enabled: enabled}, ActorName: actor.Name}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('referral_tokens','id')),transaction_timestamp()").Scan(&r.ID, &r.CreatedAt); e != nil {
		return out, e
	}
	if enabled {
		expiry := r.CreatedAt.Add(ttl)
		r.ExpiresAt = &expiry
		entropy := make([]byte, 32)
		if _, e = rand.Read(entropy); e != nil {
			return out, e
		}
		out.Token = "rp_" + base64.RawURLEncoding.EncodeToString(entropy)
		r.Hash, e = s.referralRelationDigest("token-index", out.Token)
		if e != nil {
			return out, e
		}
		r.Masked = "rp_****" + out.Token[len(out.Token)-4:]
		nonce := make([]byte, s.aead.NonceSize())
		if _, e = rand.Read(nonce); e != nil {
			return out, e
		}
		r.Cipher = append(nonce, s.aead.Seal(nil, nonce, []byte(out.Token), referralTokenAAD(r))...)
	}
	r.Proof, e = s.referralTokenFingerprint(r)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO referral_tokens(id,merchant_id,version,enabled,token_hash,token_cipher,token_masked,actor_name,proof_hash,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", r.ID, mid, r.Version, enabled, r.Hash, r.Cipher, r.Masked, actor.Name, r.Proof, r.CreatedAt, r.ExpiresAt); e != nil {
		return IssuedReferralToken{}, e
	}
	if current == 0 {
		_, e = tx.Exec(ctx, "INSERT INTO referral_token_current(merchant_id,token_id,version) VALUES($1,$2,$3)", mid, r.ID, r.Version)
	} else {
		var tag pgconn.CommandTag
		tag, e = tx.Exec(ctx, "UPDATE referral_token_current SET token_id=$2,version=$3 WHERE merchant_id=$1 AND version=$4", mid, r.ID, r.Version, current)
		if e == nil && tag.RowsAffected() != 1 {
			e = ErrConflict
		}
	}
	if e != nil {
		return IssuedReferralToken{}, e
	}
	op := "rotate"
	if !enabled {
		op = "revoke"
	} else if current == 0 {
		op = "issue"
	}
	audit, _ := json.Marshal(map[string]any{"merchant_id": fmt.Sprint(mid), "version": fmt.Sprint(r.Version), "enabled": enabled, "masked": r.Masked})
	if _, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,$2,'referral-tokens',$3,$4,$5)", mid, actor.Name, r.ID, op, audit); e != nil {
		return IssuedReferralToken{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return IssuedReferralToken{}, e
	}
	out.ReferralTokenView = r.ReferralTokenView
	return out, nil
}
func (s *Store) ReferralToken(ctx context.Context, mid int64, actor TransferBatchActor) (ReferralTokenView, error) {
	if !referralOwnerValid(mid, actor) {
		return ReferralTokenView{}, ErrInvalid
	}
	r, e := s.scanReferralToken(s.pool.QueryRow(ctx, "SELECT "+referralTokenColumns+" FROM referral_tokens t JOIN referral_token_current c ON c.token_id=t.id AND c.merchant_id=t.merchant_id AND c.version=t.version WHERE c.merchant_id=$1", mid))
	return r.ReferralTokenView, e
}
func (s *Store) referralRelationFingerprint(r ReferralRelation, tokenProof []byte) ([]byte, error) {
	return s.referralRelationDigest("relation", []any{r.ID, r.InvitedID, r.PromoterID, r.TokenID, r.TokenVersion, r.ChallengeID, r.CreatedAt.UTC().Format(time.RFC3339Nano), hex.EncodeToString(tokenProof)})
}
func (s *Store) ReferralRelationTx(ctx context.Context, tx referralReader, invitedID int64) (ReferralRelation, error) {
	var r ReferralRelation
	var valid bool
	e := tx.QueryRow(ctx, "SELECT r.id,r.invited_id,r.promoter_id,r.token_id,r.token_version,r.challenge_id,r.created_at,r.proof_hash,COALESCE(v.valid,false) FROM referral_relations r LEFT JOIN referral_relation_evidence v ON v.id=r.id WHERE r.invited_id=$1", invitedID).Scan(&r.ID, &r.InvitedID, &r.PromoterID, &r.TokenID, &r.TokenVersion, &r.ChallengeID, &r.CreatedAt, &r.ProofHash, &valid)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	if !valid {
		return r, ErrConflict
	}
	token, e := s.scanReferralToken(tx.QueryRow(ctx, "SELECT "+referralTokenColumns+" FROM referral_tokens t WHERE t.id=$1", r.TokenID))
	if e != nil {
		return r, e
	}
	proof, e := s.referralRelationFingerprint(r, token.Proof)
	if e != nil || !hmac.Equal(proof, r.ProofHash) || token.MerchantID != r.PromoterID || token.Version != r.TokenVersion || !token.Enabled {
		return r, ErrConflict
	}
	return r, nil
}
func (s *Store) BindReferralAtRegistrationTx(ctx context.Context, tx pgx.Tx, invitedID int64, promotionToken, challengeID string) (ReferralRelation, error) {
	var r ReferralRelation
	if invitedID <= 0 || !referralTokenPattern.MatchString(promotionToken) || len(challengeID) != 43 {
		return r, ErrInvalid
	}
	// A late caller does not acquire any existing merchant lock before graph
	// serialization. Valid callers own an uncommitted newborn merchant row.
	var newborn bool
	if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM merchants WHERE id=$1 AND enabled AND created_at=transaction_timestamp())", invitedID).Scan(&newborn); e != nil {
		return r, e
	}
	if !newborn {
		return r, ErrState
	}
	if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(74973256)"); e != nil {
		return r, e
	}
	var exists bool
	if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM referral_relations WHERE invited_id=$1)", invitedID).Scan(&exists); e != nil {
		return r, e
	}
	if exists {
		return r, ErrConflict
	}
	index, e := s.referralRelationDigest("token-index", promotionToken)
	if e != nil {
		return r, e
	}
	token, e := s.scanReferralToken(tx.QueryRow(ctx, "SELECT "+referralTokenColumns+" FROM referral_tokens t JOIN referral_token_current c ON c.token_id=t.id AND c.merchant_id=t.merchant_id AND c.version=t.version WHERE t.token_hash=$1 AND t.enabled AND t.expires_at>clock_timestamp() FOR SHARE OF c", index))
	if errors.Is(e, ErrNotFound) {
		return r, ErrInvalid
	}
	if e != nil {
		return r, e
	}
	if token.MerchantID == invitedID {
		return r, ErrInvalid
	}
	var promoterEnabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR SHARE", token.MerchantID).Scan(&promoterEnabled); e != nil {
		return r, e
	}
	if !promoterEnabled {
		return r, ErrInvalid
	}
	var unexpired bool
	if e = tx.QueryRow(ctx, "SELECT $1::timestamptz>clock_timestamp()", token.ExpiresAt).Scan(&unexpired); e != nil {
		return r, e
	}
	if !unexpired {
		return r, ErrInvalid
	}
	r = ReferralRelation{InvitedID: invitedID, PromoterID: token.MerchantID, TokenID: token.ID, TokenVersion: token.Version, ChallengeID: challengeID}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('referral_relations','id')),transaction_timestamp()").Scan(&r.ID, &r.CreatedAt); e != nil {
		return r, e
	}
	r.ProofHash, e = s.referralRelationFingerprint(r, token.Proof)
	if e != nil {
		return r, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO referral_relations(id,invited_id,promoter_id,token_id,token_version,challenge_id,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", r.ID, r.InvitedID, r.PromoterID, r.TokenID, r.TokenVersion, challengeID, r.ProofHash, r.CreatedAt); e != nil {
		return r, e
	}
	audit, _ := json.Marshal(map[string]string{"invited_id": fmt.Sprint(invitedID), "promoter_id": fmt.Sprint(r.PromoterID), "token_version": fmt.Sprint(r.TokenVersion)})
	if _, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('public',$1,'','referral-relations',$2,'bind',$3)", invitedID, r.ID, audit); e != nil {
		return r, e
	}
	// The complete enrollment proof must already be consumed in this transaction.
	return s.ReferralRelationTx(ctx, tx, invitedID)
}

// ReferralInvitees is scoped to the authenticated promoter. It returns IDs and
// immutable registration times, never OTP evidence, bearer tokens or email.
func (s *Store) ReferralInvitees(ctx context.Context, promoterID, afterID int64, limit int, actor TransferBatchActor) ([]ReferralRelation, error) {
	if !referralOwnerValid(promoterID, actor) || afterID < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var ids []int64
	if e = tx.QueryRow(ctx, "SELECT COALESCE(array_agg(invited_id ORDER BY id),'{}'::bigint[]) FROM (SELECT id,invited_id FROM referral_relations WHERE promoter_id=$1 AND id>$2 ORDER BY id LIMIT $3) r", promoterID, afterID, limit).Scan(&ids); e != nil {
		return nil, e
	}
	out := make([]ReferralRelation, 0, len(ids))
	for _, id := range ids {
		r, e := s.ReferralRelationTx(ctx, tx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, tx.Commit(ctx)
}
