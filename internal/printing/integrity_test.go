package printing

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestPrintingCurrentConfigCannotRewindOrDisappear(t *testing.T) {
	for _, family := range []string{"platform", "merchant"} {
		t.Run(family, func(t *testing.T) {
			s, _ := printFixture(t)
			configurePrinting(t, s, 2)
			ctx := context.Background()
			var first int64
			if family == "platform" {
				v, e := s.Platform(ctx, printAdmin)
				if e != nil {
					t.Fatal(e)
				}
				first = v.ID
				if _, e = s.SetPlatform(ctx, PlatformInput{ExpectedRevision: 1, Enabled: false, Provider: "local_fake", CredentialAction: "keep"}, printAdmin); e != nil {
					t.Fatal(e)
				}
			} else {
				v, e := s.Merchant(ctx, 901, printOwner)
				if e != nil {
					t.Fatal(e)
				}
				first = v.ID
				if _, e = s.SetMerchant(ctx, 901, MerchantInput{ExpectedRevision: 1, Mode: 0, Copies: 1, DeviceAction: "keep"}, printOwner); e != nil {
					t.Fatal(e)
				}
			}
			table, guard := "printing_platform_head", "printing_platform_head_guard"
			where := "true"
			if family == "merchant" {
				table = "printing_merchant_heads"
				guard = "printing_merchant_head_guard"
				where = "merchant_id=901"
			}
			if _, e := s.pool.Exec(ctx, "UPDATE "+table+" SET config_id=$1,revision=1 WHERE "+where, first); e == nil {
				t.Fatal("normal rewind committed")
			}
			// An isolated damaged-restore simulation drops only the head CAS guard;
			// immutable config histories and every payment/ledger guard remain enabled.
			tx, e := s.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = tx.Exec(ctx, "ALTER TABLE "+table+" DISABLE TRIGGER "+guard); e != nil {
				t.Fatal(e)
			}
			if _, e = tx.Exec(ctx, "UPDATE "+table+" SET config_id=$1,revision=1 WHERE "+where, first); e != nil {
				t.Fatal(e)
			}
			if _, e = tx.Exec(ctx, "ALTER TABLE "+table+" ENABLE TRIGGER "+guard); e != nil {
				t.Fatal(e)
			}
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			if family == "platform" {
				_, e = s.Platform(ctx, printAdmin)
			} else {
				_, e = s.Merchant(ctx, 901, printOwner)
			}
			if !errors.Is(e, ErrConflict) {
				t.Fatal("keyed old config became current", e)
			}
		})
	}
}

func TestPrintingCheckerUsesParentsAndVerifiesEveryEvidenceFamily(t *testing.T) {
	for _, family := range []string{"origin", "source", "platform", "merchant", "intent", "claim", "result", "task", "recovery", "missing_task", "missing_prepare"} {
		t.Run(family, func(t *testing.T) {
			s, p := printFixture(t)
			configurePrinting(t, s, 2)
			ctx := context.Background()
			o, _, _, _ := printPaidOrder(t, p, "checker-"+family)
			j, e := s.PrepareAutomatic(ctx, o.ID)
			if e != nil {
				t.Fatal(e)
			}
			if family == "recovery" {
				if _, e = s.pool.Exec(ctx, "UPDATE jobs SET state='dead',attempts=max_attempts WHERE id=$1", j.TaskID); e != nil {
					t.Fatal(e)
				}
				v, e := s.Recover(ctx, 901, j.ID, j.TaskID, printOwner)
				if e != nil {
					t.Fatal(e)
				}
				j = &v
			} else if family == "claim" || family == "result" {
				if _, e = s.Dispatch(ctx, j.ID, LocalFake{State: "unknown"}); e != nil {
					t.Fatal(e)
				}
			}
			if n, e := s.Check(ctx); e != nil || n != 0 {
				t.Fatal("valid printing failed check", n, e)
			}
			table, trigger := "", ""
			switch family {
			case "origin":
				table = "order_print_origins"
				trigger = "order_print_origin_immutable"
			case "source":
				table = "payment_print_sources"
				trigger = "payment_print_source_immutable"
			case "platform":
				table = "printing_platform_configs"
				trigger = "printing_platform_immutable"
			case "merchant":
				table = "printing_merchant_configs"
				trigger = "printing_merchant_immutable"
			case "intent":
				table = "printing_intents"
				trigger = "printing_intent_immutable"
			case "claim":
				table = "printing_claims"
				trigger = "printing_claim_immutable"
			case "result":
				table = "printing_results"
				trigger = "printing_result_immutable"
			case "recovery":
				table = "printing_recoveries"
				trigger = "printing_recovery_immutable"
			}
			// Simulate an isolated damaged restore of one printing family only. The
			// normal trigger is restored before the public checker opens its snapshot.
			if family == "missing_task" {
				_, e = s.pool.Exec(ctx, "DELETE FROM jobs WHERE id=$1", j.TaskID)
			} else if family == "missing_prepare" {
				_, e = s.pool.Exec(ctx, "DELETE FROM jobs WHERE event_key=$1", "receipt-paid:"+fmt.Sprint(o.ID))
			} else if family == "task" {
				_, e = s.pool.Exec(ctx, "UPDATE jobs SET payload='{}' WHERE id=$1", j.TaskID)
			} else {
				_, e = s.pool.Exec(ctx, "ALTER TABLE "+table+" DISABLE TRIGGER "+trigger+"; UPDATE "+table+" SET proof_hash=decode(repeat('00',32),'hex'); ALTER TABLE "+table+" ENABLE TRIGGER "+trigger)
			}
			if e != nil {
				t.Fatal(e)
			}
			if n, e := s.Check(ctx); e != nil || n < 1 {
				t.Fatal("damaged evidence disappeared from check", family, n, e)
			}
		})
	}
}
