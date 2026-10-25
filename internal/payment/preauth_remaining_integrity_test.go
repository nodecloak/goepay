package payment

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPreauthRemaining75LostHistoryOrForgedHeadNeverBecomesLegacyOrSendable(t *testing.T) {
	for _, kind := range []string{"terms", "source-proof", "audit", "head-reset", "source-missing", "claim-missing", "claim-proof", "orphan-audit"} {
		t.Run(kind, func(t *testing.T) {
			s, g, o, token, _ := observationFixture68(t)
			ctx := context.Background()
			q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("e", 64)}, token, "merchant", observationCheck68)
			if e != nil {
				t.Fatal(e)
			}
			in := PreauthRemainingReleaseInput{ExpectedObservationID: q.ID, ExpectedRemainingCents: 450}
			if _, e = s.bindPreauthRemainingRelease(remainingSourceGuard75(t, s), o.MerchantID, o.TradeNo, in, token, "merchant"); e != nil {
				t.Fatal(e)
			}
			if kind == "claim-missing" || kind == "claim-proof" {
				if _, e = s.markPreauthRemainingSent(ctx, o.TradeNo, 1, observationCheck68); e != nil {
					t.Fatal(e)
				}
			}
			tx, e := s.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			if _, e = tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
				t.Fatal(e)
			}
			sql := map[string]string{
				"terms":          `UPDATE authorization_remaining_sources SET terms=jsonb_set(terms,'{amount}','"451"') WHERE order_id=$1`,
				"source-proof":   `UPDATE authorization_remaining_sources SET proof_hash=decode(repeat('01',32),'hex') WHERE order_id=$1`,
				"audit":          `DELETE FROM backoffice_audit WHERE resource='authorization-remaining-sources' AND changes->>'order_id'=$1::bigint::text`,
				"head-reset":     `UPDATE payment_authorization_releases SET remaining_source_id=NULL,remaining_source_revision=0,remaining_source_hash=NULL WHERE order_id=$1`,
				"source-missing": `DELETE FROM authorization_remaining_sources WHERE order_id=$1`,
				"claim-missing":  `DELETE FROM authorization_remaining_claims WHERE order_id=$1`,
				"claim-proof":    `UPDATE authorization_remaining_claims SET proof_hash=decode(repeat('01',32),'hex') WHERE order_id=$1`,
				"orphan-audit":   `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)SELECT actor_role,actor_id,actor_name,resource,resource_id+999999,operation,changes FROM backoffice_audit WHERE resource='authorization-remaining-sources' AND changes->>'order_id'=$1::bigint::text`,
			}[kind]
			if _, e = tx.Exec(ctx, sql, o.ID); e != nil {
				t.Fatal(e)
			}
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			if _, e = s.AuthorizationRelease(ctx, o.MerchantID, o.TradeNo); !errors.Is(e, ErrConflict) {
				t.Fatal("corrupt release read", e)
			}
			if e = s.MarkAuthorizationReleaseSent(ctx, o.TradeNo); !errors.Is(e, ErrConflict) && !errors.Is(e, ErrState) {
				t.Fatal("legacy sender accepted corruption", e)
			}
			if e = s.ResolveAuthorizationRelease(ctx, o.TradeNo, "unknown"); !errors.Is(e, ErrConflict) {
				t.Fatal("resolver accepted corruption", e)
			}
			report, e := s.CheckLedger(ctx)
			if e != nil || report.InvalidAuthorizationReleases != 1 {
				t.Fatal("checker hid invalid release", report, e)
			}
		})
	}
}
