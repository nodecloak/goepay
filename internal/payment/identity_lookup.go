package payment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	provider "goepay/internal/adapters/payment"
	"strings"
	"time"
)

var ErrIdentityLookup = errors.New("identity lookup is unavailable or no longer authorized")

type IdentityLookupBinding struct {
	MerchantID   int64  `json:"merchant_id,string"`
	ChannelID    int64  `json:"channel_id,string"`
	Provider     string `json:"provider"`
	AppID        string `json:"app_id"`
	Scope        string `json:"scope"`
	AccountScope string `json:"-"`
}
type IdentityLookup struct {
	IdentityLookupBinding
	ID                         string    `json:"request_id"`
	Status                     string    `json:"status"`
	ExpiresAt                  time.Time `json:"expires_at"`
	AuthorizationToken         string    `json:"-"`
	sessionHash                []byte
	actorUsername, actorRole   string
	actorID, actorVersion      int64
	browserHash, subjectCipher []byte
	accountHash                []byte
}

const identityLookupColumns = `id,merchant_id,channel_id,provider,app_id,scope,status,expires_at,session_hash,actor_username,actor_role,actor_id,actor_auth_version,browser_hash,subject_cipher,account_scope_hash`

func scanIdentityLookup(row pgx.Row) (IdentityLookup, error) {
	var q IdentityLookup
	e := row.Scan(&q.ID, &q.MerchantID, &q.ChannelID, &q.Provider, &q.AppID, &q.Scope, &q.Status, &q.ExpiresAt, &q.sessionHash, &q.actorUsername, &q.actorRole, &q.actorID, &q.actorVersion, &q.browserHash, &q.subjectCipher, &q.accountHash)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrIdentityLookup
	}
	return q, e
}
func lookupNonce() (string, error) {
	b := make([]byte, 32)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func lookupSessionHash(token string) []byte { h := sha256.Sum256([]byte(token)); return h[:] }
func validIdentityLookupBinding(b IdentityLookupBinding) bool {
	return b.MerchantID > 0 && b.ChannelID > 0 && b.AppID != "" && len(b.AppID) <= 128 && b.AccountScope != "" && len(b.AccountScope) <= 2048 && strings.TrimSpace(b.AppID) == b.AppID && !strings.ContainsAny(b.AppID, "\r\n\x00") && (b.Provider == "wxpay" && b.Scope == "snsapi_base" || b.Provider == "alipay" && b.Scope == "auth_base")
}
func validIdentityCode(code string) bool {
	if code == "" || len(code) > 4096 {
		return false
	}
	for _, c := range code {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.~", c)) {
			return false
		}
	}
	return true
}

// Durable session verification deliberately ignores caller-supplied User fields.
func lookupSession(ctx context.Context, tx pgx.Tx, q *IdentityLookup) error {
	var username, role, class string
	var id, version int64
	var expiry time.Time
	e := tx.QueryRow(ctx, `SELECT u.username,u.role,u.subject_id,u.auth_version,u.admin_class,ss.expires_at FROM console_sessions ss JOIN console_users u ON u.username=ss.username AND u.role=ss.role AND u.subject_id=ss.subject_id AND u.auth_version=ss.auth_version JOIN merchants m ON m.id=$2 WHERE ss.token_hash=$1 AND ss.expires_at>clock_timestamp() AND u.enabled AND m.enabled AND NOT EXISTS(SELECT 1 FROM console_session_issuers i WHERE i.token_hash=ss.token_hash) FOR SHARE OF ss,u,m`, q.sessionHash, q.MerchantID).Scan(&username, &role, &id, &version, &class, &expiry)
	if e != nil || role == "merchant" && id != q.MerchantID || role == "admin" && class != "operator" && class != "super" || role != "merchant" && role != "admin" {
		return ErrIdentityLookup
	}
	if q.actorUsername != "" && (q.actorUsername != username || q.actorRole != role || q.actorID != id || q.actorVersion != version) {
		return ErrIdentityLookup
	}
	q.actorUsername, q.actorRole, q.actorID, q.actorVersion = username, role, id, version
	if q.ExpiresAt.IsZero() || expiry.Before(q.ExpiresAt) {
		q.ExpiresAt = expiry
	}
	return nil
}
func lookupEvent(ctx context.Context, tx pgx.Tx, id, operation string) error {
	_, e := tx.Exec(ctx, `INSERT INTO identity_lookup_events(request_id,operation) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, operation)
	return e
}
func (s *Store) CreateIdentityLookup(ctx context.Context, token, role string, b IdentityLookupBinding) (IdentityLookup, error) {
	q := IdentityLookup{IdentityLookupBinding: b, sessionHash: lookupSessionHash(token), Status: "pending"}
	if len(token) != 43 || !validIdentityLookupBinding(b) || s.aead == nil || len(s.indexKey) < 32 {
		return q, ErrIdentityLookup
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return q, e
	}
	defer tx.Rollback(ctx)
	if e = lookupSession(ctx, tx, &q); e != nil || role != q.actorRole {
		return q, ErrIdentityLookup
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,47))`, hex.EncodeToString(q.sessionHash)); e != nil {
		return q, e
	}
	var n int
	if e = tx.QueryRow(ctx, `SELECT(SELECT count(*) FROM identity_lookup_requests WHERE session_hash=$1 AND expires_at>clock_timestamp() AND status IN ('pending','processing','verified'))+(SELECT count(*) FROM profile_identity_lookup_requests WHERE session_hash=$1 AND expires_at>clock_timestamp() AND status IN ('pending','processing','verified'))`, q.sessionHash).Scan(&n); e != nil {
		return q, e
	}
	if n >= 5 {
		return q, ErrIdentityLookup
	}
	q.ID, e = lookupNonce()
	if e != nil {
		return q, e
	}
	q.AuthorizationToken, e = lookupNonce()
	if e != nil {
		return q, e
	}
	q.accountHash = s.oauthHash("identity-account", b.AccountScope)
	e = tx.QueryRow(ctx, `INSERT INTO identity_lookup_requests(id,authorization_hash,session_hash,actor_username,actor_role,actor_id,actor_auth_version,merchant_id,channel_id,provider,app_id,scope,account_scope_hash,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,LEAST($14,statement_timestamp()+interval '5 minutes')) RETURNING expires_at`, q.ID, s.oauthHash("identity-authorization", q.AuthorizationToken), q.sessionHash, q.actorUsername, q.actorRole, q.actorID, q.actorVersion, b.MerchantID, b.ChannelID, b.Provider, b.AppID, b.Scope, q.accountHash, q.ExpiresAt).Scan(&q.ExpiresAt)
	if e != nil {
		return q, e
	}
	if e = lookupEvent(ctx, tx, q.ID, "created"); e != nil {
		return q, e
	}
	return q, tx.Commit(ctx)
}
func lookupAlive(ctx context.Context, tx pgx.Tx, q *IdentityLookup) error {
	if e := lookupSession(ctx, tx, q); e != nil {
		return e
	}
	var alive bool
	if e := tx.QueryRow(ctx, `SELECT expires_at>clock_timestamp() FROM identity_lookup_requests WHERE id=$1`, q.ID).Scan(&alive); e != nil {
		return e
	}
	if !alive {
		return ErrIdentityLookup
	}
	return nil
}
func (s *Store) StartIdentityLookup(ctx context.Context, token, browser string) (IdentityLookup, string, error) {
	var q IdentityLookup
	if !validOAuthBrowser(token) || !validOAuthBrowser(browser) {
		return q, "", ErrIdentityLookup
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return q, "", e
	}
	defer tx.Rollback(ctx)
	q, e = scanIdentityLookup(tx.QueryRow(ctx, "SELECT "+identityLookupColumns+" FROM identity_lookup_requests WHERE authorization_hash=$1 FOR UPDATE", s.oauthHash("identity-authorization", token)))
	if e != nil || q.Status != "pending" || q.browserHash != nil {
		return q, "", ErrIdentityLookup
	}
	if e = lookupAlive(ctx, tx, &q); e != nil {
		return q, "", e
	}
	state, e := lookupNonce()
	if e != nil {
		return q, "", e
	}
	hash := s.oauthHash("identity-browser", browser)
	if _, e = tx.Exec(ctx, `UPDATE identity_lookup_requests SET browser_hash=$2 WHERE id=$1`, q.ID, hash); e != nil {
		return q, "", e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO identity_lookup_states(state_hash,request_id,browser_hash,expires_at) VALUES($1,$2,$3,$4)`, s.oauthHash("identity-state", state), q.ID, hash, q.ExpiresAt); e != nil {
		return q, "", e
	}
	if e = lookupEvent(ctx, tx, q.ID, "authorized"); e != nil {
		return q, "", e
	}
	if e = lookupAlive(ctx, tx, &q); e != nil {
		return q, "", e
	}
	return q, state, tx.Commit(ctx)
}
func (s *Store) ConsumeIdentityLookup(ctx context.Context, id, p string, channel int64, state, browser, code string) (IdentityLookup, error) {
	var q IdentityLookup
	if !validOAuthBrowser(id) || !validOAuthBrowser(state) || !validOAuthBrowser(browser) || !validIdentityCode(code) {
		return q, ErrIdentityLookup
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return q, e
	}
	defer tx.Rollback(ctx)
	q, e = scanIdentityLookup(tx.QueryRow(ctx, "SELECT "+identityLookupColumns+" FROM identity_lookup_requests WHERE id=$1 FOR UPDATE", id))
	if e != nil || q.Provider != p || q.ChannelID != channel || q.Status != "pending" || subtle.ConstantTimeCompare(q.browserHash, s.oauthHash("identity-browser", browser)) != 1 {
		return q, ErrIdentityLookup
	}
	tag, e := tx.Exec(ctx, `UPDATE identity_lookup_states SET consumed_at=clock_timestamp() WHERE request_id=$1 AND state_hash=$2 AND browser_hash=$3 AND consumed_at IS NULL AND expires_at>clock_timestamp()`, id, s.oauthHash("identity-state", state), q.browserHash)
	if e != nil {
		return q, e
	}
	if tag.RowsAffected() != 1 {
		return q, ErrIdentityLookup
	}
	claimed, e := s.claimOAuthAuthorizationCode(ctx, tx, q.Provider, q.AppID, "web", code, s.oauthHash("identity-state", state))
	if e != nil {
		return q, e
	}
	accepted := claimed && lookupAlive(ctx, tx, &q) == nil
	status := "processing"
	if !accepted {
		status = "failed"
	}
	if _, e = tx.Exec(ctx, `UPDATE identity_lookup_requests SET status=$2 WHERE id=$1`, id, status); e != nil {
		return q, e
	}
	if e = lookupEvent(ctx, tx, id, status); e != nil {
		return q, e
	}
	if e = tx.Commit(ctx); e != nil {
		return q, e
	}
	q.Status = status
	if !accepted {
		return q, ErrIdentityLookup
	}
	return q, nil
}
func (q IdentityLookup) aad() []byte {
	return []byte(fmt.Sprintf("goepay/identity-lookup/v1/%s/%d/%s/%s/%x", q.ID, q.MerchantID, q.Provider, q.AppID, q.accountHash))
}

func (s *Store) IdentityLookupAccountMatches(q IdentityLookup, scope string) bool {
	return scope != "" && subtle.ConstantTimeCompare(q.accountHash, s.oauthHash("identity-account", scope)) == 1
}
func (s *Store) AuthorizeIdentityLookup(ctx context.Context, token, role string, merchant int64) error {
	if len(token) != 43 || merchant <= 0 {
		return ErrIdentityLookup
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := IdentityLookup{IdentityLookupBinding: IdentityLookupBinding{MerchantID: merchant}, sessionHash: lookupSessionHash(token)}
	if e = lookupSession(ctx, tx, &q); e != nil || q.actorRole != role {
		return ErrIdentityLookup
	}
	return tx.Commit(ctx)
}
func (s *Store) CompleteIdentityLookup(ctx context.Context, id string, identity provider.OAuthIdentity) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q, e := scanIdentityLookup(tx.QueryRow(ctx, "SELECT "+identityLookupColumns+" FROM identity_lookup_requests WHERE id=$1 FOR UPDATE", id))
	if e != nil {
		return e
	}
	if q.Status != "processing" || lookupAlive(ctx, tx, &q) != nil || identity.Provider != q.Provider || identity.AppID != q.AppID || !validIdentityCode(identity.SubjectID) || len(identity.SubjectID) > 128 {
		return ErrIdentityLookup
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	cipher := s.aead.Seal(nonce, nonce, []byte(identity.SubjectID), q.aad())
	if _, e = tx.Exec(ctx, `UPDATE identity_lookup_requests SET status='verified',subject_cipher=$2,completed_at=clock_timestamp() WHERE id=$1`, id, cipher); e != nil {
		return e
	}
	if e = lookupEvent(ctx, tx, id, "verified"); e != nil {
		return e
	}
	if e = lookupAlive(ctx, tx, &q); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) FailIdentityLookup(ctx context.Context, id string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE identity_lookup_requests SET status='failed',completed_at=clock_timestamp() WHERE id=$1 AND status IN ('pending','processing')`, id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 1 {
		if e = lookupEvent(ctx, tx, id, "failed"); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) readIdentityLookup(ctx context.Context, tx pgx.Tx, id, token, role string) (IdentityLookup, error) {
	var q IdentityLookup
	if !validOAuthBrowser(id) || len(token) != 43 {
		return q, ErrIdentityLookup
	}
	q, e := scanIdentityLookup(tx.QueryRow(ctx, "SELECT "+identityLookupColumns+" FROM identity_lookup_requests WHERE id=$1 AND session_hash=$2 AND actor_role=$3 FOR UPDATE", id, lookupSessionHash(token), role))
	if e != nil {
		return q, e
	}
	return q, lookupAlive(ctx, tx, &q)
}
func (s *Store) ReadIdentityLookup(ctx context.Context, id, token, role string) (IdentityLookup, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return IdentityLookup{}, e
	}
	defer tx.Rollback(ctx)
	q, e := s.readIdentityLookup(ctx, tx, id, token, role)
	if e != nil {
		return q, e
	}
	return q, tx.Commit(ctx)
}
func (s *Store) RevealIdentityLookup(ctx context.Context, id, token, role string) (provider.OAuthIdentity, error) {
	var identity provider.OAuthIdentity
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return identity, e
	}
	defer tx.Rollback(ctx)
	q, e := s.readIdentityLookup(ctx, tx, id, token, role)
	if e != nil {
		return identity, e
	}
	if q.Status != "verified" || s.aead == nil || len(q.subjectCipher) < s.aead.NonceSize() {
		return identity, ErrIdentityLookup
	}
	n := s.aead.NonceSize()
	raw, e := s.aead.Open(nil, q.subjectCipher[:n], q.subjectCipher[n:], q.aad())
	if e != nil {
		return identity, ErrIdentityLookup
	}
	if _, e = tx.Exec(ctx, `UPDATE identity_lookup_requests SET status='revealed',revealed_at=clock_timestamp() WHERE id=$1`, id); e != nil {
		return identity, e
	}
	if e = lookupEvent(ctx, tx, id, "revealed"); e != nil {
		return identity, e
	}
	if e = lookupAlive(ctx, tx, &q); e != nil {
		return identity, e
	}
	if e = tx.Commit(ctx); e != nil {
		return identity, e
	}
	return provider.OAuthIdentity{Provider: q.Provider, AppID: q.AppID, SubjectID: string(raw)}, nil
}
