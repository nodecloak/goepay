package payment

import (
	"context"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"sync"
	"testing"
)

func transferSetting(t *testing.T, s *Store, key, value string) {
	t.Helper()
	if _, e := s.pool.Exec(context.Background(), `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text)) ON CONFLICT ((body->>'key')) WHERE resource='settings' DO UPDATE SET body=EXCLUDED.body`, key, value); e != nil {
		t.Fatal(e)
	}
}
func TestTransferLimitsRejectBeforeReserveAndSnapshotDefault(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	transferSetting(t, s, "transfer_minmoney", "2.00")
	transferSetting(t, s, "transfer_maxmoney", "3.00")
	transferSetting(t, s, "transfer_desc", "默认转账")
	for i, amount := range []int64{100, 400} {
		if _, e := flow.Transfer(ctx, 501, 7, fmt.Sprint(i), adapter.TransferRequest{Type: "wxpay", Account: "recipient", Amount: amount, Remark: "test"}); e == nil {
			t.Fatal("limit not enforced", amount)
		}
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 || p.transfers.Load() != 0 {
		t.Fatalf("limit rejected after reserve %+v calls%d", b, p.transfers.Load())
	}
	req := adapter.TransferRequest{Type: "wxpay", Account: "recipient", Amount: 200}
	first, e := flow.Transfer(ctx, 501, 7, "default", req)
	if e != nil || first.Remark != "默认转账" {
		t.Fatalf("default desc %+v %v", first, e)
	}
	transferSetting(t, s, "transfer_desc", "新的默认备注")
	transferSetting(t, s, "transfer_minmoney", "3.00")
	replay, e := flow.Transfer(ctx, 501, 7, "default", req)
	if e != nil || replay.ID != first.ID || replay.Remark != "默认转账" || p.transfers.Load() != 1 {
		t.Fatalf("default replay changed %+v %v", replay, e)
	}
	req.Remark = "默认转账"
	if _, e = flow.Transfer(ctx, 501, 7, "default", req); e == nil {
		t.Fatal("different explicit request matched an implicit default")
	}
}
func TestTransferDailyRecipientQuotaIsConcurrentAndUnknownCannotReset(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	p.unknown = true
	transferSetting(t, s, "transfer_maxlimit", "3")
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			account := "private-recipient"
			if n%2 == 0 {
				account = "  private-recipient  "
			}
			flow.Transfer(ctx, 501, 7, fmt.Sprintf("daily-%d", n), adapter.TransferRequest{Type: "wxpay", Account: account, Amount: 100, Remark: "test"})
		}(i)
	}
	wg.Wait()
	if p.transfers.Load() != 3 {
		t.Fatalf("concurrent limit calls%d", p.transfers.Load())
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 670 || b.Reserved != 330 {
		t.Fatalf("daily reservations %+v", b)
	}
	var no string
	if e := s.pool.QueryRow(ctx, "SELECT transfer_no FROM transfers WHERE merchant_id=501 ORDER BY id LIMIT 1").Scan(&no); e != nil {
		t.Fatal(e)
	}
	if _, e := flow.QueryTransfer(ctx, 501, no); e != nil {
		t.Fatal(e)
	}
	if _, e := flow.Transfer(ctx, 501, 7, "after-success", adapter.TransferRequest{Type: "wxpay", Account: "private-recipient", Amount: 100, Remark: "test"}); !errors.Is(e, ErrState) {
		t.Fatal("confirmed success reset daily quota")
	}
	if _, e := flow.Transfer(ctx, 501, 7, "other", adapter.TransferRequest{Type: "wxpay", Account: "different-recipient", Amount: 100, Remark: "test"}); e == nil {
		t.Fatal("test provider should return unknown")
	} else if p.transfers.Load() != 4 {
		t.Fatalf("different recipient rejected: %v, calls%d", e, p.transfers.Load())
	}
	if p.transfers.Load() != 4 {
		t.Fatal("different recipient shared quota")
	}
	var rows int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM provider_transfer_policies WHERE octet_length(recipient_hash)=32 AND octet_length(request_hash)=32").Scan(&rows); e != nil || rows != 4 {
		t.Fatal("recipient HMAC index missing", rows, e)
	}
	if _, e := s.pool.Exec(ctx, "UPDATE provider_transfer_policies SET recipient_hash=decode(repeat('00',32),'hex')"); e == nil {
		t.Fatal("recipient limit identity mutable")
	}
}
func TestTransferDailyQuotaBackfillsEncryptedLegacyReservations(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	transferSetting(t, s, "transfer_maxlimit", "1")
	old, e := s.ReserveTransfer(ctx, 501, "legacy", 100)
	if e != nil {
		t.Fatal(e)
	}
	req := adapter.TransferRequest{Type: "wxpay", TransferNo: fmt.Sprintf("TF%d", old.ID), Account: "legacy-recipient", Amount: 100, Remark: "test"}
	cipher, e := s.encryptTransfer(old.ID, req)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "INSERT INTO provider_transfer_attempts(transfer_id,channel_id,recipient_cipher,created_at) VALUES($1,7,$2,now()-interval '2 days')", old.ID, cipher); e != nil {
		t.Fatal(e)
	}
	if _, e = flow.Transfer(ctx, 501, 7, "new", adapter.TransferRequest{Type: "wxpay", Account: "legacy-recipient", Amount: 100, Remark: "test"}); e == nil {
		t.Fatal("legacy unknown bypassed current day quota")
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 900 || b.Reserved != 100 || p.transfers.Load() != 0 {
		t.Fatalf("legacy quota rejected after reserve %+v", b)
	}
}

type aliPolicyProvider struct{ *outgoingProvider }

func (*aliPolicyProvider) TransferTypes() []string { return []string{"alipay"} }
func TestTransferDailyQuotaCanonicalAccountAcrossChannelsAndSeparateTenants(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	p.unknown = true
	ali := &aliPolicyProvider{p}
	flow.Providers[7] = ali
	flow.Providers[8] = ali
	transferSetting(t, s, "transfer_maxlimit", "1")
	r := adapter.TransferRequest{Type: "alipay", Account: "PAYEE@EXAMPLE.INVALID", Amount: 100}
	flow.Transfer(ctx, 501, 7, "case-original", r)
	r.Account = "payee@example.invalid"
	if _, e := flow.Transfer(ctx, 501, 8, "other-channel", r); !errors.Is(e, ErrState) {
		t.Fatalf("account aliases or channel bypassed quota %v", e)
	}
	if p.transfers.Load() != 1 {
		t.Fatal("alias submitted upstream")
	}
	if e := s.CreateMerchant(ctx, Merchant{ID: 502, Name: "other", APIKey: "other-test-secret"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true}' WHERE id=502`); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 502, OutTradeNo: "fund", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "other-fund"); e != nil {
		t.Fatal(e)
	}
	flow.Transfer(ctx, 502, 7, "other-tenant", r)
	if p.transfers.Load() != 2 {
		t.Fatal("quota leaked across tenants")
	}
}
func TestTransferDefiniteFailureDoesNotConsumeRecipientQuota(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	p.fail = true
	transferSetting(t, s, "transfer_maxlimit", "1")
	for _, no := range []string{"failed-one", "failed-two"} {
		r, e := flow.Transfer(ctx, 501, 7, no, adapter.TransferRequest{Type: "wxpay", Account: "failed-account", Amount: 100, Remark: "test"})
		if e != nil || r.Status != "failed" {
			t.Fatalf("failed quota release %+v %v", r, e)
		}
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 || p.transfers.Load() != 2 {
		t.Fatalf("failed transfers consumed quota %+v", b)
	}
}

type preflightTransferProvider struct {
	*outgoingProvider
	validated int
}

func (p *preflightTransferProvider) ValidateTransfer(r adapter.TransferRequest) error {
	p.validated++
	if r.Account == "invalid-recipient" || r.Remark == "" || r.TransferNo == "" {
		return adapter.ErrInvalid
	}
	return nil
}
func TestTransferPureAdapterPreflightRunsAfterDefaultsBeforeReserve(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	validator := &preflightTransferProvider{outgoingProvider: p}
	flow.Providers[7] = validator
	transferSetting(t, s, "transfer_desc", "configured-desc")
	r := adapter.TransferRequest{Type: "wxpay", Account: "invalid-recipient", Amount: 100}
	if _, e := flow.Transfer(ctx, 501, 7, "invalid", r); !errors.Is(e, adapter.ErrInvalid) {
		t.Fatalf("adapter preflight ignored %v", e)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 || p.transfers.Load() != 0 {
		t.Fatalf("invalid adapter request reserved %+v", b)
	}
	r.Account = "valid-recipient"
	if _, e := flow.Transfer(ctx, 501, 7, "valid", r); e != nil {
		t.Fatal("default not available to pure validation", e)
	}
	if validator.validated != 2 || p.transfers.Load() != 1 {
		t.Fatal("preflight not called before provider", validator.validated, p.transfers.Load())
	}
}
