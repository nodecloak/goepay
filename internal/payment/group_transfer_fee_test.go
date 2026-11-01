package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

func setGroupTransferFields(t *testing.T, s *Store, fields map[string]any) {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(t.Context(), `UPDATE backoffice_records SET body=body||$1::jsonb WHERE resource='groups' AND id=0`, string(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestGroupTransferFeeSourcesExactCostAndOriginalReplay(t *testing.T) {
	s, flow, provider := transferFixture(t)
	ctx := context.Background()
	transferSetting(t, s, "settlement_fee_rate_bps", "250")
	for i, tc := range []struct {
		value, settlement string
		rate, cost        int64
	}{
		{"", "", 1000, 110}, {"_", "", 1000, 110},
		{"1.25", "", 125, 101}, {"0", "", 250, 103},
		{"0", "3.21", 321, 103}, {"0.00", "3.21", 0, 100},
	} {
		setGroupTransferFields(t, s, map[string]any{"transfer_rate": tc.value, "settle_rate": tc.settlement})
		no := fmt.Sprintf("group-transfer-fee-%d", i)
		transfer, err := flow.Transfer(ctx, 501, 7, no, batchInputs(100)[0].Request)
		if err != nil || transfer.FeeRateBPS != tc.rate || transfer.CostAmount != tc.cost {
			t.Errorf("group fee %q with settlement %q: rate=%d cost=%d err=%v, want rate=%d cost=%d", tc.value, tc.settlement, transfer.FeeRateBPS, transfer.CostAmount, err, tc.rate, tc.cost)
		}
	}
	before := provider.transfers.Load()
	for _, bad := range []string{"bad", "100.01", "1.001", "-1"} {
		setGroupTransferFields(t, s, map[string]any{"transfer_rate": bad})
		if _, err := flow.Transfer(ctx, 501, 7, "bad-group-fee-"+bad, batchInputs(1)[0].Request); err == nil {
			t.Error("corrupt group transfer fee admitted a new transfer", bad)
		}
	}
	if provider.transfers.Load() != before {
		t.Fatal("invalid group fee reached the provider")
	}
	replayed, err := flow.Transfer(ctx, 501, 7, "group-transfer-fee-2", batchInputs(100)[0].Request)
	if err != nil || replayed.FeeRateBPS != 125 || replayed.CostAmount != 101 || provider.transfers.Load() != before {
		t.Fatal("current group fee rewrote or resent an original transfer", replayed, err)
	}
}

func TestGroupTransferFeeBatchReservationsShareExactRateAndPreserveOriginal(t *testing.T) {
	s, flow, provider := batchFixture(t)
	setGroupTransferFields(t, s, map[string]any{"transfer_rate": "1.25"})
	batch, err := flow.PrepareTransferBatch(t.Context(), 501, "group-fee-batch", batchInputs(100, 200), batchMerchantActor)
	if err != nil || batch.CostAmount != 304 || len(batch.Items) != 2 {
		t.Fatalf("batch did not reserve the same effective per-item fee: cost=%d err=%v", batch.CostAmount, err)
	}
	for _, item := range batch.Items {
		if item.Transfer.FeeRateBPS != 125 {
			t.Fatal("batch item has a different fee source", item.Transfer.FeeRateBPS)
		}
	}
	setGroupTransferFields(t, s, map[string]any{"transfer_rate": "bad"})
	replayed, err := flow.PrepareTransferBatch(t.Context(), 501, "group-fee-batch", batchInputs(100, 200), batchMerchantActor)
	if err != nil || replayed.ID != batch.ID || replayed.CostAmount != 304 || provider.transfers.Load() != 0 {
		t.Fatal("current group fee repriced an original prepared batch", replayed, err)
	}
}

func TestGroupRedpackFeatureAdmissionAndExistingClaimUseCurrentFeeWithoutResend(t *testing.T) {
	s, flow, provider := transferFixture(t)
	setGroupTransferFields(t, s, map[string]any{"user_transfer_red": "1", "transfer_rate": "1.25"})
	input := redpackInput("2026110100000000201", 100)
	packet, err := s.CreateRedpack(t.Context(), input)
	if err != nil {
		t.Fatal("group could not enable redpack creation", err)
	}
	if balance, err := s.Balance(t.Context(), 501); err != nil || balance.Available != 1000 || balance.Reserved != 0 {
		t.Fatal("redpack creation changed the no-reservation lifecycle", balance, err)
	}
	setGroupTransferFields(t, s, map[string]any{"user_transfer_red": "0", "transfer_rate": "2.50"})
	if _, err := s.CreateRedpack(t.Context(), redpackInput("2026110100000000202", 100)); !errors.Is(err, ErrState) {
		t.Fatal("closed group redpack feature admitted a new packet", err)
	}
	if same, err := s.CreateRedpack(t.Context(), input); err != nil || same.ID != packet.ID {
		t.Fatal("closed feature blocked original packet recovery", same, err)
	}
	provider.unknown = true
	_, transfer, err := flow.ClaimRedpack(t.Context(), packet.ID, "synthetic-group-claimer")
	if err == nil || transfer.FeeRateBPS != 250 || transfer.CostAmount != 103 || provider.transfers.Load() != 1 {
		t.Fatal("existing packet did not create exactly one transfer at the current effective fee", transfer, provider.transfers.Load(), err)
	}
	setGroupTransferFields(t, s, map[string]any{"transfer_rate": "bad"})
	_, _, _ = flow.ClaimRedpack(t.Context(), packet.ID, "synthetic-group-claimer")
	resolved, _, err := flow.QueryRedpack(t.Context(), packet.ID)
	if err != nil || resolved.Status != "succeeded" || provider.transfers.Load() != 1 {
		t.Fatal("closed feature/corrupt new fee blocked original result recovery or resent", resolved, err, provider.transfers.Load())
	}
}

func TestGroupRedpackCreationChecksSameEffectiveFeeForAvailableFunds(t *testing.T) {
	s, _, _ := transferFixture(t)
	setGroupTransferFields(t, s, map[string]any{"user_transfer_red": "1", "transfer_rate": "100.00"})
	if _, err := s.CreateRedpack(t.Context(), redpackInput("2026110100000000203", 700)); !errors.Is(err, ErrInsufficient) {
		t.Fatal("redpack creation checked the profile fee instead of the effective group fee", err)
	}
}

func TestGroupTransferFeeExpiryDuringIntentPersistenceRollsBack(t *testing.T) {
	for _, operation := range []string{"transfer", "redpack"} {
		t.Run(operation, func(t *testing.T) {
			s, flow, provider := transferFixture(t)
			ctx := t.Context()
			setGroupTransferFields(t, s, map[string]any{"user_transfer_red": "0"})
			var vip int64
			if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"expiring fee","enabled":true,"transfer_rate":"0.00","user_transfer_red":"1"}') RETURNING id`).Scan(&vip); err != nil {
				t.Fatal(err)
			}
			if _, err := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(501,$1,clock_timestamp()+interval '1 second')`, vip); err != nil {
				t.Fatal(err)
			}
			table := "provider_transfer_attempts"
			if operation == "redpack" {
				table = "redpacks"
			}
			if _, err := s.pool.Exec(ctx, `CREATE FUNCTION wait_group_fee_expiry() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(GREATEST(0,(SELECT extract(epoch FROM expires_at-clock_timestamp()) FROM memberships WHERE merchant_id=501))::double precision+0.05); RETURN NEW; END $$; CREATE TRIGGER wait_group_fee_expiry BEFORE INSERT ON `+table+` FOR EACH ROW EXECUTE FUNCTION wait_group_fee_expiry()`); err != nil {
				t.Fatal(err)
			}
			var err error
			if operation == "transfer" {
				_, err = flow.Transfer(ctx, 501, 7, "fee-expiry-at-persistence", batchInputs(100)[0].Request)
			} else {
				_, err = s.CreateRedpack(ctx, redpackInput("2026110100000000206", 100))
			}
			if !errors.Is(err, ErrConflict) || provider.transfers.Load() != 0 {
				t.Fatal("expired group fee crossed the persistence wait", operation, err, provider.transfers.Load())
			}
			balance, err := s.Balance(ctx, 501)
			if err != nil || balance.Available != 1000 || balance.Reserved != 0 {
				t.Fatal("expired quote left a financial reservation", balance, err)
			}
		})
	}
}

func TestGroupTransferFeeFirstGlobalFallbackChangeDuringConfirmationRollsBack(t *testing.T) {
	for _, operation := range []string{"transfer", "batch"} {
		t.Run(operation, func(t *testing.T) {
			s, flow, provider := batchFixture(t)
			ctx := t.Context()
			setGroupTransferFields(t, s, map[string]any{"transfer_rate": "0"})
			guarded := WithFundsActionGuard(ctx, func(c context.Context, _ pgx.Tx, kind, key string) error {
				_, err := s.pool.Exec(c, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settlement_fee_rate_bps","value":"250"}')`)
				return err
			})
			var err error
			if operation == "transfer" {
				_, err = flow.Transfer(guarded, 501, 7, "new-fallback-during-confirmation", batchInputs(100)[0].Request)
			} else {
				_, err = flow.PrepareTransferBatch(guarded, 501, "new-fallback-batch-confirmation", batchInputs(100, 200), batchMerchantActor)
			}
			if !errors.Is(err, ErrConflict) || provider.transfers.Load() != 0 {
				t.Fatal("first global fallback price crossed confirmation without a new quote", operation, err, provider.transfers.Load())
			}
			balance, err := s.Balance(ctx, 501)
			if err != nil || balance.Available != 1000 || balance.Reserved != 0 {
				t.Fatal("changed fallback quote left a reservation", balance, err)
			}
		})
	}
}
