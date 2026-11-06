package captcha

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

type Actor struct {
	User        identity.User
	Token, CSRF string
	Delegated   bool
}
type KeyView struct {
	Configured  bool   `json:"configured"`
	Fingerprint string `json:"fingerprint"`
}
type KeyUpdate struct {
	Action   string `json:"action"`
	Key      string `json:"key,omitempty"`
	Expected string `json:"expected_fingerprint"`
}
type Store struct {
	pool              *pgxpool.Pool
	aead              cipher.AEAD
	key               []byte
	legacyProofKey205 []byte
}

func NewStore(pool *pgxpool.Pool, key []byte) (*Store, error) {
	if pool == nil || len(key) < 32 {
		return nil, ErrUnavailable
	}
	derive := func(label string) []byte {
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte("goepay/captcha203/" + label))
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
	legacy := hmac.New(sha256.New, key)
	legacy.Write([]byte("goepay/public-testpay203/integrity"))
	return &Store{pool: pool, aead: aead, key: derive("integrity"), legacyProofKey205: legacy.Sum(nil)}, nil
}
func (s *Store) seal(revision int64, raw []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(strconv.FormatInt(revision, 10) + "\n"))
	mac.Write(raw)
	return mac.Sum(nil)
}
func (s *Store) fingerprint(revision int64, raw []byte) string {
	return hex.EncodeToString(s.seal(revision, raw))
}
func (s *Store) ReadTx(ctx context.Context, tx pgx.Tx) (KeyView, string, error) {
	var revision int64
	var raw, proof []byte
	e := tx.QueryRow(ctx, "SELECT revision,ciphertext,proof_hash FROM captcha_credentials203 WHERE id=true FOR SHARE").Scan(&revision, &raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return KeyView{Fingerprint: s.fingerprint(0, nil)}, "", nil
	}
	if e != nil {
		return KeyView{}, "", e
	}
	if revision < 1 || !hmac.Equal(proof, s.seal(revision, raw)) {
		return KeyView{}, "", ErrConflict
	}
	view := KeyView{Configured: len(raw) > 0, Fingerprint: s.fingerprint(revision, raw)}
	if len(raw) == 0 {
		return view, "", nil
	}
	if len(raw) <= s.aead.NonceSize() {
		return KeyView{}, "", ErrConflict
	}
	key, e := s.aead.Open(nil, raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():], []byte("geetest203"))
	if e != nil {
		return KeyView{}, "", ErrConflict
	}
	if len(key) < 16 || len(key) > 256 {
		return KeyView{}, "", ErrConflict
	}
	return view, string(key), nil
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
func (s *Store) Read(ctx context.Context, a Actor) (KeyView, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return KeyView{}, e
	}
	defer tx.Rollback(ctx)
	if e = s.actor(ctx, tx, a); e != nil {
		return KeyView{}, e
	}
	out, _, e := s.ReadTx(ctx, tx)
	if e == nil {
		e = s.actor(ctx, tx, a)
	}
	return out, e
}
func (s *Store) Update(ctx context.Context, a Actor, in KeyUpdate) (KeyView, error) {
	if len(in.Expected) != 64 || in.Action != "replace" && in.Action != "clear" || in.Action == "replace" && (len(in.Key) < 16 || len(in.Key) > 256) || in.Action == "clear" && in.Key != "" {
		return KeyView{}, ErrInvalid
	}
	for _, c := range in.Key {
		if c < 0x21 || c > 0x7e {
			return KeyView{}, ErrInvalid
		}
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return KeyView{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('captcha-credential203',203))"); e != nil {
		return KeyView{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return KeyView{}, e
	}
	old, _, e := s.ReadTx(ctx, tx)
	if e != nil {
		return KeyView{}, e
	}
	if !hmac.Equal([]byte(old.Fingerprint), []byte(in.Expected)) {
		return KeyView{}, ErrConflict
	}
	var revision int64
	if e = tx.QueryRow(ctx, "SELECT COALESCE((SELECT revision FROM captcha_credentials203 WHERE id=true),0)+1").Scan(&revision); e != nil {
		return KeyView{}, e
	}
	raw := []byte{}
	if in.Action == "replace" {
		nonce := make([]byte, s.aead.NonceSize())
		if _, e = rand.Read(nonce); e != nil {
			return KeyView{}, e
		}
		raw = append(nonce, s.aead.Seal(nil, nonce, []byte(in.Key), []byte("geetest203"))...)
	}
	if _, e = tx.Exec(ctx, `INSERT INTO captcha_credentials203(id,revision,ciphertext,proof_hash)VALUES(true,$1,$2,$3) ON CONFLICT(id)DO UPDATE SET revision=EXCLUDED.revision,ciphertext=EXCLUDED.ciphertext,proof_hash=EXCLUDED.proof_hash,updated_at=clock_timestamp()`, revision, raw, s.seal(revision, raw)); e != nil {
		return KeyView{}, e
	}
	changes, _ := json.Marshal(map[string]any{"configured": in.Action == "replace", "revision": strconv.FormatInt(revision, 10)})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,'captcha-credentials',1,$3,$4::jsonb)`, a.User.ID, a.User.Username, in.Action, string(changes)); e != nil {
		return KeyView{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return KeyView{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return KeyView{}, e
	}
	return KeyView{Configured: len(raw) > 0, Fingerprint: s.fingerprint(revision, raw)}, nil
}
