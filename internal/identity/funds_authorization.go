package identity

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrFundsInvalid        = errors.New("invalid funds authorization input")
	ErrFundsAuthentication = errors.New("funds authentication failed")
	ErrFundsAuthorization  = errors.New("funds action authorization required or invalid")
	ErrFundsNotConfigured  = errors.New("funds credential not configured")
	ErrFundsRateLimited    = errors.New("too many funds verification attempts")
	ErrFundsBusy           = errors.New("funds verification capacity unavailable")
)

type FundsQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}
type FundsActor struct {
	User                   User
	SessionToken, ClientIP string
	Delegated              bool
}
type FundsAction struct {
	Kind, ResourceID string
	digest           [32]byte
}
type FundsStatus struct {
	Configured      bool  `json:"configured"`
	Enforced        bool  `json:"enforced"`
	CredentialEpoch int64 `json:"credential_epoch"`
	TOTPRequired    bool  `json:"totp_required"`
}
type FundsChallenge struct {
	ChallengeID string    `json:"challenge_id"`
	ExpiresAt   time.Time `json:"expires_at"`
}
type FundsGrant struct {
	Authorization string    `json:"authorization"`
	ExpiresAt     time.Time `json:"expires_at"`
}
type FundsAuthorization struct {
	pool *pgxpool.Pool
	mfa  *MFA
}

var fundsVerificationSlots = make(chan struct{}, 4)
var fundsKindPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}(\.[a-z][a-z0-9_-]{0,31}){1,3}$`)
var fundsKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func NewFundsAuthorization(pool *pgxpool.Pool, mfa *MFA) *FundsAuthorization {
	return &FundsAuthorization{pool: pool, mfa: mfa}
}

// NewFundsAction is for trusted application code. HTTP must reconstruct its
// payload from a typed, authorized business request and current DB snapshots.
// Only its final digest is persisted; plaintext recipient data is never stored.
func NewFundsAction(kind, resource string, payload map[string]string) (FundsAction, error) {
	if !fundsKindPattern.MatchString(kind) || resource == "" || len(resource) > 256 || !utf8.ValidString(resource) || strings.ContainsAny(resource, "\x00\r\n") || len(payload) == 0 || len(payload) > 32 {
		return FundsAction{}, ErrFundsInvalid
	}
	for k, v := range payload {
		if !fundsKeyPattern.MatchString(k) || len(v) > 4096 || !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return FundsAction{}, ErrFundsInvalid
		}
	}
	raw, e := json.Marshal(struct {
		Version  int               `json:"version"`
		Kind     string            `json:"kind"`
		Resource string            `json:"resource"`
		Payload  map[string]string `json:"payload"`
	}{1, kind, resource, payload})
	if e != nil || len(raw) > 20000 {
		return FundsAction{}, ErrFundsInvalid
	}
	return FundsAction{Kind: kind, ResourceID: resource, digest: sha256.Sum256(raw)}, nil
}
func (a FundsAction) Digest() []byte { return append([]byte(nil), a.digest[:]...) }
func (a FundsAction) valid() bool {
	return fundsKindPattern.MatchString(a.Kind) && a.ResourceID != "" && a.digest != [32]byte{}
}

type fundsAccount struct {
	hash           string
	sessionExpiry  time.Time
	credentialHash string
	epoch          int64
	configured     bool
}

func fundsActorTx(ctx context.Context, tx pgx.Tx, a FundsActor) (fundsAccount, error) {
	var out fundsAccount
	if a.User.Role != "admin" || a.User.ID <= 0 || a.User.AuthVersion <= 0 || a.User.Username == "" || len(a.SessionToken) != 43 || a.Delegated {
		return out, ErrFundsAuthentication
	}
	var id, version int64
	var class string
	e := tx.QueryRow(ctx, `SELECT subject_id,auth_version,password_hash,admin_class FROM console_users WHERE username=$1 AND role='admin' AND enabled FOR SHARE`, a.User.Username).Scan(&id, &version, &out.hash, &class)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			e = ErrFundsAuthentication
		}
		return out, e
	}
	if id != a.User.ID || version != a.User.AuthVersion || (class != "super" && class != "operator") {
		return out, ErrFundsAuthentication
	}
	var sid, sv int64
	var user string
	e = tx.QueryRow(ctx, `SELECT username,subject_id,auth_version,expires_at FROM console_sessions WHERE token_hash=$1 AND role='admin' FOR SHARE`, tokenDigest(a.SessionToken)).Scan(&user, &sid, &sv, &out.sessionExpiry)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			e = ErrFundsAuthentication
		}
		return out, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return out, e
	}
	if user != a.User.Username || sid != id || sv != version || !now.Before(out.sessionExpiry) {
		return out, ErrFundsAuthentication
	}
	var delegated bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM console_session_issuers WHERE token_hash=$1)", tokenDigest(a.SessionToken)).Scan(&delegated); e != nil {
		return out, e
	}
	if delegated {
		return out, ErrFundsAuthentication
	}
	return out, nil
}
func fundsCredentialTx(ctx context.Context, tx pgx.Tx, a FundsActor, out *fundsAccount, write bool) error {
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	var enabled bool
	e := tx.QueryRow(ctx, "SELECT password_hash,credential_epoch,enabled FROM admin_funds_credentials WHERE username=$1"+lock, a.User.Username).Scan(&out.credentialHash, &out.epoch, &enabled)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	out.configured = enabled
	return nil
}
func fundsAudit(ctx context.Context, tx pgx.Tx, a FundsActor, operation string, action FundsAction, epoch int64, key string) error {
	var digest any
	if action.valid() {
		digest = action.digest[:]
	}
	_, e := tx.Exec(ctx, `INSERT INTO admin_funds_audit(username,subject_id,session_hash,operation,action_kind,resource_id,action_digest,credential_epoch,operation_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.User.Username, a.User.ID, tokenDigest(a.SessionToken), operation, action.Kind, action.ResourceID, digest, epoch, key)
	return e
}

func FundsEnforcedTx(ctx context.Context, q FundsQuerier) (bool, error) {
	var value string
	e := q.QueryRow(ctx, `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='admin_funds_authorization_enabled'`).Scan(&value)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if value != "true" && value != "false" {
		return false, ErrFundsInvalid
	}
	return value == "true", nil
}
func (f *FundsAuthorization) Status(ctx context.Context, a FundsActor) (FundsStatus, error) {
	var out FundsStatus
	if f == nil || f.pool == nil {
		return out, ErrFundsInvalid
	}
	tx, e := f.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	acct, e := fundsActorTx(ctx, tx, a)
	if e != nil {
		return out, e
	}
	if e = fundsCredentialTx(ctx, tx, a, &acct, false); e != nil {
		return out, e
	}
	out.Configured = acct.configured
	out.CredentialEpoch = acct.epoch
	out.Enforced, e = FundsEnforcedTx(ctx, tx)
	if e != nil {
		return out, e
	}
	e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM console_mfa WHERE username=$1 AND role='admin' AND secret IS NOT NULL)", a.User.Username).Scan(&out.TOTPRequired)
	return out, e
}

func fundsIP(raw string) string {
	p, e := netip.ParseAddr(raw)
	if e != nil {
		return "invalid-remote"
	}
	return p.Unmap().String()
}

// Throttle rows are locked in a stable order until verification commits. No
// successful verification or credential/session rotation resets these windows.
func fundsBudget(ctx context.Context, tx pgx.Tx, a FundsActor) ([][]byte, error) {
	keys := [][]byte{}
	for _, suffix := range []string{"account:" + a.User.Username, "account-ip:" + a.User.Username + ":" + fundsIP(a.ClientIP)} {
		h := sha256.Sum256([]byte("goepay/funds/v1/" + suffix))
		keys = append(keys, h[:])
	}
	for i, key := range keys {
		var attempts, failures int
		e := tx.QueryRow(ctx, `INSERT INTO admin_funds_throttle(key_hash,window_start,attempts,failures,updated_at) VALUES($1,clock_timestamp(),1,0,clock_timestamp())
   ON CONFLICT(key_hash) DO UPDATE SET
   window_start=CASE WHEN admin_funds_throttle.window_start<=clock_timestamp()-interval '15 minutes' THEN clock_timestamp() ELSE admin_funds_throttle.window_start END,
   attempts=CASE WHEN admin_funds_throttle.window_start<=clock_timestamp()-interval '15 minutes' THEN 1 ELSE LEAST(admin_funds_throttle.attempts+1,301) END,
   failures=CASE WHEN admin_funds_throttle.window_start<=clock_timestamp()-interval '15 minutes' THEN 0 ELSE admin_funds_throttle.failures END,
   updated_at=clock_timestamp() RETURNING attempts,failures`, key).Scan(&attempts, &failures)
		if e != nil {
			return nil, e
		}
		limit := 300
		if i == 1 {
			limit = 150
		}
		if attempts > limit || failures >= 5 {
			return keys, ErrFundsRateLimited
		}
	}
	return keys, nil
}
func fundsFailure(ctx context.Context, tx pgx.Tx, a FundsActor, keys [][]byte, action FundsAction, epoch int64) error {
	for _, key := range keys {
		if _, e := tx.Exec(ctx, "UPDATE admin_funds_throttle SET failures=LEAST(failures+1,6),updated_at=clock_timestamp() WHERE key_hash=$1", key); e != nil {
			return e
		}
	}
	if e := fundsAudit(ctx, tx, a, "verification-failed", action, epoch, ""); e != nil {
		return e
	}
	if e := tx.Commit(ctx); e != nil {
		return e
	}
	return ErrFundsAuthentication
}
func fundsLimited(ctx context.Context, tx pgx.Tx, a FundsActor, epoch int64) error {
	if e := fundsAudit(ctx, tx, a, "verification-limited", FundsAction{}, epoch, ""); e != nil {
		return e
	}
	if e := tx.Commit(ctx); e != nil {
		return e
	}
	return ErrFundsRateLimited
}
func claimFundsVerification() bool {
	select {
	case fundsVerificationSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

// verifyExistingTOTP participates in this credential transaction, including
// replay protection. An enabled MFA row with no available key is fail-closed.
func (f *FundsAuthorization) verifyExistingTOTP(ctx context.Context, tx pgx.Tx, a FundsActor, code string) error {
	var raw []byte
	var last int64
	e := tx.QueryRow(ctx, "SELECT secret,last_counter FROM console_mfa WHERE username=$1 AND role='admin' FOR UPDATE", a.User.Username).Scan(&raw, &last)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && raw == nil {
		return nil
	}
	if e != nil {
		return e
	}
	if f.mfa == nil {
		return ErrMFA
	}
	secret, e := f.mfa.open(raw, a.User.Username, "admin")
	if e != nil {
		return ErrMFA
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return e
	}
	counter, ok := matchTOTP(secret, code, now)
	if !ok || counter <= last {
		return ErrMFA
	}
	_, e = tx.Exec(ctx, "UPDATE console_mfa SET last_counter=$2 WHERE username=$1 AND role='admin'", a.User.Username, counter)
	return e
}

func (f *FundsAuthorization) SetPassword(ctx context.Context, a FundsActor, login, old, next, code string) error {
	if f == nil || f.pool == nil || len(login) > 1024 || len(old) > 1024 || len(next) < 12 || len(next) > 1024 || next == login || !utf8.ValidString(next) || strings.ContainsRune(next, 0) {
		return ErrFundsInvalid
	}
	if !claimFundsVerification() {
		return ErrFundsBusy
	}
	defer func() { <-fundsVerificationSlots }()
	tx, e := f.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	acct, e := fundsActorTx(ctx, tx, a)
	if e != nil {
		return e
	}
	keys, e := fundsBudget(ctx, tx, a)
	if errors.Is(e, ErrFundsRateLimited) {
		return fundsLimited(ctx, tx, a, acct.epoch)
	}
	if e != nil {
		return e
	}
	if e = fundsCredentialTx(ctx, tx, a, &acct, true); e != nil {
		return e
	}
	if !CheckPassword(acct.hash, login) || CheckPassword(acct.hash, next) || acct.configured && !CheckPassword(acct.credentialHash, old) {
		return fundsFailure(ctx, tx, a, keys, FundsAction{}, acct.epoch)
	}
	if e = f.verifyExistingTOTP(ctx, tx, a, code); e != nil {
		if errors.Is(e, ErrMFA) {
			return fundsFailure(ctx, tx, a, keys, FundsAction{}, acct.epoch)
		}
		return e
	}
	hash, e := HashPassword(next)
	if e != nil {
		return e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return e
	}
	if !now.Before(acct.sessionExpiry) {
		return ErrFundsAuthentication
	}
	e = tx.QueryRow(ctx, `INSERT INTO admin_funds_credentials(username,password_hash) VALUES($1,$2) ON CONFLICT(username) DO UPDATE SET password_hash=EXCLUDED.password_hash,enabled=true RETURNING credential_epoch`, a.User.Username, hash).Scan(&acct.epoch)
	if e != nil {
		return e
	}
	if e = fundsAudit(ctx, tx, a, "credential-set", FundsAction{}, acct.epoch, ""); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (f *FundsAuthorization) Prepare(ctx context.Context, a FundsActor, action FundsAction) (FundsChallenge, error) {
	var out FundsChallenge
	if f == nil || f.pool == nil || !action.valid() {
		return out, ErrFundsInvalid
	}
	tx, e := f.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	acct, e := fundsActorTx(ctx, tx, a)
	if e != nil {
		return out, e
	}
	if e = fundsCredentialTx(ctx, tx, a, &acct, false); e != nil {
		return out, e
	}
	if !acct.configured {
		return out, ErrFundsNotConfigured
	}
	// Serialize per session to bound outstanding state across all replicas.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,41))", a.SessionToken); e != nil {
		return out, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return out, e
	}
	var active int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM admin_funds_challenges WHERE session_hash=$1 AND expires_at>$2 AND confirmed_at IS NULL", tokenDigest(a.SessionToken), now).Scan(&active); e != nil {
		return out, e
	}
	if active >= 8 {
		return out, ErrFundsRateLimited
	}
	token, e := randomToken()
	if e != nil {
		return out, e
	}
	expires := now.Add(2 * time.Minute)
	if acct.sessionExpiry.Before(expires) {
		expires = acct.sessionExpiry
	}
	if !now.Before(expires) {
		return out, ErrFundsAuthentication
	}
	_, e = tx.Exec(ctx, `INSERT INTO admin_funds_challenges(challenge_hash,session_hash,username,subject_id,auth_version,credential_epoch,action_kind,resource_id,action_digest,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, tokenDigest(token), tokenDigest(a.SessionToken), a.User.Username, a.User.ID, a.User.AuthVersion, acct.epoch, action.Kind, action.ResourceID, action.digest[:], now, expires)
	if e != nil {
		return out, e
	}
	if e = fundsAudit(ctx, tx, a, "challenge", action, acct.epoch, ""); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return FundsChallenge{token, expires}, nil
}

func (f *FundsAuthorization) Authorize(ctx context.Context, a FundsActor, challenge, password string) (FundsGrant, error) {
	var out FundsGrant
	if f == nil || f.pool == nil || len(challenge) != 43 || len(password) > 1024 {
		return out, ErrFundsInvalid
	}
	if !claimFundsVerification() {
		return out, ErrFundsBusy
	}
	defer func() { <-fundsVerificationSlots }()
	tx, e := f.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	acct, e := fundsActorTx(ctx, tx, a)
	if e != nil {
		return out, e
	}
	keys, e := fundsBudget(ctx, tx, a)
	if errors.Is(e, ErrFundsRateLimited) {
		return out, fundsLimited(ctx, tx, a, acct.epoch)
	}
	if e != nil {
		return out, e
	}
	if e = fundsCredentialTx(ctx, tx, a, &acct, false); e != nil {
		return out, e
	}
	if !acct.configured {
		return out, ErrFundsNotConfigured
	}
	var action FundsAction
	var digest []byte
	var expires time.Time
	var confirmed *time.Time
	var epoch, version, id int64
	var user string
	var session []byte
	e = tx.QueryRow(ctx, `SELECT action_kind,resource_id,action_digest,session_hash,username,subject_id,auth_version,credential_epoch,expires_at,confirmed_at FROM admin_funds_challenges WHERE challenge_hash=$1 FOR UPDATE`, tokenDigest(challenge)).Scan(&action.Kind, &action.ResourceID, &digest, &session, &user, &id, &version, &epoch, &expires, &confirmed)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, fundsFailure(ctx, tx, a, keys, FundsAction{}, acct.epoch)
	}
	if e != nil {
		return out, e
	}
	copy(action.digest[:], digest)
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return out, e
	}
	if confirmed != nil || !now.Before(expires) || user != a.User.Username || id != a.User.ID || version != a.User.AuthVersion || epoch != acct.epoch || !equalFundsDigest(session, tokenDigest(a.SessionToken)) {
		return out, ErrFundsAuthorization
	}
	if !CheckPassword(acct.credentialHash, password) {
		return out, fundsFailure(ctx, tx, a, keys, action, acct.epoch)
	}
	// Argon2 may run across a deadline. Re-read wall clock after it, not transaction time.
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return out, e
	}
	if !now.Before(expires) || !now.Before(acct.sessionExpiry) {
		return out, ErrFundsAuthorization
	}
	token, e := randomToken()
	if e != nil {
		return out, e
	}
	until := now.Add(5 * time.Minute)
	if acct.sessionExpiry.Before(until) {
		until = acct.sessionExpiry
	}
	_, e = tx.Exec(ctx, `INSERT INTO admin_funds_grants(grant_hash,challenge_hash,session_hash,username,subject_id,auth_version,credential_epoch,action_kind,resource_id,action_digest,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tokenDigest(token), tokenDigest(challenge), session, user, id, version, epoch, action.Kind, action.ResourceID, digest, now, until)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "UPDATE admin_funds_challenges SET confirmed_at=$2 WHERE challenge_hash=$1", tokenDigest(challenge), now); e != nil {
		return out, e
	}
	if e = fundsAudit(ctx, tx, a, "authorize", action, epoch, ""); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return FundsGrant{token, until}, nil
}
func equalFundsDigest(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// ConsumeTx must run inside the first immutable business intent/reservation
// transaction. It never commits. Rollback restores the grant. Existing intents
// must be returned before this hook; even the same operation key cannot replay.
func (f *FundsAuthorization) ConsumeTx(ctx context.Context, tx pgx.Tx, a FundsActor, token string, action FundsAction, operationKey string) error {
	enforced, e := FundsEnforcedTx(ctx, tx)
	if e != nil {
		return e
	}
	if !enforced {
		return nil
	}
	return f.ConsumeRequiredTx(ctx, tx, a, token, action, operationKey)
}

// ConsumeRequiredTx always validates and consumes a real one-use grant. A
// persistent scheduler authority must not inherit the optional manual-action
// policy switch. The caller still owns the business transaction and rollback.
func (f *FundsAuthorization) ConsumeRequiredTx(ctx context.Context, tx pgx.Tx, a FundsActor, token string, action FundsAction, operationKey string) error {
	if !action.valid() || len(operationKey) < 1 || len(operationKey) > 256 || strings.ContainsAny(operationKey, "\x00\r\n") || len(token) != 43 {
		return ErrFundsAuthorization
	}
	acct, e := fundsActorTx(ctx, tx, a)
	if e != nil {
		return e
	}
	if e = fundsCredentialTx(ctx, tx, a, &acct, false); e != nil {
		return e
	}
	if !acct.configured {
		return ErrFundsNotConfigured
	}
	var session, digest []byte
	var user, kind, resource string
	var id, version, epoch int64
	var expires time.Time
	var consumed *time.Time
	e = tx.QueryRow(ctx, `SELECT session_hash,username,subject_id,auth_version,credential_epoch,action_kind,resource_id,action_digest,expires_at,consumed_at FROM admin_funds_grants WHERE grant_hash=$1 FOR UPDATE`, tokenDigest(token)).Scan(&session, &user, &id, &version, &epoch, &kind, &resource, &digest, &expires, &consumed)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrFundsAuthorization
	}
	if e != nil {
		return e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return e
	}
	if consumed != nil || !now.Before(expires) || !now.Before(acct.sessionExpiry) || user != a.User.Username || id != a.User.ID || version != a.User.AuthVersion || epoch != acct.epoch || kind != action.Kind || resource != action.ResourceID || !equalFundsDigest(session, tokenDigest(a.SessionToken)) || !equalFundsDigest(digest, action.digest[:]) {
		return ErrFundsAuthorization
	}
	if _, e = tx.Exec(ctx, "UPDATE admin_funds_grants SET consumed_at=$2,operation_key=$3 WHERE grant_hash=$1", tokenDigest(token), now, operationKey); e != nil {
		return e
	}
	return fundsAudit(ctx, tx, a, "consume", action, epoch, operationKey)
}
