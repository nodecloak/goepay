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
	"sync"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func TestChannelSharingReturnAlipaySignedRoyaltyEvidenceIsRequiredForOriginalCredit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 751, Name: "sharing-owner", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	merchant, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	var sends, queries atomic.Int32
	var phase atomic.Int32
	var mu sync.Mutex
	var psNo, trade string
	returns := map[string]string{}
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		method := r.Form.Get("method")
		var input map[string]json.RawMessage
		if json.Unmarshal([]byte(r.Form.Get("biz_content")), &input) != nil || r.Form.Get("sign") == "" {
			t.Error("invalid signed request")
			w.WriteHeader(400)
			return
		}
		field := func(key string) string { var v string; json.Unmarshal(input[key], &v); return v }
		result := map[string]any{"code": "10000"}
		switch method {
		case "alipay.trade.royalty.relation.bind":
			result["result_code"] = "SUCCESS"
		case "alipay.trade.order.settle":
			mu.Lock()
			psNo, trade = field("out_request_no"), field("trade_no")
			mu.Unlock()
			result["trade_no"] = field("trade_no")
			result["settle_no"] = "ALI_ORIGINAL_PS"
		case "alipay.trade.order.settle.query":
			mu.Lock()
			expectedNo, expectedTrade := psNo, trade
			mu.Unlock()
			if field("out_request_no") != expectedNo || field("trade_no") != expectedTrade {
				t.Error("changed original PS query")
			}
			result["out_request_no"] = expectedNo
			result["royalty_detail_list"] = []map[string]string{{"trans_in_type": "userId", "trans_in": "2088000000000001", "amount": "0.20", "state": "SUCCESS", "detail_id": "ALI_ORIGINAL_SUCCESS", "operation_type": "transfer"}}
		case "alipay.trade.refund":
			sends.Add(1)
			no := field("out_request_no")
			var royalties []map[string]string
			json.Unmarshal(input["refund_royalty_parameters"], &royalties)
			mu.Lock()
			expectedTrade := trade
			returns[no] = field("trade_no")
			mu.Unlock()
			if field("trade_no") != expectedTrade || field("refund_amount") != "0.00" || len(no) != 32 || !strings.HasPrefix(no, "PR") || len(royalties) != 1 || royalties[0]["trans_out"] != "2088000000000001" || royalties[0]["amount"] != "0.12" {
				t.Error("return became a buyer refund or changed original recipient")
			}
			result["trade_no"] = expectedTrade
			result["refund_fee"] = "0.00"
		case "alipay.trade.fastpay.refund.query":
			queries.Add(1)
			no := field("out_request_no")
			mu.Lock()
			expectedTrade, ok := returns[no]
			mu.Unlock()
			if !ok || field("trade_no") != expectedTrade {
				t.Error("return query changed original identity")
			}
			result["trade_no"] = expectedTrade
			result["out_request_no"] = no
			result["refund_amount"] = "0.00"
			result["refund_status"] = "REFUND_SUCCESS"
			if phase.Load() != 0 {
				account, amount := "2088000000000001", "0.12"
				if phase.Load() == 2 {
					amount = "0.13"
				}
				if phase.Load() == 3 {
					account = "2088000000000002"
				}
				result["refund_royaltys"] = []map[string]string{{"royalty_type": "transfer", "trans_out": account, "ori_trans_out": account, "refund_amount": amount, "result_code": "SUCCESS"}}
			}
		default:
			t.Error("unexpected synthetic Alipay method", method)
			w.WriteHeader(400)
			return
		}
		node, _ := json.Marshal(result)
		digest := sha256.Sum256(node)
		signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(method, ".", "_") + "_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer up.Close()
	provider := func(enableReturn bool) adapter.Provider {
		p, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "local-ali-return-app", SellerID: "2088000000000009", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client(), Methods: []string{"app"}, EnableChannelSharing: true, EnableChannelSharingReturn: enableReturn})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: provider(false)}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
	receiver, e := flow.CreateChannelSharingReceiver(ctx, 751, 7, "ali-return-receiver", adapter.SharingReceiver{Type: "userId", Account: "2088000000000001"}, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = flow.BindChannelSharingReceiver(ctx, 751, receiver.ID, "ali-return-bind", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = flow.SetChannelSharingRule(ctx, 751, 7, ChannelSharingRuleInput{Enabled: true, Methods: []string{"app"}, Items: []ChannelSharingRuleItem{{ReceiverID: receiver.ID, Mode: "rate", RateBPS: 2000}}}, 0, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "ali-return-original", Type: "alipay", Amount: 100, Name: "synthetic original"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = flow.Submit(ctx, o.TradeNo, 7, "app", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.markPaid(ctx, o.TradeNo, "ali-return-receipt", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "ALI_" + o.TradeNo}); e != nil {
		t.Fatal(e)
	}
	ps, e := flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "ali-return-original-sharing", channelSharingActor)
	if e != nil || ps.State != "pending" {
		t.Fatal(ps, e)
	}
	ps, e = flow.QueryChannelSharing(ctx, 751, "ali-return-original-sharing", channelSharingActor)
	if e != nil || !ps.FundsReleased {
		t.Fatal(ps, e)
	}
	if _, e = flow.SubmitChannelSharingReturn(ctx, 751, ps.ExternalNo, receiver.ID, "ali-original-return", 12, channelSharingActor); !errors.Is(e, adapter.ErrUnsupported) || sends.Load() != 0 {
		t.Fatal("unverified product permission attempted return", e, sends.Load())
	}
	flow.Providers[7] = provider(true)
	first, e := flow.SubmitChannelSharingReturn(ctx, 751, ps.ExternalNo, receiver.ID, "ali-original-return", 12, channelSharingActor)
	if e != nil || first.State != "pending" || sends.Load() != 1 {
		t.Fatal(first, e)
	}
	flow.AllowLive = false
	for _, bad := range []int32{0, 2, 3} {
		phase.Store(bad)
		if _, e = flow.ReconcileChannelSharingReturn(ctx, first.ID); e == nil {
			t.Fatal("zero buyer refund, wrong amount or wrong recipient proved a royalty return", bad)
		}
		b, e := s.Balance(ctx, 751)
		if e != nil || b.Available != 80 {
			t.Fatal(b, e)
		}
	}
	phase.Store(1)
	final, e := flow.ReconcileChannelSharingReturn(ctx, first.ID)
	if e != nil || final.State != "succeeded" {
		t.Fatal(final, e)
	}
	b, e := s.Balance(ctx, 751)
	if e != nil || b.Available != 92 || sends.Load() != 1 || queries.Load() != 4 {
		t.Fatal(b, e, sends.Load(), queries.Load())
	}
	if _, e = flow.QueryChannelSharingReturn(ctx, 751, "ali-original-return", channelSharingActor); e != nil || queries.Load() != 4 {
		t.Fatal("terminal replay made another upstream request", e, queries.Load())
	}
	amount, e := s.OrderIncomeFreezable(ctx, 751, o.TradeNo)
	if e != nil || amount != 92 {
		t.Fatal("Ali return restored another source amount", amount, e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidChannelSharingReturns != 0 || report.InvalidChannelSharingFundings != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal(report, e)
	}
}
