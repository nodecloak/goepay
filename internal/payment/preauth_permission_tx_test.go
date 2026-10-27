package payment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestPreauthObservation68ExplicitTransactionPermissionCannotBeOmitted(t *testing.T) {
	s, g, o, token, p := observationFixture68(t)
	ctx := t.Context()
	in := PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("f", 64)}
	for _, checks := range [][]func(context.Context, pgx.Tx) error{nil, {nil}, {observationCheckTx68, observationCheckTx68}} {
		if _, e := g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68, checks...); !errors.Is(e, ErrPreauthObservation) || p.reads.Load() != 0 {
			t.Fatal("missing or ambiguous transaction permission allowed a provider read", e, p.reads.Load())
		}
	}
	var count int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM preauth_observations`).Scan(&count); e != nil || count != 0 {
		t.Fatal("invalid transaction permission created an observation", count, e)
	}
	q, e := g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68, observationCheckTx68)
	if e != nil || !q.SourceVerified || p.reads.Load() != 1 {
		t.Fatal("explicit transaction permission baseline failed", q, e, p.reads.Load())
	}
	if _, e = g.ReadPreauthorizationObservation(ctx, q.ID, token, "merchant", observationCheck68); !errors.Is(e, ErrPreauthObservation) {
		t.Fatal("historical read silently fell back to a context-only permission", e)
	}
	g.AllowLive = true
	if _, e = g.ReleaseRemainingAuthorization(ctx, o.MerchantID, o.TradeNo, PreauthRemainingReleaseInput{ExpectedObservationID: q.ID, ExpectedRemainingCents: 450}, token, "merchant", observationCheck68); e == nil {
		t.Fatal("remaining release silently fell back to a context-only permission")
	}
	var sources, claims, journals int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM authorization_remaining_sources),(SELECT count(*) FROM authorization_remaining_claims),(SELECT count(*) FROM journals)`).Scan(&sources, &claims, &journals); e != nil || sources != 0 || claims != 0 || journals != 0 || p.reads.Load() != 1 {
		t.Fatal("missing transaction permission changed funds/source/send state", sources, claims, journals, p.reads.Load(), e)
	}
}

func TestPreauthObservation68TransactionPermissionSeesAndRejectsFinalResultWrite(t *testing.T) {
	s, g, o, token, p := observationFixture68(t)
	ctx := t.Context()
	finalChecks := 0
	checkTx := func(c context.Context, tx pgx.Tx) error {
		var results int
		// This result is still uncommitted. Only the supplied transaction can
		// see it; neither a pool reader nor a pre-write check is equivalent.
		if e := tx.QueryRow(c, `SELECT count(*) FROM preauth_observations WHERE trade_no=$1 AND status='verified'`, o.TradeNo).Scan(&results); e != nil {
			return e
		}
		if results != 0 {
			finalChecks++
			return ErrState
		}
		return nil
	}
	q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("e", 64)}, token, "merchant", observationCheck68, checkTx)
	if e == nil || q.SourceVerified || finalChecks != 1 || p.reads.Load() != 1 {
		t.Fatal("result commit bypassed the final in-transaction permission", q, e, finalChecks, p.reads.Load())
	}
	var verified, unknown int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status='verified'),count(*) FILTER(WHERE status='unknown') FROM preauth_observations`).Scan(&verified, &unknown); e != nil || verified != 0 || unknown != 1 {
		t.Fatal("rejected result was published or its read claim reopened", verified, unknown, e)
	}
}

func TestPreauthRemaining75TransactionPermissionSeesAndRejectsFinalSendClaim(t *testing.T) {
	s, g, o, token, p := observationFixture68(t)
	ctx := t.Context()
	q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("d", 64)}, token, "merchant", observationCheck68, observationCheckTx68)
	if e != nil {
		t.Fatal(e)
	}
	in := PreauthRemainingReleaseInput{ExpectedObservationID: q.ID, ExpectedRemainingCents: 450}
	if _, e = s.bindPreauthRemainingRelease(remainingSourceGuard75(t, s), o.MerchantID, o.TradeNo, in, token, "merchant"); e != nil {
		t.Fatal(e)
	}
	checks, finalChecks := 0, 0
	checkTx := func(c context.Context, tx pgx.Tx) error {
		checks++
		var claims int
		if e := tx.QueryRow(c, `SELECT count(*) FROM authorization_remaining_claims WHERE order_id=$1`, o.ID).Scan(&claims); e != nil {
			return e
		}
		if claims != 0 {
			finalChecks++
			return ErrState
		}
		return s.observationAlive(c, tx, q)
	}
	if _, e = s.markPreauthRemainingSent(ctx, o.TradeNo, 1, checkTx); !errors.Is(e, ErrPreauthObservation) || checks != 2 || finalChecks != 1 {
		t.Fatal("send claim bypassed either original in-transaction permission", e, checks, finalChecks)
	}
	r, e := s.AuthorizationRelease(ctx, o.MerchantID, o.TradeNo)
	if e != nil || r.Sent || r.State != "checking" || r.Amount != 450 || r.SourceRevision != 1 || p.reads.Load() != 1 {
		t.Fatal("failed final permission changed the original source or send boundary", r, e, p.reads.Load())
	}
	var claims, claimAudits, journals int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM authorization_remaining_claims),(SELECT count(*) FROM backoffice_audit WHERE resource='authorization-remaining-sources' AND operation='claim'),(SELECT count(*) FROM journals)`).Scan(&claims, &claimAudits, &journals); e != nil || claims != 0 || claimAudits != 0 || journals != 0 {
		t.Fatal("failed final permission did not roll back claim and audit", claims, claimAudits, journals, e)
	}
}
