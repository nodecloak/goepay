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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/ledger"
)

func TestSettlementAuto73TLSClaimsOriginalSourceBeforeIOAndRecoversOnlyOriginalTF(t *testing.T) {
	s, g, _, _, authority, id := autoReady73(t)
	exerciseOriginalAutoTLS(t, s, g, authority, id, 7395, 1000, "10.00")
}

func exerciseOriginalAutoTLS(t *testing.T, s *Store, g ProviderFlow, authority SettlementAutoAuthorizationView, id, mid, available int64, net string) {
	t.Helper()
	ctx := context.Background()
	netCents, e := ledger.Parse(net)
	if e != nil || netCents <= 0 {
		t.Fatal("invalid local expected net", e)
	}
	merchant, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	var writes, reads, phase atomic.Int32
	var originalTF string
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
		if e != nil || sig != params["sign"] || params["app_id"] != "batch-test-app" {
			t.Error("local signed automatic application mismatch", e)
			w.WriteHeader(400)
			return
		}
		var biz map[string]any
		if e = json.Unmarshal([]byte(params["biz_content"]), &biz); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		p, e := s.SettlementPayout(ctx, id)
		if e != nil || p.SendState != "claimed" {
			t.Error("I/O before committed original payout claim", p.SendState, e)
			w.WriteHeader(400)
			return
		}
		wantTF := transferRequest(p.Transfer).TransferNo
		if originalTF == "" {
			originalTF = wantTF
		}
		if originalTF != wantTF || biz["out_biz_no"] != originalTF {
			t.Error("automatic recovery replaced original TF")
			w.WriteHeader(400)
			return
		}
		switch params["method"] {
		case "alipay.fund.trans.toaccount.transfer":
			writes.Add(1)
			if biz["amount"] != net || biz["payee_account"] != "local-recipient@example.test" || biz["payee_real_name"] != "Local Recipient" {
				t.Error("automatic original recipient/net changed", biz)
			}
			var claims, audits int
			if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM settlement_auto_claims WHERE request_id=$1),(SELECT count(*) FROM settlement_payout_events WHERE request_id=$1 AND action='claim' AND actor_role='system' AND actor_id=0 AND actor_name='settlement-auto')`, id).Scan(&claims, &audits); e != nil || claims != 1 || audits != 1 {
				t.Error("external call preceded atomic automatic proof/audit", claims, audits, e)
			}
			w.WriteHeader(503)
			return
		case "alipay.fund.trans.common.query":
			reads.Add(1)
		default:
			t.Error("unexpected automatic TLS operation", params["method"])
			w.WriteHeader(400)
			return
		}
		amount := net
		if phase.Load() == 0 {
			amount = ledger.Format(netCents + 1)
		}
		node, _ := json.Marshal(map[string]any{"code": "10000", "out_biz_no": originalTF, "trans_amount": amount, "order_id": "local-auto73-provider-id", "status": "SUCCESS", "pay_date": "2026-10-24 12:00:00"})
		digest := sha256.Sum256(node)
		signed, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		if e != nil {
			t.Error(e)
			w.WriteHeader(500)
			return
		}
		if phase.Load() == 1 {
			signed[0] ^= 1
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_fund_trans_common_query_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(signed)})
	}))
	defer up.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "batch-test-app", SellerID: "2088000000000073", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	// The sealed account/product/app identity survives a credential rotation.
	g.Providers[7] = ali
	p, e := g.ExecuteSettlementAuto(ctx, id, nil)
	var unknown *adapter.UnknownOutcome
	if !errors.As(e, &unknown) || p.State != "unknown" || writes.Load() != 1 {
		t.Fatal(p.State, writes.Load(), e)
	}
	if e = s.RevokeSettlementAuto(ctx, authority.ID, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	g.AllowLive = false
	for _, step := range []int32{0, 1} {
		phase.Store(step)
		if _, e = g.QuerySettlementPayout(ctx, id, settlementAdmin); !errors.Is(e, adapter.ErrAuthentication) {
			t.Fatal("unverified automatic query resolved funds", step, e)
		}
		b, e := s.Balance(ctx, mid)
		if e != nil || b.Available != available || b.Reserved != 1000 {
			t.Fatal("unverified result released original reserve", b, e)
		}
	}
	phase.Store(2)
	p, e = g.QuerySettlementPayout(ctx, id, settlementAdmin)
	if e != nil || p.State != "succeeded" || p.Funding.State != "paid" || writes.Load() != 1 || reads.Load() != 3 {
		t.Fatal(p.State, writes.Load(), reads.Load(), e)
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "local-recipient@example.test") || strings.Contains(string(raw), "Local Recipient") {
		t.Fatal("automatic response exposed private payee")
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report != (LedgerCheck{}) {
		t.Fatal(report, e)
	}
}
