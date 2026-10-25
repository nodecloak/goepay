package payment

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	provider "goepay/internal/adapters/payment"
	"strings"
	"time"
)

var ErrApplicationAuthorization = errors.New("application authorization is unavailable or no longer authorized")

const applicationAuthorizationPurpose = "alipay_app_authorization"

// ApplicationAuthorizationBinding is resolved by trusted server configuration.
// It grants no payment capability and contains no caller-selectable credential.
type ApplicationAuthorizationBinding struct {
	MerchantID        int64                                 `json:"merchant_id,string"`
	ProfileID         int64                                 `json:"profile_id,string"`
	ProfileRevision   int64                                 `json:"profile_revision,string"`
	IssuerAppID       string                                `json:"issuer_app_id"`
	ExpectedAuthAppID string                                `json:"expected_auth_app_id"`
	LaunchKind        string                                `json:"launch_kind"`
	AppTypes          []provider.AlipayAuthorizationAppType `json:"app_types"`
	Fingerprint       string                                `json:"-"`
}
type ApplicationAuthorizationRequest struct {
	ApplicationAuthorizationBinding
	ID                                                                                string    `json:"request_id"`
	Purpose                                                                           string    `json:"purpose"`
	Status                                                                            string    `json:"status"`
	ExpiresAt                                                                         time.Time `json:"expires_at"`
	AuthorizationToken                                                                string    `json:"-"`
	session                                                                           IdentityLookup
	merchantRevision                                                                  int64
	profileHash, authorizationHash, sourceProof, stateHash, launchProof, resultCipher []byte
}

const applicationAuthorizationColumns = `id,purpose,merchant_id,merchant_revision,profile_id,profile_revision,issuer_app_id,expected_auth_app_id,launch_kind,app_types,profile_hash,session_hash,actor_username,actor_role,actor_id,actor_auth_version,authorization_hash,source_proof,state_hash,browser_hash,launch_proof,status,result_cipher,expires_at`

func applicationAuthorizationAppID(v string) bool {
	if len(v) != 16 {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func validApplicationAuthorizationBinding(b ApplicationAuthorizationBinding) bool {
	if b.MerchantID <= 0 || b.ProfileID <= 0 || b.ProfileRevision <= 0 || !applicationAuthorizationAppID(b.IssuerAppID) || !applicationAuthorizationAppID(b.ExpectedAuthAppID) || len(b.Fingerprint) != 64 || strings.ToLower(b.Fingerprint) != b.Fingerprint {
		return false
	}
	if _, e := hex.DecodeString(b.Fingerprint); e != nil {
		return false
	}
	if b.LaunchKind == "basic" {
		return len(b.AppTypes) == 0
	}
	if b.LaunchKind != "assigned" || len(b.AppTypes) < 1 || len(b.AppTypes) > 5 {
		return false
	}
	seen := map[provider.AlipayAuthorizationAppType]bool{}
	for _, v := range b.AppTypes {
		switch v {
		case provider.AlipayAuthorizationMobileApp, provider.AlipayAuthorizationWebApp, provider.AlipayAuthorizationPublicApp, provider.AlipayAuthorizationTinyApp, provider.AlipayAuthorizationBaseApp:
		default:
			return false
		}
		if seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
func (s *Store) applicationAuthorizationSource(q ApplicationAuthorizationRequest) []byte {
	raw, _ := json.Marshal([]any{q.ID, q.Purpose, q.ApplicationAuthorizationBinding, q.profileHash, q.session.sessionHash, q.session.actorUsername, q.session.actorRole, q.session.actorID, q.session.actorVersion, q.authorizationHash, q.ExpiresAt.UTC().Format(time.RFC3339Nano), q.merchantRevision})
	return s.oauthHash("application-authorization-source-v1", string(raw))
}
func (s *Store) applicationAuthorizationLaunch(q ApplicationAuthorizationRequest) []byte {
	raw, _ := json.Marshal([]any{q.ID, q.sourceProof, q.stateHash, q.session.browserHash})
	return s.oauthHash("application-authorization-launch-v1", string(raw))
}
func (s *Store) scanApplicationAuthorization(row pgx.Row) (ApplicationAuthorizationRequest, error) {
	var q ApplicationAuthorizationRequest
	var types []byte
	e := row.Scan(&q.ID, &q.Purpose, &q.MerchantID, &q.merchantRevision, &q.ProfileID, &q.ProfileRevision, &q.IssuerAppID, &q.ExpectedAuthAppID, &q.LaunchKind, &types, &q.profileHash, &q.session.sessionHash, &q.session.actorUsername, &q.session.actorRole, &q.session.actorID, &q.session.actorVersion, &q.authorizationHash, &q.sourceProof, &q.stateHash, &q.session.browserHash, &q.launchProof, &q.Status, &q.resultCipher, &q.ExpiresAt)
	if e != nil || json.Unmarshal(types, &q.AppTypes) != nil || q.Purpose != applicationAuthorizationPurpose || len(q.sourceProof) != 32 || subtle.ConstantTimeCompare(q.sourceProof, s.applicationAuthorizationSource(q)) != 1 {
		return q, ErrApplicationAuthorization
	}
	if len(q.stateHash) > 0 && (len(q.launchProof) != 32 || subtle.ConstantTimeCompare(q.launchProof, s.applicationAuthorizationLaunch(q)) != 1) {
		return q, ErrApplicationAuthorization
	}
	q.session.MerchantID = q.MerchantID
	q.session.ExpiresAt = q.ExpiresAt
	return q, nil
}
func (s *Store) applicationAuthorizationRow(ctx context.Context, tx pgx.Tx, where string, args ...any) (ApplicationAuthorizationRequest, error) {
	return s.scanApplicationAuthorization(tx.QueryRow(ctx, "SELECT "+applicationAuthorizationColumns+" FROM application_authorization_requests WHERE "+where+" FOR UPDATE", args...))
}
func applicationAuthorizationEvent(ctx context.Context, tx pgx.Tx, id, op string) error {
	_, e := tx.Exec(ctx, `INSERT INTO application_authorization_events(request_id,operation)VALUES($1,$2)ON CONFLICT DO NOTHING`, id, op)
	return e
}
func applicationAuthorizationSession(ctx context.Context, tx pgx.Tx, q *ApplicationAuthorizationRequest) error {
	if lookupSession(ctx, tx, &q.session) != nil || q.session.actorRole != "admin" {
		return ErrApplicationAuthorization
	}
	var class string
	if e := tx.QueryRow(ctx, `SELECT admin_class FROM console_users WHERE username=$1 AND role='admin'`, q.session.actorUsername).Scan(&class); e != nil || class != "super" {
		return ErrApplicationAuthorization
	}
	return nil
}
func (s *Store) applicationAuthorizationClaimProof(q ApplicationAuthorizationRequest, codeHash []byte) []byte {
	return s.oauthHash("application-authorization-app-code-claim-v1", q.ID+"/"+q.IssuerAppID+"/"+hex.EncodeToString(codeHash)+"/"+hex.EncodeToString(q.sourceProof))
}
func (s *Store) applicationAuthorizationClaimValid(ctx context.Context, tx pgx.Tx, q ApplicationAuthorizationRequest) bool {
	var issuer string
	var codeHash, proof []byte
	if e := tx.QueryRow(ctx, `SELECT issuer_app_id,code_hash,claim_proof FROM application_authorization_code_claims WHERE request_id=$1`, q.ID).Scan(&issuer, &codeHash, &proof); e != nil {
		return false
	}
	return issuer == q.IssuerAppID && len(codeHash) == 32 && len(proof) == 32 && subtle.ConstantTimeCompare(proof, s.applicationAuthorizationClaimProof(q, codeHash)) == 1
}
func (s *Store) applicationAuthorizationAlive(ctx context.Context, tx pgx.Tx, q *ApplicationAuthorizationRequest) error {
	if applicationAuthorizationSession(ctx, tx, q) != nil {
		return ErrApplicationAuthorization
	}
	var revision int64
	var alive bool
	if e := tx.QueryRow(ctx, `SELECT config_revision,$2::timestamptz>clock_timestamp() FROM merchants WHERE id=$1`, q.MerchantID, q.ExpiresAt).Scan(&revision, &alive); e != nil || revision != q.merchantRevision || revision <= 0 || !alive {
		return ErrApplicationAuthorization
	}
	if (q.Status == "processing" || q.Status == "verified" || q.Status == "revealed") && !s.applicationAuthorizationClaimValid(ctx, tx, *q) {
		return ErrApplicationAuthorization
	}
	return nil
}
func (s *Store) MatchesApplicationAuthorization(q ApplicationAuthorizationRequest, b ApplicationAuthorizationBinding) bool {
	if !validApplicationAuthorizationBinding(b) {
		return false
	}
	fingerprint := b.Fingerprint
	a := normalizeApplicationAuthorizationBinding(q.ApplicationAuthorizationBinding)
	a.Fingerprint = ""
	b = normalizeApplicationAuthorizationBinding(b)
	b.Fingerprint = ""
	rawA, _ := json.Marshal(a)
	rawB, _ := json.Marshal(b)
	return string(rawA) == string(rawB) && subtle.ConstantTimeCompare(q.profileHash, s.oauthHash("application-authorization-runtime-v1", fingerprint)) == 1
}
func applicationAuthorizationFailedTx(ctx context.Context, tx pgx.Tx, q ApplicationAuthorizationRequest) error {
	if q.Status == "pending" || q.Status == "processing" || q.Status == "verified" {
		if _, e := tx.Exec(ctx, `UPDATE application_authorization_requests SET status='failed',completed_at=COALESCE(completed_at,clock_timestamp()) WHERE id=$1`, q.ID); e != nil {
			return e
		}
		return applicationAuthorizationEvent(ctx, tx, q.ID, "failed")
	}
	return nil
}
func (s *Store) rejectApplicationAuthorizationBinding(ctx context.Context, tx pgx.Tx, q ApplicationAuthorizationRequest, b ApplicationAuthorizationBinding) error {
	if s.MatchesApplicationAuthorization(q, b) {
		return nil
	}
	if e := applicationAuthorizationFailedTx(ctx, tx, q); e != nil {
		return e
	}
	if e := tx.Commit(ctx); e != nil {
		return e
	}
	return ErrApplicationAuthorization
}
func normalizeApplicationAuthorizationBinding(b ApplicationAuthorizationBinding) ApplicationAuthorizationBinding {
	b.AppTypes = append([]provider.AlipayAuthorizationAppType{}, b.AppTypes...)
	return b
}
func (s *Store) CreateApplicationAuthorization(ctx context.Context, token, role string, b ApplicationAuthorizationBinding) (ApplicationAuthorizationRequest, error) {
	b = normalizeApplicationAuthorizationBinding(b)
	q := ApplicationAuthorizationRequest{ApplicationAuthorizationBinding: b, Purpose: applicationAuthorizationPurpose, Status: "pending"}
	if len(token) != 43 || role != "admin" || !validApplicationAuthorizationBinding(b) || s.aead == nil || len(s.indexKey) < 32 {
		return q, ErrApplicationAuthorization
	}
	q.session = IdentityLookup{IdentityLookupBinding: IdentityLookupBinding{MerchantID: b.MerchantID}, sessionHash: lookupSessionHash(token)}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return q, e
	}
	defer tx.Rollback(ctx)
	if applicationAuthorizationSession(ctx, tx, &q) != nil {
		return q, ErrApplicationAuthorization
	}
	if e = tx.QueryRow(ctx, `SELECT config_revision FROM merchants WHERE id=$1`, q.MerchantID).Scan(&q.merchantRevision); e != nil || q.merchantRevision <= 0 {
		return q, ErrApplicationAuthorization
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,66))`, hex.EncodeToString(q.session.sessionHash)); e != nil {
		return q, e
	}
	var n int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM application_authorization_requests WHERE session_hash=$1 AND expires_at>clock_timestamp() AND status IN('pending','processing','verified')`, q.session.sessionHash).Scan(&n); e != nil {
		return q, e
	}
	if n >= 5 {
		return q, ErrApplicationAuthorization
	}
	if e = tx.QueryRow(ctx, `SELECT LEAST($1::timestamptz,statement_timestamp()+interval '5 minutes')`, q.session.ExpiresAt).Scan(&q.ExpiresAt); e != nil {
		return q, e
	}
	if q.ID, e = lookupNonce(); e != nil {
		return q, e
	}
	if q.AuthorizationToken, e = lookupNonce(); e != nil {
		return q, e
	}
	q.authorizationHash = s.oauthHash("application-authorization-authorize-v1", q.AuthorizationToken)
	q.profileHash = s.oauthHash("application-authorization-runtime-v1", b.Fingerprint)
	q.sourceProof = s.applicationAuthorizationSource(q)
	types, _ := json.Marshal(b.AppTypes)
	_, e = tx.Exec(ctx, `INSERT INTO application_authorization_requests(id,purpose,merchant_id,merchant_revision,profile_id,profile_revision,issuer_app_id,expected_auth_app_id,launch_kind,app_types,profile_hash,session_hash,actor_username,actor_role,actor_id,actor_auth_version,authorization_hash,source_proof,expires_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`, q.ID, q.Purpose, q.MerchantID, q.merchantRevision, q.ProfileID, q.ProfileRevision, q.IssuerAppID, q.ExpectedAuthAppID, q.LaunchKind, types, q.profileHash, q.session.sessionHash, q.session.actorUsername, q.session.actorRole, q.session.actorID, q.session.actorVersion, q.authorizationHash, q.sourceProof, q.ExpiresAt)
	if e != nil {
		return q, e
	}
	if e = applicationAuthorizationEvent(ctx, tx, q.ID, "created"); e != nil {
		return q, e
	}
	if e = s.applicationAuthorizationAlive(ctx, tx, &q); e != nil {
		return q, e
	}
	return q, tx.Commit(ctx)
}
func (s *Store) StartApplicationAuthorization(ctx context.Context, authorization, browser string, b ApplicationAuthorizationBinding) (ApplicationAuthorizationRequest, string, error) {
	var q ApplicationAuthorizationRequest
	if !validOAuthBrowser(authorization) || !validOAuthBrowser(browser) {
		return q, "", ErrApplicationAuthorization
	}
	b = normalizeApplicationAuthorizationBinding(b)
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return q, "", e
	}
	defer tx.Rollback(ctx)
	q, e = s.applicationAuthorizationRow(ctx, tx, "authorization_hash=$1", s.oauthHash("application-authorization-authorize-v1", authorization))
	if e != nil || q.Status != "pending" || len(q.stateHash) > 0 {
		return q, "", ErrApplicationAuthorization
	}
	if e = s.applicationAuthorizationAlive(ctx, tx, &q); e != nil {
		return q, "", e
	}
	if e = s.rejectApplicationAuthorizationBinding(ctx, tx, q, b); e != nil {
		return q, "", e
	}
	state, e := lookupNonce()
	if e != nil {
		return q, "", e
	}
	q.stateHash = s.oauthHash("application-authorization-state-v1", state)
	q.session.browserHash = s.oauthHash("application-authorization-browser-v1", browser)
	q.launchProof = s.applicationAuthorizationLaunch(q)
	if _, e = tx.Exec(ctx, `UPDATE application_authorization_requests SET state_hash=$2,browser_hash=$3,launch_proof=$4 WHERE id=$1`, q.ID, q.stateHash, q.session.browserHash, q.launchProof); e != nil {
		return q, "", e
	}
	if e = applicationAuthorizationEvent(ctx, tx, q.ID, "authorized"); e != nil {
		return q, "", e
	}
	if e = s.applicationAuthorizationAlive(ctx, tx, &q); e != nil {
		return q, "", e
	}
	return q, state, tx.Commit(ctx)
}
func (s *Store) ConsumeApplicationAuthorization(ctx context.Context, id, state, browser, appAuthCode string, b ApplicationAuthorizationBinding) (ApplicationAuthorizationRequest, error) {
	var q ApplicationAuthorizationRequest
	if !validOAuthBrowser(id) || !validOAuthBrowser(state) || !validOAuthBrowser(browser) || !validIdentityCode(appAuthCode) {
		return q, ErrApplicationAuthorization
	}
	b = normalizeApplicationAuthorizationBinding(b)
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return q, e
	}
	defer tx.Rollback(ctx)
	q, e = s.applicationAuthorizationRow(ctx, tx, "id=$1", id)
	if e != nil || q.Status != "pending" || subtle.ConstantTimeCompare(q.stateHash, s.oauthHash("application-authorization-state-v1", state)) != 1 || subtle.ConstantTimeCompare(q.session.browserHash, s.oauthHash("application-authorization-browser-v1", browser)) != 1 {
		return q, ErrApplicationAuthorization
	}
	if e = s.applicationAuthorizationAlive(ctx, tx, &q); e != nil {
		return q, e
	}
	if e = s.rejectApplicationAuthorizationBinding(ctx, tx, q, b); e != nil {
		return q, e
	}
	codeHash := s.oauthHash("application-authorization-app-code-v1", appAuthCode)
	proof := s.applicationAuthorizationClaimProof(q, codeHash)
	tag, e := tx.Exec(ctx, `INSERT INTO application_authorization_code_claims(issuer_app_id,code_hash,request_id,claim_proof)VALUES($1,$2,$3,$4)ON CONFLICT DO NOTHING`, q.IssuerAppID, codeHash, q.ID, proof)
	if e != nil {
		return q, e
	}
	accepted := tag.RowsAffected() == 1 && s.applicationAuthorizationAlive(ctx, tx, &q) == nil
	q.Status = "processing"
	if !accepted {
		q.Status = "failed"
	}
	if _, e = tx.Exec(ctx, `UPDATE application_authorization_requests SET status=$2 WHERE id=$1`, id, q.Status); e != nil {
		return q, e
	}
	if e = applicationAuthorizationEvent(ctx, tx, id, q.Status); e != nil {
		return q, e
	}
	if accepted && s.applicationAuthorizationAlive(ctx, tx, &q) != nil {
		if e = applicationAuthorizationFailedTx(ctx, tx, q); e != nil {
			return q, e
		}
		q.Status = "failed"
		accepted = false
	}
	if e = tx.Commit(ctx); e != nil {
		return q, e
	}
	if !accepted {
		return q, ErrApplicationAuthorization
	}
	return q, nil
}

// The secret has an explicit private persistence representation. The provider's
// public DTO deliberately excludes it from ordinary JSON serialization.
type applicationAuthorizationSecret struct {
	Token     string `json:"token"`
	UserID    string `json:"user_id"`
	ExpiresIn int64  `json:"expires_in"`
}

func validApplicationAuthorizationToken(token string) bool {
	if len(token) < 1 || len(token) > 4096 {
		return false
	}
	for _, c := range token {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func validApplicationAuthorizationResult(r provider.ApplicationAuthorizationResult, q ApplicationAuthorizationRequest) bool {
	return r.Provider == "alipay" && r.IssuerAppID == q.IssuerAppID && r.AuthAppID == q.ExpectedAuthAppID && len(r.UserID) == 16 && strings.HasPrefix(r.UserID, "2088") && applicationAuthorizationAppID(r.UserID) && validApplicationAuthorizationToken(r.AppAuthToken) && r.TokenExpiresInSeconds >= 0
}
func (s *Store) CompleteApplicationAuthorization(ctx context.Context, id string, result provider.ApplicationAuthorizationResult, b ApplicationAuthorizationBinding) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q, e := s.applicationAuthorizationRow(ctx, tx, "id=$1", id)
	if e != nil || q.Status != "processing" || s.applicationAuthorizationAlive(ctx, tx, &q) != nil {
		return ErrApplicationAuthorization
	}
	if e = s.rejectApplicationAuthorizationBinding(ctx, tx, q, normalizeApplicationAuthorizationBinding(b)); e != nil {
		return e
	}
	if !validApplicationAuthorizationResult(result, q) || s.aead == nil {
		return ErrApplicationAuthorization
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	raw, _ := json.Marshal(applicationAuthorizationSecret{result.AppAuthToken, result.UserID, result.TokenExpiresInSeconds})
	cipher := s.aead.Seal(nonce, nonce, raw, q.sourceProof)
	if _, e = tx.Exec(ctx, `UPDATE application_authorization_requests SET status='verified',result_cipher=$2,completed_at=clock_timestamp() WHERE id=$1`, id, cipher); e != nil {
		return e
	}
	if e = applicationAuthorizationEvent(ctx, tx, id, "verified"); e != nil {
		return e
	}
	if e = s.applicationAuthorizationAlive(ctx, tx, &q); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) FailApplicationAuthorization(ctx context.Context, id string, b ApplicationAuthorizationBinding) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q, e := s.applicationAuthorizationRow(ctx, tx, "id=$1", id)
	if e != nil || s.applicationAuthorizationAlive(ctx, tx, &q) != nil {
		return ErrApplicationAuthorization
	}
	if e = s.rejectApplicationAuthorizationBinding(ctx, tx, q, normalizeApplicationAuthorizationBinding(b)); e != nil {
		return e
	}
	if e = applicationAuthorizationFailedTx(ctx, tx, q); e != nil {
		return e
	}
	if e = s.applicationAuthorizationAlive(ctx, tx, &q); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) applicationAuthorizationOwned(ctx context.Context, tx pgx.Tx, id, token, role string, b ApplicationAuthorizationBinding) (ApplicationAuthorizationRequest, error) {
	var q ApplicationAuthorizationRequest
	if !validOAuthBrowser(id) || len(token) != 43 || role != "admin" {
		return q, ErrApplicationAuthorization
	}
	q, e := s.applicationAuthorizationRow(ctx, tx, "id=$1 AND session_hash=$2 AND actor_role='admin'", id, lookupSessionHash(token))
	if e != nil || s.applicationAuthorizationAlive(ctx, tx, &q) != nil {
		return q, ErrApplicationAuthorization
	}
	return q, s.rejectApplicationAuthorizationBinding(ctx, tx, q, normalizeApplicationAuthorizationBinding(b))
}
func (s *Store) ReadApplicationAuthorization(ctx context.Context, id, token, role string, b ApplicationAuthorizationBinding) (ApplicationAuthorizationRequest, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ApplicationAuthorizationRequest{}, e
	}
	defer tx.Rollback(ctx)
	q, e := s.applicationAuthorizationOwned(ctx, tx, id, token, role, b)
	if e != nil {
		return q, e
	}
	return q, tx.Commit(ctx)
}
func (s *Store) RevealApplicationAuthorization(ctx context.Context, id, token, role string, b ApplicationAuthorizationBinding) (provider.ApplicationAuthorizationResult, error) {
	var out provider.ApplicationAuthorizationResult
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	q, e := s.applicationAuthorizationOwned(ctx, tx, id, token, role, b)
	if e != nil || q.Status != "verified" || s.aead == nil || len(q.resultCipher) < s.aead.NonceSize() {
		return out, ErrApplicationAuthorization
	}
	n := s.aead.NonceSize()
	raw, e := s.aead.Open(nil, q.resultCipher[:n], q.resultCipher[n:], q.sourceProof)
	if e != nil {
		return out, ErrApplicationAuthorization
	}
	var secret applicationAuthorizationSecret
	if json.Unmarshal(raw, &secret) != nil {
		return out, ErrApplicationAuthorization
	}
	out = provider.ApplicationAuthorizationResult{Provider: "alipay", IssuerAppID: q.IssuerAppID, AuthAppID: q.ExpectedAuthAppID, UserID: secret.UserID, AppAuthToken: secret.Token, TokenExpiresInSeconds: secret.ExpiresIn}
	if !validApplicationAuthorizationResult(out, q) {
		return provider.ApplicationAuthorizationResult{}, ErrApplicationAuthorization
	}
	if _, e = tx.Exec(ctx, `UPDATE application_authorization_requests SET status='revealed',revealed_at=clock_timestamp() WHERE id=$1`, id); e != nil {
		return provider.ApplicationAuthorizationResult{}, e
	}
	if e = applicationAuthorizationEvent(ctx, tx, id, "revealed"); e != nil {
		return provider.ApplicationAuthorizationResult{}, e
	}
	if e = s.applicationAuthorizationAlive(ctx, tx, &q); e != nil {
		return provider.ApplicationAuthorizationResult{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return provider.ApplicationAuthorizationResult{}, e
	}
	return out, nil
}

// Metadata is solely for trusted runtime policy resolution; it does not grant
// ownership or reveal permission and must not be emitted as a public status.
func (s *Store) ApplicationAuthorizationMetadata(ctx context.Context, id string) (ApplicationAuthorizationRequest, error) {
	if !validOAuthBrowser(id) {
		return ApplicationAuthorizationRequest{}, ErrApplicationAuthorization
	}
	return s.applicationAuthorizationMetadata(ctx, "id=$1", id)
}
func (s *Store) ApplicationAuthorizationByAuthorization(ctx context.Context, authorization string) (ApplicationAuthorizationRequest, error) {
	if !validOAuthBrowser(authorization) {
		return ApplicationAuthorizationRequest{}, ErrApplicationAuthorization
	}
	return s.applicationAuthorizationMetadata(ctx, "authorization_hash=$1", s.oauthHash("application-authorization-authorize-v1", authorization))
}
func (s *Store) applicationAuthorizationMetadata(ctx context.Context, where string, arg any) (ApplicationAuthorizationRequest, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ApplicationAuthorizationRequest{}, e
	}
	defer tx.Rollback(ctx)
	q, e := s.applicationAuthorizationRow(ctx, tx, where, arg)
	if e != nil || s.applicationAuthorizationAlive(ctx, tx, &q) != nil {
		return ApplicationAuthorizationRequest{}, ErrApplicationAuthorization
	}
	return q, tx.Commit(ctx)
}
