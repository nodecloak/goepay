package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestProviderRefundResultReviewFreshTerminalCannotOmitEvidence(t *testing.T) {
	s, flow, p, o := setupOperations(t)
	ctx := context.Background()
	if _, err := flow.Query(ctx, 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	p.unknown = true
	r, err := flow.Refund(ctx, 1, o.TradeNo, "review-no-result-evidence", 60)
	if err == nil {
		t.Fatal("fixture requires an unknown original provider refund")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Fresh operation and refund were born after schema59. Supply all ledger
	// and state/audit structure, but no authenticated result evidence at all.
	if err = post(ctx, tx, fmt.Sprintf("refund:%d:resolve", r.ID), "unkeyed refund restore", 1, "reserved", 1, "available", 60); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE refunds SET status='failed' WHERE id=$1", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE provider_operations SET state='failed' WHERE kind='refund' AND refund_id=$1", r.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		// A new-result completeness constraint is also a valid defense. Verify
		// that it rejected the complete transaction rather than hiding a
		// partially restored balance behind a failed commit result.
		current, readErr := s.Refund(ctx, 1, r.RefundNo)
		if readErr != nil || current.Status != "unknown" {
			t.Fatal("rejected terminal left partial state", current, readErr)
		}
		b, readErr := s.Balance(ctx, 1)
		if readErr != nil || b.Available != 40 || b.Reserved != 60 {
			t.Fatal("rejected terminal released original unknown funds", b, readErr)
		}
		var evidence int
		if readErr = s.pool.QueryRow(ctx, "SELECT count(*) FROM provider_refund_result_evidence WHERE refund_id=$1", r.ID).Scan(&evidence); readErr != nil || evidence != 0 {
			t.Fatal("rejection invented an authenticated result", evidence, readErr)
		}
		return
	}
	if _, err = s.Refund(ctx, 1, r.RefundNo); !errors.Is(err, ErrConflict) {
		t.Fatal("fresh unkeyed terminal was treated as an historical/internal refund", err)
	}
}

func reviewUnknownRefund(t *testing.T) (*Store, ProviderFlow, *operationsProvider, Refund) {
	t.Helper()
	s, flow, p, o := setupOperations(t)
	ctx := context.Background()
	if _, err := flow.Query(ctx, 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	p.unknown = true
	r, err := flow.Refund(ctx, 1, o.TradeNo, "review-restored-result", 60)
	if err == nil {
		t.Fatal("fixture requires unknown provider refund")
	}
	return s, flow, p, r
}

func reviewForgeRefundResult(t *testing.T, s *Store, r Refund, source string) error {
	t.Helper()
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Schema63 authenticates the independent principal-source resolution too.
	// Supply its genuine source proof so this fixture continues to isolate the
	// missing/forged schema59 provider result with all schema63 guards enabled.
	funding, err := s.scanRefundFunding(ctx, tx, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.resolveRefundFunding(ctx, tx, r, funding, "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE refunds SET status='failed' WHERE id=$1", r.ID); err != nil {
		t.Fatal(err)
	}
	if source == "internal" {
		_, err = tx.Exec(ctx, `INSERT INTO provider_refund_result_evidence(refund_id,order_id,amount,refund_no,prior_refund_state,state,proof_hash,source) VALUES($1,$2,$3,$4,'unknown','failed',$5,'internal')`, r.ID, r.OrderID, r.Amount, r.RefundNo, make([]byte, 32))
	} else if source == "provider" {
		_, err = tx.Exec(ctx, `INSERT INTO provider_refund_result_evidence(refund_id,operation_id,order_id,channel_id,amount,refund_no,prior_refund_state,prior_operation_state,state,proof_hash,source) SELECT $1,id,$2,channel_id,$3,$4,'unknown',state,'failed',$5,'provider' FROM provider_operations WHERE kind='refund' AND refund_id=$1`, r.ID, r.OrderID, r.Amount, r.RefundNo, make([]byte, 32))
	}
	if err != nil {
		t.Fatal(err)
	}
	if source != "internal" {
		if _, err = tx.Exec(ctx, "UPDATE provider_operations SET state='failed' WHERE kind='refund' AND refund_id=$1", r.ID); err != nil {
			t.Fatal(err)
		}
	}
	return tx.Commit(ctx)
}

func reviewAssertUntrustedRefundResult(t *testing.T, s *Store, flow ProviderFlow, p *operationsProvider, r Refund) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.Refund(ctx, 1, r.RefundNo); !errors.Is(err, ErrConflict) {
		t.Fatal("public read adopted restored unkeyed terminal", err)
	}
	if err := s.ResolveRefund(ctx, r.ID, "failed"); !errors.Is(err, ErrConflict) {
		t.Fatal("internal resolution retroactively sealed restored terminal", err)
	}
	var trade string
	if err := s.pool.QueryRow(ctx, "SELECT trade_no FROM orders WHERE id=$1", r.OrderID).Scan(&trade); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Refund(ctx, 1, trade, r.RefundNo, 60); !errors.Is(err, ErrConflict) || p.refunds.Load() != 1 {
		t.Fatal("same-number replay adopted restored terminal", err, p.refunds.Load())
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidProviderRefundResults != 1 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal("parent-based checker missed balanced restored forgery", report, err)
	}
}

func TestProviderRefundResultReviewBrokenRestoreMissingProofIsReported(t *testing.T) {
	s, flow, p, r := reviewUnknownRefund(t)
	ctx := context.Background()
	// Model an incomplete restore by disabling only the new completeness
	// trigger in this isolated schema. Every ledger and identity guard stays on.
	if _, err := s.pool.Exec(ctx, "ALTER TABLE refunds DISABLE TRIGGER refund_requires_terminal_result"); err != nil {
		t.Fatal(err)
	}
	if err := reviewForgeRefundResult(t, s, r, ""); err != nil {
		t.Fatal("broken-restore fixture failed before keyed read", err)
	}
	if _, err := s.pool.Exec(ctx, "ALTER TABLE refunds ENABLE TRIGGER refund_requires_terminal_result"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE refunds SET result_evidence_version=1 WHERE id=$1", r.ID); err == nil {
		t.Fatal("new mandatory evidence could be downgraded to historical")
	}
	reviewAssertUntrustedRefundResult(t, s, flow, p, r)
}

func TestProviderRefundResultReviewSQLShapedProviderAndInternalProofsAreRejected(t *testing.T) {
	for _, source := range []string{"provider", "internal"} {
		t.Run(source, func(t *testing.T) {
			s, flow, p, r := reviewUnknownRefund(t)
			if err := reviewForgeRefundResult(t, s, r, source); err != nil {
				t.Fatal("SQL-shaped evidence must satisfy enabled structural guards", err)
			}
			reviewAssertUntrustedRefundResult(t, s, flow, p, r)
		})
	}
}
