// Package logincaptcha admits one merchant password authentication attempt.
// CAPTCHA success never authenticates an account or replaces its MFA check.
package logincaptcha

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/captcha"
	"goepay/internal/identity"
)

var ErrInvalid = captcha.ErrInvalid
var ErrConflict = captcha.ErrConflict
var ErrUnavailable = captcha.ErrUnavailable

const CookieName = "goepay_login205"
const Path = "/console/v1/public/login-captcha"

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var identifier = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Service struct {
	pool        *pgxpool.Pool
	credentials *captcha.Store
	key         []byte
	origin      string
	verifier    captcha.Verifier
	limiter     func(string, string) bool
}

// Origin is the console origin login CAPTCHA proofs are bound to.
func (s *Service) Origin() string {
	if s == nil {
		return ""
	}
	return s.origin
}

func New(pool *pgxpool.Pool, master []byte, origin string, verifier captcha.Verifier, limiter func(string, string) bool) (*Service, error) {
	if pool == nil || len(master) < 32 {
		return nil, ErrUnavailable
	}
	if origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || u.Scheme != "http" && u.Scheme != "https" {
			return nil, ErrUnavailable
		}
		origin = u.Scheme + "://" + u.Host
	}
	credentials, e := captcha.NewStore(pool, master)
	if e != nil {
		return nil, e
	}
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte("goepay/login-captcha205/integrity"))
	return &Service{pool: pool, credentials: credentials, key: mac.Sum(nil), origin: origin, verifier: verifier, limiter: limiter}, nil
}
func (s *Service) sum(purpose string, raw []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(purpose + "\n"))
	mac.Write(raw)
	return mac.Sum(nil)
}
func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, e := rand.Read(raw); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func validUsername(v string) bool {
	return v != "" && len(v) <= 128 && utf8.ValidString(v) && strings.TrimSpace(v) == v && !strings.ContainsAny(v, "\x00\r\n\t")
}
func browser(r *http.Request) (string, error) {
	value := ""
	count := 0
	for _, c := range r.Cookies() {
		if c.Name == CookieName {
			count++
			value = c.Value
		}
	}
	if count != 1 || !tokenPattern.MatchString(value) {
		return "", ErrInvalid
	}
	return value, nil
}
func dbNow(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var now time.Time
	e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now.UTC(), e
}
func alive(ctx context.Context, tx pgx.Tx, until time.Time) error {
	now, e := dbNow(ctx, tx)
	if e != nil {
		return e
	}
	if !now.Before(until) {
		return ErrInvalid
	}
	return nil
}

type policy struct {
	Enabled     bool
	Values      map[string]string
	Fingerprint string
	Config      captcha.Config
}

// SHARE conflicts with every settings INSERT/UPDATE/DELETE, including a first
// insertion when a flag was absent. Hold through commit. Then take credential
// advisory SHARE, matching the vault updater's exclusive advisory before row
// locks. All reads use this tx: no second pool connection or external session I/O.
func (s *Service) policyTx(ctx context.Context, tx pgx.Tx) (policy, error) {
	p := policy{Values: map[string]string{"captcha_open_login": "0", "captcha_version": "0", "captcha_id": ""}}
	if _, e := tx.Exec(ctx, `LOCK TABLE backoffice_records IN SHARE MODE`); e != nil {
		return p, e
	}
	// The table lock already prevents changes through commit. A row lock here
	// would deadlock with a directory writer that locked its row before UPDATE.
	rows, e := tx.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1)`, []string{"captcha_open_login", "captcha_version", "captcha_id"})
	if e != nil {
		return p, e
	}
	seen := map[string]bool{}
	for rows.Next() {
		var k, v string
		var raw []byte
		if e = rows.Scan(&k, &raw); e != nil {
			rows.Close()
			return p, e
		}
		if seen[k] || json.Unmarshal(raw, &v) != nil || strings.TrimSpace(v) != v {
			rows.Close()
			return p, ErrUnavailable
		}
		seen[k] = true
		p.Values[k] = v
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return p, e
	}
	if p.Values["captcha_open_login"] != "0" && p.Values["captcha_open_login"] != "1" || p.Values["captcha_version"] != "0" && p.Values["captcha_version"] != "1" {
		return p, ErrUnavailable
	}
	p.Enabled = p.Values["captcha_open_login"] == "1"
	p.Fingerprint = s.policyFingerprint(p, "")
	return p, nil
}
func (s *Service) policyFingerprint(p policy, credential string) string {
	raw, _ := json.Marshal(struct {
		Values     map[string]string
		Credential string
	}{p.Values, credential})
	return hex.EncodeToString(s.sum("configuration", raw))
}
func (s *Service) fullPolicyTx(ctx context.Context, tx pgx.Tx, p policy) (policy, error) {
	if !p.Enabled {
		return p, nil
	}
	if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended('captcha-credential203',203))`); e != nil {
		return p, e
	}
	view, key, e := s.credentials.ReadTx(ctx, tx)
	if e != nil {
		return p, e
	}
	p.Config = captcha.Config{ID: p.Values["captcha_id"], Key: key}
	if p.Values["captcha_version"] == "1" {
		p.Config.Version = 1
	}
	p.Fingerprint = s.policyFingerprint(p, view.Fingerprint)
	if s.origin == "" || s.verifier == nil || !identifier.MatchString(p.Config.ID) || len(key) < 16 {
		return p, ErrUnavailable
	}
	return p, nil
}

// Grant is only created by Consume. No browser field may reconstruct it.
// The deadline is the original challenge's deadline, never a refreshed lease.
type Grant struct {
	valid, enabled   bool
	username, config string
	token, browser   []byte
	until            time.Time
}
type challenge struct {
	Token, Browser, Account     []byte
	Role, Mode, Purpose, Config string
	Protocol                    captcha.Challenge
	Until                       time.Time
}

func (s *Service) challengeSeal(c challenge) []byte {
	c.Until = c.Until.UTC()
	raw, _ := json.Marshal(c)
	return s.sum("challenge", raw)
}
func (s *Service) readChallenge(ctx context.Context, tx pgx.Tx, token []byte) (challenge, *time.Time, *time.Time, error) {
	var c challenge
	var raw, seal []byte
	var consumed, session *time.Time
	e := tx.QueryRow(ctx, `SELECT token_hash,browser_hash,account_hash,role,mode,purpose,config_hash,protocol,expires_at,consumed_at,session_at,proof_hash FROM login_captcha205_challenges WHERE token_hash=$1 FOR UPDATE`, token).Scan(&c.Token, &c.Browser, &c.Account, &c.Role, &c.Mode, &c.Purpose, &c.Config, &raw, &c.Until, &consumed, &session, &seal)
	if errors.Is(e, pgx.ErrNoRows) {
		return c, nil, nil, ErrInvalid
	}
	if e != nil {
		return c, nil, nil, e
	}
	if json.Unmarshal(raw, &c.Protocol) != nil || !hmac.Equal(seal, s.challengeSeal(c)) || c.Role != "merchant" || c.Mode != "password" || c.Purpose != "login" {
		return c, nil, nil, ErrConflict
	}
	return c, consumed, session, nil
}

// Consume commits the spent proof before credentials are attempted. A wrong
// password/MFA, cancellation or later session failure cannot revive that proof.
func (s *Service) Consume(ctx context.Context, r *http.Request, username string, proof captcha.Proof) (Grant, error) {
	var grant Grant
	if s == nil || !validUsername(username) {
		return grant, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return grant, e
	}
	defer tx.Rollback(ctx)
	p, e := s.policyTx(ctx, tx)
	if e != nil {
		return grant, e
	}
	if !p.Enabled {
		if proof != (captcha.Proof{}) {
			return grant, ErrConflict
		}
		now, e := dbNow(ctx, tx)
		if e != nil {
			return grant, e
		}
		grant = Grant{valid: true, username: username, config: p.Fingerprint, until: now.Add(2 * time.Minute)}
		return grant, tx.Commit(ctx)
	}
	if !tokenPattern.MatchString(proof.CaptchaToken) {
		return grant, ErrInvalid
	}
	p, e = s.fullPolicyTx(ctx, tx, p)
	if e != nil {
		return grant, e
	}
	if r == nil || r.Header.Get("Origin") != s.origin {
		return grant, ErrInvalid
	}
	value, e := browser(r)
	if e != nil {
		return grant, e
	}
	c, consumed, _, e := s.readChallenge(ctx, tx, s.sum("token", []byte(proof.CaptchaToken)))
	if e != nil {
		return grant, e
	}
	if consumed != nil || !hmac.Equal(c.Browser, s.sum("browser", []byte(value))) || !hmac.Equal(c.Account, s.sum("account", []byte(username))) {
		return grant, ErrInvalid
	}
	if c.Config != p.Fingerprint {
		return grant, ErrConflict
	}
	if e = alive(ctx, tx, c.Until); e != nil {
		return grant, e
	}
	source, e := sourceIP(r)
	if e != nil {
		return grant, e
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	e = s.verifier.Verify(verifyCtx, p.Config, c.Protocol, proof, source)
	cancel()
	if e != nil {
		return grant, e
	}
	if e = alive(ctx, tx, c.Until); e != nil {
		return grant, e
	}
	if e = s.credentials.Claim(ctx, tx, p.Config, proof, "login", c.Token); e != nil {
		return grant, e
	}
	if _, e = tx.Exec(ctx, `UPDATE login_captcha205_challenges SET consumed_at=clock_timestamp() WHERE token_hash=$1`, c.Token); e != nil {
		return grant, e
	}
	if e = alive(ctx, tx, c.Until); e != nil {
		return grant, e
	}
	grant = Grant{valid: true, enabled: true, username: username, config: p.Fingerprint, token: c.Token, browser: c.Browser, until: c.Until}
	return grant, tx.Commit(ctx)
}

// IssueSession makes policy/credential rotation and session insertion atomic.
// It performs no network I/O. A grant expires at its original challenge time,
// including time spent checking the password, MFA, locks and all final writes.
func (s *Service) IssueSession(ctx context.Context, g Grant, u identity.User, sessions *identity.Sessions) (string, string, error) {
	if s == nil || !g.valid || g.username != u.Username || u.Role != "merchant" || sessions == nil {
		return "", "", ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return "", "", e
	}
	defer tx.Rollback(ctx)
	p, e := s.policyTx(ctx, tx)
	if e != nil {
		return "", "", e
	}
	if p.Enabled != g.enabled {
		return "", "", ErrConflict
	}
	p, e = s.fullPolicyTx(ctx, tx, p)
	if e != nil {
		return "", "", e
	}
	if p.Enabled != g.enabled || p.Fingerprint != g.config {
		return "", "", ErrConflict
	}
	if e = alive(ctx, tx, g.until); e != nil {
		return "", "", e
	}
	if g.enabled {
		c, consumed, session, e := s.readChallenge(ctx, tx, g.token)
		if e != nil {
			return "", "", e
		}
		if consumed == nil || session != nil || c.Config != g.config || !c.Until.Equal(g.until) || !hmac.Equal(c.Browser, g.browser) || !hmac.Equal(c.Account, s.sum("account", []byte(u.Username))) {
			return "", "", ErrInvalid
		}
	}
	// Match the existing merchant -> durable user lock order used by key login.
	var active bool
	if e = tx.QueryRow(ctx, `SELECT enabled FROM merchants WHERE id=$1 FOR SHARE`, u.ID).Scan(&active); e != nil || !active {
		if e != nil {
			return "", "", e
		}
		return "", "", identity.ErrOriginalMerchantSession
	}
	var subject, version int64
	if e = tx.QueryRow(ctx, `SELECT subject_id,auth_version FROM console_users WHERE username=$1 AND role='merchant' AND enabled FOR SHARE`, u.Username).Scan(&subject, &version); e != nil {
		return "", "", e
	}
	if subject != u.ID || version != u.AuthVersion {
		return "", "", identity.ErrOriginalMerchantSession
	}
	token, csrf, e := sessions.CreateMerchantSessionTx(ctx, tx, u)
	if e != nil {
		return "", "", e
	}
	if g.enabled {
		if _, e = tx.Exec(ctx, `UPDATE login_captcha205_challenges SET session_at=clock_timestamp() WHERE token_hash=$1`, g.token); e != nil {
			return "", "", e
		}
	}
	if e = identity.CheckOriginalMerchantSessionTx(ctx, tx, u, token, csrf); e != nil {
		return "", "", e
	}
	if e = alive(ctx, tx, g.until); e != nil {
		return "", "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", "", e
	}
	return token, csrf, nil
}
