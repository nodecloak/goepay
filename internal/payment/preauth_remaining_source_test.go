package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func remainingSourceGuard75(t *testing.T, s *Store) context.Context {
	t.Helper()
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, "CREATE TABLE remaining_grants75(action_key text PRIMARY KEY)"); e != nil {
		t.Fatal(e)
	}
	return WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "authorization-release-remaining" {
			return ErrInvalid
		}
		_, e := tx.Exec(c, "INSERT INTO remaining_grants75(action_key)VALUES($1)", key)
		return e
	})
}

func TestPreauthRemaining75SourceRevisionsKeepOriginalUAAndClaimExactlyOneBoundRemainder(t *testing.T) {
	s, g, o, token, p := observationFixture68(t)
	ctx := context.Background()
	q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("c", 64)}, token, "merchant", observationCheck68, observationCheckTx68)
	if e != nil {
		t.Fatal(e)
	}
	guarded := remainingSourceGuard75(t, s)
	in := PreauthRemainingReleaseInput{ExpectedObservationID: q.ID, ExpectedRemainingCents: 450}
	r, e := s.bindPreauthRemainingRelease(guarded, o.MerchantID, o.TradeNo, in, token, "merchant")
	if e != nil || r.Amount != 450 || r.OriginalAmount != 1000 || r.SourceRevision != 1 || r.RequestNo != fmt.Sprintf("UA%d", o.ID) || r.Sent {
		t.Fatal(r, e)
	}
	if e = s.MarkAuthorizationReleaseSent(ctx, o.TradeNo); !errors.Is(e, ErrState) {
		t.Fatal("old full-release writer claimed new remaining source", e)
	}
	if _, owner, e := s.BeginPreauthorizationCapture(ctx, o.TradeNo); e != nil || owner {
		t.Fatal("remaining intent failed to block capture", owner, e)
	}
	p.remaining = 400
	q2, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("d", 64)}, token, "merchant", observationCheck68, observationCheckTx68)
	if e != nil {
		t.Fatal(e)
	}
	in2 := PreauthRemainingReleaseInput{ExpectedObservationID: q2.ID, ExpectedRemainingCents: 400, ExpectedSourceRevision: 1}
	r, e = s.bindPreauthRemainingRelease(guarded, o.MerchantID, o.TradeNo, in2, token, "merchant")
	if e != nil || r.Amount != 400 || r.OriginalAmount != 1000 || r.SourceRevision != 2 || r.RequestNo != fmt.Sprintf("UA%d", o.ID) {
		t.Fatal(r, e)
	}
	if _, e = s.bindPreauthRemainingRelease(guarded, o.MerchantID, o.TradeNo, in, token, "merchant"); !errors.Is(e, ErrConflict) {
		t.Fatal("old confirmation adopted a new remainder", e)
	}
	if _, e = s.markPreauthRemainingSent(ctx, o.TradeNo, 1, observationCheckTx68); !errors.Is(e, ErrConflict) {
		t.Fatal("old source revision claimed new terms", e)
	}
	r, e = s.markPreauthRemainingSent(ctx, o.TradeNo, 2, observationCheckTx68)
	if e != nil || !r.Sent || r.Amount != 400 || r.State != "processing" {
		t.Fatal("original remaining send boundary", r, e)
	}
	if _, e = s.markPreauthRemainingSent(ctx, o.TradeNo, 2, observationCheckTx68); !errors.Is(e, ErrState) {
		t.Fatal("claimed remaining source rearmed", e)
	}
	if _, e = s.bindPreauthRemainingRelease(guarded, o.MerchantID, o.TradeNo, in2, token, "merchant"); e == nil {
		t.Fatal("sent source acquired new first-send authority")
	}
	var grants, sources, claims, journals int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM remaining_grants75),(SELECT count(*) FROM authorization_remaining_sources),(SELECT count(*) FROM authorization_remaining_claims),(SELECT count(*) FROM journals)").Scan(&grants, &sources, &claims, &journals); e != nil || grants != 2 || sources != 2 || claims != 1 || journals != 0 || p.reads.Load() != 2 {
		t.Fatal("binding/claim invented provider I/O or merchant money", grants, sources, claims, journals, p.reads.Load(), e)
	}
	bad, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer bad.Rollback(ctx)
	if _, e = bad.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
		t.Fatal(e)
	}
	if _, e = bad.Exec(ctx, "DELETE FROM authorization_remaining_sources WHERE revision=2 AND order_id=$1", o.ID); e != nil {
		t.Fatal(e)
	}
	if e = bad.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthorizationRelease(ctx, o.MerchantID, o.TradeNo); !errors.Is(e, ErrConflict) {
		t.Fatal("missing remaining source became legacy full release", e)
	}
}
