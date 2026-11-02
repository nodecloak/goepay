// Package noticeconfig231 owns encrypted administrator notification accounts.
// It performs no network I/O. Workers resolve a snapshot inside their claim tx.
package noticeconfig231

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/connectconfig"
	"goepay/internal/identity"
	"regexp"
	"strconv"
	"time"
)

var ErrInvalid = errors.New("invalid notification account")
var ErrConflict = errors.New("notification account changed or damaged")

type Actor struct {
	User        identity.User
	Token, CSRF string
	Delegated   bool
}
type Credential struct {
	AppID       string `json:"app_id"`
	Secret      string `json:"secret"`
	Recipient   string `json:"recipient"`
	Phone       string `json:"phone"`
	TokenSource string `json:"token_source"`
}

func (Credential) String() string   { return "notice231.Credential(redacted)" }
func (Credential) GoString() string { return "notice231.Credential(redacted)" }

type View struct {
	Revision        int64  `json:"revision,string"`
	Enabled         bool   `json:"enabled"`
	AppIDMasked     string `json:"app_id_masked"`
	SecretPresent   bool   `json:"secret_present"`
	Bound           bool   `json:"bound"`
	RecipientMasked string `json:"recipient_masked"`
	PhoneMasked     string `json:"phone_masked"`
	TokenSource     string `json:"token_source"`
}
type Input struct {
	ExpectedRevision int64  `json:"expected_revision,string"`
	Enabled          bool   `json:"enabled"`
	Action           string `json:"credential_action"`
	AppID            string `json:"app_id"`
	Secret           string `json:"secret"`
	Phone            string `json:"phone"`
	TokenSource      string `json:"token_source"`
}
type Store struct {
	pool             *pgxpool.Pool
	aead             cipher.AEAD
	key, checkoutKey []byte
}

func New(pool *pgxpool.Pool, key []byte) (*Store, error) {
	if pool == nil || len(key) != 32 {
		return nil, ErrInvalid
	}
	derive := func(label string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte("goepay/notice231/" + label))
		return m.Sum(nil)
	}
	block, e := aes.NewCipher(derive("encryption"))
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	return &Store{pool: pool, aead: a, key: derive("integrity"), checkoutKey: append([]byte(nil), key...)}, nil
}
func FromMaster(pool *pgxpool.Pool, key []byte) (*Store, error) {
	if len(key) != 32 {
		return nil, ErrInvalid
	}
	k := sha256.Sum256(append([]byte("checkout-signing:"), key...))
	return New(pool, k[:])
}
func validKind(k string) bool { return k == "robot" || k == "qqbot" || k == "telegram" }
func printable(v string, max int) bool {
	if v == "" || len(v) > max {
		return false
	}
	for _, c := range v {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

var botID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var phone = regexp.MustCompile(`^\+?[0-9]{5,20}$`)
var chat = regexp.MustCompile(`^@[A-Za-z0-9_]{1,64}$`)
var telegramToken = regexp.MustCompile(`^[1-9][0-9]{0,18}:[A-Za-z0-9_-]{1,256}$`)

func validCredential(k string, c Credential) bool {
	switch k {
	case "robot":
		return (validWebhook(c.AppID) || (c.AppID == "" && phone.MatchString(c.Phone))) && c.Secret == "" && c.Recipient == "" && c.TokenSource == "" && (c.Phone == "" || phone.MatchString(c.Phone))
	case "qqbot":
		return botID.MatchString(c.AppID) && printable(c.Secret, 1024) && (c.Recipient == "" || botID.MatchString(c.Recipient)) && c.Phone == "" && c.TokenSource == ""
	case "telegram":
		n, e := strconv.ParseInt(c.AppID, 10, 64)
		recipientOK := (e == nil && n != 0 && strconv.FormatInt(n, 10) == c.AppID) || chat.MatchString(c.AppID)
		return recipientOK && c.Recipient == "" && c.Phone == "" && ((c.TokenSource == "oauth" && c.Secret == "") || ((c.TokenSource == "" || c.TokenSource == "dedicated") && telegramToken.MatchString(c.Secret)))
	}
	return false
}
func masked(v string) string {
	if v == "" {
		return ""
	}
	if len(v) > 4 {
		return "***" + v[len(v)-4:]
	}
	return "***"
}
func (s *Store) digest(parts ...any) []byte {
	b, _ := json.Marshal(parts)
	m := hmac.New(sha256.New, s.key)
	m.Write(b)
	return m.Sum(nil)
}
func (s *Store) read(ctx context.Context, tx pgx.Tx, k string) (View, Credential, bool, error) {
	var v View
	var c Credential
	var raw, proof []byte
	if !validKind(k) || tx == nil {
		return v, c, false, ErrInvalid
	}
	e := tx.QueryRow(ctx, "SELECT revision,enabled,credential_cipher,proof_hash FROM notice_accounts231 WHERE kind=$1 FOR SHARE", k).Scan(&v.Revision, &v.Enabled, &raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var exists bool
		e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='notice-accounts231' AND changes->>'kind'=$1)", k).Scan(&exists)
		if e == nil && exists {
			e = ErrConflict
		}
		return v, c, exists, e
	}
	if e != nil {
		return v, c, true, e
	}
	if !hmac.Equal(proof, s.digest(k, v.Revision, v.Enabled, raw)) {
		return View{}, c, true, ErrConflict
	}
	var ok bool
	e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE actor_role='admin' AND actor_id>0 AND changes->>'revision'=$2 AND changes->>'proof_hash'=$3)=1 AND count(*) FILTER(WHERE (changes->>'revision')::bigint>$4)=0 FROM backoffice_audit WHERE resource='notice-accounts231' AND changes->>'kind'=$1`, k, strconv.FormatInt(v.Revision, 10), hex.EncodeToString(proof), v.Revision).Scan(&ok)
	if e != nil {
		return View{}, c, true, e
	}
	if !ok {
		return View{}, c, true, ErrConflict
	}
	if len(raw) > 0 {
		n := s.aead.NonceSize()
		if len(raw) <= n {
			return View{}, c, true, ErrConflict
		}
		plain, e := s.aead.Open(nil, raw[:n], raw[n:], []byte(fmt.Sprint(k, "/", v.Revision)))
		if e != nil || json.Unmarshal(plain, &c) != nil || !validCredential(k, c) {
			return View{}, Credential{}, true, ErrConflict
		}
	}
	if v.Enabled && c.AppID == "" {
		return View{}, Credential{}, true, ErrConflict
	}
	v.AppIDMasked = masked(c.AppID)
	v.SecretPresent = c.AppID != ""
	v.Bound = c.Recipient != ""
	v.RecipientMasked = masked(c.Recipient)
	v.PhoneMasked = masked(c.Phone)
	v.TokenSource = c.TokenSource
	return v, c, true, nil
}
func (s *Store) ResolveTx(ctx context.Context, tx pgx.Tx, k string) (View, Credential, bool, error) {
	return s.read(ctx, tx, k)
}
func (s *Store) ChangedAtTx(ctx context.Context, tx pgx.Tx, k string, revision int64) (time.Time, error) {
	var at time.Time
	e := tx.QueryRow(ctx, "SELECT a.created_at FROM backoffice_audit a JOIN notice_accounts231 n ON n.kind=$1 AND n.revision=$3 AND a.changes->>'proof_hash'=encode(n.proof_hash,'hex') WHERE a.resource='notice-accounts231' AND a.actor_role='admin' AND a.actor_id>0 AND a.changes->>'kind'=$1 AND a.changes->>'revision'=$2", k, strconv.FormatInt(revision, 10), revision).Scan(&at)
	if e != nil {
		return at, e
	}
	if k == "telegram" {
		_, c, _, e := s.read(ctx, tx, k)
		if e != nil {
			return at, e
		}
		if c.TokenSource == "oauth" {
			var shared time.Time
			e = tx.QueryRow(ctx, "SELECT COALESCE(max(created_at),'epoch'::timestamptz) FROM backoffice_audit WHERE resource='oauth-credentials' AND operation='update' AND changes->>'field'='telegram'").Scan(&shared)
			if e != nil {
				return at, e
			}
			if shared.After(at) {
				at = shared
			}
		}
	}
	return at, nil
}

// EffectiveTx includes the shared OAuth Telegram token in the private snapshot
// and fingerprint, so rotating it cannot send an old event using a new bot.
func (s *Store) EffectiveTx(ctx context.Context, tx pgx.Tx, k string) (View, Credential, bool, string, error) {
	v, c, m, e := s.read(ctx, tx, k)
	if e != nil {
		return v, c, m, "", e
	}
	if m && k == "telegram" && c.TokenSource == "oauth" {
		config, e := connectconfig.NewStore(s.pool, s.checkoutKey)
		if e != nil {
			return v, Credential{}, m, "", e
		}
		c.Secret, e = config.ResolveTx(ctx, tx, "telegram")
		if e != nil {
			return v, Credential{}, m, "", e
		}
	}
	return v, c, m, hex.EncodeToString(s.digest("effective", k, v.Revision, v.Enabled, c)), nil
}
func (s *Store) Effective(ctx context.Context, k string) (View, Credential, bool, string, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, Credential{}, false, "", e
	}
	defer tx.Rollback(ctx)
	return s.EffectiveTx(ctx, tx, k)
}
func (s *Store) Resolve(ctx context.Context, k string) (View, Credential, bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, Credential{}, false, e
	}
	defer tx.Rollback(ctx)
	return s.read(ctx, tx, k)
}
func (s *Store) actor(ctx context.Context, tx pgx.Tx, a Actor) error {
	if a.Delegated || a.User.Role != "admin" || a.User.AdminClass != "super" {
		return identity.ErrAdminSession
	}
	if e := identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, false); e != nil {
		return e
	}
	d := sha256.Sum256([]byte(a.Token))
	var csrf string
	if e := tx.QueryRow(ctx, "SELECT csrf FROM console_sessions WHERE token_hash=$1 AND role='admin' FOR SHARE", d[:]).Scan(&csrf); e != nil || a.CSRF == "" || subtle.ConstantTimeCompare([]byte(a.CSRF), []byte(csrf)) != 1 {
		return identity.ErrAdminSession
	}
	return nil
}
func (s *Store) Read(ctx context.Context, a Actor, k string) (View, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, e
	}
	defer tx.Rollback(ctx)
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	v, _, _, e := s.read(ctx, tx, k)
	return v, e
}
func (s *Store) write(ctx context.Context, tx pgx.Tx, a Actor, k string, old View, c Credential, enabled bool, op string) (View, error) {
	rev := old.Revision + 1
	raw := []byte{}
	if c != (Credential{}) {
		if !validCredential(k, c) {
			return View{}, ErrInvalid
		}
		plain, _ := json.Marshal(c)
		n := make([]byte, s.aead.NonceSize())
		if _, e := rand.Read(n); e != nil {
			return View{}, e
		}
		raw = s.aead.Seal(n, n, plain, []byte(fmt.Sprint(k, "/", rev)))
	}
	if enabled && (len(raw) == 0 || c.AppID == "") {
		return View{}, ErrInvalid
	}
	proof := s.digest(k, rev, enabled, raw)
	if _, e := tx.Exec(ctx, `INSERT INTO notice_accounts231(kind,revision,enabled,credential_cipher,proof_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT(kind) DO UPDATE SET revision=$2,enabled=$3,credential_cipher=$4,proof_hash=$5`, k, rev, enabled, raw, proof); e != nil {
		return View{}, e
	}
	changes, _ := json.Marshal(map[string]any{"kind": k, "revision": rev, "enabled": enabled, "proof_hash": hex.EncodeToString(proof), "bound": c.Recipient != ""})
	if _, e := tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'notice-accounts231',1,$3,$4)`, a.User.ID, a.User.Username, op, changes); e != nil {
		return View{}, e
	}
	v, _, _, e := s.read(ctx, tx, k)
	return v, e
}
func (s *Store) Update(ctx context.Context, a Actor, k string, in Input) (View, error) {
	if !validKind(k) || in.ExpectedRevision < 0 {
		return View{}, ErrInvalid
	}
	c := Credential{AppID: in.AppID, Secret: in.Secret, Phone: in.Phone, TokenSource: in.TokenSource}
	if in.Action == "replace" {
		if !validCredential(k, c) {
			return View{}, ErrInvalid
		}
	} else if in.Action != "keep" && in.Action != "clear" || c != (Credential{}) {
		return View{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('notice231/'||$1,0))", k); e != nil {
		return View{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	v, old, _, e := s.read(ctx, tx, k)
	if e != nil {
		return View{}, e
	}
	if v.Revision != in.ExpectedRevision {
		return View{}, ErrConflict
	}
	if in.Action == "keep" {
		c = old
	} else if in.Action == "clear" {
		c = Credential{}
	}
	if in.Enabled && k == "telegram" && c.TokenSource == "oauth" {
		config, e := connectconfig.NewStore(s.pool, s.checkoutKey)
		if e != nil {
			return View{}, e
		}
		token, e := config.ResolveTx(ctx, tx, "telegram")
		if e != nil {
			return View{}, e
		}
		if !telegramToken.MatchString(token) {
			return View{}, ErrInvalid
		}
	}
	out, e := s.write(ctx, tx, a, k, v, c, in.Enabled, "update")
	if e != nil {
		return View{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return View{}, e
	}
	return out, nil
}
