package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestTransferBatchFundsGuardDeniesWholePrepareExecuteAndCancel(t *testing.T) {
	s, g, p := batchFixture(t)
	ctx := context.Background()
	denied := errors.New("authorization denied")
	guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return denied })
	if _, err := g.PrepareTransferBatch(guarded, 501, "denied", batchInputs(100), batchMerchantActor); !errors.Is(err, denied) {
		t.Fatal("prepare bypassed authorization", err)
	}
	b, err := g.PrepareTransferBatch(ctx, 501, "prepared", batchInputs(100, 100), batchMerchantActor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.ExecuteTransferBatch(guarded, 501, b.ID, 2, batchMerchantActor, nil); !errors.Is(err, denied) {
		t.Fatal("execute bypassed authorization", err)
	}
	if _, err = g.CancelUnsentTransferBatch(guarded, 501, b.ID, batchMerchantActor); !errors.Is(err, denied) {
		t.Fatal("cancel bypassed authorization", err)
	}
	current, err := s.TransferBatch(ctx, 501, b.ID)
	if err != nil || current.Summary.Unsent != 2 || p.transfers.Load() != 0 {
		t.Fatal("denial changed claims or sent money", current, err, p.transfers.Load())
	}
}

func TestTransferBatchExecutionConsumesOnceAndPersistsExactSubset(t *testing.T) {
	s, g, p := batchFixture(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, "CREATE TABLE test_batch_authorization(kind text,business_key text)"); err != nil {
		t.Fatal(err)
	}
	guarded := WithFundsActionGuard(ctx, func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "batch-prepare" && kind != "batch-execute" {
			t.Fatal("inner transfer requested separate authorization", kind)
		}
		_, err := tx.Exec(ctx, "INSERT INTO test_batch_authorization VALUES($1,$2)", kind, key)
		return err
	})
	b, err := g.PrepareTransferBatch(guarded, 501, "guarded", batchInputs(100, 100, 100), batchMerchantActor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.ExecuteTransferBatch(guarded, 501, b.ID, 2, batchMerchantActor, nil)
	if err != nil || got.Summary.Unsent != 1 || p.transfers.Load() != 2 {
		t.Fatal("bounded execute", got, err, p.transfers.Load())
	}
	var consumed, executions, claims, subset int
	if err = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM test_batch_authorization),(SELECT count(*) FROM transfer_batch_executions),(SELECT count(*) FROM transfer_batch_execution_claims),(SELECT cardinality(transfer_ids) FROM transfer_batch_executions LIMIT 1)`).Scan(&consumed, &executions, &claims, &subset); err != nil || consumed != 2 || executions != 1 || claims != 2 || subset != 2 {
		t.Fatal("authorization is not atomic exact subset", consumed, executions, claims, subset, err)
	}
	if _, err = s.pool.Exec(ctx, "UPDATE transfer_batch_executions SET transfer_ids=transfer_ids||999::bigint"); err == nil {
		t.Fatal("authorized subset mutable")
	}
}
