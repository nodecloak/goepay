// Package mailpolicy194 owns the original shared mail frequency controls.
// No recipients, OTP codes, SMTP credentials or mutable delivery counters are
// stored in these policy records. Saving a policy never resets a send budget.
package mailpolicy194

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
)

var ErrSource = errors.New("mail frequency source cannot be confirmed")
var ErrConflict = errors.New("mail frequency changed")
var ErrInvalid = errors.New("invalid mail frequency")

const platformLock = int64(7170140001)

type Values struct {
	EmailInterval string `json:"mail_interval_email"`
	IPInterval    string `json:"mail_interval_ip"`
	EmailDaily    string `json:"mail_daily_email"`
	IPDaily       string `json:"mail_daily_ip"`
}
type Limits struct{ EmailInterval, IPInterval, EmailDaily, IPDaily int }
type View struct {
	Values
	Effective   Values `json:"effective"`
	Configured  bool   `json:"configured"`
	Revision    string `json:"revision"`
	Fingerprint string `json:"fingerprint"`
}
type Actor struct {
	User      identity.User
	Token     string
	Delegated bool
}
type facts struct {
	Version   int
	Revision  int64
	Limits    Limits
	ActorID   int64
	ActorName string
	ChangedAt time.Time
}
type state struct {
	facts   facts
	proof   []byte
	managed bool
	schema  bool
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
func Original() Limits { return Limits{60, 60, 10, 20} }
func legacy() Limits   { return Limits{60, 0, 7, 11} }
func values(l Limits) Values {
	return Values{strconv.Itoa(l.EmailInterval), strconv.Itoa(l.IPInterval), strconv.Itoa(l.EmailDaily), strconv.Itoa(l.IPDaily)}
}
func Parse(v Values) (Limits, error) {
	var l Limits
	for _, item := range []struct {
		raw string
		out *int
		max int
	}{{v.EmailInterval, &l.EmailInterval, 86400}, {v.IPInterval, &l.IPInterval, 86400}, {v.EmailDaily, &l.EmailDaily, 100000}, {v.IPDaily, &l.IPDaily, 100000}} {
		n, e := strconv.Atoi(item.raw)
		if e != nil || n <= 0 || n > item.max || strconv.Itoa(n) != item.raw {
			return l, ErrInvalid
		}
		*item.out = n
	}
	return l, nil
}
func (s *Store) digest(purpose string, v any) ([]byte, error) {
	if len(s.key) != 32 {
		return nil, ErrSource
	}
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("goepay/mailpolicy194/" + purpose + "\x00"))
	if e := json.NewEncoder(m).Encode(v); e != nil {
		return nil, e
	}
	return m.Sum(nil), nil
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
	var registry bool
	if e := tx.QueryRow(ctx, `SELECT num_nonnulls(to_regclass(current_schema()||'.mail_rate_policies194'),to_regclass(current_schema()||'.mail_rate_witnesses194'),to_regclass(current_schema()||'.mail_rate_head194')),to_regclass(current_schema()||'.schema_migrations')IS NOT NULL`).Scan(&tables, &registry); e != nil {
		return out, e
	}
	registered := false
	if registry {
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000108_mail_frequency.up.sql')`).Scan(&registered); e != nil {
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
	var revision, rows, maximum, witnesses, witnessMax, audits, auditMax int64
	if e := tx.QueryRow(ctx, `SELECT managed,revision,(SELECT count(*)FROM mail_rate_policies194),(SELECT coalesce(max(revision),0)FROM mail_rate_policies194),(SELECT count(*)FROM mail_rate_witnesses194),(SELECT coalesce(max(revision),0)FROM mail_rate_witnesses194),(SELECT count(*)FROM backoffice_audit WHERE resource='mail-frequency194'),(SELECT coalesce(max(resource_id),0)FROM backoffice_audit WHERE resource='mail-frequency194')FROM mail_rate_head194 WHERE singleton`).Scan(&out.managed, &revision, &rows, &maximum, &witnesses, &witnessMax, &audits, &auditMax); e != nil {
		return out, ErrSource
	}
	if rows != maximum || rows != witnesses || rows != witnessMax || rows != audits || rows != auditMax || revision != maximum || (out.managed != (revision > 0)) {
		return out, ErrSource
	}
	if !out.managed {
		return out, nil
	}
	var raw, policy, witness []byte
	if e := tx.QueryRow(ctx, `SELECT p.facts,p.proof_hash,w.policy_proof,w.witness_proof FROM mail_rate_policies194 p JOIN mail_rate_witnesses194 w USING(revision) WHERE p.revision=$1`, revision).Scan(&raw, &out.proof, &policy, &witness); e != nil {
		return out, ErrSource
	}
	if len(raw) > 4096 {
		return out, ErrSource
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&out.facts); e != nil {
		return out, ErrSource
	}
	expected, e := s.digest("policy", out.facts)
	if e != nil || !hmac.Equal(expected, out.proof) || !hmac.Equal(policy, out.proof) || out.facts.Version != 1 || out.facts.Revision != revision || out.facts.ActorID <= 0 || out.facts.ActorName == "" || out.facts.ChangedAt.IsZero() {
		return out, ErrSource
	}
	if _, e = Parse(values(out.facts.Limits)); e != nil {
		return out, ErrSource
	}
	want, e := s.digest("witness", []any{revision, out.proof})
	if e != nil || !hmac.Equal(want, witness) {
		return out, ErrSource
	}
	var matches int
	if e = tx.QueryRow(ctx, `SELECT count(*)FROM backoffice_audit WHERE resource='mail-frequency194' AND resource_id=$1 AND actor_id=$2 AND changes->>'commitment'=$3`, revision, out.facts.ActorID, hex.EncodeToString(out.proof)).Scan(&matches); e != nil || matches != 1 {
		return out, ErrSource
	}
	return out, nil
}
func (s *Store) view(v state) (View, error) {
	out := View{Values: values(Original()), Effective: values(legacy()), Configured: v.managed, Revision: strconv.FormatInt(v.facts.Revision, 10)}
	if v.managed {
		out.Values = values(v.facts.Limits)
		out.Effective = out.Values
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
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	state, e := s.load(ctx, tx, false)
	if e != nil {
		return out, e
	}
	if !state.schema {
		return out, ErrSource
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
func (s *Store) Save(ctx context.Context, a Actor, v Values, expected string) (View, error) {
	var out View
	l, e := Parse(v)
	if e != nil || len(expected) != 64 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	// Configuration lock precedes durable actor locks, matching other platform
	// writers and the registration/verification consumer's lock ordering.
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
	view, e := s.view(before)
	if e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(view.Fingerprint), []byte(expected)) {
		return out, ErrConflict
	}
	var at time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return out, e
	}
	current := facts{Version: 1, Revision: before.facts.Revision + 1, Limits: l, ActorID: a.User.ID, ActorName: a.User.Username, ChangedAt: at.UTC()}
	proof, e := s.digest("policy", current)
	if e != nil {
		return out, e
	}
	witness, e := s.digest("witness", []any{current.Revision, proof})
	if e != nil {
		return out, e
	}
	raw, e := json.Marshal(current)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO mail_rate_policies194(revision,facts,proof_hash)VALUES($1,$2::jsonb,$3)`, current.Revision, string(raw), proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO mail_rate_witnesses194(revision,policy_proof,witness_proof)VALUES($1,$2,$3)`, current.Revision, proof, witness); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE mail_rate_head194 SET managed=true,revision=$1 WHERE singleton`, current.Revision); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,'mail-frequency194',$3,'mail-frequency-update',jsonb_build_object('commitment',$4::text))`, a.User.ID, a.User.Username, current.Revision, hex.EncodeToString(proof)); e != nil {
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

// Effective participates in the caller's send-budget transaction. Only a
// complete explicit managed policy changes legacy behavior; partial or missing
// managed sources deny rather than returning the old/default limits.
func (s *Store) Effective(ctx context.Context, tx pgx.Tx) (Limits, bool, error) {
	state, e := s.load(ctx, tx, false)
	if e != nil {
		return Limits{}, false, e
	}
	if !state.managed {
		return legacy(), false, nil
	}
	return state.facts.Limits, true, nil
}
