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

func TestChannelSharingAlipayQueryDocumentedResponseWithoutSettleNumber(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "lost-acceptance"}[lost], func(t *testing.T) {
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
			var mu sync.Mutex
			var number, trade string
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if e := r.ParseForm(); e != nil {
					t.Error(e)
					w.WriteHeader(400)
					return
				}
				method := r.Form.Get("method")
				var input map[string]json.RawMessage
				if json.Unmarshal([]byte(r.Form.Get("biz_content")), &input) != nil {
					t.Error("invalid business JSON")
					w.WriteHeader(400)
					return
				}
				field := func(key string) string { var v string; json.Unmarshal(input[key], &v); return v }
				result := map[string]any{"code": "10000"}
				switch method {
				case "alipay.trade.royalty.relation.bind":
					result["result_code"] = "SUCCESS"
				case "alipay.trade.order.settle":
					sends.Add(1)
					mu.Lock()
					number, trade = field("out_request_no"), field("trade_no")
					mu.Unlock()
					var extension map[string]string
					json.Unmarshal(input["extend_params"], &extension)
					if extension["royalty_finish"] != "true" || field("royalty_mode") != "async" {
						t.Error("original automatic completion intent missing")
					}
					if lost {
						w.WriteHeader(503)
						return
					}
					result["trade_no"] = field("trade_no")
					result["settle_no"] = "ALI_ACCEPTED_SHARING"
				case "alipay.trade.order.settle.query":
					queries.Add(1)
					mu.Lock()
					expectedNo, expectedTrade := number, trade
					mu.Unlock()
					if field("out_request_no") != expectedNo || field("trade_no") != expectedTrade {
						t.Error("query changed original operation")
					}
					// The official SDK response has no settle_no or trade_no fields.
					result["out_request_no"] = expectedNo
					result["royalty_detail_list"] = []map[string]string{{"trans_in_type": "userId", "trans_in": "2088000000000001", "amount": "0.20", "state": "SUCCESS", "detail_id": "ALI_ORIGINAL_DETAIL", "operation_type": "transfer"}}
				default:
					t.Error("unexpected method", method)
					w.WriteHeader(500)
					return
				}
				node, _ := json.Marshal(result)
				digest := sha256.Sum256(node)
				signature, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
				if e != nil {
					t.Error(e)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(method, ".", "_") + "_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(signature)})
			}))
			defer up.Close()
			p, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "local-ali-app", SellerID: "2088000000000009", PrivateKey: merchant, PublicKey: &platform.PublicKey, EnableChannelSharing: true, GatewayURL: up.URL, HTTPClient: up.Client(), Methods: []string{"app"}})
			if e != nil {
				t.Fatal(e)
			}
			flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
			receiver, e := flow.CreateChannelSharingReceiver(ctx, 751, 7, "ali-execution-receiver", adapter.SharingReceiver{Type: "userId", Account: "2088000000000001"}, channelSharingActor)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = flow.BindChannelSharingReceiver(ctx, 751, receiver.ID, "ali-execution-bind", channelSharingActor); e != nil {
				t.Fatal(e)
			}
			if _, e = flow.SetChannelSharingRule(ctx, 751, 7, ChannelSharingRuleInput{Enabled: true, Methods: []string{"app"}, Items: []ChannelSharingRuleItem{{ReceiverID: receiver.ID, Mode: "rate", RateBPS: 2000}}}, 0, channelSharingActor); e != nil {
				t.Fatal(e)
			}
			o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "ali-execution", Type: "alipay", Amount: 100, Name: "local sharing"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = flow.Submit(ctx, o.TradeNo, 7, "app", "127.0.0.1", ""); e != nil {
				t.Fatal(e)
			}
			if e = s.markPaid(ctx, o.TradeNo, "local-ali-receipt", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "ALI_" + o.TradeNo}); e != nil {
				t.Fatal(e)
			}
			initial, e := flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "ali-sharing", channelSharingActor)
			if lost {
				var unknown *adapter.UnknownOutcome
				if !errors.As(e, &unknown) || initial.State != "unknown" {
					t.Fatal(initial, e)
				}
			} else if e != nil || initial.State != "pending" {
				t.Fatal(initial, e)
			}
			flow.AllowLive = false
			final, e := flow.QueryChannelSharing(ctx, 751, "ali-sharing", channelSharingActor)
			if e != nil || !final.FundsReleased || final.ReleasedAmount != 80 || sends.Load() != 1 || queries.Load() != 1 {
				t.Fatal(final, e, sends.Load(), queries.Load())
			}
			if lost && final.ProviderOrderNo != "" || !lost && final.ProviderOrderNo != "ALI_ACCEPTED_SHARING" {
				t.Fatal("invented or discarded authenticated acceptance number", final)
			}
			var kind string
			var remaining *int64
			if e = s.pool.QueryRow(ctx, "SELECT proof_kind,observed_remaining FROM channel_sharing_completions WHERE order_id=$1", o.ID).Scan(&kind, &remaining); e != nil || kind != "alipay_royalty_finish" || remaining != nil {
				t.Fatal("Alipay completion fabricated a remaining amount", kind, remaining, e)
			}
		})
	}
}
