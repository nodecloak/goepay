package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"goepay/internal/platform/jobs"
)

type scheduledJobFact struct {
	ID             int64
	Attempt        int
	Kind, EventKey string
	PayloadHash    []byte
}
type scheduledAdmissionFacts struct {
	RunID      int64
	IntentHash []byte
	Task       string
	Generation int64
	Job        scheduledJobFact
	Until      time.Time
}

var scheduledJobTasks = []string{"notify", "order_query", "transfer", "settle", "profitsharing"}

func scheduledJobDigest(raw []byte) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if e := decoder.Decode(&value); e != nil {
		return nil, e
	}
	canonical, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	digest := sha256.Sum256(canonical)
	return digest[:], nil
}

// Current partial schema loss must never restore the legacy worker path.
func scheduledAdmissionSchema(ctx context.Context, tx pgx.Tx) (bool, error) {
	var tasks, admissions, claims, migrations bool
	e := tx.QueryRow(ctx, `SELECT to_regclass('scheduled_tasks') IS NOT NULL,to_regclass('scheduled_job_admissions') IS NOT NULL,to_regclass('scheduled_job_claims') IS NOT NULL,to_regclass('schema_migrations') IS NOT NULL`).Scan(&tasks, &admissions, &claims, &migrations)
	if e != nil {
		return false, e
	}
	if tasks && admissions && claims {
		return true, nil
	}
	if tasks || admissions || claims {
		return false, ErrConflict
	}
	var audits bool
	if e = tx.QueryRow(ctx, `SELECT to_regclass('backoffice_audit') IS NOT NULL`).Scan(&audits); e != nil {
		return false, e
	}
	if audits {
		var configuredOrigin bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scheduled-tasks' AND operation='configure')`).Scan(&configuredOrigin); e != nil {
			return false, e
		}
		if configuredOrigin {
			return false, ErrConflict
		}
	}
	if migrations {
		var registered bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000100_real_scheduled_tasks.up.sql')`).Scan(&registered); e != nil {
			return false, e
		}
		if registered {
			return false, ErrConflict
		}
	}
	return false, nil
}

// This predicate describes only original cron work. Explicit manual notification
// requests retain their own original permissions, even when coalesced with an
// automatic job. Preauthorization capture/release and other reconciliation kinds
// remain independent of unpaid ordinary payment polling.
func scheduledManagedPredicate(key string) string {
	explicitRetry := `NOT EXISTS(SELECT 1 FROM backoffice_audit jb WHERE jb.resource='jobs' AND jb.resource_id=jobs.id AND jb.operation='retry' AND jb.actor_role='admin')`
	switch key {
	case "notify":
		return `(jobs.kind='payment.notify' AND jobs.attempts>0 AND NOT EXISTS(SELECT 1 FROM notification_resend_requests nr WHERE nr.job_id=jobs.id) AND ` + explicitRetry + `)`
	case "order_query":
		return `(jobs.kind='payment.reconcile' AND jobs.event_key='payment-reconcile:'||(jobs.payload->>'trade_no') AND EXISTS(SELECT 1 FROM orders o JOIN provider_attempts pa ON pa.order_id=o.id WHERE o.trade_no=jobs.payload->>'trade_no' AND pa.method<>'preauth') AND ` + explicitRetry + `)`
	case "transfer":
		return `((jobs.kind='settlement-auto.payout' AND EXISTS(SELECT 1 FROM settlement_auto_authorizations sa JOIN settlement_plan_versions v ON(v.plan_id,v.revision)=(sa.plan_id,sa.plan_revision) WHERE sa.id::text=jobs.payload->>'authorization_id' AND v.mode='balance_threshold')) OR (jobs.kind='settlement-plan.generate' AND EXISTS(SELECT 1 FROM settlement_plan_runs sr JOIN settlement_plan_versions v ON(v.plan_id,v.revision)=(sr.plan_id,sr.plan_revision) WHERE sr.id::text=jobs.payload->>'run_id' AND v.mode='balance_threshold')))`
	case "settle":
		return strings.ReplaceAll(scheduledManagedPredicate("transfer"), "'balance_threshold'", "'generate_only'")
	case "profitsharing":
		return `(jobs.kind IN('channel-sharing.reconcile','channel-sharing-finish.reconcile'))`
	}
	return `false`
}

func (s *Store) ClaimFilter(ctx context.Context, tx pgx.Tx, arg int) (string, []any, error) {
	ready, e := scheduledAdmissionSchema(ctx, tx)
	if e != nil || !ready {
		return "", nil, e
	}
	filter := ""
	values := []any{}
	for _, key := range scheduledJobTasks {
		p, configured, e := s.scheduledPolicy(ctx, tx, key, false)
		if e != nil {
			return "", nil, e
		}
		if !configured {
			continue
		}
		ids := []int64{}
		if p.Enabled {
			rows, e := tx.Query(ctx, `SELECT a.job_id FROM scheduled_job_admissions a JOIN scheduled_task_runs r ON r.id=a.run_id JOIN jobs j ON j.id=a.job_id WHERE r.task_key=$1 AND r.generation=$2 AND a.valid_until>clock_timestamp() AND a.next_attempt=j.attempts+1 AND NOT EXISTS(SELECT 1 FROM scheduled_job_claims c WHERE c.admission_id=a.id)`, key, p.Generation)
			if e != nil {
				return "", nil, e
			}
			for rows.Next() {
				var id int64
				if e = rows.Scan(&id); e != nil {
					rows.Close()
					return "", nil, e
				}
				ids = append(ids, id)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return "", nil, e
			}
		}
		filter += fmt.Sprintf(` AND (NOT %s OR jobs.id=ANY($%d::bigint[]))`, scheduledManagedPredicate(key), arg+len(values))
		values = append(values, ids)
	}
	return filter, values, nil
}

func (s *Store) scheduledJobTask(ctx context.Context, tx pgx.Tx, j jobs.Job) (string, error) {
	for _, key := range scheduledJobTasks {
		var matches bool
		// Queue has incremented attempts already; the first delivery stays independent.
		predicate := scheduledManagedPredicate(key)
		if key == "notify" {
			predicate = strings.Replace(predicate, "jobs.attempts>0", "jobs.attempts>1", 1)
		}
		if e := tx.QueryRow(ctx, `SELECT `+predicate+` FROM jobs WHERE id=$1`, j.ID).Scan(&matches); e != nil {
			return "", e
		}
		if matches {
			return key, nil
		}
	}
	return "", nil
}
func (s *Store) readScheduledAdmission(ctx context.Context, tx pgx.Tx, id int64) (scheduledAdmissionFacts, error) {
	var facts scheduledAdmissionFacts
	var raw, proof []byte
	var run, job int64
	var attempt int
	var until time.Time
	e := tx.QueryRow(ctx, `SELECT run_id,job_id,next_attempt,facts,valid_until,proof_hash FROM scheduled_job_admissions WHERE id=$1`, id).Scan(&run, &job, &attempt, &raw, &until, &proof)
	if e != nil {
		return facts, e
	}
	if decodeScheduledJSON(raw, &facts) != nil {
		return facts, ErrConflict
	}
	expected, e := s.referralDigest("scheduler171/job-admission", []any{id, facts})
	if e != nil || !hmac.Equal(expected, proof) || facts.RunID != run || facts.Job.ID != job || facts.Job.Attempt != attempt || !facts.Until.Equal(until) {
		return facts, ErrConflict
	}
	r, e := s.readScheduledRun(ctx, tx, run)
	if e != nil || !hmac.Equal(facts.IntentHash, r.proof) || facts.Task != r.facts.Policy.Task || facts.Generation != r.facts.Policy.Generation {
		return facts, ErrConflict
	}
	return facts, nil
}
func (s *Store) AuthorizeClaim(ctx context.Context, tx pgx.Tx, j jobs.Job) error {
	ready, e := scheduledAdmissionSchema(ctx, tx)
	if e != nil || !ready {
		return e
	}
	if e = s.verifyScheduledNotificationBudget(ctx, tx, j); e != nil {
		return e
	}
	key, e := s.scheduledJobTask(ctx, tx, j)
	if e != nil || key == "" {
		return e
	}
	p, configured, e := s.scheduledPolicy(ctx, tx, key, false)
	if e != nil || !configured {
		return e
	}
	if !p.Enabled {
		return ErrState
	}
	var id int64
	e = tx.QueryRow(ctx, `SELECT a.id FROM scheduled_job_admissions a JOIN scheduled_task_runs r ON r.id=a.run_id WHERE a.job_id=$1 AND a.next_attempt=$2 AND r.task_key=$3 AND r.generation=$4 AND a.valid_until>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM scheduled_job_claims c WHERE c.admission_id=a.id) ORDER BY a.id LIMIT 1 FOR UPDATE OF a`, j.ID, j.Attempts, key, p.Generation).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrState
	}
	if e != nil {
		return e
	}
	f, e := s.readScheduledAdmission(ctx, tx, id)
	if e != nil {
		return e
	}
	digest, e := scheduledJobDigest(j.Payload)
	if e != nil || f.Job.Kind != j.Kind || f.Job.EventKey != j.EventKey || !hmac.Equal(f.Job.PayloadHash, digest) {
		return ErrConflict
	}
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return e
	}
	if !now.Before(f.Until) {
		return ErrState
	}
	token := sha256.Sum256([]byte(j.Token))
	proof, e := s.referralDigest("scheduler171/job-claim", []any{id, j.ID, j.Attempts, token[:], now.Format(time.RFC3339Nano)})
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO scheduled_job_claims(admission_id,job_id,attempt,token_hash,claimed_at,proof_hash)VALUES($1,$2,$3,$4,$5,$6)`, id, j.ID, j.Attempts, token[:], now, proof)
	if e != nil {
		return e
	}
	// The independent finite admission is not a replacement for the original
	// job lease. Recheck both after the last claim write and any trigger/lock wait.
	var final time.Time
	var originalLeaseLive bool
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp(),EXISTS(SELECT 1 FROM jobs WHERE id=$1 AND state='running' AND lease_token=$2 AND attempts=$3 AND kind=$4 AND event_key=$5 AND payload=$6::jsonb AND lease_until>clock_timestamp())`, j.ID, j.Token, j.Attempts, j.Kind, j.EventKey, j.Payload).Scan(&final, &originalLeaseLive); e != nil {
		return e
	}
	if !originalLeaseLive {
		return jobs.ErrLeaseLost
	}
	if !final.Before(f.Until) {
		return ErrState
	}
	return nil
}

// VerifyScheduledJobClaim also protects alternate consumers that forgot the
// queue hook. A consumed finite claim remains valid for its original in-flight
// lease if an administrator changes the next scheduling policy afterwards.
func (s *Store) VerifyScheduledJobClaim(ctx context.Context, j jobs.Job) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = s.verifyScheduledJobClaimTx(ctx, tx, j); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) verifyScheduledJobClaimTx(ctx context.Context, tx pgx.Tx, j jobs.Job) error {
	ready, e := scheduledAdmissionSchema(ctx, tx)
	if e != nil || !ready {
		return e
	}
	potential := []string{}
	switch j.Kind {
	case "payment.notify":
		potential = []string{"notify"}
	case "payment.reconcile":
		potential = []string{"order_query"}
	case SettlementAutoJobKind:
		potential = []string{"transfer", "settle"}
	case "settlement-plan.generate":
		potential = []string{"transfer", "settle"}
	case "channel-sharing.reconcile", "channel-sharing-finish.reconcile":
		potential = []string{"profitsharing"}
	default:
		return nil
	}
	controlled := false
	for _, candidate := range potential {
		_, configured, err := s.scheduledPolicy(ctx, tx, candidate, false)
		if err != nil {
			return err
		}
		controlled = controlled || configured
	}
	if !controlled {
		return nil
	}
	var actual jobs.Job
	e = tx.QueryRow(ctx, `SELECT id,kind,event_key,payload,attempts,lease_token FROM jobs WHERE id=$1 AND state='running' AND lease_until>clock_timestamp() AND lease_token=$2`, j.ID, j.Token).Scan(&actual.ID, &actual.Kind, &actual.EventKey, &actual.Payload, &actual.Attempts, &actual.Token)
	if e != nil {
		return ErrState
	}
	if e = s.verifyScheduledNotificationBudget(ctx, tx, actual); e != nil {
		return e
	}
	actualDigest, ae := scheduledJobDigest(actual.Payload)
	givenDigest, ge := scheduledJobDigest(j.Payload)
	if actual.Kind != j.Kind || actual.Attempts != j.Attempts || ae != nil || ge != nil || !hmac.Equal(actualDigest, givenDigest) {
		return ErrConflict
	}
	key, e := s.scheduledJobTask(ctx, tx, actual)
	if e != nil || key == "" {
		return e
	}
	_, configured, e := s.scheduledPolicy(ctx, tx, key, false)
	if e != nil || !configured {
		return e
	}
	token := sha256.Sum256([]byte(j.Token))
	var id int64
	var at time.Time
	var proof []byte
	e = tx.QueryRow(ctx, `SELECT admission_id,claimed_at,proof_hash FROM scheduled_job_claims WHERE job_id=$1 AND attempt=$2 AND token_hash=$3`, j.ID, j.Attempts, token[:]).Scan(&id, &at, &proof)
	if e != nil {
		return ErrState
	}
	expected, e := s.referralDigest("scheduler171/job-claim", []any{id, j.ID, j.Attempts, token[:], at.UTC().Format(time.RFC3339Nano)})
	if e != nil || !hmac.Equal(expected, proof) {
		return ErrConflict
	}
	f, e := s.readScheduledAdmission(ctx, tx, id)
	digest, de := scheduledJobDigest(j.Payload)
	if e != nil || de != nil || f.Task != key || f.Job.ID != j.ID || f.Job.Attempt != j.Attempts || f.Job.Kind != j.Kind || f.Job.EventKey != actual.EventKey || !hmac.Equal(f.Job.PayloadHash, digest) || !at.Before(f.Until) {
		return ErrConflict
	}
	return nil
}

func (s *Store) scheduledJobSnapshot(ctx context.Context, tx pgx.Tx, p scheduledTaskPolicy, now time.Time) ([]scheduledJobFact, error) {
	if p.Task == "profitsharing" {
		return s.snapshotScheduledSharingJobs(ctx, tx, p)
	}
	predicate := scheduledManagedPredicate(p.Task)
	extra := ""
	args := []any{}
	if p.Task == "notify" {
		extra = ` AND jobs.event_key='notify:'||(jobs.payload->>'trade_no') AND EXISTS(SELECT 1 FROM orders o WHERE o.trade_no=jobs.payload->>'trade_no' AND o.status='paid' AND o.paid_at>=(date_trunc('day',clock_timestamp() AT TIME ZONE 'Asia/Shanghai')-interval '1 day')AT TIME ZONE 'Asia/Shanghai')`
	}
	if p.Task == "order_query" {
		extra = ` AND EXISTS(SELECT 1 FROM orders o WHERE o.trade_no=jobs.payload->>'trade_no' AND o.status IN('created','submitting','pending','unknown') AND o.created_at<=$1::timestamptz-$2::bigint*interval '1 minute' AND o.created_at>$1::timestamptz-($2::bigint+360)*interval '1 minute')`
		args = []any{now, p.Params["unpaid_minutes"]}
	}
	if p.Task == "transfer" || p.Task == "settle" {
		scope, ok := ctx.Value(scheduledAutoQueueScopeKey{}).(scheduledAutoQueueScope)
		if !ok {
			return nil, ErrState
		}
		extra = ` AND (EXISTS(SELECT 1 FROM settlement_plan_items si JOIN settlement_plan_runs sr ON sr.id=si.run_id WHERE si.request_id::text=jobs.payload->>'request_id' AND sr.id=ANY($1::bigint[]) AND jobs.payload->>'authorization_id'=ANY($2::text[])) OR (jobs.kind='settlement-plan.generate' AND jobs.payload->>'run_id'=ANY($3::text[])))`
		ids := make([]string, len(scope.AuthorityIDs))
		for i, id := range scope.AuthorityIDs {
			ids[i] = fmt.Sprint(id)
		}
		runs := make([]string, len(scope.RunIDs))
		for i, id := range scope.RunIDs {
			runs[i] = fmt.Sprint(id)
		}
		args = []any{scope.RunIDs, ids, runs}
	}
	limit := 20
	if p.Task == "order_query" {
		limit = 50
	}
	if p.Task == "transfer" || p.Task == "settle" {
		limit = 100
	}
	rows, e := tx.Query(ctx, `SELECT jobs.id,jobs.kind,jobs.event_key,jobs.payload,jobs.attempts+1 FROM jobs WHERE `+predicate+extra+` AND jobs.attempts<jobs.max_attempts AND jobs.run_at<=clock_timestamp() AND(jobs.state='pending' OR jobs.state='running' AND jobs.lease_until<clock_timestamp()) ORDER BY jobs.run_at,jobs.id LIMIT `+fmt.Sprint(limit), args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []scheduledJobFact{}
	for rows.Next() {
		var f scheduledJobFact
		var payload []byte
		if e = rows.Scan(&f.ID, &f.Kind, &f.EventKey, &payload, &f.Attempt); e != nil {
			return nil, e
		}
		f.PayloadHash, e = scheduledJobDigest(payload)
		if e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) snapshotScheduledSharingJobs(ctx context.Context, tx pgx.Tx, p scheduledTaskPolicy) ([]scheduledJobFact, error) {
	out := []scheduledJobFact{}
	for _, phase := range []struct{ kind, parameter, prefix, field string }{{"channel-sharing.reconcile", "profitsharing_limit", "channel-sharing:", "sharing_id"}, {"channel-sharing-finish.reconcile", "settle_query_limit", "channel-sharing-finish:", "finish_id"}} {
		limit := p.Params[phase.parameter]
		if limit > 100 {
			limit = 100
		}
		rows, e := tx.Query(ctx, `SELECT id,kind,event_key,payload,attempts+1 FROM jobs WHERE kind=$1 AND event_key=$2||(payload->>$3)||':reconcile' AND attempts<max_attempts AND run_at<=clock_timestamp() AND(state='pending' OR state='running' AND lease_until<clock_timestamp()) ORDER BY run_at,id LIMIT $4`, phase.kind, phase.prefix, phase.field, limit)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var f scheduledJobFact
			var payload []byte
			if e = rows.Scan(&f.ID, &f.Kind, &f.EventKey, &payload, &f.Attempt); e != nil {
				rows.Close()
				return nil, e
			}
			f.PayloadHash, e = scheduledJobDigest(payload)
			if e != nil {
				rows.Close()
				return nil, e
			}
			out = append(out, f)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}

func (s *Store) admitScheduledJobs(ctx context.Context, r scheduledTaskRun) (ScheduledTaskCounts, error) {
	var counts ScheduledTaskCounts
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return counts, e
	}
	defer tx.Rollback(ctx)
	var token []byte
	var epoch int64
	var until time.Time
	e = tx.QueryRow(ctx, `SELECT lease_token,lease_epoch,lease_until FROM scheduled_task_runs WHERE id=$1 AND state='running' FOR UPDATE`, r.id).Scan(&token, &epoch, &until)
	if e != nil {
		return counts, e
	}
	if epoch != r.epoch || !hmac.Equal(token, r.token) {
		return counts, ErrState
	}
	p, configured, e := s.scheduledPolicy(ctx, tx, r.facts.Policy.Task, false)
	if e != nil {
		return counts, e
	}
	if !configured || !p.Enabled || p.Generation != r.facts.Policy.Generation {
		return counts, ErrState
	}
	now, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return counts, e
	}
	if !now.Before(until) {
		return counts, ErrState
	}
	for _, job := range r.facts.Jobs {
		counts.Examined++
		var actual scheduledJobFact
		var payload []byte
		e = tx.QueryRow(ctx, `SELECT id,kind,event_key,payload,attempts+1 FROM jobs WHERE id=$1 AND attempts<max_attempts AND run_at<=clock_timestamp() AND(state='pending' OR state='running' AND lease_until<clock_timestamp()) FOR SHARE`, job.ID).Scan(&actual.ID, &actual.Kind, &actual.EventKey, &payload, &actual.Attempt)
		if errors.Is(e, pgx.ErrNoRows) {
			counts.Skipped++
			continue
		}
		if e != nil {
			return counts, e
		}
		actual.PayloadHash, e = scheduledJobDigest(payload)
		if e != nil || actual.ID != job.ID || actual.Kind != job.Kind || actual.EventKey != job.EventKey || actual.Attempt != job.Attempt || !hmac.Equal(actual.PayloadHash, job.PayloadHash) {
			counts.Skipped++
			continue
		}
		var existing int64
		e = tx.QueryRow(ctx, `SELECT id FROM scheduled_job_admissions WHERE run_id=$1 AND job_id=$2 AND next_attempt=$3`, r.id, job.ID, job.Attempt).Scan(&existing)
		if e == nil {
			if _, e = s.readScheduledAdmission(ctx, tx, existing); e != nil {
				return counts, e
			}
			counts.Enqueued++
			continue
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return counts, e
		}
		var id int64
		if e = tx.QueryRow(ctx, `SELECT nextval('scheduled_job_admissions_id_seq')`).Scan(&id); e != nil {
			return counts, e
		}
		f := scheduledAdmissionFacts{r.id, r.proof, r.facts.Policy.Task, r.facts.Policy.Generation, job, r.facts.CreatedAt.Add(5 * time.Minute)}
		if !now.Before(f.Until) {
			return counts, ErrState
		}
		proof, e := s.referralDigest("scheduler171/job-admission", []any{id, f})
		if e != nil {
			return counts, e
		}
		raw, e := json.Marshal(f)
		if e != nil {
			return counts, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO scheduled_job_admissions(id,run_id,job_id,next_attempt,facts,valid_until,proof_hash)VALUES($1,$2,$3,$4,$5::jsonb,$6,$7)`, id, r.id, job.ID, job.Attempt, string(raw), f.Until, proof); e != nil {
			return counts, e
		}
		counts.Enqueued++
	}
	final, e := scheduledDBNow(ctx, tx)
	if e != nil {
		return counts, e
	}
	if !final.Before(until) {
		return counts, ErrState
	}
	return counts, tx.Commit(ctx)
}
