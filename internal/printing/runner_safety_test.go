package printing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPrintingTaskPayloadStrictCanonical(t *testing.T) {
	for _, raw := range []string{`{"intent_id":"1","intent_id":"1"}`, `{"intent_id":"01"}`, `{"intent_id":1}`, `{"order_id":"1"}`, `{"intent_id":"1","other":"2"}`, `{"intent_id":"1"} {}`, `{"intent_id":"9223372036854775808"}`, `{"intent_id":"0"}`} {
		if _, e := parsePrintingTask([]byte(raw), "receipt-print.send"); !errors.Is(e, ErrInvalid) {
			t.Fatalf("accepted malformed payload %s: %v", raw, e)
		}
	}
	if id, e := parsePrintingTask([]byte(`{"intent_id":"9223372036854775807"}`), "receipt-print.send"); e != nil || id != 9223372036854775807 {
		t.Fatal(id, e)
	}
}
func TestPrintingDeadTaskRecoveryIdempotentAndBounded(t *testing.T) {
	s, p := printFixture(t)
	configurePrinting(t, s, 2)
	o, _, _, _ := printPaidOrder(t, p, "recover-original")
	j, e := s.PrepareAutomatic(context.Background(), o.ID)
	if e != nil || j == nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	original := j.TaskID
	if original <= 0 {
		t.Fatal("missing queue task")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE jobs SET state='dead',attempts=max_attempts WHERE id=$1", original); e != nil {
		t.Fatal(e)
	}
	view, e := s.Job(ctx, 901, j.ID, printOwner)
	if e != nil || view.State != "blocked" || !view.CanRecover {
		t.Fatal(view, e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	ids := make(chan int64, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.Recover(ctx, 901, j.ID, original, printOwner)
			errs <- e
			ids <- v.TaskID
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first int64
	for id := range ids {
		if first == 0 {
			first = id
		}
		if id != first || id == original {
			t.Fatal("lost response replay allocated replacement", first, id)
		}
	}
	current := first
	for n := 2; n <= 5; n++ {
		if _, e = s.pool.Exec(ctx, "UPDATE jobs SET state='dead',attempts=max_attempts WHERE id=$1", current); e != nil {
			t.Fatal(e)
		}
		v, e := s.Recover(ctx, 901, j.ID, current, printOwner)
		if e != nil {
			t.Fatal(n, e)
		}
		current = v.TaskID
	}
	if _, e = s.pool.Exec(ctx, "UPDATE jobs SET state='dead',attempts=max_attempts WHERE id=$1", current); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Recover(ctx, 901, j.ID, current, printOwner); !errors.Is(e, ErrState) {
		t.Fatal("unbounded recovery", e)
	}
	old, e := s.Recover(ctx, 901, j.ID, original, printOwner)
	if e != nil || old.TaskID != first {
		t.Fatal("old replay consumed budget", old, e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM printing_recoveries WHERE intent_id=$1", j.ID).Scan(&count); e != nil || count != 5 {
		t.Fatal(count, e)
	}
}

type blockingPrintConnector struct{ sent chan struct{} }

func (blockingPrintConnector) LocalOnly() bool { return true }
func (b blockingPrintConnector) Send(ctx context.Context, _ PrintCommand) (Outcome, error) {
	close(b.sent)
	<-ctx.Done()
	return Outcome{}, ctx.Err()
}
func TestPrintingRunnerCancellationAfterClaimKeepsUnknownAndNeverResends(t *testing.T) {
	s, p := printFixture(t)
	configurePrinting(t, s, 2)
	o, _, _, _ := printPaidOrder(t, p, "runner-cancel")
	j, e := s.PrepareAutomatic(context.Background(), o.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(context.Background(), "UPDATE jobs SET state='succeeded' WHERE kind='receipt-print.prepare'"); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := blockingPrintConnector{make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, e := s.runOne(ctx, true, true, b, time.Second); done <- e }()
	select {
	case <-b.sent:
	case <-time.After(5 * time.Second):
		t.Fatal("connector not reached")
	}
	cancel()
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled run did not finish")
	}
	v, e := s.Job(context.Background(), 901, j.ID, printOwner)
	if e != nil || v.State != "unknown" || v.CanRecover {
		t.Fatal(v, e)
	}
	if _, e = s.Dispatch(context.Background(), j.ID, LocalFake{}); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = s.pool.QueryRow(context.Background(), "SELECT count(*) FROM printing_claims WHERE intent_id=$1", j.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal(fmt.Sprint(count), e)
	}
}
