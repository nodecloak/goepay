package contract_test

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

type merchant174Provider struct {
	mu                           sync.Mutex
	payments                     map[string]adapter.CreateRequest
	refundWrites, transferWrites atomic.Int32
}

func (*merchant174Provider) ID() string                      { return "alipay" }
func (*merchant174Provider) ApplicationID() string           { return "synthetic-app174" }
func (*merchant174Provider) TransferAccountIdentity() string { return "synthetic-account174" }
func (*merchant174Provider) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Methods: []string{"native"}, Query: true, Refund: true, RefundQuery: true}
}
func (p *merchant174Provider) Create(_ context.Context, r adapter.CreateRequest) (adapter.CreateResult, error) {
	p.mu.Lock()
	p.payments[r.TradeNo] = r
	p.mu.Unlock()
	return adapter.CreateResult{MaterialType: "qrcode", Material: "https://local.example.invalid/174"}, nil
}
func (p *merchant174Provider) Query(_ context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	p.mu.Lock()
	v, ok := p.payments[r.TradeNo]
	p.mu.Unlock()
	if !ok {
		return adapter.QueryResult{}, adapter.ErrInvalid
	}
	return adapter.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "paid174-" + r.TradeNo, State: "SUCCESS", Currency: "CNY", Amount: v.Amount}, nil
}
func (*merchant174Provider) Close(context.Context, adapter.QueryRequest) error {
	return adapter.ErrUnsupported
}
func (p *merchant174Provider) Refund(_ context.Context, r adapter.RefundRequest) (adapter.RefundResult, error) {
	p.refundWrites.Add(1)
	return adapter.RefundResult{RefundNo: r.RefundNo, Amount: r.Amount, State: "PROCESSING"}, nil
}
func (*merchant174Provider) QueryRefund(_ context.Context, r adapter.RefundRequest) (adapter.RefundResult, error) {
	return adapter.RefundResult{RefundNo: r.RefundNo, Amount: r.Amount, State: "SUCCESS"}, nil
}
func (*merchant174Provider) TransferTypes() []string { return []string{"alipay"} }
func (p *merchant174Provider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.transferWrites.Add(1)
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, State: "PROCESSING", ProviderTransferNo: "transfer174-" + r.TransferNo}, nil
}
func (*merchant174Provider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, State: "SUCCESS", ProviderTransferNo: "transfer174-" + r.TransferNo, PayDate: "2026-10-26 12:00:00"}, nil
}
func (*merchant174Provider) ApplyTransferProof(context.Context, string) (adapter.TransferProofResult, error) {
	return adapter.TransferProofResult{FileID: "synthetic-proof174", State: "SUCCESS", DownloadURL: "https://local.example.invalid/proof174.pdf"}, nil
}
func (*merchant174Provider) QueryTransferProof(context.Context, string) (adapter.TransferProofResult, error) {
	return adapter.TransferProofResult{FileID: "synthetic-proof174", State: "SUCCESS", DownloadURL: "https://local.example.invalid/proof174.pdf"}, nil
}

func merchant174Invoke(t *testing.T, f signatureFixture163, path string, p compat.Params) map[string]any {
	t.Helper()
	p["pid"] = "2"
	if path == "/api.php" {
		p["key"] = "contract-rsa-secret"
	} else {
		p["timestamp"] = fmt.Sprint(time.Now().Unix())
		p["sign_type"] = "RSA"
		p["sign"], _ = compat.SignRSA(p, f.key)
	}
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	_, out := call(t, f.s.Handler(), "POST", path, v)
	return out
}
func merchant174Runtime(t *testing.T) (signatureFixture163, *merchant174Provider, payment.Order) {
	t.Helper()
	f := signatureFixture(t, 2)
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `UPDATE merchants SET profile=profile||'{"transfer_api_enabled":true}'::jsonb WHERE id=2;INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	p := &merchant174Provider{payments: map[string]adapter.CreateRequest{}}
	f.s.Sandbox = false
	f.s.ProviderFlow = &payment.ProviderFlow{Store: f.s.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	f.s.MerchantChannels = map[int64][]int64{2: {7}}
	f.s.CheckoutKey = bytes.Repeat([]byte{7}, 32)
	out := merchant174Invoke(t, f, "/api/pay/create", compat.Params{"out_trade_no": "paid-funds174", "type": "alipay", "method": "native", "money": "10.00", "name": "synthetic item174", "channel_id": "7"})
	if out["code"] != float64(0) {
		t.Fatal("valid signed new payment positive control", out)
	}
	o, e := f.s.Store.Order(ctx, 2, "paid-funds174")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 2, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	b, e := f.s.Store.Balance(ctx, 2)
	if e != nil || b.Available != 1000 {
		t.Fatal("trusted synthetic funding source", b, e)
	}
	return f, p, o
}
func merchant174Set(t *testing.T, f signatureFixture163, keylogin, refund, transfer int) map[string]any {
	t.Helper()
	before := apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
	return apiSwitchState174(t, f.request("PATCH", "api-switches", apiSwitchBody174(before["fingerprint"].(string), keylogin, refund, transfer), f.csrf))
}
func TestMerchant174DisabledRefundAPIRejectsNewLegacyAndV2Intent(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("legacy=", legacy), func(t *testing.T) {
			f, p, o := merchant174Runtime(t)
			ctx := context.Background()
			if legacy {
				state := signatureState163(t, f.request("GET", "signature-policy", "", f.csrf))
				signatureState163(t, f.request("PATCH", "signature-policy", signaturePatch163(state["fingerprint"].(string), 0), f.csrf))
			}
			merchant174Set(t, f, 0, 0, 0)
			path := "/api/pay/refund"
			params := compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "new-api-refund174", "money": "1.00"}
			if legacy {
				path = "/api.php"
				params["act"] = "refund"
			}
			out := merchant174Invoke(t, f, path, params)
			var refunds, operations int
			if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM refunds),(SELECT count(*) FROM provider_operations WHERE kind='refund')`).Scan(&refunds, &operations); e != nil || refunds+operations != 0 || p.refundWrites.Load() != 0 || out["code"] != float64(-1) {
				t.Fatal("personal refund API close did not reach actual new-intent consumer", out, refunds, operations, p.refundWrites.Load(), e)
			}
			read := merchant174Invoke(t, f, "/api/pay/query", compat.Params{"out_trade_no": o.OutTradeNo})
			if read["code"] != float64(0) {
				t.Fatal("keylogin0 incorrectly disabled payment signature authentication", read)
			}
		})
	}
}
func TestMerchant174DisabledTransferAPIRejectsNewIntentWithAdminGrantPreserved(t *testing.T) {
	f, p, _ := merchant174Runtime(t)
	ctx := context.Background()
	merchant174Set(t, f, 1, 1, 0)
	out := merchant174Invoke(t, f, "/api/transfer/submit", compat.Params{"type": "alipay", "channel_id": "7", "out_biz_no": "new-api-transfer174", "account": "synthetic-payee@example.invalid", "money": "1.00", "remark": "synthetic"})
	var transfers, attempts int
	if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM transfers),(SELECT count(*) FROM provider_transfer_attempts)`).Scan(&transfers, &attempts); e != nil || transfers+attempts != 0 || p.transferWrites.Load() != 0 || out["code"] != float64(-1) {
		t.Fatal("personal transfer API close did not reach actual new-intent consumer", out, transfers, attempts, p.transferWrites.Load(), e)
	}
	var granted bool
	if e := f.p.QueryRow(ctx, `SELECT (profile->>'transfer_api_enabled')::boolean FROM merchants WHERE id=2`).Scan(&granted); e != nil || !granted {
		t.Fatal("personal switch changed administrator grant", e)
	}
}
