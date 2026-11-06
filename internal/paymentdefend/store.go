package paymentdefend

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
)

var ErrInvalid = errors.New("invalid payment defend policy")
var ErrConflict = errors.New("payment defend policy source or revision conflict")
var ErrUnavailable = errors.New("payment defend policy unavailable")

type Actor struct {
	User        identity.User
	Token, CSRF string
	Delegated   bool
}

type Policy struct {
	Settings    Settings `json:"settings"`
	Configured  bool     `json:"configured"`
	Revision    int64    `json:"revision,string"`
	Fingerprint string   `json:"fingerprint"`
}

type View struct {
	Policy
	CanUpdate bool `json:"can_update"`
}

type Update struct {
	Settings            Settings `json:"settings"`
	ExpectedFingerprint string   `json:"expected_fingerprint"`
}

type Store struct {
	pool *pgxpool.Pool
	key  []byte
}

type policyFacts struct {
	Version            int
	Revision           int64
	Settings           Settings
	SavedAt            time.Time
	PreviousCommitment string
}

type policyRecord struct {
	Facts      policyFacts
	Proof      []byte
	Configured bool
}

type policyCommit struct {
	Revision                       int64
	Commitment, PreviousCommitment string
	AuditID, ActorID               int64
	BirthTransaction               string
	Proof                          []byte
}

func NewStore(pool *pgxpool.Pool, master []byte) (*Store, error) {
	if pool == nil || len(master) < 32 {
		return nil, ErrUnavailable
	}
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte("goepay/payment-defend206/integrity/v1"))
	return &Store{pool: pool, key: mac.Sum(nil)}, nil
}

func (s *Store) valid() bool {
	return s != nil && s.pool != nil && len(s.key) == sha256.Size
}

func (s *Store) sum(domain string, value any) []byte {
	raw, _ := json.Marshal(value)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(domain + "\x00"))
	mac.Write(raw)
	return mac.Sum(nil)
}

func (s *Store) policyProof(f policyFacts) []byte {
	f.SavedAt = f.SavedAt.UTC()
	return s.sum("policy", f)
}

func (s *Store) commitProof(c policyCommit) []byte {
	return s.sum("revision-witness", []any{c.Revision, c.Commitment, c.PreviousCommitment, c.AuditID, c.ActorID, c.BirthTransaction})
}

func hex64(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

func lockPolicy(ctx context.Context, tx pgx.Tx, write bool) error {
	function := "pg_advisory_xact_lock_shared"
	if write {
		function = "pg_advisory_xact_lock"
	}
	_, err := tx.Exec(ctx, "SELECT "+function+"(hashtextextended('goepay/payment-defend206/policy',206))")
	return err
}

func decodeFacts(raw []byte) (policyFacts, error) {
	var f policyFacts
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) != 5 {
		return f, ErrConflict
	}
	for _, field := range []string{"Version", "Revision", "Settings", "SavedAt", "PreviousCommitment"} {
		if value, ok := object[field]; !ok || string(value) == "null" {
			return f, ErrConflict
		}
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(object["Settings"], &settings) != nil || len(settings) != len(fields) {
		return f, ErrConflict
	}
	for _, field := range fields {
		var value string
		if rawValue, ok := settings[field]; !ok || string(rawValue) == "null" || json.Unmarshal(rawValue, &value) != nil {
			return f, ErrConflict
		}
	}
	if json.Unmarshal(raw, &f) != nil || f.Version != 1 || f.Revision < 1 || f.SavedAt.IsZero() || f.Revision == 1 && f.PreviousCommitment != "" || f.Revision > 1 && !hex64(f.PreviousCommitment) {
		return f, ErrConflict
	}
	validated, err := Validate(f.Settings)
	if err != nil {
		return f, ErrConflict
	}
	f.Settings, f.SavedAt = validated, f.SavedAt.UTC()
	return f, nil
}

// Caller holds the shared/exclusive policy lock before actor or business locks.
func (s *Store) load(ctx context.Context, tx pgx.Tx, write bool) (policyRecord, error) {
	out := policyRecord{Facts: policyFacts{Version: 1, Settings: Defaults()}}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	var revision int64
	var raw, proof []byte
	err := tx.QueryRow(ctx, `SELECT revision,facts,proof_hash FROM payment_defend206_policy WHERE id=true`+lock).Scan(&revision, &raw, &proof)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	var commits, maximum int64
	if err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(max(revision),0) FROM payment_defend206_policy_commits`).Scan(&commits, &maximum); err != nil {
		return out, err
	}
	var audits int64
	var auditOrdered bool
	if err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_and(COALESCE(resource_id=1 AND operation='configure' AND actor_role='admin' AND actor_id>0 AND changes->>'revision'=ordinal::text AND COALESCE(changes->>'commitment','') ~ '^[a-f0-9]{64}$',false)),true)
 FROM(SELECT resource_id,operation,actor_role,actor_id,changes,row_number() OVER(ORDER BY id) ordinal FROM backoffice_audit WHERE resource='payment-defend206') a`).Scan(&audits, &auditOrdered); err != nil {
		return out, err
	}
	if !found {
		if commits != 0 || audits != 0 {
			return out, ErrConflict
		}
		return out, nil
	}
	if revision < 1 || commits != revision || maximum != revision || audits != revision || !auditOrdered {
		return out, ErrConflict
	}
	f, err := decodeFacts(raw)
	if err != nil || f.Revision != revision || !hmac.Equal(proof, s.policyProof(f)) {
		return out, ErrConflict
	}
	var c policyCommit
	if err = tx.QueryRow(ctx, `SELECT revision,commitment,previous_commitment,audit_id,actor_id,birth_xid::text,proof_hash FROM payment_defend206_policy_commits WHERE revision=$1 FOR SHARE`, revision).Scan(&c.Revision, &c.Commitment, &c.PreviousCommitment, &c.AuditID, &c.ActorID, &c.BirthTransaction, &c.Proof); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrConflict
		}
		return out, err
	}
	if c.Commitment != hex.EncodeToString(proof) || c.PreviousCommitment != f.PreviousCommitment || !hmac.Equal(c.Proof, s.commitProof(c)) {
		return out, ErrConflict
	}
	if revision > 1 {
		var previous string
		if err = tx.QueryRow(ctx, `SELECT commitment FROM payment_defend206_policy_commits WHERE revision=$1`, revision-1).Scan(&previous); err != nil || previous != c.PreviousCommitment {
			return out, ErrConflict
		}
	}
	var auditValid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE id=$1 AND resource='payment-defend206' AND resource_id=1 AND operation='configure' AND actor_role='admin' AND actor_id=$2 AND changes->>'revision'=$3 AND changes->>'commitment'=$4)
 AND $1=(SELECT max(id) FROM backoffice_audit WHERE resource='payment-defend206' AND resource_id=1 AND operation='configure')`, c.AuditID, c.ActorID, strconv.FormatInt(revision, 10), c.Commitment).Scan(&auditValid); err != nil {
		return out, err
	}
	if !auditValid {
		return out, ErrConflict
	}
	if err = s.verifyCommitHistory(ctx, tx, revision, c.Commitment); err != nil {
		return out, err
	}
	return policyRecord{Facts: f, Proof: proof, Configured: true}, nil
}

func (s *Store) verifyCommitHistory(ctx context.Context, tx pgx.Tx, revision int64, commitment string) error {
	rows, err := tx.Query(ctx, `SELECT c.revision,c.commitment,c.previous_commitment,c.audit_id,c.actor_id,c.birth_xid::text,c.proof_hash,
 COALESCE(a.resource='payment-defend206' AND a.resource_id=1 AND a.operation='configure' AND a.actor_role='admin' AND a.actor_id=c.actor_id AND a.changes->>'revision'=c.revision::text AND a.changes->>'commitment'=c.commitment,false)
 FROM payment_defend206_policy_commits c JOIN backoffice_audit a ON a.id=c.audit_id ORDER BY c.revision FOR SHARE OF c,a`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var count int64
	previous := ""
	for rows.Next() {
		var c policyCommit
		var validAudit bool
		if err = rows.Scan(&c.Revision, &c.Commitment, &c.PreviousCommitment, &c.AuditID, &c.ActorID, &c.BirthTransaction, &c.Proof, &validAudit); err != nil {
			return err
		}
		count++
		if c.Revision != count || !validAudit || c.PreviousCommitment != previous || !hmac.Equal(c.Proof, s.commitProof(c)) {
			return ErrConflict
		}
		previous = c.Commitment
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if count != revision || previous != commitment {
		return ErrConflict
	}
	return nil
}

func (s *Store) policy(p policyRecord) Policy {
	settings, _ := Validate(p.Facts.Settings)
	return Policy{Settings: settings, Configured: p.Configured, Revision: p.Facts.Revision, Fingerprint: hex.EncodeToString(s.sum("fingerprint", []any{p.Configured, p.Facts, hex.EncodeToString(p.Proof)}))}
}

// ConfirmUnconfiguredTx grants only the narrow ability to confirm that no
// protected policy authority has ever been configured in this database. It
// does not construct a policy, fingerprint, or configuration write authority.
// Without a master key, a head, witness, or original audit is an unavailable
// policy, never an off policy. The caller owns the transaction/lock lifetime.
func ConfirmUnconfiguredTx(ctx context.Context, tx pgx.Tx) error {
	if tx == nil {
		return ErrUnavailable
	}
	if err := lockPolicy(ctx, tx, false); err != nil {
		return err
	}
	var empty bool
	err := tx.QueryRow(ctx, `SELECT
 NOT EXISTS(SELECT id,revision,facts,proof_hash FROM payment_defend206_policy)
 AND NOT EXISTS(SELECT revision,commitment,previous_commitment,audit_id,actor_id,birth_xid,proof_hash FROM payment_defend206_policy_commits)
 AND NOT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='payment-defend206')`).Scan(&empty)
	if err != nil {
		return err
	}
	if !empty {
		return ErrUnavailable
	}
	return nil
}

// CurrentTx reads only this independent protected policy. It holds a shared
// policy lock until the caller commits; call it before route/order locks when
// binding an admission to a business transaction. Missing/damaged schema is an
// error, never an unconfigured default or a legacy-settings fallback.
func (s *Store) CurrentTx(ctx context.Context, tx pgx.Tx) (Policy, error) {
	if !s.valid() || tx == nil {
		return Policy{}, ErrUnavailable
	}
	if err := lockPolicy(ctx, tx, false); err != nil {
		return Policy{}, err
	}
	p, err := s.load(ctx, tx, false)
	if err != nil {
		return Policy{}, err
	}
	return s.policy(p), nil
}

func (s *Store) Current(ctx context.Context) (Policy, error) {
	if !s.valid() {
		return Policy{}, ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Policy{}, err
	}
	defer tx.Rollback(ctx)
	out, err := s.CurrentTx(ctx, tx)
	if err != nil {
		return Policy{}, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) actor(ctx context.Context, tx pgx.Tx, a Actor, csrfRequired bool) error {
	if a.User.Role != "admin" || a.User.AdminClass != "super" || a.Delegated {
		return identity.ErrAdminSession
	}
	if err := identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); err != nil {
		return err
	}
	if csrfRequired {
		digest := sha256.Sum256([]byte(a.Token))
		var csrf string
		if err := tx.QueryRow(ctx, `SELECT csrf FROM console_sessions WHERE token_hash=$1 AND role='admin' FOR SHARE`, digest[:]).Scan(&csrf); err != nil || a.CSRF == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(a.CSRF)) != 1 {
			return identity.ErrAdminSession
		}
	}
	return nil
}

func (s *Store) Read(ctx context.Context, a Actor) (View, error) {
	if !s.valid() {
		return View{}, ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockPolicy(ctx, tx, false); err != nil {
		return View{}, err
	}
	if err = s.actor(ctx, tx, a, false); err != nil {
		return View{}, err
	}
	p, err := s.load(ctx, tx, false)
	if err != nil {
		return View{}, err
	}
	if err = s.actor(ctx, tx, a, false); err != nil {
		return View{}, err
	}
	return View{Policy: s.policy(p), CanUpdate: true}, tx.Commit(ctx)
}

func (s *Store) Update(ctx context.Context, a Actor, input Update) (View, error) {
	if !s.valid() {
		return View{}, ErrUnavailable
	}
	settings, err := Validate(input.Settings)
	if err != nil || !hex64(input.ExpectedFingerprint) {
		return View{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockPolicy(ctx, tx, true); err != nil {
		return View{}, err
	}
	if err = s.actor(ctx, tx, a, true); err != nil {
		return View{}, err
	}
	old, err := s.load(ctx, tx, true)
	if err != nil {
		return View{}, err
	}
	if !hmac.Equal([]byte(s.policy(old).Fingerprint), []byte(input.ExpectedFingerprint)) || old.Facts.Revision == math.MaxInt64 {
		return View{}, ErrConflict
	}
	f := policyFacts{Version: 1, Revision: old.Facts.Revision + 1, Settings: settings, PreviousCommitment: hex.EncodeToString(old.Proof)}
	var transaction string
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp(),pg_current_xact_id()::text`).Scan(&f.SavedAt, &transaction); err != nil {
		return View{}, err
	}
	f.SavedAt = f.SavedAt.UTC()
	proof := s.policyProof(f)
	c := policyCommit{Revision: f.Revision, Commitment: hex.EncodeToString(proof), PreviousCommitment: f.PreviousCommitment, ActorID: a.User.ID, BirthTransaction: transaction}
	if err = tx.QueryRow(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'payment-defend206',1,'configure',jsonb_build_object('revision',$3::text,'commitment',$4::text)) RETURNING id`, a.User.ID, a.User.Username, strconv.FormatInt(f.Revision, 10), c.Commitment).Scan(&c.AuditID); err != nil {
		return View{}, err
	}
	c.Proof = s.commitProof(c)
	if _, err = tx.Exec(ctx, `INSERT INTO payment_defend206_policy_commits(revision,commitment,previous_commitment,audit_id,actor_id,proof_hash) VALUES($1,$2,$3,$4,$5,$6)`, c.Revision, c.Commitment, c.PreviousCommitment, c.AuditID, c.ActorID, c.Proof); err != nil {
		return View{}, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return View{}, err
	}
	if old.Configured {
		tag, err := tx.Exec(ctx, `UPDATE payment_defend206_policy SET revision=$1,facts=$2::jsonb,proof_hash=$3 WHERE id=true AND revision=$4`, f.Revision, string(raw), proof, old.Facts.Revision)
		if err != nil {
			return View{}, err
		}
		if tag.RowsAffected() != 1 {
			return View{}, ErrConflict
		}
	} else if _, err = tx.Exec(ctx, `INSERT INTO payment_defend206_policy(id,revision,facts,proof_hash) VALUES(true,$1,$2::jsonb,$3)`, f.Revision, string(raw), proof); err != nil {
		return View{}, err
	}
	if err = s.actor(ctx, tx, a, true); err != nil {
		return View{}, err
	}
	out := View{Policy: s.policy(policyRecord{Facts: f, Proof: proof, Configured: true}), CanUpdate: true}
	return out, tx.Commit(ctx)
}
