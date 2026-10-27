package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"goepay/internal/identity"
	"goepay/internal/systemupdate"
)

const updateResource183 = "system-update183"

type SystemUpdateStatus struct {
	Intent           string                `json:"intent"`
	State            string                `json:"state"`
	StateFingerprint string                `json:"state_fingerprint"`
	Revision         string                `json:"revision"`
	Target           *systemupdate.Target  `json:"target"`
	Progress         systemupdate.Progress `json:"progress"`
	Applied          bool                  `json:"applied"`
	CanCancel        bool                  `json:"can_cancel"`
	CanRetry         bool                  `json:"can_retry"`
	ErrorCode        string                `json:"error_code"`
}
type SystemUpdateView struct {
	systemupdate.View
	Preparation SystemUpdateStatus `json:"preparation"`
}
type updateEvent183 struct {
	Version     int                 `json:"version"`
	Intent      string              `json:"intent"`
	Target      systemupdate.Target `json:"target"`
	From        systemupdate.Build  `json:"from"`
	Claim       string              `json:"claim"`
	State       string              `json:"state"`
	Seal        string              `json:"seal"`
	Error       string              `json:"error"`
	Revision    int64               `json:"revision"`
	ActorID     int64               `json:"actor_id"`
	ActorName   string              `json:"actor_name"`
	AuthVersion int64               `json:"auth_version"`
	Previous    string              `json:"previous"`
	Proof       string              `json:"proof"`
	Migrations  map[string]string   `json:"migrations,omitempty"`
	Repair      bool                `json:"repair,omitempty"`
}
type updateRunning183 struct {
	mu       sync.Mutex
	claim    string
	progress systemupdate.Progress
	cancel   context.CancelFunc
	done     chan struct{}
}

func updateHex183(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func updateID183(intent string) int64 {
	sum := sha256.Sum256([]byte("goepay/update183/lock/" + intent))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}
func updateLocks183(ctx context.Context, tx pgx.Tx, intent string) error {
	if !updateHex183(intent, 64) {
		return ErrInvalid
	}
	if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(74973231)`); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, updateID183(intent))
	return e
}
func (s *Store) updateProof183(e updateEvent183) (string, error) {
	e.Proof = ""
	proof, err := s.referralDigest("system-update183/audit", e)
	return hex.EncodeToString(proof), err
}
func (s *Store) updateEvent183(ctx context.Context, tx pgx.Tx, intent string) (out updateEvent183, err error) {
	rows, e := tx.Query(ctx, `SELECT operation,changes,actor_id,actor_name FROM backoffice_audit WHERE resource=$1 AND resource_id=$2 AND changes->>'intent'=$3 ORDER BY id`, updateResource183, updateID183(intent), intent)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	var previous string
	var revision int64
	for rows.Next() {
		var op, name string
		var actor int64
		var raw []byte
		if e = rows.Scan(&op, &raw, &actor, &name); e != nil {
			return out, e
		}
		var event updateEvent183
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&event) != nil {
			return out, ErrConflict
		}
		proof, e := s.updateProof183(event)
		if e != nil {
			return out, e
		}
		revision++
		if event.Version != 1 || event.Intent != intent || event.Target.Fingerprint != intent || event.Revision != revision || event.Previous != previous || event.ActorID != actor || event.ActorName != name || !updateHex183(event.Claim, 32) || !hmac.Equal([]byte(proof), []byte(event.Proof)) || op != "prepare-"+event.State {
			return out, ErrConflict
		}
		switch event.State {
		case "accepted":
			if out.State == "prepared" && !event.Repair {
				return out, ErrConflict
			}
		case "prepared", "failed", "cancelled":
			if (out.State != "accepted" && !(event.State == "cancelled" && out.State == "prepared")) || event.Claim != out.Claim {
				return out, ErrConflict
			}
		default:
			return out, ErrConflict
		}
		if out.Revision > 0 && (event.Target != out.Target || event.From != out.From) {
			return out, ErrConflict
		}
		out = event
		previous = event.Proof
	}
	return out, rows.Err()
}
func (s *Store) appendUpdate183(ctx context.Context, tx pgx.Tx, previous updateEvent183, next updateEvent183) (updateEvent183, error) {
	next.Version = 1
	next.Previous = previous.Proof
	next.Revision = previous.Revision + 1
	var e error
	next.Proof, e = s.updateProof183(next)
	if e != nil {
		return next, e
	}
	raw, e := json.Marshal(next)
	if e != nil {
		return next, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,$3,$4,$5,$6)`, next.ActorID, next.ActorName, updateResource183, updateID183(next.Intent), "prepare-"+next.State, raw)
	return next, e
}
func updateCatalog183(ctx context.Context, tx pgx.Tx) (map[string]string, int64, error) {
	rows, e := tx.Query(ctx, `SELECT name,sha256 FROM schema_migrations ORDER BY name`)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	out := map[string]string{}
	var schema int64
	for rows.Next() {
		var name, hash string
		if e = rows.Scan(&name, &hash); e != nil {
			return nil, 0, e
		}
		if len(name) < 8 || !updateHex183(hash, 64) {
			return nil, 0, ErrConflict
		}
		var n int64
		if _, e = fmt.Sscanf(name, "%d_", &n); e != nil || n < 0 || n > 999999 {
			return nil, 0, ErrConflict
		}
		if n > schema {
			schema = n
		}
		out[name] = hash
	}
	return out, schema, rows.Err()
}
func (s *Store) projectUpdate183(ctx context.Context, service *systemupdate.Service, b systemupdate.Build, intent string, event updateEvent183, catalog ...map[string]string) (SystemUpdateStatus, error) {
	status := SystemUpdateStatus{Intent: intent, State: "none", Revision: "0", Progress: systemupdate.Progress{Phase: "idle"}}
	if event.Revision > 0 {
		status.Revision = strconv.FormatInt(event.Revision, 10)
		status.Target = &event.Target
		status.ErrorCode = event.Error
		status.Progress = systemupdate.Progress{Phase: event.State, TotalBytes: event.Target.PackageBytes, TotalFiles: event.Target.FileCount}
		switch event.State {
		case "accepted":
			status.State = "unknown"
			status.CanCancel = true
			status.CanRetry = true
			if value, ok := s.updateJobs.Load(intent); ok {
				running := value.(*updateRunning183)
				running.mu.Lock()
				if running.claim == event.Claim {
					status.State = "preparing"
					status.CanRetry = false
					status.Progress = running.progress
				}
				running.mu.Unlock()
			}
		case "prepared":
			status.State = "unknown"
			status.CanCancel = true
			matched := len(catalog) == 1 && len(catalog[0]) == len(event.Migrations)
			if matched {
				for n, h := range event.Migrations {
					if catalog[0][n] != h {
						matched = false
						break
					}
				}
			}
			if b.Version == event.Target.Version && b.SourceSHA == event.Target.SourceSHA && b.SourceSHA != "" && b.Schema == event.Target.Schema && matched {
				status.State = "applied"
				status.Applied = true
				status.CanCancel = false
				status.Progress.Phase = "applied"
				status.Progress.ReadBytes = event.Target.PackageBytes
				status.Progress.VerifiedFiles = event.Target.FileCount
			} else if service != nil {
				target, e := service.VerifiedArtifact(ctx, intent, event.Seal)
				if e == nil && target == event.Target {
					status.State = "prepared"
					status.Progress.Phase = "prepared"
					status.Progress.ReadBytes = event.Target.PackageBytes
					status.Progress.VerifiedFiles = event.Target.FileCount
				}
			}
			status.CanRetry = status.State == "unknown"
		case "failed", "cancelled":
			status.State = event.State
			status.CanRetry = true
		}
	}
	if status.CanRetry {
		if service == nil {
			status.CanRetry = false
		} else {
			_, e := service.Plan(ctx, b, intent)
			status.CanRetry = e == nil
		}
	}
	proof, e := s.referralDigest("system-update183/status", []any{intent, event.Proof, status.State, b.Version, b.SourceSHA, b.Schema})
	if e != nil {
		return status, e
	}
	status.StateFingerprint = hex.EncodeToString(proof)
	return status, nil
}
func (s *Store) projectUpdateTx183(ctx context.Context, tx pgx.Tx, service *systemupdate.Service, b systemupdate.Build, intent string, event updateEvent183) (SystemUpdateStatus, error) {
	catalog, schema, e := updateCatalog183(ctx, tx)
	if e != nil {
		return SystemUpdateStatus{}, e
	}
	if schema != b.Schema {
		return SystemUpdateStatus{}, ErrConflict
	}
	return s.projectUpdate183(ctx, service, b, intent, event, catalog)
}
func clampUpdateActor183(status *SystemUpdateStatus, actor MerchantCredentialActor) {
	if actor.User.AdminClass != "super" {
		status.CanCancel = false
		status.CanRetry = false
	}
}
func (s *Store) SystemUpdateInspect(ctx context.Context, service *systemupdate.Service, b systemupdate.Build, actor MerchantCredentialActor) (SystemUpdateView, error) {
	var out SystemUpdateView
	view, e := service.Inspect(ctx, b)
	if e != nil {
		return out, e
	}
	out.View = view
	intent := view.Fingerprint
	// Following a real restart, the new build changes the check fingerprint.
	// Find its already authenticated preparation rather than pretending it is new.
	if b.SourceSHA != "" {
		var previous string
		e = s.pool.QueryRow(ctx, `SELECT changes->>'intent' FROM backoffice_audit WHERE resource=$1 AND ((changes->'target'->>'source_sha256'=$2 AND changes->'target'->>'version'=$3) OR (changes->'from'->>'source_sha256'=$2 AND changes->'from'->>'version'=$3)) AND changes->'from'->>'os'=$4 AND changes->'from'->>'arch'=$5 ORDER BY id DESC LIMIT 1`, updateResource183, b.SourceSHA, b.Version, b.GOOS, b.GOARCH).Scan(&previous)
		if e == nil && updateHex183(previous, 64) {
			intent = previous
		} else if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = updateLocks183(ctx, tx, intent); e != nil {
		return out, e
	}
	if e = identity.CheckOriginalAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	event, e := s.updateEvent183(ctx, tx, intent)
	if e != nil {
		return out, e
	}
	out.Preparation, e = s.projectUpdateTx183(ctx, tx, service, b, intent, event)
	if e != nil {
		return out, e
	}
	clampUpdateActor183(&out.Preparation, actor)
	out.CanPrepare = out.CanPrepare && actor.User.AdminClass == "super" && (out.Preparation.State == "none" || intent != view.Fingerprint && (out.Preparation.State == "failed" || out.Preparation.State == "cancelled" || out.Preparation.State == "applied"))
	if e = identity.CheckOriginalAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) SystemUpdateStatus(ctx context.Context, service *systemupdate.Service, b systemupdate.Build, actor MerchantCredentialActor, intent string) (SystemUpdateStatus, error) {
	var out SystemUpdateStatus
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = updateLocks183(ctx, tx, intent); e != nil {
		return out, e
	}
	if e = identity.CheckOriginalAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	event, e := s.updateEvent183(ctx, tx, intent)
	if e != nil {
		return out, e
	}
	out, e = s.projectUpdateTx183(ctx, tx, service, b, intent, event)
	if e != nil {
		return out, e
	}
	clampUpdateActor183(&out, actor)
	if e = identity.CheckOriginalAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) PrepareSystemUpdate(ctx context.Context, service *systemupdate.Service, b systemupdate.Build, actor MerchantCredentialActor, intent, expected string) (SystemUpdateStatus, bool, error) {
	var out SystemUpdateStatus
	if s.updateClosed.Load() {
		return out, false, ErrState
	}
	if intent != expected {
		return out, false, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, false, e
	}
	defer tx.Rollback(ctx)
	if e = updateLocks183(ctx, tx, intent); e != nil {
		return out, false, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, false, e
	}
	event, e := s.updateEvent183(ctx, tx, intent)
	if e != nil {
		return out, false, e
	}
	if event.Revision > 0 {
		out, e = s.projectUpdateTx183(ctx, tx, service, b, intent, event)
		if e != nil {
			return out, false, e
		}
		if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
			return out, false, e
		}
		return out, false, tx.Commit(ctx)
	}
	ticket, e := service.Plan(ctx, b, expected)
	if e != nil {
		return out, false, e
	}
	applied, schema, e := updateCatalog183(ctx, tx)
	if e != nil {
		return out, false, e
	}
	if schema != b.Schema {
		return out, false, ErrConflict
	}
	if e = ticket.AppliedCompatible(applied); e != nil {
		return out, false, e
	}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return out, false, e
	}
	event = updateEvent183{Intent: intent, Target: ticket.Target(), From: b, Claim: hex.EncodeToString(nonce), State: "accepted", ActorID: actor.User.ID, ActorName: actor.User.Username, AuthVersion: actor.User.AuthVersion}
	event.Migrations = ticket.DeclaredMigrations()
	event, e = s.appendUpdate183(ctx, tx, updateEvent183{}, event)
	if e != nil {
		return out, false, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, false, e
	}
	s.startUpdate183(service, ticket, applied, event, actor)
	out, e = s.projectUpdate183(ctx, service, b, intent, event)
	return out, true, e
}
func (s *Store) startUpdate183(service *systemupdate.Service, ticket *systemupdate.Ticket, applied map[string]string, event updateEvent183, actor MerchantCredentialActor) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	if s.updateClosed.Load() {
		cancel()
		return
	}
	running := &updateRunning183{claim: event.Claim, cancel: cancel, done: make(chan struct{}), progress: systemupdate.Progress{Phase: "queued", TotalBytes: event.Target.PackageBytes, TotalFiles: event.Target.FileCount}}
	if previous, ok := s.updateJobs.Swap(event.Intent, running); ok {
		previous.(*updateRunning183).cancel()
	}
	go func() {
		defer func() { cancel(); s.updateJobs.CompareAndDelete(event.Intent, running); close(running.done) }()
		if e := s.assertUpdate183(ctx, event, actor); e != nil {
			s.failUpdate183(event, "authorization_or_claim_changed")
			return
		}
		artifact, e := ticket.Stage(ctx, applied, func(p systemupdate.Progress) { running.mu.Lock(); running.progress = p; running.mu.Unlock() })
		if e != nil {
			s.failUpdate183(event, "verification_failed")
			return
		}
		defer artifact.Abort()
		running.mu.Lock()
		running.progress.Phase = "committing"
		running.mu.Unlock()
		uncertain, e := s.finishUpdate183(ctx, service, ticket, artifact, event, actor)
		if e != nil && !uncertain {
			s.failUpdate183(event, "preparation_failed")
		}
	}()
}
func (s *Store) finishUpdate183(ctx context.Context, service *systemupdate.Service, ticket *systemupdate.Ticket, artifact *systemupdate.Artifact, event updateEvent183, actor MerchantCredentialActor) (bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(context.Background())
	if e = updateLocks183(ctx, tx, event.Intent); e != nil {
		return false, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return false, e
	}
	current, e := s.updateEvent183(ctx, tx, event.Intent)
	if e != nil {
		return false, e
	}
	if current.State != "accepted" || current.Claim != event.Claim {
		return false, ErrConflict
	}
	applied, _, e := updateCatalog183(ctx, tx)
	if e != nil {
		return false, e
	}
	if e = ticket.AppliedCompatible(applied); e != nil {
		return false, e
	}
	next := current
	next.State = "prepared"
	next.Seal = artifact.Seal()
	next, e = s.appendUpdate183(ctx, tx, current, next)
	if e != nil {
		return false, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return false, e
	}
	if _, e = artifact.Commit(ctx, event.Repair); e != nil {
		return false, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return true, e
	}
	return false, nil
}
func (s *Store) failUpdate183(event updateEvent183, code string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return
	}
	defer tx.Rollback(ctx)
	if updateLocks183(ctx, tx, event.Intent) != nil {
		return
	}
	current, e := s.updateEvent183(ctx, tx, event.Intent)
	if e != nil || current.State != "accepted" || current.Claim != event.Claim {
		return
	}
	next := current
	next.State = "failed"
	next.Error = code
	if _, e = s.appendUpdate183(ctx, tx, current, next); e == nil {
		_ = tx.Commit(ctx)
	}
}
func (s *Store) StopSystemUpdates() {
	s.updateClosed.Store(true)
	var waiting []chan struct{}
	s.updateJobs.Range(func(_, v any) bool {
		running := v.(*updateRunning183)
		running.cancel()
		waiting = append(waiting, running.done)
		return true
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, done := range waiting {
		select {
		case <-done:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Store) assertUpdate183(ctx context.Context, event updateEvent183, actor MerchantCredentialActor) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = updateLocks183(ctx, tx, event.Intent); e != nil {
		return e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return e
	}
	current, e := s.updateEvent183(ctx, tx, event.Intent)
	if e != nil {
		return e
	}
	if current.State != "accepted" || current.Claim != event.Claim {
		return ErrConflict
	}
	return tx.Commit(ctx)
}
func (s *Store) CancelSystemUpdate(ctx context.Context, service *systemupdate.Service, b systemupdate.Build, actor MerchantCredentialActor, intent, expectedState string) (SystemUpdateStatus, error) {
	var out SystemUpdateStatus
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = updateLocks183(ctx, tx, intent); e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	current, e := s.updateEvent183(ctx, tx, intent)
	if e != nil {
		return out, e
	}
	status, e := s.projectUpdateTx183(ctx, tx, service, b, intent, current)
	if e != nil {
		return out, e
	}
	if !status.CanCancel || !hmac.Equal([]byte(status.StateFingerprint), []byte(expectedState)) {
		return out, ErrConflict
	}
	next := current
	next.State = "cancelled"
	next.Error = ""
	next.ActorID = actor.User.ID
	next.ActorName = actor.User.Username
	next.AuthVersion = actor.User.AuthVersion
	next, e = s.appendUpdate183(ctx, tx, current, next)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	if value, ok := s.updateJobs.Load(intent); ok {
		running := value.(*updateRunning183)
		if running.claim == current.Claim {
			running.cancel()
		}
	}
	return s.projectUpdate183(ctx, service, b, intent, next)
}
func (s *Store) RetrySystemUpdate(ctx context.Context, service *systemupdate.Service, b systemupdate.Build, actor MerchantCredentialActor, intent, expected, expectedState string) (SystemUpdateStatus, error) {
	var out SystemUpdateStatus
	if intent != expected || s.updateClosed.Load() {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = updateLocks183(ctx, tx, intent); e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	current, e := s.updateEvent183(ctx, tx, intent)
	if e != nil {
		return out, e
	}
	status, e := s.projectUpdateTx183(ctx, tx, service, b, intent, current)
	if e != nil {
		return out, e
	}
	if !status.CanRetry || !hmac.Equal([]byte(status.StateFingerprint), []byte(expectedState)) {
		return out, ErrConflict
	}
	ticket, e := service.Plan(ctx, b, intent)
	if e != nil {
		return out, e
	}
	applied, schema, e := updateCatalog183(ctx, tx)
	if e != nil {
		return out, e
	}
	if schema != b.Schema || ticket.Target() != current.Target {
		return out, ErrConflict
	}
	if e = ticket.AppliedCompatible(applied); e != nil {
		return out, e
	}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return out, e
	}
	next := current
	next.State = "accepted"
	next.Repair = true
	next.Error = ""
	next.Seal = ""
	next.Claim = hex.EncodeToString(nonce)
	next.ActorID = actor.User.ID
	next.ActorName = actor.User.Username
	next.AuthVersion = actor.User.AuthVersion
	next, e = s.appendUpdate183(ctx, tx, current, next)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	s.startUpdate183(service, ticket, applied, next, actor)
	return s.projectUpdate183(ctx, service, b, intent, next)
}
