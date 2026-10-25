package settlementscheduler

import (
	"context"
	"errors"
	"goepay/internal/platform/jobs"
	"reflect"
	"testing"
	"time"
)

type fakeQueue struct {
	claimed                []string
	complete, retry, renew int
	reason                 string
	renewErr               error
	renewDeadline          time.Duration
}

func (q *fakeQueue) Claim(_ context.Context, k []string, _ time.Duration) (jobs.Job, error) {
	q.claimed = k
	return jobs.Job{ID: 1, Kind: GenerateKind, Payload: []byte(`{"run_id":"1"}`), Attempts: 1, Token: "lease"}, nil
}
func (q *fakeQueue) Renew(ctx context.Context, _ jobs.Job, _ time.Duration) error {
	q.renew++
	if deadline, ok := ctx.Deadline(); ok {
		q.renewDeadline = time.Until(deadline)
	}
	return q.renewErr
}
func (q *fakeQueue) Complete(context.Context, jobs.Job) error { q.complete++; return nil }
func (q *fakeQueue) Retry(_ context.Context, _ jobs.Job, reason string, _ time.Duration) error {
	q.retry++
	q.reason = reason
	return nil
}

type waitGenerator struct{ wait bool }

func (g waitGenerator) GenerateSettlementPlanRun(ctx context.Context, _ int64) error {
	if g.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
func TestRunnerOnlyClaimsItsKindAndDefaultOffDoesNothing(t *testing.T) {
	q := &fakeQueue{}
	r := Runner{Queue: q, Handler: Handler{Store: &fakeStore{}}}
	if e := r.ProcessOne(context.Background()); !errors.Is(e, ErrDisabled) || q.claimed != nil {
		t.Fatal("off runner claimed", e, q.claimed)
	}
	r.Scheduler.Enabled = true
	if e := r.ProcessOne(context.Background()); e != nil || !reflect.DeepEqual(q.claimed, []string{GenerateKind}) || q.complete != 1 || q.retry != 0 {
		t.Fatal("wrong queue completion", e, q)
	}
}
func TestRunnerLeaseLossCancelsWithoutCompletingOrRetrying(t *testing.T) {
	q := &fakeQueue{renewErr: jobs.ErrLeaseLost}
	r := Runner{Scheduler: Scheduler{Enabled: true}, Queue: q, Handler: Handler{Store: waitGenerator{true}}, Lease: time.Second, RenewInterval: time.Millisecond * 10}
	if e := r.ProcessOne(context.Background()); !errors.Is(e, jobs.ErrLeaseLost) || q.renew == 0 || q.complete != 0 || q.retry != 0 {
		t.Fatal("lease fence bypassed", e, q)
	}
	if q.renewDeadline <= 0 || q.renewDeadline > time.Second/2 {
		t.Fatal("renewal may hang past the original lease", q.renewDeadline)
	}
}
func TestRunnerRetriesFixedReasonRatherThanCoreError(t *testing.T) {
	q := &fakeQueue{}
	r := Runner{Scheduler: Scheduler{Enabled: true}, Queue: q, Handler: Handler{Store: &fakeStore{err: errors.New("private-account@local.test")}}}
	if e := r.ProcessOne(context.Background()); e != nil || q.retry != 1 || q.complete != 0 || q.reason != "settlement generation unresolved; query original run" {
		t.Fatal("private reason persisted", e, q)
	}
}
