package migration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

func (w *Wizard234) authorize(ctx context.Context, tx pgx.Tx, a WizardActor234) error {
	return identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated)
}
func (w *Wizard234) authenticated(ctx context.Context, a WizardActor234) error {
	tx, e := w.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	return w.authorize(ctx, tx, a)
}
func wizardOwner234(r wizardRecord234, a WizardActor234) bool {
	return r.Owner == a.User.Username && r.Subject == a.User.ID
}
func wizardPrefix234(prefix string) bool {
	if len(prefix) > 64 {
		return false
	}
	for _, c := range prefix {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
func wizardReadView234(p wizardPayload234, ready bool, now time.Time) WizardView234 {
	v := p.View
	v.TargetReady = ready
	if v.Status != "completed" && !now.Before(v.ExpiresAt) {
		v.Issues = append(append([]Issue(nil), v.Issues...), Issue{"run", "staging_expired"})
	}
	return v
}

func (w *Wizard234) Capabilities(ctx context.Context, a WizardActor234) (Capabilities234, error) {
	out := Capabilities234{Enabled: true, MaxUploadBytes: MaxDumpBytes234, Issues: []Issue{}, Runs: []WizardView234{}}
	tx, e := w.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = w.authorize(ctx, tx, a); e != nil {
		return out, e
	}
	var busy bool
	out.TargetReady, busy, e = wizardReadTarget234(ctx, tx)
	if e != nil {
		return out, e
	}
	if busy {
		out.Issues = append(out.Issues, Issue{"target", "target_busy"})
	} else if !out.TargetReady {
		out.Issues = append(out.Issues, Issue{"target", "target_not_empty"})
	}
	rows, e := tx.Query(ctx, `SELECT id FROM migration_wizard_runs234 WHERE owner_username=$1 AND owner_subject=$2 ORDER BY created_at DESC,id DESC LIMIT 20`, a.User.Username, a.User.ID)
	if e != nil {
		return out, e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return out, e
	}
	for _, id := range ids {
		r, p, err := w.read(ctx, tx, id, false)
		if err != nil {
			return out, err
		}
		if !wizardOwner234(r, a) {
			return out, identity.ErrAdminSession
		}
		out.Runs = append(out.Runs, wizardReadView234(p, out.TargetReady, now))
	}
	if e = w.authorize(ctx, tx, a); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func (w *Wizard234) Prepare(ctx context.Context, a WizardActor234, raw []byte, source, prefix string) (WizardView234, error) {
	var empty WizardView234
	if len(raw) == 0 || int64(len(raw)) > MaxDumpBytes234 || (source != "epay_pro" && source != "epay_release") || !wizardPrefix234(prefix) {
		return empty, ErrWizardInvalid234
	}
	if e := w.authenticated(ctx, a); e != nil {
		return empty, e
	}
	// Freeze the uploaded bytes before parsing or encrypting them. No source
	// SQL, URL or server file path is executed or opened by this service.
	raw = append([]byte(nil), raw...)
	d, e := ParseDump234(raw, source, prefix, time.Now().UTC())
	issues := append([]Issue{}, d.Issues...)
	warnings := append([]Issue{}, d.Warnings...)
	if e != nil && len(issues) == 0 {
		issues = append(issues, Issue{"backup", "invalid_or_unsupported_dump"})
	}
	var report Report
	if len(d.Snapshot) > 0 {
		_, report, e = Validate(d.Snapshot)
		if e != nil {
			issues = append(issues, report.Issues...)
		}
	}
	if len(d.Snapshot) == 0 && len(issues) == 0 {
		issues = append(issues, Issue{"backup", "missing_normalized_snapshot"})
	}
	tx, e := w.pool.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	if e = w.authorize(ctx, tx, a); e != nil {
		return empty, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return empty, e
	}
	ready, busy, e := wizardReadTarget234(ctx, tx)
	if e != nil {
		return empty, e
	}
	if busy {
		issues = append(issues, Issue{"target", "target_busy"})
	} else if !ready {
		issues = append(issues, Issue{"target", "target_not_empty"})
	}
	idBytes := make([]byte, 16)
	if _, e = rand.Read(idBytes); e != nil {
		return empty, e
	}
	id := hex.EncodeToString(idBytes)
	digest := sha256.Sum256(raw)
	status := "prepared"
	if len(issues) > 0 {
		status = "blocked"
	}
	issues = wizardBoundIssues234(issues, "issues_truncated")
	warnings = wizardBoundIssues234(warnings, "warnings_truncated")
	v := WizardView234{ID: id, Status: status, SHA256: hex.EncodeToString(digest[:]), Source: source, Prefix: d.Prefix, TableCounts: d.Tables, Merchants: report.Merchants, Orders: report.Orders, Refunds: report.Refunds, Transfers: report.Transfers, AvailableTotal: report.AvailableTotal, ReservedTotal: report.ReservedTotal, Issues: issues, Warnings: warnings, TargetReady: ready, Progress: 0, ExpiresAt: now.Add(24 * time.Hour)}
	if v.TableCounts == nil {
		v.TableCounts = []DumpTable234{}
	}
	if v.AvailableTotal == "" {
		v.AvailableTotal = "0.00"
	}
	if v.ReservedTotal == "" {
		v.ReservedTotal = "0.00"
	}
	viewJSON, e := json.Marshal(v)
	if e != nil {
		return empty, ErrWizardInvalid234
	}
	if e = wizardCapacity234(ctx, tx, a, int64(len(raw))+int64(len(d.Snapshot))+int64(len(viewJSON))+84); e != nil {
		return empty, e
	}
	r := wizardRecord234{ID: id, Owner: a.User.Username, Subject: a.User.ID, Revision: 1, Status: status, Created: now, Expires: v.ExpiresAt}
	if e = w.save(ctx, tx, &r, wizardPayload234{Raw: raw, Snapshot: d.Snapshot, View: v}, true); e != nil {
		return empty, e
	}
	if e = w.authorize(ctx, tx, a); e != nil {
		return empty, e
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, e
	}
	return v, nil
}

func (w *Wizard234) Status(ctx context.Context, a WizardActor234, id string) (WizardView234, error) {
	var empty WizardView234
	if !wizardID234(id) {
		return empty, ErrWizardInvalid234
	}
	tx, e := w.pool.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	if e = w.authorize(ctx, tx, a); e != nil {
		return empty, e
	}
	r, p, e := w.read(ctx, tx, id, false)
	if e != nil {
		return empty, e
	}
	if !wizardOwner234(r, a) {
		return empty, identity.ErrAdminSession
	}
	ready := false
	if r.Status != "applying" && r.Status != "completed" {
		ready, _, e = wizardReadTarget234(ctx, tx)
		if e != nil {
			return empty, e
		}
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return empty, e
	}
	if e = w.authorize(ctx, tx, a); e != nil {
		return empty, e
	}
	v := wizardReadView234(p, ready, now)
	return v, tx.Commit(ctx)
}

func (w *Wizard234) Apply(ctx context.Context, a WizardActor234, id string, in ApplyInput234) (WizardView234, error) {
	var empty WizardView234
	if !wizardID234(id) || len(in.ExpectedSHA256) != 64 || !in.SourceFrozen || !in.ArchiveAcknowledged || in.Confirmation != "MIGRATE" {
		return empty, ErrWizardInvalid234
	}
	tx, e := w.pool.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	if e = w.authorize(ctx, tx, a); e != nil {
		return empty, e
	}
	r, p, e := w.read(ctx, tx, id, true)
	if e != nil {
		return empty, e
	}
	if !wizardOwner234(r, a) {
		return empty, identity.ErrAdminSession
	}
	if e = w.authorize(ctx, tx, a); e != nil {
		return empty, e
	}
	if in.ExpectedSHA256 != p.View.SHA256 {
		return empty, ErrWizardConflict234
	}
	if r.Status == "completed" {
		return p.View, nil
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return empty, e
	}
	if r.Status != "prepared" || !now.Before(r.Expires) {
		return empty, ErrWizardConflict234
	}
	r.Revision++
	r.Status = "applying"
	p.View.Status = "applying"
	p.View.Progress = 10
	if e = w.save(ctx, tx, &r, p, false); e != nil {
		return empty, e
	}
	if e = w.authorize(ctx, tx, a); e != nil {
		return empty, e
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, e
	}
	v, e := w.execute(ctx, a, r)
	if e != nil {
		w.finishFailed(ctx, r, e)
		return empty, e
	}
	return v, nil
}

func (w *Wizard234) execute(ctx context.Context, a WizardActor234, claim wizardRecord234) (WizardView234, error) {
	var empty WizardView234
	tx, e := w.pool.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(74973232)"); e != nil {
		return empty, e
	}
	ready, e := wizardTargetReady234(ctx, tx, true)
	if e != nil {
		return empty, e
	}
	if e = w.authorize(ctx, tx, a); e != nil {
		return empty, e
	}
	r, p, e := w.read(ctx, tx, claim.ID, true)
	if e != nil {
		return empty, e
	}
	if !wizardOwner234(r, a) || r.Revision != claim.Revision || r.Status != "applying" {
		return empty, ErrWizardConflict234
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return empty, e
	}
	if !now.Before(r.Expires) {
		return empty, ErrWizardConflict234
	}
	if !ready {
		return empty, ErrWizardTarget234
	}
	if e = w.inputs(ctx, tx, &r, &p); e != nil {
		return empty, e
	}
	plan, report, e := Validate(p.Snapshot)
	if e != nil || plan.Source != p.View.Source {
		return empty, ErrWizardConflict234
	}
	aead, e := w.aead()
	if e != nil {
		return empty, e
	}
	report, e = applyValidatedTx234(ctx, tx, w.key, plan, report, aead)
	if e != nil {
		return empty, e
	}
	if report.AlreadyImported || !report.Applied {
		return empty, ErrWizardConflict234
	}
	r.Revision++
	r.Status = "completed"
	p.View.Status = "completed"
	p.View.Progress = 100
	p.View.TargetReady = false
	publicReport := wizardPublicReport234(report)
	p.View.Report = &publicReport
	if e = w.save(ctx, tx, &r, p, false); e != nil {
		return empty, e
	}
	if e = w.authorize(ctx, tx, a); e != nil {
		return empty, e
	}
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return empty, e
	}
	if !now.Before(r.Expires) {
		return empty, ErrWizardConflict234
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, e
	}
	return p.View, nil
}

// Failure cleanup changes only the exact original claimed wizard record. It
// cannot authorize an import, refund, fee or any other business operation.
func (w *Wizard234) finishFailed(original context.Context, claim wizardRecord234, cause error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(original), 10*time.Second)
	defer cancel()
	tx, e := w.pool.Begin(ctx)
	if e != nil {
		return
	}
	defer tx.Rollback(ctx)
	r, p, e := w.read(ctx, tx, claim.ID, true)
	if e != nil || r.Status != "applying" || r.Revision != claim.Revision || r.Owner != claim.Owner || r.Subject != claim.Subject {
		return
	}
	code := "import_rolled_back"
	if errors.Is(cause, ErrWizardTarget234) {
		code = "target_not_empty"
	}
	if errors.Is(cause, ErrWizardBusy234) {
		code = "target_busy"
	}
	if errors.Is(cause, identity.ErrAdminSession) {
		code = "administrator_session_changed"
	}
	r.Revision++
	r.Status = "failed"
	p.View.Status = "failed"
	p.View.Progress = 0
	p.View.Issues = append(p.View.Issues, Issue{"run", code})
	if e = w.save(ctx, tx, &r, p, false); e != nil {
		return
	}
	_ = tx.Commit(ctx)
}
