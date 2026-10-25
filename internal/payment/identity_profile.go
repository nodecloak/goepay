package payment

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	provider "goepay/internal/adapters/payment"
	"strings"
	"time"
)

type IdentityProfileBinding struct {
	MerchantID      int64  `json:"merchant_id,string"`
	ChannelID       int64  `json:"channel_id,string"`
	ProfileID       int64  `json:"profile_id,string"`
	ProfileRevision int64  `json:"profile_revision,string"`
	Provider        string `json:"provider"`
	AppID           string `json:"app_id"`
	Kind            string `json:"kind"`
	Path            string `json:"path"`
	Fingerprint     string `json:"-"`
}
type ProfileIdentityLookup struct {
	IdentityProfileBinding
	ID                                                                  string    `json:"request_id"`
	Status                                                              string    `json:"status"`
	ExpiresAt                                                           time.Time `json:"expires_at"`
	State                                                               string    `json:"-"`
	AuthorizationToken                                                  string    `json:"-"`
	session                                                             IdentityLookup
	merchantRevision                                                    int64
	profileHash, authorizationHash, sourceProof, stateHash, launchProof []byte
}

const profileColumns = `id,merchant_id,channel_id,profile_id,profile_revision,provider,app_id,kind,path,profile_hash,session_hash,actor_username,actor_role,actor_id,actor_auth_version,authorization_hash,source_proof,state_hash,browser_hash,launch_proof,status,subject_cipher,expires_at,merchant_revision`

func profileBindingValid(b IdentityProfileBinding) bool {
	if b.MerchantID <= 0 || b.ChannelID <= 0 || b.ProfileID <= 0 || b.ProfileRevision <= 0 || b.AppID == "" || len(b.AppID) > 128 || strings.TrimSpace(b.AppID) != b.AppID || strings.ContainsAny(b.AppID, "\r\n\x00") || (b.Provider != "wxpay" && b.Provider != "alipay") || len(b.Fingerprint) != 64 {
		return false
	}
	if _, e := hex.DecodeString(b.Fingerprint); e != nil || strings.ToLower(b.Fingerprint) != b.Fingerprint {
		return false
	}
	if b.Kind == "web" {
		return b.Path == ""
	}
	if b.Kind != "mini" || len(b.Path) < 1 || len(b.Path) > 256 {
		return false
	}
	for _, part := range strings.Split(b.Path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_.-", c)) {
				return false
			}
		}
	}
	return true
}
func (s *Store) profileSource(q ProfileIdentityLookup) []byte {
	raw, _ := json.Marshal([]any{q.ID, q.IdentityProfileBinding, q.profileHash, q.session.sessionHash, q.session.actorUsername, q.session.actorRole, q.session.actorID, q.session.actorVersion, q.authorizationHash, q.ExpiresAt.UTC().Format(time.RFC3339Nano), q.merchantRevision})
	return s.oauthHash("identity-profile-source-v1", string(raw))
}
func (s *Store) profileLaunch(q ProfileIdentityLookup) []byte {
	raw, _ := json.Marshal([]any{q.ID, q.sourceProof, q.stateHash, q.session.browserHash})
	return s.oauthHash("identity-profile-launch-v1", string(raw))
}
func (s *Store) scanProfile(row pgx.Row) (ProfileIdentityLookup, error) {
	var q ProfileIdentityLookup
	e := row.Scan(&q.ID, &q.MerchantID, &q.ChannelID, &q.ProfileID, &q.ProfileRevision, &q.Provider, &q.AppID, &q.Kind, &q.Path, &q.profileHash, &q.session.sessionHash, &q.session.actorUsername, &q.session.actorRole, &q.session.actorID, &q.session.actorVersion, &q.authorizationHash, &q.sourceProof, &q.stateHash, &q.session.browserHash, &q.launchProof, &q.Status, &q.session.subjectCipher, &q.ExpiresAt, &q.merchantRevision)
	if e != nil {
		return q, ErrIdentityLookup
	}
	if len(q.sourceProof) != 32 || subtle.ConstantTimeCompare(q.sourceProof, s.profileSource(q)) != 1 {
		return q, ErrIdentityLookup
	}
	if len(q.stateHash) > 0 && (len(q.launchProof) != 32 || subtle.ConstantTimeCompare(q.launchProof, s.profileLaunch(q)) != 1) {
		return q, ErrIdentityLookup
	}
	q.session.MerchantID = q.MerchantID
	q.session.ExpiresAt = q.ExpiresAt
	return q, nil
}
func (s *Store) profileRow(ctx context.Context, tx pgx.Tx, where string, args ...any) (ProfileIdentityLookup, error) {
	return s.scanProfile(tx.QueryRow(ctx, "SELECT "+profileColumns+" FROM profile_identity_lookup_requests WHERE "+where+" FOR UPDATE", args...))
}
func profileEvent(ctx context.Context, tx pgx.Tx, id, op string) error {
	_, e := tx.Exec(ctx, `INSERT INTO profile_identity_lookup_events(request_id,operation)VALUES($1,$2)ON CONFLICT DO NOTHING`, id, op)
	return e
}
func profileAlive(ctx context.Context, tx pgx.Tx, q *ProfileIdentityLookup) error {
	if e := lookupSession(ctx, tx, &q.session); e != nil {
		return e
	}
	// lookupSession holds the target merchant FOR SHARE in this transaction.
	// Bind its monotonic configuration revision as well as the actor's epoch:
	// an admin session itself survives a target merchant disable/enable cycle.
	var revision int64
	if e := tx.QueryRow(ctx, `SELECT config_revision FROM merchants WHERE id=$1`, q.MerchantID).Scan(&revision); e != nil || revision != q.merchantRevision || revision <= 0 {
		return ErrIdentityLookup
	}
	var alive bool
	if e := tx.QueryRow(ctx, `SELECT $1::timestamptz>clock_timestamp()`, q.ExpiresAt).Scan(&alive); e != nil || !alive {
		return ErrIdentityLookup
	}
	return nil
}
func (s *Store) MatchesProfileIdentityLookup(q ProfileIdentityLookup, b IdentityProfileBinding) bool {
	if !profileBindingValid(b) {
		return false
	}
	a := q.IdentityProfileBinding
	a.Fingerprint = ""
	b0 := b
	b0.Fingerprint = ""
	return a == b0 && subtle.ConstantTimeCompare(q.profileHash, s.oauthHash("identity-profile-runtime-v1", b.Fingerprint)) == 1
}
func profileFailedTx(ctx context.Context, tx pgx.Tx, q ProfileIdentityLookup) error {
	if q.Status == "pending" || q.Status == "processing" || q.Status == "verified" {
		if _, e := tx.Exec(ctx, `UPDATE profile_identity_lookup_requests SET status='failed',completed_at=COALESCE(completed_at,clock_timestamp()) WHERE id=$1`, q.ID); e != nil {
			return e
		}
		return profileEvent(ctx, tx, q.ID, "failed")
	}
	return nil
}
func (s *Store) rejectProfileBinding(ctx context.Context, tx pgx.Tx, q ProfileIdentityLookup, b IdentityProfileBinding) error {
	if s.MatchesProfileIdentityLookup(q, b) {
		return nil
	}
	if e := profileFailedTx(ctx, tx, q); e != nil {
		return e
	}
	if e := tx.Commit(ctx); e != nil {
		return e
	}
	return ErrIdentityLookup
}

func (s *Store) CreateProfileIdentityLookup(ctx context.Context, token, role string, b IdentityProfileBinding) (ProfileIdentityLookup, error) {
	q := ProfileIdentityLookup{IdentityProfileBinding: b, Status: "pending"}
	if len(token) != 43 || !profileBindingValid(b) || s.aead == nil || len(s.indexKey) < 32 {
		return q, ErrIdentityLookup
	}
	q.session = IdentityLookup{IdentityLookupBinding: IdentityLookupBinding{MerchantID: b.MerchantID}, sessionHash: lookupSessionHash(token)}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return q, e
	}
	defer tx.Rollback(ctx)
	if e = lookupSession(ctx, tx, &q.session); e != nil || q.session.actorRole != role {
		return q, ErrIdentityLookup
	}
	if e = tx.QueryRow(ctx, `SELECT config_revision FROM merchants WHERE id=$1`, q.MerchantID).Scan(&q.merchantRevision); e != nil || q.merchantRevision <= 0 {
		return q, ErrIdentityLookup
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,47))`, hex.EncodeToString(q.session.sessionHash)); e != nil {
		return q, e
	}
	var n int
	if e = tx.QueryRow(ctx, `SELECT(SELECT count(*) FROM identity_lookup_requests WHERE session_hash=$1 AND expires_at>clock_timestamp() AND status IN ('pending','processing','verified'))+(SELECT count(*) FROM profile_identity_lookup_requests WHERE session_hash=$1 AND expires_at>clock_timestamp() AND status IN ('pending','processing','verified'))`, q.session.sessionHash).Scan(&n); e != nil {
		return q, e
	}
	if n >= 5 {
		return q, ErrIdentityLookup
	}
	if e = tx.QueryRow(ctx, `SELECT LEAST($1::timestamptz,statement_timestamp()+interval '5 minutes')`, q.session.ExpiresAt).Scan(&q.ExpiresAt); e != nil {
		return q, e
	}
	q.ID, e = lookupNonce()
	if e != nil {
		return q, e
	}
	q.profileHash = s.oauthHash("identity-profile-runtime-v1", b.Fingerprint)
	if b.Kind == "web" {
		q.AuthorizationToken, e = lookupNonce()
		q.authorizationHash = s.oauthHash("identity-profile-authorize-v1", q.AuthorizationToken)
	} else {
		q.State, e = lookupNonce()
		q.stateHash = s.oauthHash("identity-profile-state-v1", q.State)
	}
	if e != nil {
		return q, e
	}
	q.sourceProof = s.profileSource(q)
	if len(q.stateHash) > 0 {
		q.launchProof = s.profileLaunch(q)
	}
	_, e = tx.Exec(ctx, `INSERT INTO profile_identity_lookup_requests(id,merchant_id,channel_id,profile_id,profile_revision,provider,app_id,kind,path,profile_hash,session_hash,actor_username,actor_role,actor_id,actor_auth_version,authorization_hash,source_proof,state_hash,launch_proof,expires_at,merchant_revision)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`, q.ID, q.MerchantID, q.ChannelID, q.ProfileID, q.ProfileRevision, q.Provider, q.AppID, q.Kind, q.Path, q.profileHash, q.session.sessionHash, q.session.actorUsername, q.session.actorRole, q.session.actorID, q.session.actorVersion, q.authorizationHash, q.sourceProof, q.stateHash, q.launchProof, q.ExpiresAt, q.merchantRevision)
	if e != nil {
		return q, e
	}
	if e = profileEvent(ctx, tx, q.ID, "created"); e != nil {
		return q, e
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return q, e
	}
	return q, tx.Commit(ctx)
}
func (s *Store) StartProfileIdentityLookup(ctx context.Context, authorization, browser string, b IdentityProfileBinding) (ProfileIdentityLookup, string, error) {
	var q ProfileIdentityLookup
	if !validOAuthBrowser(authorization) || !validOAuthBrowser(browser) {
		return q, "", ErrIdentityLookup
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return q, "", e
	}
	defer tx.Rollback(ctx)
	q, e = s.profileRow(ctx, tx, "authorization_hash=$1", s.oauthHash("identity-profile-authorize-v1", authorization))
	if e != nil || q.Kind != "web" || q.Status != "pending" || len(q.stateHash) > 0 {
		return q, "", ErrIdentityLookup
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return q, "", e
	}
	if e = s.rejectProfileBinding(ctx, tx, q, b); e != nil {
		return q, "", e
	}
	state, e := lookupNonce()
	if e != nil {
		return q, "", e
	}
	q.stateHash = s.oauthHash("identity-profile-state-v1", state)
	q.session.browserHash = s.oauthHash("identity-profile-browser-v1", browser)
	q.launchProof = s.profileLaunch(q)
	if _, e = tx.Exec(ctx, `UPDATE profile_identity_lookup_requests SET state_hash=$2,browser_hash=$3,launch_proof=$4 WHERE id=$1`, q.ID, q.stateHash, q.session.browserHash, q.launchProof); e != nil {
		return q, "", e
	}
	if e = profileEvent(ctx, tx, q.ID, "authorized"); e != nil {
		return q, "", e
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return q, "", e
	}
	return q, state, tx.Commit(ctx)
}
func (s *Store) ConsumeProfileIdentityLookup(ctx context.Context, id, state, browser, code string, b IdentityProfileBinding) (ProfileIdentityLookup, error) {
	var q ProfileIdentityLookup
	if !validOAuthBrowser(id) || !validOAuthBrowser(state) || !validIdentityCode(code) {
		return q, ErrIdentityLookup
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return q, e
	}
	defer tx.Rollback(ctx)
	q, e = s.profileRow(ctx, tx, "id=$1", id)
	if e != nil || q.Status != "pending" || subtle.ConstantTimeCompare(q.stateHash, s.oauthHash("identity-profile-state-v1", state)) != 1 {
		return q, ErrIdentityLookup
	}
	if q.Kind == "web" && (!validOAuthBrowser(browser) || subtle.ConstantTimeCompare(q.session.browserHash, s.oauthHash("identity-profile-browser-v1", browser)) != 1) || q.Kind == "mini" && browser != "" {
		return q, ErrIdentityLookup
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return q, e
	}
	if e = s.rejectProfileBinding(ctx, tx, q, b); e != nil {
		return q, e
	}
	domain := "web"
	if q.Kind == "mini" {
		domain = "mini"
	}
	claimed, e := s.claimOAuthAuthorizationCode(ctx, tx, q.Provider, q.AppID, domain, code, q.stateHash)
	if e != nil {
		return q, e
	}
	claimed = claimed && profileAlive(ctx, tx, &q) == nil
	q.Status = "processing"
	if !claimed {
		q.Status = "failed"
	}
	if _, e = tx.Exec(ctx, `UPDATE profile_identity_lookup_requests SET status=$2 WHERE id=$1`, q.ID, q.Status); e != nil {
		return q, e
	}
	if e = profileEvent(ctx, tx, q.ID, q.Status); e != nil {
		return q, e
	}
	if claimed && profileAlive(ctx, tx, &q) != nil {
		if e = profileFailedTx(ctx, tx, q); e != nil {
			return q, e
		}
		q.Status = "failed"
		claimed = false
	}
	if e = tx.Commit(ctx); e != nil {
		return q, e
	}
	if !claimed {
		return q, ErrIdentityLookup
	}
	return q, nil
}
func (s *Store) CompleteProfileIdentityLookup(ctx context.Context, id string, identity provider.OAuthIdentity, b IdentityProfileBinding) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q, e := s.profileRow(ctx, tx, "id=$1", id)
	if e != nil || q.Status != "processing" || profileAlive(ctx, tx, &q) != nil {
		return ErrIdentityLookup
	}
	if e = s.rejectProfileBinding(ctx, tx, q, b); e != nil {
		return e
	}
	if identity.Provider != q.Provider || identity.AppID != q.AppID || !validIdentityCode(identity.SubjectID) || len(identity.SubjectID) > 128 {
		return ErrIdentityLookup
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	cipher := s.aead.Seal(nonce, nonce, []byte(identity.SubjectID), q.sourceProof)
	if _, e = tx.Exec(ctx, `UPDATE profile_identity_lookup_requests SET status='verified',subject_cipher=$2,completed_at=clock_timestamp() WHERE id=$1`, id, cipher); e != nil {
		return e
	}
	if e = profileEvent(ctx, tx, id, "verified"); e != nil {
		return e
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) FailProfileIdentityLookup(ctx context.Context, id string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q, e := s.profileRow(ctx, tx, "id=$1", id)
	if e != nil {
		return e
	}
	if e = profileFailedTx(ctx, tx, q); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) profileOwned(ctx context.Context, tx pgx.Tx, id, token, role string, b IdentityProfileBinding) (ProfileIdentityLookup, error) {
	var q ProfileIdentityLookup
	if !validOAuthBrowser(id) || len(token) != 43 {
		return q, ErrIdentityLookup
	}
	q, e := s.profileRow(ctx, tx, "id=$1 AND session_hash=$2 AND actor_role=$3", id, lookupSessionHash(token), role)
	if e != nil {
		return q, e
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return q, e
	}
	return q, s.rejectProfileBinding(ctx, tx, q, b)
}
func (s *Store) ReadProfileIdentityLookup(ctx context.Context, id, token, role string, b IdentityProfileBinding) (ProfileIdentityLookup, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ProfileIdentityLookup{}, e
	}
	defer tx.Rollback(ctx)
	q, e := s.profileOwned(ctx, tx, id, token, role, b)
	if e != nil {
		return q, e
	}
	return q, tx.Commit(ctx)
}
func (s *Store) RevealProfileIdentityLookup(ctx context.Context, id, token, role string, b IdentityProfileBinding) (provider.OAuthIdentity, error) {
	var out provider.OAuthIdentity
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	q, e := s.profileOwned(ctx, tx, id, token, role, b)
	if e != nil || q.Status != "verified" || s.aead == nil || len(q.session.subjectCipher) < s.aead.NonceSize() {
		return out, ErrIdentityLookup
	}
	n := s.aead.NonceSize()
	raw, e := s.aead.Open(nil, q.session.subjectCipher[:n], q.session.subjectCipher[n:], q.sourceProof)
	if e != nil {
		return out, ErrIdentityLookup
	}
	if _, e = tx.Exec(ctx, `UPDATE profile_identity_lookup_requests SET status='revealed',revealed_at=clock_timestamp() WHERE id=$1`, id); e != nil {
		return out, e
	}
	if e = profileEvent(ctx, tx, id, "revealed"); e != nil {
		return out, e
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return provider.OAuthIdentity{Provider: q.Provider, AppID: q.AppID, SubjectID: string(raw)}, nil
}

// Metadata is for server-side runtime-policy resolution only. It is not a public
// owner read and must never be emitted as an authenticated status response.
func (s *Store) ProfileIdentityLookupMetadata(ctx context.Context, id string) (ProfileIdentityLookup, error) {
	return s.profileMetadata(ctx, "id=$1", id)
}
func (s *Store) ProfileIdentityLookupByAuthorization(ctx context.Context, authorization string) (ProfileIdentityLookup, error) {
	if !validOAuthBrowser(authorization) {
		return ProfileIdentityLookup{}, ErrIdentityLookup
	}
	return s.profileMetadata(ctx, "authorization_hash=$1", s.oauthHash("identity-profile-authorize-v1", authorization))
}
func (s *Store) profileMetadata(ctx context.Context, where string, arg any) (ProfileIdentityLookup, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ProfileIdentityLookup{}, e
	}
	defer tx.Rollback(ctx)
	q, e := s.profileRow(ctx, tx, where, arg)
	if e != nil {
		return q, e
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return q, e
	}
	return q, tx.Commit(ctx)
}
