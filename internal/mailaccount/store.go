// Package mailaccount owns the original SMTP account without exposing its
// password or performing network I/O while reading or saving configuration.
package mailaccount

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	"goepay/internal/mailer"
)

var ErrSource = errors.New("mail account source cannot be confirmed")
var ErrInvalid = errors.New("invalid SMTP account")
var ErrConflict = errors.New("mail account changed")

const platformLock = int64(7170140001)

type Config struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
}
type SecretConfig struct {
	Config
	Password string `json:"password"`
}
type View struct {
	Name            string `json:"name"`
	Title           string `json:"title"`
	Status          int    `json:"status"`
	Configured      bool   `json:"configured"`
	Config          Config `json:"config"`
	PasswordSet     bool   `json:"password_set"`
	Revision        string `json:"revision"`
	Fingerprint     string `json:"fingerprint"`
	TestAvailable   bool   `json:"test_available"`
	EffectiveSource string `json:"effective_source"`
}
type Actor struct {
	User      identity.User
	Token     string
	Delegated bool
}
type Input struct {
	Name         string
	Status       int
	Config       Config
	Password     *string
	KeepPassword bool
	Expected     string
}
type facts struct {
	Version    int
	Revision   int64
	Status     int
	Nonce      []byte
	Ciphertext []byte
	ActorID    int64
	ActorName  string
	ChangedAt  time.Time
}
type state struct {
	facts           facts
	proof           []byte
	managed, schema bool
	config          SecretConfig
}
type Store struct {
	pool *pgxpool.Pool
	key  []byte
}

func New(pool *pgxpool.Pool, checkoutKey []byte) *Store {
	return &Store{pool: pool, key: append([]byte(nil), checkoutKey...)}
}
func FromMaster(pool *pgxpool.Pool, master []byte) *Store {
	if len(master) != 32 {
		return New(pool, nil)
	}
	key := sha256.Sum256(append([]byte("checkout-signing:"), master...))
	return New(pool, key[:])
}
func (s *Store) digest(domain string, v any) ([]byte, error) {
	if len(s.key) != 32 {
		return nil, ErrSource
	}
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("goepay/mail-account195/" + domain + "\x00"))
	if e := json.NewEncoder(m).Encode(v); e != nil {
		return nil, e
	}
	return m.Sum(nil), nil
}
func (s *Store) aead() (cipher.AEAD, error) {
	key, e := s.digest("encryption-key", 1)
	if e != nil {
		return nil, e
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func (s *Store) seal(v SecretConfig, revision int64) ([]byte, []byte, error) {
	g, e := s.aead()
	if e != nil {
		return nil, nil, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, nil, e
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return nil, nil, e
	}
	aad := []byte("goepay/mail-account195/smtp/" + strconv.FormatInt(revision, 10))
	return nonce, g.Seal(nil, nonce, raw, aad), nil
}
func (s *Store) open(f facts) (SecretConfig, error) {
	var out SecretConfig
	g, e := s.aead()
	if e != nil || len(f.Nonce) != g.NonceSize() || len(f.Ciphertext) > 8192 {
		return out, ErrSource
	}
	raw, e := g.Open(nil, f.Nonce, f.Ciphertext, []byte("goepay/mail-account195/smtp/"+strconv.FormatInt(f.Revision, 10)))
	if e != nil {
		return out, ErrSource
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || decoder.Decode(new(any)) != io.EOF {
		return SecretConfig{}, ErrSource
	}
	if _, e = SMTPConfig(out); e != nil {
		return SecretConfig{}, ErrSource
	}
	return out, nil
}

// SMTPConfig validates syntax only; construction does not resolve or dial host.
// The original username is the From/Reply-To mailbox. Plaintext is never used.
func SMTPConfig(v SecretConfig) (mailer.SMTPConfig, error) {
	port, e := strconv.Atoi(v.Port)
	if e != nil || strconv.Itoa(port) != v.Port {
		return mailer.SMTPConfig{}, ErrInvalid
	}
	security := "implicit"
	if port == 587 || port < 465 {
		security = "starttls"
	}
	c := mailer.SMTPConfig{Host: v.Host, Port: port, Security: security, Username: v.Username, Password: v.Password, From: v.Username}
	if _, e = mailer.NewSMTP(c); e != nil {
		return mailer.SMTPConfig{}, ErrInvalid
	}
	return c, nil
}
func (s *Store) load(ctx context.Context, tx pgx.Tx, write bool) (state, error) {
	var out state
	lock := "pg_advisory_xact_lock_shared"
	if write {
		lock = "pg_advisory_xact_lock"
	}
	if _, e := tx.Exec(ctx, "SELECT "+lock+"($1)", platformLock); e != nil {
		return out, e
	}
	var tables int
	var registry, registered bool
	if e := tx.QueryRow(ctx, `SELECT num_nonnulls(to_regclass(current_schema()||'.mail_account_revisions195'),to_regclass(current_schema()||'.mail_account_witnesses195'),to_regclass(current_schema()||'.mail_account_head195')),to_regclass(current_schema()||'.schema_migrations')IS NOT NULL`).Scan(&tables, &registry); e != nil {
		return out, e
	}
	if registry {
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000109_mail_accounts.up.sql')`).Scan(&registered); e != nil {
			return out, e
		}
	}
	if tables == 0 && !registered {
		return out, nil
	}
	if tables != 3 || (registry && !registered) {
		return out, ErrSource
	}
	out.schema = true
	var revision, rows, max, witnesses, witnessMax, audits, auditMax int64
	if e := tx.QueryRow(ctx, `SELECT managed,revision,(SELECT count(*)FROM mail_account_revisions195),(SELECT coalesce(max(revision),0)FROM mail_account_revisions195),(SELECT count(*)FROM mail_account_witnesses195),(SELECT coalesce(max(revision),0)FROM mail_account_witnesses195),(SELECT count(*)FROM backoffice_audit WHERE resource='mail-account195'),(SELECT coalesce(max(resource_id),0)FROM backoffice_audit WHERE resource='mail-account195')FROM mail_account_head195 WHERE singleton`).Scan(&out.managed, &revision, &rows, &max, &witnesses, &witnessMax, &audits, &auditMax); e != nil {
		return out, ErrSource
	}
	if rows != max || rows != witnesses || rows != witnessMax || rows != audits || rows != auditMax || revision != max || out.managed != (revision > 0) {
		return out, ErrSource
	}
	if !out.managed {
		return out, nil
	}
	var raw, account, witness []byte
	if e := tx.QueryRow(ctx, `SELECT r.facts,r.proof_hash,w.account_proof,w.witness_proof FROM mail_account_revisions195 r JOIN mail_account_witnesses195 w USING(revision)WHERE r.revision=$1`, revision).Scan(&raw, &out.proof, &account, &witness); e != nil || len(raw) > 16384 {
		return out, ErrSource
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out.facts) != nil || decoder.Decode(new(any)) != io.EOF {
		return out, ErrSource
	}
	expected, e := s.digest("revision", out.facts)
	if e != nil || !hmac.Equal(expected, out.proof) || !hmac.Equal(account, out.proof) || out.facts.Version != 1 || out.facts.Revision != revision || out.facts.Status < 0 || out.facts.Status > 1 || out.facts.ActorID <= 0 || out.facts.ActorName == "" || out.facts.ChangedAt.IsZero() {
		return out, ErrSource
	}
	expected, e = s.digest("witness", []any{revision, out.proof})
	if e != nil || !hmac.Equal(expected, witness) {
		return out, ErrSource
	}
	var matches int
	if e = tx.QueryRow(ctx, `SELECT count(*)FROM backoffice_audit WHERE resource='mail-account195'AND resource_id=$1 AND actor_id=$2 AND changes->>'commitment'=$3`, revision, out.facts.ActorID, hex.EncodeToString(out.proof)).Scan(&matches); e != nil || matches != 1 {
		return out, ErrSource
	}
	out.config, e = s.open(out.facts)
	if e != nil {
		return out, e
	}
	return out, nil
}
func (s *Store) view(v state) (View, error) {
	out := View{Name: "smtp", Title: "SMTP发信", Config: Config{Port: "465"}, Configured: v.managed, Revision: strconv.FormatInt(v.facts.Revision, 10), EffectiveSource: "deployment"}
	if v.managed {
		out.Config = v.config.Config
		out.Status = v.facts.Status
		out.PasswordSet = v.config.Password != ""
		out.EffectiveSource = "console"
	}
	fp, e := s.digest("cas", []any{v.managed, v.facts, v.proof})
	if e != nil {
		return out, e
	}
	out.Fingerprint = hex.EncodeToString(fp)
	return out, nil
}
func (s *Store) Read(ctx context.Context, a Actor) (View, error) {
	var out View
	if s.pool == nil {
		return out, ErrSource
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	state, e := s.load(ctx, tx, false)
	if e != nil {
		return out, e
	}
	if !state.schema {
		return out, ErrSource
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	out, e = s.view(state)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) Save(ctx context.Context, a Actor, in Input) (View, error) {
	var out View
	if s.pool == nil {
		return out, ErrSource
	}
	if in.Name != "smtp" || in.Status < 0 || in.Status > 1 || len(in.Expected) != 64 || (in.KeepPassword == (in.Password != nil)) {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	before, e := s.load(ctx, tx, true)
	if e != nil {
		return out, e
	}
	if !before.schema {
		return out, ErrSource
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	v, e := s.view(before)
	if e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(in.Expected), []byte(v.Fingerprint)) {
		return out, ErrConflict
	}
	config := SecretConfig{Config: in.Config}
	if in.KeepPassword {
		if !before.managed || before.config.Config != in.Config {
			return out, ErrInvalid
		}
		config.Password = before.config.Password
	} else {
		config.Password = *in.Password
	}
	if _, e = SMTPConfig(config); e != nil {
		return out, e
	}
	revision := before.facts.Revision + 1
	nonce, ciphertext, e := s.seal(config, revision)
	if e != nil {
		return out, e
	}
	var at time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return out, e
	}
	f := facts{Version: 1, Revision: revision, Status: in.Status, Nonce: nonce, Ciphertext: ciphertext, ActorID: a.User.ID, ActorName: a.User.Username, ChangedAt: at.UTC()}
	proof, e := s.digest("revision", f)
	if e != nil {
		return out, e
	}
	witness, e := s.digest("witness", []any{revision, proof})
	if e != nil {
		return out, e
	}
	raw, e := json.Marshal(f)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO mail_account_revisions195(revision,facts,proof_hash)VALUES($1,$2::jsonb,$3)`, revision, string(raw), proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO mail_account_witnesses195(revision,account_proof,witness_proof)VALUES($1,$2,$3)`, revision, proof, witness); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE mail_account_head195 SET managed=true,revision=$1 WHERE singleton`, revision); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,'mail-account195',$3,'mail-account-update',jsonb_build_object('commitment',$4::text))`, a.User.ID, a.User.Username, revision, hex.EncodeToString(proof)); e != nil {
		return out, e
	}
	after, e := s.load(ctx, tx, true)
	if e != nil {
		return out, e
	}
	out, e = s.view(after)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
