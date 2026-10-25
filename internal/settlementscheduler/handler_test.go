package settlementscheduler

import (
	"context"
	"errors"
	"goepay/internal/platform/jobs"
	"testing"
)

type fakeStore struct {
	ticks     int
	generated []int64
	err       error
}

func (s *fakeStore) ScheduleSettlementPlans(context.Context) error { s.ticks++; return s.err }
func (s *fakeStore) GenerateSettlementPlanRun(_ context.Context, id int64) error {
	s.generated = append(s.generated, id)
	return s.err
}
func TestHandlerAcceptsOnlyCanonicalUniqueRunID(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"run_id":1}`, `{"run_id":null}`, `{"run_id":"0"}`, `{"run_id":"01"}`, `{"run_id":"-1"}`, `{"run_id":"1","run_id":"2"}`, `{"run_id":"1","extra":"2"}`, `{"run_id":"9223372036854775808"}`, `{"run_id":"1"} {}`, `{"run_id":"1e2"}`} {
		s := &fakeStore{}
		if err := (Handler{Store: s}).Handle(context.Background(), jobs.Job{Kind: GenerateKind, Payload: []byte(raw)}); err == nil || len(s.generated) != 0 {
			t.Fatalf("unsafe payload accepted %q: %v %+v", raw, err, s.generated)
		}
	}
	s := &fakeStore{}
	h := Handler{Store: s}
	if err := h.Handle(context.Background(), jobs.Job{Kind: "payment.notify", Payload: []byte(`{"run_id":"1"}`)}); err == nil || len(s.generated) != 0 {
		t.Fatal("wrong kind accepted", err)
	}
	if err := h.Handle(context.Background(), jobs.Job{Kind: GenerateKind, Payload: []byte(`{"run_id":"12"}`)}); err != nil || len(s.generated) != 1 || s.generated[0] != 12 {
		t.Fatal("valid run rejected", err, s.generated)
	}
	s.err = errors.New("local failure")
	if err := h.Handle(context.Background(), jobs.Job{Kind: GenerateKind, Payload: []byte(`{"run_id":"12"}`)}); !errors.Is(err, s.err) {
		t.Fatal("handler hid core error", err)
	}
}
func TestSchedulerDefaultDisabledAndTickFailure(t *testing.T) {
	s := &fakeStore{}
	if err := (Scheduler{Store: s}).Tick(context.Background()); !errors.Is(err, ErrDisabled) || s.ticks != 0 {
		t.Fatal("default scheduler executed", err, s.ticks)
	}
	if err := (Scheduler{Store: s, Enabled: true}).Tick(context.Background()); err != nil || s.ticks != 1 {
		t.Fatal(err, s.ticks)
	}
	s.err = errors.New("tick failed")
	if err := (Scheduler{Store: s, Enabled: true}).Tick(context.Background()); !errors.Is(err, s.err) {
		t.Fatal("tick error hidden", err)
	}
}
