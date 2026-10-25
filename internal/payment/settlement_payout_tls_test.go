package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSettlementPayoutTLSAuthenticatesOriginalNetIdentityAndRecoversOnlyOriginalTF(t *testing.T) {
	testSettlementPayoutTLSOriginalIdentity(t, false)
}
func TestSettlementBatches84TLSAuthenticatesOriginalPaymentBeforeCompletion(t *testing.T) {
	testSettlementPayoutTLSOriginalIdentity(t, true)
}
func testSettlementPayoutTLSOriginalIdentity(t *testing.T, withBatch bool) {
	s, g, _ := batchFixture(t)
	ctx := context.Background()
	transferSetting(t, s, "settlement_fee_rate_bps", "300")
	merchant, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	var writes, reads atomic.Int32
	var phase atomic.Int32
	var expectedTF string
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		params := map[string]string{}
		for k, v := range r.PostForm {
			params[k] = v[0]
		}
		sig, e := adapter.SignAlipayRSA2(params, merchant)
		if e != nil || sig != params["sign"] || params["app_id"] != "payout-local-app" {
			t.Error("request signature/application mismatch", e)
		}
		var biz map[string]any
		json.Unmarshal([]byte(params["biz_content"]), &biz)
		if biz["out_biz_no"] != expectedTF {
			t.Error("replacement transfer number", biz)
		}
		switch params["method"] {
		case "alipay.fund.trans.toaccount.transfer":
			writes.Add(1)
			if biz["amount"] != "9.70" || biz["payee_account"] != "original@example.invalid" || biz["payee_real_name"] != "Original Synthetic Name" {
				t.Error("original payout snapshot changed", biz)
			}
			var claims, audits int
			if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM settlement_payouts WHERE send_state='claimed'),(SELECT count(*) FROM backoffice_audit WHERE operation='payout-claim')").Scan(&claims, &audits); e != nil || claims != 1 || audits != 1 {
				t.Error("I/O preceded durable claim/actor intent", claims, audits, e)
			}
			w.WriteHeader(503)
			return
		case "alipay.fund.trans.common.query":
			reads.Add(1)
		default:
			t.Error("unexpected payout operation", params["method"])
			w.WriteHeader(500)
			return
		}
		amount := "9.70"
		if phase.Load() == 0 {
			amount = "9.71"
		}
		node, _ := json.Marshal(map[string]any{"code": "10000", "out_biz_no": expectedTF, "trans_amount": amount, "order_id": "original-provider-id", "status": "SUCCESS", "pay_date": "2026-10-23 12:00:00"})
		digest := sha256.Sum256(node)
		signed, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		if phase.Load() == 1 {
			signed[0] ^= 1
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_fund_trans_common_query_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(signed)})
	}))
	defer up.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "payout-local-app", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	g.Providers[7] = ali
	cfg, e := s.SetSettlementRecipient(ctx, 501, SettlementRecipientInput{Type: "alipay", Account: "original@example.invalid", Name: "Original Synthetic Name"}, nil, SettlementRecipientActor{Role: "merchant", ID: 501, Name: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.RequestFundsWithSettlementRecipient(ctx, "settlements", 501, 0, "tls-payout", 1000, "merchant:501", nil, &cfg.ConfigID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReviewFundsRequest(ctx, v.ID, true, "admin:1"); e != nil {
		t.Fatal(e)
	}
	var batch SettlementBatch
	if withBatch {
		batch, e = s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "tls-batch84", RequestIDs: []int64{v.ID}}, batchAdmin84)
		if e != nil {
			t.Fatal(e)
		}
	}
	// A new profile version cannot change the request-time payee.
	if _, e = s.SetSettlementRecipient(ctx, 501, SettlementRecipientInput{Type: "alipay", Account: "replacement@example.invalid", Name: "Replacement Synthetic Name"}, &cfg.ConfigID, SettlementRecipientActor{Role: "merchant", ID: 501, Name: "merchant"}); e != nil {
		t.Fatal(e)
	}
	p, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
	if e != nil || writes.Load()+reads.Load() != 0 {
		t.Fatal(p, e)
	}
	expectedTF = transferRequest(p.Transfer).TransferNo
	p, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil)
	var unknown *adapter.UnknownOutcome
	if !errors.As(e, &unknown) || p.State != "unknown" || writes.Load() != 1 {
		t.Fatal(p, e)
	}
	g.AllowLive = false
	for _, n := range []int32{0, 1} {
		phase.Store(n)
		if _, e = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin); !errors.Is(e, adapter.ErrAuthentication) {
			t.Fatal("unverified query accepted", n, e)
		}
		balance, _ := s.Balance(ctx, 501)
		if balance.Available != 0 || balance.Reserved != 1000 {
			t.Fatal(balance)
		}
		if withBatch {
			if _, e = s.CompleteSettlementBatch(ctx, batch.ID, batchAdmin84); !errors.Is(e, ErrState) {
				t.Fatal("unauthenticated query completed batch", e)
			}
		}
	}
	phase.Store(2)
	p, e = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
	if e != nil || p.State != "succeeded" || p.Funding.State != "paid" || writes.Load() != 1 || reads.Load() != 3 {
		t.Fatal(p, e, writes.Load(), reads.Load())
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "original@example.invalid") || strings.Contains(string(raw), "Original Synthetic Name") {
		t.Fatal("PII result", string(raw))
	}
	if withBatch {
		completed, err := s.CompleteSettlementBatch(ctx, batch.ID, batchAdmin84)
		if err != nil || !completed.AllPaid || completed.Status != "completed" {
			t.Fatal(completed, err)
		}
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidSettlementBatches != 0 || report.InvalidSettlementFundings != 0 || report.InvalidAccounts != 0 {
		t.Fatal(report, e)
	}
}
