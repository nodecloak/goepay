package payment

import (
	"context"
	"errors"
	"testing"
)

func TestOrderBatch185IndependentDedicatedLockSlotsAreBoundedAndRelease(t *testing.T) {
	s, _, _, _ := setupOperations(t)
	ctx := context.Background()
	var releases []func()
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	for i := int64(0); i < 4; i++ {
		release, e := s.acquireOrderBusinessBatch185Execution(ctx, 900000+i)
		if e != nil {
			t.Fatal(i, e)
		}
		releases = append(releases, release)
	}
	if release, e := s.acquireOrderBusinessBatch185Execution(ctx, 900009); !errors.Is(e, ErrConflict) {
		if release != nil {
			release()
		}
		t.Fatal("dedicated execution slots not bounded", e)
	}
	if n := s.pool.Stat().AcquiredConns(); n != 0 {
		t.Fatal("dedicated lock retained domain pool connections", n)
	}
	releases[3]()
	releases = releases[:3]
	if release, e := s.acquireOrderBusinessBatch185Execution(ctx, 900000); !errors.Is(e, ErrConflict) {
		if release != nil {
			release()
		}
		t.Fatal("same batch lost database session exclusion", e)
	}
	release, e := s.acquireOrderBusinessBatch185Execution(ctx, 900009)
	if e != nil {
		t.Fatal("failed acquisition leaked a slot", e)
	}
	release()
}
