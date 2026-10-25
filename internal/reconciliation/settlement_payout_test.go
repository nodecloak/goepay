package reconciliation

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"testing"
)

type settlementReconcileProvider struct{ *queryProvider }

func (*settlementReconcileProvider) ApplicationID() string { return "reconcile-settlement-app" }
func TestSettlementPayoutReconciliationNeverClaimsUnsentAndRecoversClaimedOriginalOnly(t *testing.T) {
	h, p, o := fixture(t)
	ctx := context.Background()
	if e := h.Flow.Store.MarkPaid(ctx, o.TradeNo, "settlement-reconcile-fund"); e != nil {
		t.Fatal(e)
	}
	if _, e := h.Pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"10"}' WHERE id=1;INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}'),('settings','{"key":"settlement_fee_rate_bps","value":"300"}')`); e != nil {
		t.Fatal(e)
	}
	h.Flow.Providers[7] = &settlementReconcileProvider{p}
	h.Flow.AllowLive = true
	if _, e := h.Flow.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "wxpay", AppID: "reconcile-settlement-app", Account: "synthetic-recipient", Name: "Synthetic Name"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant"}); e != nil {
		t.Fatal(e)
	}
	v, e := h.Flow.Store.RequestFunds(ctx, "settlements", 1, 0, "worker-settlement", 100, "merchant:1")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.Flow.Store.ReviewFundsRequest(ctx, v.ID, true, "admin:1"); e != nil {
		t.Fatal(e)
	}
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", ID: 1, Name: "admin"}
	out, e := h.Flow.PrepareSettlementPayout(ctx, v.ID, 7, actor, nil)
	if e != nil {
		t.Fatal(e)
	}
	task := job("transfer.reconcile", map[string]int64{"transfer_id": out.TransferID})
	if e = h.Handle(ctx, task); !errors.Is(e, ErrPending) || p.transfers != 0 || p.transferQueries != 0 {
		t.Fatal("worker claimed unsent", e, p.transfers, p.transferQueries)
	}
	if _, e = h.Flow.ExecuteSettlementPayout(ctx, v.ID, actor, nil); e == nil {
		t.Fatal("test provider must lose response")
	}
	h.Flow.AllowLive = false
	for i := 0; i < 2; i++ {
		if e = h.Handle(ctx, task); e != nil {
			t.Fatal(e)
		}
	}
	if p.transfers != 1 || p.transferQueries != 1 {
		t.Fatal("worker wrote/repeated", p.transfers, p.transferQueries)
	}
	current, e := h.Flow.Store.SettlementPayout(ctx, v.ID)
	if e != nil || current.State != "succeeded" || current.Transfer.Amount != 97 {
		t.Fatal(current, e)
	}
	balance, _ := h.Flow.Store.Balance(ctx, 1)
	if balance.Available != 0 || balance.Reserved != 0 {
		t.Fatal(balance)
	}
	_ = adapter.ErrUnsupported
}
