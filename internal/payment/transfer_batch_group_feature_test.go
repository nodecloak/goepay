package payment

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

func setBatchGroupFeature(t *testing.T, s *Store, value string) {
	t.Helper()
	if _, err := s.pool.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{user_transfer_batch}',to_jsonb($1::text)) WHERE resource='groups' AND id=0`, value); err != nil {
		t.Fatal(err)
	}
}

func TestTransferBatchGroupFeatureCloseWhileMerchantLockWaitsCannotPrepareOrClaim(t *testing.T) {
	for _, operation := range []string{"prepare", "execute"} {
		t.Run(operation, func(t *testing.T) {
			s, flow, provider := batchFixture(t)
			setBatchGroupFeature(t, s, "1")
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			var batch TransferBatch
			var err error
			if operation == "execute" {
				batch, err = flow.PrepareTransferBatch(ctx, 501, "before-close", batchInputs(100), batchMerchantActor)
				if err != nil {
					t.Fatal(err)
				}
			}
			locked, err := s.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer locked.Rollback(context.Background())
			if _, err = locked.Exec(ctx, `SELECT id FROM merchants WHERE id=501 FOR NO KEY UPDATE`); err != nil {
				t.Fatal(err)
			}
			reached := make(chan struct{})
			var once sync.Once
			check := func(context.Context, int64) error { once.Do(func() { close(reached) }); return nil }
			done := make(chan error, 1)
			go func() {
				if operation == "prepare" {
					_, err := flow.PrepareTransferBatchChecked(ctx, 501, "blocked-before-close", batchInputs(100), batchMerchantActor, check)
					done <- err
				} else {
					_, err := flow.ExecuteTransferBatch(ctx, 501, batch.ID, 1, batchMerchantActor, check)
					done <- err
				}
			}()
			select {
			case <-reached:
			case <-ctx.Done():
				t.Fatal("operation never passed its outer/preflight checks", ctx.Err())
			}
			setBatchGroupFeature(t, s, "0")
			if err = locked.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if !errors.Is(err, ErrState) {
					t.Fatalf("feature closed during merchant lock wait but %s proceeded: %v", operation, err)
				}
			case <-ctx.Done():
				t.Fatal("operation did not finish after its merchant lock released", ctx.Err())
			}
			if provider.transfers.Load() != 0 {
				t.Fatal("closed group feature sent a payment")
			}
			if operation == "prepare" {
				balance, err := s.Balance(ctx, 501)
				if err != nil || balance.Available != 1000 || balance.Reserved != 0 {
					t.Fatal("closed feature left a new reservation", balance, err)
				}
			} else {
				current, err := s.TransferBatch(ctx, 501, batch.ID)
				if err != nil || current.Summary.Unsent != 1 {
					t.Fatal("closed feature consumed an original unsent item", current.Summary, err)
				}
				// An original request remains recoverable without another reserve.
				replayed, err := flow.PrepareTransferBatch(ctx, 501, "before-close", batchInputs(100), batchMerchantActor)
				if err != nil || replayed.ID != batch.ID {
					t.Fatal("feature closure changed original same-number recovery", replayed.ID, err)
				}
			}
		})
	}
}

func TestTransferBatchGroupFeatureMembershipExpiryDuringFundsConfirmationCannotPrepareOrClaim(t *testing.T) {
	for _, operation := range []string{"prepare", "execute"} {
		t.Run(operation, func(t *testing.T) {
			s, flow, provider := batchFixture(t)
			ctx := t.Context()
			setBatchGroupFeature(t, s, "1")
			var batch TransferBatch
			var err error
			if operation == "execute" {
				batch, err = flow.PrepareTransferBatch(ctx, 501, "before-expiry", batchInputs(100), batchMerchantActor)
				if err != nil {
					t.Fatal(err)
				}
			}
			setBatchGroupFeature(t, s, "0")
			var vip int64
			if err = s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"batch feature VIP","enabled":true,"user_transfer_batch":"1"}') RETURNING id`).Scan(&vip); err != nil {
				t.Fatal(err)
			}
			if _, err = s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(501,$1,clock_timestamp()+interval '1 second')`, vip); err != nil {
				t.Fatal(err)
			}
			guarded := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
				if kind != "batch-prepare" && kind != "batch-execute" {
					return ErrInvalid
				}
				_, err := tx.Exec(c, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))::double precision+0.05) FROM memberships WHERE merchant_id=501`)
				return err
			})
			if operation == "prepare" {
				_, err = flow.PrepareTransferBatch(guarded, 501, "expired-before-prepare", batchInputs(100), batchMerchantActor)
			} else {
				_, err = flow.ExecuteTransferBatch(guarded, 501, batch.ID, 1, batchMerchantActor, nil)
			}
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("membership expired during confirmation but %s proceeded: %v", operation, err)
			}
			if provider.transfers.Load() != 0 {
				t.Fatal("expired group feature sent a payment")
			}
			if operation == "prepare" {
				balance, err := s.Balance(ctx, 501)
				if err != nil || balance.Available != 1000 || balance.Reserved != 0 {
					t.Fatal("expired feature left a new reservation", balance, err)
				}
			} else {
				current, err := s.TransferBatch(ctx, 501, batch.ID)
				if err != nil || current.Summary.Unsent != 1 {
					t.Fatal("expired feature consumed an original unsent item", current.Summary, err)
				}
			}
		})
	}
}

func TestTransferBatchGroupFeatureFirstGlobalClosureDuringConfirmationCannotPrepareOrClaim(t *testing.T) {
	for _, operation := range []string{"prepare", "execute"} {
		t.Run(operation, func(t *testing.T) {
			s, flow, provider := batchFixture(t)
			ctx := t.Context()
			var batch TransferBatch
			var err error
			if operation == "execute" {
				batch, err = flow.PrepareTransferBatch(ctx, 501, "before-first-global-close", batchInputs(100), batchMerchantActor)
				if err != nil {
					t.Fatal(err)
				}
			}
			guarded := WithFundsActionGuard(ctx, func(c context.Context, _ pgx.Tx, kind, key string) error {
				_, err := s.pool.Exec(c, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"user_transfer_batch","value":"0"}')`)
				return err
			})
			if operation == "prepare" {
				_, err = flow.PrepareTransferBatch(guarded, 501, "first-global-close", batchInputs(100), batchMerchantActor)
			} else {
				_, err = flow.ExecuteTransferBatch(guarded, 501, batch.ID, 1, batchMerchantActor, nil)
			}
			if !errors.Is(err, ErrState) || provider.transfers.Load() != 0 {
				t.Fatal("first explicit global close crossed the confirmation barrier", operation, err, provider.transfers.Load())
			}
		})
	}
}

func TestTransferBatchGroupFeatureAdminManagementAndOriginalRecoveryRemainIndependent(t *testing.T) {
	s, flow, provider := batchFixture(t)
	ctx := t.Context()
	setBatchGroupFeature(t, s, "0")
	admin := TransferBatchActor{Role: "admin", Class: "super", ID: 1, Name: "Synthetic batch administrator"}
	batch, err := flow.PrepareTransferBatch(ctx, 501, "admin-independent-feature", batchInputs(100), admin)
	if err != nil || len(batch.Items) != 1 {
		t.Fatal("merchant group feature blocked independent administrator management", err)
	}
	if _, err = flow.QueryTransferBatch(ctx, 501, batch.ID, batchMerchantActor); err != nil {
		t.Fatal("feature closure blocked original merchant query", err)
	}
	if _, err = flow.CancelUnsentTransferBatch(ctx, 501, batch.ID, batchMerchantActor); err != nil {
		t.Fatal("feature closure blocked proven-unsent cancellation", err)
	}
	current, err := s.TransferBatch(ctx, 501, batch.ID)
	if err != nil || current.Summary.Cancelled != 1 || provider.transfers.Load() != 0 {
		t.Fatal("original recovery changed the send boundary", current.Summary, err, provider.transfers.Load())
	}
	balance, err := s.Balance(ctx, 501)
	if err != nil || balance.Available != 1000 || balance.Reserved != 0 {
		t.Fatal("original cancellation did not restore exactly its reservation", balance, err)
	}
}

type batchFeatureClosingProvider struct {
	batchTestProvider
	store *Store
}

func (p *batchFeatureClosingProvider) Transfer(ctx context.Context, in adapter.TransferRequest) (adapter.TransferResult, error) {
	result, err := p.batchTestProvider.Transfer(ctx, in)
	if err == nil && p.transfers.Load() == 1 {
		_, err = p.store.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{user_transfer_batch}','"0"') WHERE resource='groups' AND id=0`)
	}
	return result, err
}

func TestTransferBatchGroupFeatureEachClaimRechecksAfterAnEarlierItem(t *testing.T) {
	s, flow, _ := batchFixture(t)
	provider := &batchFeatureClosingProvider{store: s}
	flow.Providers[7] = provider
	setBatchGroupFeature(t, s, "1")
	batch, err := flow.PrepareTransferBatch(t.Context(), 501, "close-between-items", batchInputs(100, 100, 100), batchMerchantActor)
	if err != nil {
		t.Fatal(err)
	}
	current, err := flow.ExecuteTransferBatch(t.Context(), 501, batch.ID, 3, batchMerchantActor, nil)
	if !errors.Is(err, ErrState) || provider.transfers.Load() != 1 || current.Summary.Unsent != 2 {
		t.Fatal("feature closed after original first send but a later item was claimed", current.Summary, provider.transfers.Load(), err)
	}
}
