// Package voiceconfig230 owns the cloud-speaker agent pair. The console sees
// masked metadata; only the notification worker can resolve plaintext values.
package voiceconfig230

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
	"goepay/internal/identity"
	"strconv"
)

var ErrInvalid = errors.New("invalid voice configuration")
var ErrConflict = errors.New("voice configuration changed")

type Actor struct {
	User        identity.User
	Token, CSRF string
	Delegated   bool
}
type Credential struct {
	AgentID string `json:"agent_id"`
	Secret  string `json:"secret"`
}

func (Credential) String() string   { return "voice230.Credential(redacted)" }
func (Credential) GoString() string { return "voice230.Credential(redacted)" }

type View struct {
	Revision      int64  `json:"revision,string"`
	Enabled       bool   `json:"enabled"`
	AppIDMasked   string `json:"app_id_masked"`
	SecretPresent bool   `json:"secret_present"`
}
type Input struct {
	ExpectedRevision int64  `json:"expected_revision,string"`
	Enabled          bool   `json:"enabled"`
	Action           string `json:"credential_action"`
	AgentID          string `json:"app_id"`
	Secret           string `json:"secret"`
}
type Store struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
	key  []byte
}

func New(pool *pgxpool.Pool, key []byte) (*Store, error) {
	if pool == nil || len(key) != 32 {
		return nil, ErrInvalid
	}
	derive := func(purpose string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte("goepay/voice230/" + purpose))
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
	return &Store{pool, a, derive("integrity")}, nil
}

// FromMaster matches the checkout signing-key derivation used by the API.
func FromMaster(pool *pgxpool.Pool, master []byte) (*Store, error) {
	if len(master) != 32 {
		return nil, ErrInvalid
	}
	k := sha256.Sum256(append([]byte("checkout-signing:"), master...))
	return New(pool, k[:])
}
func (s *Store) proof(rev int64, enabled bool, raw []byte) []byte {
	m := hmac.New(sha256.New, s.key)
	fmt.Fprintf(m, "voice230/%d/%t/", rev, enabled)
	m.Write(raw)
	return m.Sum(nil)
}
func valid(v string, max int) bool {
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
func (s *Store) read(ctx context.Context, tx pgx.Tx) (View, Credential, bool, error) {
	var v View
	var c Credential
	var raw, proof []byte
	e := tx.QueryRow(ctx, "SELECT revision,enabled,credential_cipher,proof_hash FROM voice_settings230 WHERE id=true FOR SHARE").Scan(&v.Revision, &v.Enabled, &raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var prior bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='voice-settings230')").Scan(&prior); e != nil {
			return v, c, true, e
		}
		if prior {
			return View{}, Credential{}, true, ErrConflict
		}
		return v, c, false, nil
	}
	if e != nil {
		return v, c, false, e
	}
	if v.Revision < 1 || !hmac.Equal(proof, s.proof(v.Revision, v.Enabled, raw)) {
		return View{}, c, true, ErrConflict
	}
	var validAudit bool
	e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE actor_role='admin' AND actor_id>0 AND changes->>'revision'=$1 AND changes->>'proof_hash'=$2 AND changes->>'enabled'=$3)=1 AND count(*) FILTER(WHERE (changes->>'revision')::bigint>$4)=0 FROM backoffice_audit WHERE resource='voice-settings230' AND operation='update'`, strconv.FormatInt(v.Revision, 10), hex.EncodeToString(proof), strconv.FormatBool(v.Enabled), v.Revision).Scan(&validAudit)
	if e != nil {
		return View{}, Credential{}, true, e
	}
	if !validAudit {
		return View{}, Credential{}, true, ErrConflict
	}
	if len(raw) > 0 {
		n := s.aead.NonceSize()
		if len(raw) <= n {
			return View{}, c, true, ErrConflict
		}
		plain, e := s.aead.Open(nil, raw[:n], raw[n:], []byte(fmt.Sprintf("voice230/%d", v.Revision)))
		if e != nil || json.Unmarshal(plain, &c) != nil || !valid(c.AgentID, 128) || !valid(c.Secret, 1024) {
			return View{}, Credential{}, true, ErrConflict
		}
	}
	if v.Enabled && c.Secret == "" {
		return View{}, Credential{}, true, ErrConflict
	}
	v.SecretPresent = c.Secret != ""
	if c.AgentID != "" {
		v.AppIDMasked = "***"
		if len(c.AgentID) > 4 {
			v.AppIDMasked += c.AgentID[len(c.AgentID)-4:]
		}
	}
	return v, c, true, nil
}
func (s *Store) actor(ctx context.Context, tx pgx.Tx, a Actor) error {
	if a.Delegated || a.User.Role != "admin" || a.User.AdminClass != "super" {
		return identity.ErrAdminSession
	}
	if e := identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return e
	}
	d := sha256.Sum256([]byte(a.Token))
	var csrf string
	if e := tx.QueryRow(ctx, "SELECT csrf FROM console_sessions WHERE token_hash=$1 AND role='admin' FOR SHARE", d[:]).Scan(&csrf); e != nil || a.CSRF == "" || subtle.ConstantTimeCompare([]byte(a.CSRF), []byte(csrf)) != 1 {
		return identity.ErrAdminSession
	}
	return nil
}
func (s *Store) Read(ctx context.Context, a Actor) (View, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, e
	}
	defer tx.Rollback(ctx)
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	v, _, _, e := s.read(ctx, tx)
	return v, e
}
func (s *Store) Update(ctx context.Context, a Actor, in Input) (View, error) {
	if in.ExpectedRevision < 0 {
		return View{}, ErrInvalid
	}
	if in.Action != "keep" && in.Action != "replace" && in.Action != "clear" {
		return View{}, ErrInvalid
	}
	if in.Action == "replace" {
		if !valid(in.AgentID, 128) || !valid(in.Secret, 1024) {
			return View{}, ErrInvalid
		}
	} else if in.AgentID != "" || in.Secret != "" {
		return View{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('voice-settings230',0))"); e != nil {
		return View{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	old, c, _, e := s.read(ctx, tx)
	if e != nil {
		return View{}, e
	}
	if in.ExpectedRevision != old.Revision {
		return View{}, ErrConflict
	}
	if in.Action == "replace" {
		c = Credential{in.AgentID, in.Secret}
	} else if in.Action == "clear" {
		c = Credential{}
	}
	if in.Enabled && c.Secret == "" {
		return View{}, ErrInvalid
	}
	rev := old.Revision + 1
	raw := []byte{}
	if c.Secret != "" {
		plain, _ := json.Marshal(c)
		n := make([]byte, s.aead.NonceSize())
		if _, e = rand.Read(n); e != nil {
			return View{}, e
		}
		raw = s.aead.Seal(n, n, plain, []byte(fmt.Sprintf("voice230/%d", rev)))
	}
	if _, e = tx.Exec(ctx, `INSERT INTO voice_settings230(id,revision,enabled,credential_cipher,proof_hash) VALUES(true,$1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET revision=EXCLUDED.revision,enabled=EXCLUDED.enabled,credential_cipher=EXCLUDED.credential_cipher,proof_hash=EXCLUDED.proof_hash,updated_at=clock_timestamp()`, rev, in.Enabled, raw, s.proof(rev, in.Enabled, raw)); e != nil {
		return View{}, e
	}
	changes, _ := json.Marshal(map[string]any{"revision": rev, "enabled": in.Enabled, "credential_action": in.Action, "secret_present": c.Secret != "", "proof_hash": hex.EncodeToString(s.proof(rev, in.Enabled, raw))})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'voice-settings230',1,'update',$3)`, a.User.ID, a.User.Username, changes); e != nil {
		return View{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	v, _, _, e := s.read(ctx, tx)
	if e != nil {
		return View{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return View{}, e
	}
	return v, nil
}

// Resolve distinguishes an unmanaged legacy installation from an explicitly
// disabled or cleared managed configuration. Errors never fall back to env.
func (s *Store) Resolve(ctx context.Context) (View, Credential, bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, Credential{}, false, e
	}
	defer tx.Rollback(ctx)
	return s.read(ctx, tx)
}

// ResolveTx reuses the notice-claim transaction. The shared row lock binds
// enabled and credentials until the claim commits, without nesting pool leases.
func (s *Store) ResolveTx(ctx context.Context, tx pgx.Tx) (View, Credential, bool, error) {
	if tx == nil {
		return View{}, Credential{}, false, ErrInvalid
	}
	return s.read(ctx, tx)
}
