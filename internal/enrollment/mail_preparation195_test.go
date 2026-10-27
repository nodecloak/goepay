package enrollment

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
)

type preparingSender195 struct {
	prepare  func(context.Context, pgx.Tx) (Sender, error)
	fallback atomic.Int32
}

func (p *preparingSender195) PrepareVerification(ctx context.Context, tx pgx.Tx) (Sender, error) {
	return p.prepare(ctx, tx)
}
func (p *preparingSender195) SendVerification(context.Context, string, string, string) error {
	p.fallback.Add(1)
	return nil
}

func TestMailPreparation195RejectsAccountSourceBeforeBudgetsChallengeOrFallback(t *testing.T) {
	s, _ := enrollmentFixture(t)
	var prepared atomic.Int32
	sender := &preparingSender195{prepare: func(context.Context, pgx.Tx) (Sender, error) {
		prepared.Add(1)
		return nil, errors.New("synthetic unavailable account source")
	}}
	s.sender = sender
	if _, e := s.Issue(t.Context(), VerificationRequest{Purpose: "recover", Email: "unavailable195@example.test", Source: "192.0.2.195"}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("unavailable prepared account fell through", e)
	}
	var rows int
	if e := s.pool.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM enrollment_challenges)+(SELECT count(*)FROM enrollment_limits)`).Scan(&rows); e != nil || rows != 0 || prepared.Load() != 1 || sender.fallback.Load() != 0 {
		t.Fatal("unavailable account left durable attempt or fallback send", rows, prepared.Load(), sender.fallback.Load(), e)
	}
}

func TestMailPreparation195BindsOneSenderBeforeCommitAndOnlySendsAfterCommit(t *testing.T) {
	s, _ := enrollmentFixture(t)
	var prepared, sends atomic.Int32
	sender := &preparingSender195{prepare: func(ctx context.Context, tx pgx.Tx) (Sender, error) {
		var n int
		if e := tx.QueryRow(ctx, `SELECT count(*)FROM enrollment_challenges`).Scan(&n); e != nil || n != 0 {
			t.Fatal("account was not selected before challenge write", n, e)
		}
		prepared.Add(1)
		return SenderFunc(func(ctx context.Context, email, purpose, code string) error {
			var sent bool
			if e := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM enrollment_challenges WHERE state='sending')`).Scan(&sent); e != nil || !sent {
				t.Fatal("delivery before committed admission", sent, e)
			}
			if email != "bound195@example.test" || purpose != "recover" || len(code) != 7 {
				t.Fatal("prepared sender lost original envelope")
			}
			sends.Add(1)
			return nil
		}), nil
	}}
	s.sender = sender
	if _, e := s.Issue(t.Context(), VerificationRequest{Purpose: "recover", Email: "bound195@example.test", Source: "192.0.2.195"}); e != nil {
		t.Fatal(e)
	}
	if prepared.Load() != 1 || sends.Load() != 1 || sender.fallback.Load() != 0 {
		t.Fatal("original prepared account unused or duplicated", prepared.Load(), sends.Load(), sender.fallback.Load())
	}
}
