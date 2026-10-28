// Package proxyconfig owns the 中转代理 secrets (original proxy_pwd and
// proxy_apikey) without ever returning them to the console. They are stored
// AEAD-encrypted in a single versioned row, like captcha_credentials203. The
// non-secret proxy dimensions (mode, server, port, user, protocol, API url)
// remain ordinary backoffice settings.
package proxyconfig

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
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
)

var ErrUnavailable = errors.New("proxy credential settings unavailable")
var ErrInvalid = errors.New("invalid proxy credential operation")
var ErrConflict = errors.New("proxy credential settings changed")

type Actor struct {
	User        identity.User
	Token, CSRF string
	Delegated   bool
}

// View never carries a secret value: only whether each one is set.
type View struct {
	PasswordSet bool   `json:"password_set"`
	APIKeySet   bool   `json:"apikey_set"`
	Fingerprint string `json:"fingerprint"`
}

// Update replaces or clears each secret independently. An omitted field keeps
// its stored value; the original password and API key are never echoed.
type Update struct {
	Password    *string `json:"password,omitempty"`
	APIKey      *string `json:"apikey,omitempty"`
	ClearPwd    bool    `json:"clear_password,omitempty"`
	ClearAPIKey bool    `json:"clear_apikey,omitempty"`
	Expected    string  `json:"expected_fingerprint"`
}

// Secrets are the decrypted values, for the notify sender only.
type Secrets struct{ Password, APIKey string }

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
		mac.Write([]byte("goepay/proxyconfig/" + label))
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

// FromMaster derives the checkout signing key from the 32-byte deployment
// master (the same derivation cmd/api and mailaccount195 use), so a worker
// with only the master can read the proxy secrets.
func FromMaster(pool *pgxpool.Pool, master []byte) (*Store, error) {
	if len(master) != 32 {
		return nil, ErrUnavailable
	}
	key := sha256.Sum256(append([]byte("checkout-signing:"), master...))
	return NewStore(pool, key[:])
}

func (s *Store) seal(revision int64, pwd, apikey []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(strconv.FormatInt(revision, 10) + "\n"))
	mac.Write([]byte(strconv.Itoa(len(pwd)) + "\n"))
	mac.Write(pwd)
	mac.Write(apikey)
	return mac.Sum(nil)
}
func (s *Store) fingerprint(revision int64, pwd, apikey []byte) string {
	return hex.EncodeToString(s.seal(revision, pwd, apikey))
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

func (s *Store) readTx(ctx context.Context, tx pgx.Tx) (int64, []byte, []byte, error) {
	var revision int64
	var pwd, apikey, proof []byte
	e := tx.QueryRow(ctx, "SELECT revision,pwd_cipher,apikey_cipher,proof_hash FROM proxy_credentials WHERE id=true FOR SHARE").Scan(&revision, &pwd, &apikey, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, nil, nil, nil
	}
	if e != nil {
		return 0, nil, nil, e
	}
	if revision < 1 || !hmac.Equal(proof, s.seal(revision, pwd, apikey)) {
		return 0, nil, nil, ErrConflict
	}
	return revision, pwd, apikey, nil
}

func (s *Store) viewTx(ctx context.Context, tx pgx.Tx) (View, error) {
	revision, pwd, apikey, e := s.readTx(ctx, tx)
	if e != nil {
		return View{}, e
	}
	return View{PasswordSet: len(pwd) > 0, APIKeySet: len(apikey) > 0, Fingerprint: s.fingerprint(revision, pwd, apikey)}, nil
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
	if len(v) < 1 || len(v) > 256 {
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
	if len(in.Expected) != 64 || (in.Password != nil && in.ClearPwd) || (in.APIKey != nil && in.ClearAPIKey) {
		return View{}, ErrInvalid
	}
	if (in.Password != nil && !validSecret(*in.Password)) || (in.APIKey != nil && !validSecret(*in.APIKey)) {
		return View{}, ErrInvalid
	}
	if in.Password == nil && in.APIKey == nil && !in.ClearPwd && !in.ClearAPIKey {
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
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('proxy-credential',128))"); e != nil {
		return View{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	revision, pwd, apikey, e := s.readTx(ctx, tx)
	if e != nil {
		return View{}, e
	}
	if !hmac.Equal([]byte(in.Expected), []byte(s.fingerprint(revision, pwd, apikey))) {
		return View{}, ErrConflict
	}
	switch {
	case in.Password != nil:
		if pwd, e = s.encrypt("proxy-pwd", *in.Password); e != nil {
			return View{}, e
		}
	case in.ClearPwd:
		pwd = []byte{}
	}
	switch {
	case in.APIKey != nil:
		if apikey, e = s.encrypt("proxy-apikey", *in.APIKey); e != nil {
			return View{}, e
		}
	case in.ClearAPIKey:
		apikey = []byte{}
	}
	revision++
	if pwd == nil {
		pwd = []byte{}
	}
	if apikey == nil {
		apikey = []byte{}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO proxy_credentials(id,revision,pwd_cipher,apikey_cipher,proof_hash) VALUES(true,$1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET revision=EXCLUDED.revision,pwd_cipher=EXCLUDED.pwd_cipher,apikey_cipher=EXCLUDED.apikey_cipher,proof_hash=EXCLUDED.proof_hash,updated_at=clock_timestamp()`, revision, pwd, apikey, s.seal(revision, pwd, apikey)); e != nil {
		return View{}, e
	}
	changes, _ := json.Marshal(map[string]any{"password_set": len(pwd) > 0, "apikey_set": len(apikey) > 0, "revision": strconv.FormatInt(revision, 10)})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'proxy-credentials',1,'update',$3::jsonb)`, a.User.ID, a.User.Username, string(changes)); e != nil {
		return View{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return View{}, e
	}
	return View{PasswordSet: len(pwd) > 0, APIKeySet: len(apikey) > 0, Fingerprint: s.fingerprint(revision, pwd, apikey)}, nil
}

// Resolve returns the decrypted secrets for the async-notify sender. It takes
// no actor: it is a server-internal read, never reachable from the console.
func (s *Store) Resolve(ctx context.Context) (Secrets, error) {
	if s.pool == nil {
		return Secrets{}, ErrUnavailable
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Secrets{}, e
	}
	defer tx.Rollback(ctx)
	_, pwd, apikey, e := s.readTx(ctx, tx)
	if e != nil {
		return Secrets{}, e
	}
	password, e := s.decrypt("proxy-pwd", pwd)
	if e != nil {
		return Secrets{}, e
	}
	key, e := s.decrypt("proxy-apikey", apikey)
	if e != nil {
		return Secrets{}, e
	}
	return Secrets{Password: password, APIKey: key}, tx.Commit(ctx)
}
