package payment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"sync"
	"sync/atomic"
	"testing"
)

type outgoingProvider struct {
	flowProvider
	transfers  atomic.Int32
	query      atomic.Int32
	seenMu     sync.Mutex
	seen       []adapter.TransferRequest
	unknown    bool
	fail       bool
	proofApply atomic.Int32
	proofQuery atomic.Int32
}

func (p *outgoingProvider) TransferTypes() []string { return []string{"wxpay"} }
func (p *outgoingProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.transfers.Add(1)
	p.seenMu.Lock()
	p.seen = append(p.seen, r)
	p.seenMu.Unlock()
	if p.unknown {
		return adapter.TransferResult{}, &adapter.UnknownOutcome{Operation: "transfer", Cause: errors.New("lost response")}
	}
	state := "PROCESSING"
	if p.fail {
		state = "FAILED"
	}
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "wx-outgoing-1", State: state, Amount: r.Amount}, nil
}
func (p *outgoingProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.query.Add(1)
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "wx-outgoing-1", State: "SUCCESS", Amount: r.Amount, PayDate: "2026-10-23 08:00:00"}, nil
}
func (p *outgoingProvider) ApplyTransferProof(_ context.Context, no string) (adapter.TransferProofResult, error) {
	p.proofApply.Add(1)
	if no != "wx-outgoing-1" {
		return adapter.TransferProofResult{}, errors.New("wrong provider transfer")
	}
	return adapter.TransferProofResult{FileID: "receipt-1", State: "PROCESSING"}, nil
}
func (p *outgoingProvider) QueryTransferProof(_ context.Context, id string) (adapter.TransferProofResult, error) {
	p.proofQuery.Add(1)
	return adapter.TransferProofResult{FileID: id, State: "SUCCESS", DownloadURL: "https://receipts.example/private-token"}, nil
}
func transferFixture(t *testing.T) (*Store, ProviderFlow, *outgoingProvider) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 501, Name: "outgoing", APIKey: "test-only-transfer-secret"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"10"}' WHERE id=501`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 501, OutTradeNo: "fund", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "outgoing-fund"); e != nil {
		t.Fatal(e)
	}
	p := &outgoingProvider{}
	return s, ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true}, p
}
func TestTransferUnknownReservedIdentityAndGlobalNamespace(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	p.unknown = true
	req := adapter.TransferRequest{Type: "wxpay", Account: "private-openid", Name: "private-name", Amount: 100, Remark: "test"}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); flow.Transfer(ctx, 501, 7, "merchant-no", req) }()
	}
	wg.Wait()
	if p.transfers.Load() != 1 {
		t.Fatalf("external calls%d", p.transfers.Load())
	}
	r, e := s.Transfer(ctx, 501, "merchant-no")
	if e != nil || r.Status != "unknown" || r.CostAmount != 110 {
		t.Fatalf("reservation %+v %v", r, e)
	}
	p.seenMu.Lock()
	sent := p.seen[0]
	p.seenMu.Unlock()
	if sent.TransferNo != "TF"+fmt.Sprint(r.ID) {
		t.Fatalf("upstream id%q", sent.TransferNo)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 890 || b.Reserved != 110 {
		t.Fatalf("unknown released %+v", b)
	}
	changed := req
	changed.Account = "attacker"
	if _, e = flow.Transfer(ctx, 501, 7, "merchant-no", changed); !errors.Is(e, ErrConflict) {
		t.Fatalf("recipient changed %v", e)
	}
	if _, e = flow.QueryTransfer(ctx, 999, "merchant-no"); !errors.Is(e, ErrNotFound) {
		t.Fatalf("cross tenant query %v", e)
	}
	var cipher []byte
	if e = s.pool.QueryRow(ctx, "SELECT recipient_cipher FROM provider_transfer_attempts WHERE transfer_id=$1", r.ID).Scan(&cipher); e != nil || bytes.Contains(cipher, []byte("private-openid")) {
		t.Fatalf("plaintext recipient %v", e)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := flow.QueryTransfer(ctx, 501, "merchant-no"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	b, _ = s.Balance(ctx, 501)
	var fees int64
	s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees'").Scan(&fees)
	if b.Available != 890 || b.Reserved != 0 || fees != 10 {
		t.Fatalf("success net%+v fee%d", b, fees)
	}
}
func TestTransferDefiniteFailureReleasesAndNoBlindRetry(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	p.fail = true
	req := adapter.TransferRequest{Type: "wxpay", Account: "openid", Amount: 100, Remark: "test"}
	r, e := flow.Transfer(ctx, 501, 7, "failed", req)
	if e != nil || r.Status != "failed" {
		t.Fatalf("failure %+v %v", r, e)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatalf("failure reservation %+v", b)
	}
	if _, e = flow.Transfer(ctx, 501, 7, "failed", req); e != nil {
		t.Fatal(e)
	}
	if p.transfers.Load() != 1 {
		t.Fatal("failed write retried")
	}
	flow.AllowLive = false
	if _, e = flow.Transfer(ctx, 501, 7, "disabled", req); !errors.Is(e, ErrLiveDisabled) {
		t.Fatalf("livegate %v", e)
	}
}
func TestTransferProofPersistsApplyThenQueriesOwnedReceipt(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	if _, e := flow.Transfer(ctx, 501, 7, "proof", adapter.TransferRequest{Type: "wxpay", Account: "openid", Amount: 100, Remark: "test"}); e != nil {
		t.Fatal(e)
	}
	if _, e := flow.QueryTransfer(ctx, 501, "proof"); e != nil {
		t.Fatal(e)
	}
	if _, e := flow.TransferProof(ctx, 999, "proof"); !errors.Is(e, ErrNotFound) {
		t.Fatalf("ownership %v", e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); flow.TransferProof(ctx, 501, "proof") }()
	}
	wg.Wait()
	if p.proofApply.Load() != 1 {
		t.Fatalf("receipt apply calls %d", p.proofApply.Load())
	}
	flow.AllowLive = false
	result, e := flow.TransferProof(ctx, 501, "proof")
	if e != nil || result.State != "SUCCESS" || result.DownloadURL == "" {
		t.Fatalf("proof %+v %v", result, e)
	}
	var encrypted []byte
	if e = s.pool.QueryRow(ctx, "SELECT download_cipher FROM provider_transfer_proofs").Scan(&encrypted); e != nil || bytes.Contains(encrypted, []byte("private-token")) {
		t.Fatalf("receipt URL unencrypted %v", e)
	}
	if p.proofApply.Load() != 1 || p.proofQuery.Load() < 1 {
		t.Fatal("proof applied again")
	}
}
func TestTransferRejectsInvalidRecipientBeforeReservation(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	for i, r := range []adapter.TransferRequest{{Type: "wxpay", Account: "openid", Amount: 100}, {Type: "wxpay", Account: string(bytes.Repeat([]byte("x"), 129)), Amount: 100, Remark: "test"}} {
		if _, e := flow.Transfer(ctx, 501, 7, fmt.Sprint(i), r); !errors.Is(e, ErrInvalid) {
			t.Fatalf("invalid before reserve %v", e)
		}
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 || p.transfers.Load() != 0 {
		t.Fatalf("invalid request reserved %+v", b)
	}
}
func TestTransferRequiresExplicitAPIEnableAndCapability(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, "UPDATE merchants SET profile='{}' WHERE id=501"); e != nil {
		t.Fatal(e)
	}
	if _, e := flow.Transfer(ctx, 501, 7, "off", adapter.TransferRequest{Type: "wxpay", Account: "openid", Amount: 100}); e == nil {
		t.Fatal("disabledmerchant submitted")
	}
	if p.transfers.Load() != 0 {
		t.Fatal("called disabled transfer")
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatal("disabled transfer reserved funds")
	}
}
