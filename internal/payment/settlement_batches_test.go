package payment

import (
	"context"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"sync"
	"testing"
)

var batchAdmin84 = TransferBatchActor{Role: "admin", Class: "super", ID: 1, Name: "batch-admin84"}

func settlementBatchFixture84(t *testing.T) (*Store, []LedgerRequest) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	transferSetting(t, s, "settlement_fee_rate_bps", "250")
	if err := s.CreateMerchant(ctx, Merchant{ID: 8401, Name: "batch84", APIKey: "synthetic-batch84"}); err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 8401, OutTradeNo: "seed84", Amount: 20000})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkPaid(ctx, o.TradeNo, "synthetic-batch84-seed"); err != nil {
		t.Fatal(err)
	}
	items := []LedgerRequest{}
	for i, amount := range []int64{5000, 2000} {
		v, err := s.RequestFunds(ctx, "settlements", 8401, 0, fmt.Sprint("member84-", i), amount, "merchant:8401")
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, v)
	}
	return s, items
}

func TestSettlementBatches84GroupsOriginalNetWithoutMovingFundsOrApproving(t *testing.T) {
	s, items := settlementBatchFixture84(t)
	ctx := context.Background()
	before, err := s.Balance(ctx, 8401)
	if err != nil {
		t.Fatal(err)
	}
	var journals int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM journals`).Scan(&journals); err != nil {
		t.Fatal(err)
	}
	batch, err := s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "batch84-first", RequestIDs: []int64{items[1].ID, items[0].ID}}, batchAdmin84)
	if err != nil {
		t.Fatal(err)
	}
	if batch.ID <= 0 || batch.Batch == "" || batch.Count != 2 || batch.AllMoney != "68.25" || batch.GrossAmount != 7000 || batch.FeeAmount != 175 || batch.NetAmount != 6825 {
		t.Fatal("original net/count/batch fields not preserved", batch)
	}
	after, err := s.Balance(ctx, 8401)
	if err != nil || before != after {
		t.Fatal("grouping moved funds", before, after, err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM journals`).Scan(&count); err != nil || count != journals {
		t.Fatal("grouping posted journal", count, err)
	}
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_requests WHERE id=ANY($1::bigint[]) AND status='pending'`, []int64{items[0].ID, items[1].ID}).Scan(&count); err != nil || count != 2 {
		t.Fatal("grouping approved original requests", count, err)
	}
	again, err := s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "batch84-first", RequestIDs: []int64{items[0].ID, items[1].ID}}, batchAdmin84)
	if err != nil || again.ID != batch.ID {
		t.Fatal("stable group retry duplicated", again, err)
	}
	if _, err = s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "batch84-second", RequestIDs: []int64{items[0].ID}}, batchAdmin84); !errors.Is(err, ErrConflict) {
		t.Fatal("member assigned twice", err)
	}
}

func TestSettlementBatches84ConcurrentGroupCannotClaimSameOriginalRequestTwice(t *testing.T) {
	s, items := settlementBatchFixture84(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: fmt.Sprint("concurrent84-", n), RequestIDs: []int64{items[0].ID}}, batchAdmin84)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("original request not uniquely grouped", success)
	}
}

func TestSettlementBatches84CompletesOnlyAfterOriginalPayoutQueryProvesPayment(t *testing.T) {
	s, flow, provider, request := payoutFixture(t)
	ctx := context.Background()
	b, err := s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "paid-proof84", RequestIDs: []int64{request.ID}}, batchAdmin84)
	if err != nil {
		t.Fatal(err)
	}
	provider.unknown = true
	if _, err = flow.PrepareSettlementPayout(ctx, request.ID, 7, settlementAdmin, nil); err != nil {
		t.Fatal(err)
	}
	var unknown *adapter.UnknownOutcome
	if _, err = flow.ExecuteSettlementPayout(ctx, request.ID, settlementAdmin, nil); !errors.As(err, &unknown) {
		t.Fatal("simulated lost acknowledgement not retained", err)
	}
	if _, err = s.CompleteSettlementBatch(ctx, b.ID, batchAdmin84); !errors.Is(err, ErrState) {
		t.Fatal("unknown payment completed batch", err)
	}
	if _, err = flow.QuerySettlementPayout(ctx, request.ID, settlementAdmin); err != nil {
		t.Fatal(err)
	}
	paid, err := s.CompleteSettlementBatch(ctx, b.ID, batchAdmin84)
	if err != nil || !paid.AllPaid || paid.Status != "completed" {
		t.Fatal("trusted original payment not completed", paid, err)
	}
	if _, err = s.CompleteSettlementBatch(ctx, b.ID, batchAdmin84); err != nil {
		t.Fatal("completion retry", err)
	}
	var events int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM settlement_batch_events WHERE batch_id=$1 AND operation='complete'`, b.ID).Scan(&events); err != nil || events != 1 || provider.transfers.Load() != 1 {
		t.Fatal("completion replay caused payment or duplicate event", events, err)
	}
}

func TestSettlementBatches84CompletedBankHistoryReflectsLaterUnconfirmedAndVerifiedReturn(t *testing.T) {
	for _, product := range []string{adapter.BankProductWechat, adapter.BankProductAlipay} {
		t.Run(product, func(t *testing.T) {
			s, flow, request, state := settlementBankFixture(t, product)
			ctx := context.Background()
			b, err := s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "bank-history84", RequestIDs: []int64{request.ID}}, batchAdmin84)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = flow.PrepareSettlementPayout(ctx, request.ID, 7, settlementAdmin, nil); err != nil {
				t.Fatal(err)
			}
			if _, err = flow.ExecuteSettlementPayout(ctx, request.ID, settlementAdmin, nil); err != nil {
				t.Fatal(err)
			}
			if _, err = flow.QuerySettlementPayout(ctx, request.ID, settlementAdmin); err != nil {
				t.Fatal(err)
			}
			if paid, e := s.CompleteSettlementBatch(ctx, b.ID, batchAdmin84); e != nil || !paid.AllPaid {
				t.Fatal("bank original payment", paid, e)
			}
			state("unverified")
			if _, err = flow.QuerySettlementPayout(ctx, request.ID, settlementAdmin); !errors.Is(err, ErrBankReversalPending) {
				t.Fatal("expected original pending reversal", err)
			}
			history, err := s.SettlementBatch(ctx, b.ID)
			if err != nil || history.Status != "completed" || history.AllPaid || history.Items[0].PayoutState != "reversal_pending" {
				t.Fatal("later uncertainty damaged historical group or remained all paid", history, err)
			}
			state("returned")
			if _, err = flow.QuerySettlementPayout(ctx, request.ID, settlementAdmin); err != nil {
				t.Fatal(err)
			}
			history, err = s.SettlementBatch(ctx, b.ID)
			if err != nil || history.Status != "completed" || history.AllPaid || history.Items[0].FundingState != "returned" || history.AllMoney != "9.70" {
				t.Fatal("return changed original group or hid current outcome", history, err)
			}
			report, err := s.CheckLedger(ctx)
			if err != nil || report.InvalidSettlementBatches != 0 || report.InvalidSettlementFundings != 0 {
				t.Fatal(report, err)
			}
		})
	}
}

func TestSettlementBatches84HistoricalMembershipSurvivesReviewAndArchiveNeverCompletesUnknown(t *testing.T) {
	s, items := settlementBatchFixture84(t)
	ctx := context.Background()
	b, err := s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "history84", RequestIDs: []int64{items[0].ID, items[1].ID}}, batchAdmin84)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompleteSettlementBatch(ctx, b.ID, batchAdmin84); !errors.Is(err, ErrState) {
		t.Fatal("unpaid group falsely completed", err)
	}
	if _, err = s.ReviewFundsRequest(ctx, items[0].ID, true, "admin:1"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReviewFundsRequest(ctx, items[1].ID, false, "admin:1"); err != nil {
		t.Fatal(err)
	}
	transferSetting(t, s, "settlement_fee_rate_bps", "500")
	history, err := s.SettlementBatch(ctx, b.ID)
	if err != nil || history.AllMoney != "68.25" || history.Items[0].Status != "approved" || history.Items[1].Status != "rejected" {
		t.Fatal("current policy or review changed original group", history, err)
	}
	archived, err := s.ArchiveSettlementBatch(ctx, b.ID, batchAdmin84)
	if err != nil || archived.Status != "archived" || archived.Count != 2 {
		t.Fatal(archived, err)
	}
	again, err := s.ArchiveSettlementBatch(ctx, b.ID, batchAdmin84)
	if err != nil || again.ID != b.ID || again.Status != "archived" {
		t.Fatal("archive replay", again, err)
	}
	var requests, events int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_requests WHERE id=ANY($1::bigint[])`, []int64{items[0].ID, items[1].ID}).Scan(&requests); err != nil || requests != 2 {
		t.Fatal("archive deleted original requests", requests, err)
	}
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM settlement_batch_events WHERE batch_id=$1 AND operation='complete'`, b.ID).Scan(&events); err != nil || events != 0 {
		t.Fatal("archive manufactured completion proof", events, err)
	}
}

func TestSettlementBatches84RejectsAmbiguousAuthorityAndLegacyOrOtherRequestDomains(t *testing.T) {
	s, items := settlementBatchFixture84(t)
	ctx := context.Background()
	for _, input := range []SettlementBatchInput{{ExternalNo: "duplicate84", RequestIDs: []int64{items[0].ID, items[0].ID}}, {ExternalNo: " space84", RequestIDs: []int64{items[0].ID}}, {ExternalNo: "empty84"}, {ExternalNo: "zero84", RequestIDs: []int64{0}}} {
		if _, err := s.CreateSettlementBatch(ctx, input, batchAdmin84); !errors.Is(err, ErrInvalid) {
			t.Fatal("ambiguous batch input", err)
		}
	}
	for _, actor := range []TransferBatchActor{{Role: "merchant", ID: 8401, Name: "owner"}, {Role: "admin", Class: "auditor", ID: 1, Name: "auditor"}, {Role: "system", Class: "super", ID: 1, Name: "system"}} {
		if _, err := s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "authority84", RequestIDs: []int64{items[0].ID}}, actor); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid batch authority", err)
		}
	}
	if err := s.CreateMerchant(ctx, Merchant{ID: 8402, Name: "other84", APIKey: "synthetic-other84"}); err != nil {
		t.Fatal(err)
	}
	internal, err := s.RequestFunds(ctx, "internal-transfers", 8401, 8402, "wrong-domain84", 100, "merchant:8401")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "other-domain84", RequestIDs: []int64{internal.ID}}, batchAdmin84); !errors.Is(err, ErrInvalid) {
		t.Fatal("ordinary transfer adopted as settlement", err)
	}
	var legacy int64
	if err = s.pool.QueryRow(ctx, `INSERT INTO ledger_requests(kind,merchant_id,external_no,amount) VALUES('settlements',8401,'unscoped84',100) RETURNING id`).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "legacy84", RequestIDs: []int64{legacy}}, batchAdmin84); !errors.Is(err, ErrConflict) {
		t.Fatal("unscoped legacy request adopted", err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM settlement_batches`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected input left partial batches", count, err)
	}
}

func TestSettlementBatches84SnapshotListAndIntegrityDetectOriginalAuditTampering(t *testing.T) {
	s, items := settlementBatchFixture84(t)
	ctx := context.Background()
	first, err := s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "list84-first", RequestIDs: []int64{items[0].ID}}, batchAdmin84)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "list84-second", RequestIDs: []int64{items[1].ID}}, batchAdmin84)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.ListSettlementBatches(ctx, "", 0, 1)
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != second.ID || page.NextBeforeID != second.ID || len(page.Items[0].Items) != 0 {
		t.Fatal(page, err)
	}
	page, err = s.ListSettlementBatches(ctx, "", page.NextBeforeID, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != first.ID || page.NextBeforeID != 0 {
		t.Fatal(page, err)
	}
	if _, err = s.ArchiveSettlementBatch(ctx, first.ID, batchAdmin84); err != nil {
		t.Fatal(err)
	}
	filtered, err := s.ListSettlementBatchesFiltered(ctx, SettlementBatchQuery{Batch: first.Batch, Status: "archived", Limit: 20})
	if err != nil || filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].ID != first.ID {
		t.Fatal("original batch/status filters", filtered, err)
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidSettlementBatches != 0 {
		t.Fatal(report, err)
	}
	if _, err = s.pool.Exec(ctx, `ALTER TABLE backoffice_audit DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `DELETE FROM backoffice_audit WHERE resource='settlement-batches' AND resource_id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `ALTER TABLE backoffice_audit ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SettlementBatch(ctx, first.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("deleted source audit accepted", err)
	}
	report, err = s.CheckLedger(ctx)
	if err != nil || report.InvalidSettlementBatches != 1 {
		t.Fatal("original corruption not counted", report, err)
	}
}
