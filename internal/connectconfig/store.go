// Package connectconfig owns the 快捷登录 provider secrets (the original
// login_qq_appkey / login_appkey / login_tg_bot_token) without ever returning
// them to the console. They are stored AEAD-encrypted in a single versioned
// row, like proxyconfig / captcha_credentials203. The non-secret dimensions
// (enable switches, appids, relay API url) remain ordinary backoffice settings.
package connectconfig

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
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
)

var ErrUnavailable = errors.New("oauth credential settings unavailable")
var ErrInvalid = errors.New("invalid oauth credential operation")
var ErrConflict = errors.New("oauth credential settings changed")

// Fields are the four secret slots, in a fixed order used for sealing and the
// stored columns. The field name is the console-facing identifier.
var Fields = []string{"qq", "wx", "alipay", "telegram"}

func validField(f string) bool {
	for _, k := range Fields {
		if k == f {
			return true
		}
	}
	return false
}

type Actor struct {
	User        identity.User
	Token, CSRF string
	Delegated   bool
}

// View never carries a secret value: only whether each one is set.
type View struct {
	QQSet       bool   `json:"qq_set"`
	WXSet       bool   `json:"wx_set"`
	AlipaySet   bool   `json:"alipay_set"`
	TelegramSet bool   `json:"telegram_set"`
	Fingerprint string `json:"fingerprint"`
}

// Update replaces or clears one secret. Exactly one field is addressed; an
// omitted field keeps its stored value and the original is never echoed.
type Update struct {
	Field    string  // one of Fields
	Value    *string // non-nil to replace
	Clear    bool    // true to clear
	Expected string  // expected fingerprint
}

type Store struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
	key  []byte
}

func NewStore(pool *pgxpool.Pool, checkoutKey []byte) (*Store, error) {
	if pool == nil || len(checkoutKey) < 32 {
		return nil, ErrUnavailable
	}
	derive := func(label string) []byte {
		mac := hmac.New(sha256.New, checkoutKey)
		mac.Write([]byte("goepay/connectconfig/" + label))
		return mac.Sum(nil)
	}
	block, e := aes.NewCipher(derive("encryption"))
	if e != nil {
		return nil, e
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	return &Store{pool: pool, aead: aead, key: derive("integrity")}, nil
}

// slots holds the four ciphertexts in Fields order.
type slots struct{ qq, wx, alipay, tg []byte }

func (s *Store) seal(revision int64, c slots) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(strconv.FormatInt(revision, 10) + "\n"))
	for _, b := range [][]byte{c.qq, c.wx, c.alipay, c.tg} {
		mac.Write([]byte(strconv.Itoa(len(b)) + "\n"))
		mac.Write(b)
	}
	return mac.Sum(nil)
}
func (s *Store) fingerprint(revision int64, c slots) string {
	return hex.EncodeToString(s.seal(revision, c))
}
func (s *Store) encrypt(aad, value string) ([]byte, error) {
	if value == "" {
		return []byte{}, nil
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		return nil, e
	}
	return append(nonce, s.aead.Seal(nil, nonce, []byte(value), []byte(aad))...), nil
}
func (s *Store) decrypt(aad string, raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	if len(raw) <= s.aead.NonceSize() {
		return "", ErrConflict
	}
	plain, e := s.aead.Open(nil, raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():], []byte(aad))
	if e != nil {
		return "", ErrConflict
	}
	return string(plain), nil
}

func aadFor(field string) string { return "oauth-" + field }

func (s *Store) readTx(ctx context.Context, tx pgx.Tx) (int64, slots, error) {
	var revision int64
	var c slots
	var proof []byte
	e := tx.QueryRow(ctx, "SELECT revision,qq_cipher,wx_cipher,alipay_cipher,tg_cipher,proof_hash FROM connect_login_credentials WHERE id=true FOR SHARE").Scan(&revision, &c.qq, &c.wx, &c.alipay, &c.tg, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, slots{}, nil
	}
	if e != nil {
		return 0, slots{}, e
	}
	if revision < 1 || !hmac.Equal(proof, s.seal(revision, c)) {
		return 0, slots{}, ErrConflict
	}
	return revision, c, nil
}

func (s *Store) viewTx(ctx context.Context, tx pgx.Tx) (View, error) {
	revision, c, e := s.readTx(ctx, tx)
	if e != nil {
		return View{}, e
	}
	return View{QQSet: len(c.qq) > 0, WXSet: len(c.wx) > 0, AlipaySet: len(c.alipay) > 0, TelegramSet: len(c.tg) > 0, Fingerprint: s.fingerprint(revision, c)}, nil
}

func (s *Store) actor(ctx context.Context, tx pgx.Tx, a Actor) error {
	if a.Delegated || a.User.Role != "admin" || a.User.AdminClass != "super" {
		return identity.ErrAdminSession
	}
	if e := identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return e
	}
	digest := sha256.Sum256([]byte(a.Token))
	var csrf string
	if e := tx.QueryRow(ctx, "SELECT csrf FROM console_sessions WHERE token_hash=$1 AND role='admin' FOR SHARE", digest[:]).Scan(&csrf); e != nil {
		return identity.ErrAdminSession
	}
	if a.CSRF == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(a.CSRF)) != 1 {
		return identity.ErrAdminSession
	}
	return nil
}

func (s *Store) Read(ctx context.Context, a Actor) (View, error) {
	if s.pool == nil {
		return View{}, ErrUnavailable
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, e
	}
	defer tx.Rollback(ctx)
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	out, e := s.viewTx(ctx, tx)
	if e == nil {
		e = s.actor(ctx, tx, a)
	}
	return out, e
}

func validSecret(v string) bool {
	if len(v) < 1 || len(v) > 512 {
		return false
	}
	for _, c := range v {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

func (s *Store) Update(ctx context.Context, a Actor, in Update) (View, error) {
	if len(in.Expected) != 64 || !validField(in.Field) {
		return View{}, ErrInvalid
	}
	if (in.Value != nil) == in.Clear {
		// exactly one of replace / clear
		return View{}, ErrInvalid
	}
	if in.Value != nil && !validSecret(*in.Value) {
		return View{}, ErrInvalid
	}
	if s.pool == nil {
		return View{}, ErrUnavailable
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('oauth-credential',128))"); e != nil {
		return View{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	revision, c, e := s.readTx(ctx, tx)
	if e != nil {
		return View{}, e
	}
	if !hmac.Equal([]byte(in.Expected), []byte(s.fingerprint(revision, c))) {
		return View{}, ErrConflict
	}
	slot := map[string]*[]byte{"qq": &c.qq, "wx": &c.wx, "alipay": &c.alipay, "telegram": &c.tg}[in.Field]
	switch {
	case in.Value != nil:
		if *slot, e = s.encrypt(aadFor(in.Field), *in.Value); e != nil {
			return View{}, e
		}
	case in.Clear:
		*slot = []byte{}
	}
	revision++
	for _, p := range []*[]byte{&c.qq, &c.wx, &c.alipay, &c.tg} {
		if *p == nil {
			*p = []byte{}
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO connect_login_credentials(id,revision,qq_cipher,wx_cipher,alipay_cipher,tg_cipher,proof_hash) VALUES(true,$1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET revision=EXCLUDED.revision,qq_cipher=EXCLUDED.qq_cipher,wx_cipher=EXCLUDED.wx_cipher,alipay_cipher=EXCLUDED.alipay_cipher,tg_cipher=EXCLUDED.tg_cipher,proof_hash=EXCLUDED.proof_hash,updated_at=clock_timestamp()`, revision, c.qq, c.wx, c.alipay, c.tg, s.seal(revision, c)); e != nil {
		return View{}, e
	}
	changes, _ := json.Marshal(map[string]any{"field": in.Field, "set": in.Value != nil, "revision": strconv.FormatInt(revision, 10)})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'oauth-credentials',1,'update',$3::jsonb)`, a.User.ID, a.User.Username, string(changes)); e != nil {
		return View{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return View{}, e
	}
	return View{QQSet: len(c.qq) > 0, WXSet: len(c.wx) > 0, AlipaySet: len(c.alipay) > 0, TelegramSet: len(c.tg) > 0, Fingerprint: s.fingerprint(revision, c)}, nil
}

// Resolve returns the decrypted secret for one provider field, for the login
// flow only. It takes no actor: a server-internal read, never from the console.
// An empty string means the secret is not configured here (the caller then
// falls back to the legacy plaintext setting).
func (s *Store) Resolve(ctx context.Context, field string) (string, error) {
	if s.pool == nil {
		return "", ErrUnavailable
	}
	if !validField(field) {
		return "", ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	_, c, e := s.readTx(ctx, tx)
	if e != nil {
		return "", e
	}
	raw := map[string][]byte{"qq": c.qq, "wx": c.wx, "alipay": c.alipay, "telegram": c.tg}[field]
	value, e := s.decrypt(aadFor(field), raw)
	if e != nil {
		return "", e
	}
	return value, tx.Commit(ctx)
}

// ResolveTx reuses the caller's claim transaction and shared credential lock.
func (s *Store) ResolveTx(ctx context.Context, tx pgx.Tx, field string) (string, error) {
	if tx == nil || !validField(field) {
		return "", ErrInvalid
	}
	_, c, e := s.readTx(ctx, tx)
	if e != nil {
		return "", e
	}
	raw := map[string][]byte{"qq": c.qq, "wx": c.wx, "alipay": c.alipay, "telegram": c.tg}[field]
	return s.decrypt(aadFor(field), raw)
}

// ConfiguredFields lists which provider secrets are set, sorted, for diagnostics.
func (s *Store) ConfiguredFields(ctx context.Context) ([]string, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	_, c, e := s.readTx(ctx, tx)
	if e != nil {
		return nil, e
	}
	out := []string{}
	for field, raw := range map[string][]byte{"qq": c.qq, "wx": c.wx, "alipay": c.alipay, "telegram": c.tg} {
		if len(raw) > 0 {
			out = append(out, field)
		}
	}
	sort.Strings(out)
	return out, tx.Commit(ctx)
}
