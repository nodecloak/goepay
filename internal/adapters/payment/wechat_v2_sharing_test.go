package payment

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type sharingSingleAPI79 interface {
	ValidateChannelSharing(ChannelSharingRequest) error
	SubmitChannelSharing(context.Context, ChannelSharingRequest) (ChannelSharingResult, error)
	QueryChannelSharing(context.Context, ChannelSharingRequest) (ChannelSharingResult, error)
	ChannelSharingRemaining(context.Context, string) (int64, error)
}

func TestWechatV2Sharing79SingleAllocationKeepsOriginalReceiversAndMutualTLS(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	for _, mode := range []string{"partial", "query", "query-no-cert", "processing", "wrong-order", "wrong-amount", "missing-receiver", "missing-detail", "conflicting-alias", "duplicate-json", "remaining", "wrong-remaining-order", "no-cert", "not-single"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, e := io.ReadAll(r.Body)
				if e != nil {
					t.Error(e)
					return
				}
				fields, e := decodeXML(raw)
				if e != nil || !VerifyWechatV2(fields, key, "HMAC-SHA256", fields["sign"]) || fields["sign_type"] != "HMAC-SHA256" || fields["mch_id"] != "mch79" {
					t.Error("wrong original signed sharing request")
					return
				}
				out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "mch_id": "mch79", "nonce_str": "sharing79", "transaction_id": "TX79_ORIGINAL", "out_order_no": "PS79_ORIGINAL", "order_id": "WECHAT_PS79", "status": "FINISHED"}
				switch r.URL.Path {
				case "/secapi/pay/profitsharing":
					if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 || fields["appid"] != "app79" || fields["transaction_id"] != "TX79_ORIGINAL" || fields["out_order_no"] != "PS79_ORIGINAL" {
						t.Error("single allocation lost original context or client certificate")
					}
					var receivers []map[string]any
					if json.Unmarshal([]byte(fields["receivers"]), &receivers) != nil || len(receivers) != 2 || receivers[0]["amount"] != float64(100) || receivers[1]["amount"] != float64(200) {
						t.Error("original allocation amounts changed")
					}
					out["appid"] = "app79"
				case "/pay/profitsharingquery":
					if fields["transaction_id"] != "TX79_ORIGINAL" || fields["out_order_no"] != "PS79_ORIGINAL" {
						t.Error("query changed original keys")
					}
				case "/pay/profitsharingorderamountquery":
					out["unsplit_amount"] = "0"
					if fields["transaction_id"] != "TX79_ORIGINAL" {
						t.Error("remaining query changed original payment")
					}
				default:
					t.Error("unexpected V2 financial endpoint", r.URL.Path)
					return
				}
				rows := []map[string]any{{"type": "MERCHANT_ID", "account": "receiver79", "amount": 100, "result": "SUCCESS", "detail_id": "DETAIL79_A"}, {"type": "PERSONAL_OPENID", "account": "openid79", "amount": 200, "result": "CLOSED", "fail_reason": "ACCOUNT_ABNORMAL"}}
				if r.URL.Path == "/secapi/pay/profitsharing" {
					for _, row := range rows {
						row["receiver_account"] = row["account"]
						delete(row, "account")
					}
				}
				switch mode {
				case "processing":
					out["status"] = "PROCESSING"
					rows = nil
				case "wrong-order":
					out["out_order_no"] = "OTHER79"
				case "wrong-amount":
					rows[0]["amount"] = 101
				case "missing-receiver":
					rows = rows[:1]
				case "missing-detail":
					delete(rows[0], "detail_id")
				case "conflicting-alias":
					rows[0]["account"] = "other79"
				case "wrong-remaining-order":
					out["transaction_id"] = "OTHER79"
				}
				encoded, _ := json.Marshal(rows)
				out["receivers"] = string(encoded)
				if mode == "duplicate-json" {
					out["receivers"] = `[{"type":"MERCHANT_ID","account":"other79","account":"receiver79","amount":100,"result":"SUCCESS","detail_id":"DETAIL79_A"}]`
				}
				out["sign"], _ = SignWechatV2(out, key, "HMAC-SHA256")
				w.Write(encodeXML(out))
			}))
			up.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequestClientCert}
			up.StartTLS()
			defer up.Close()
			transport := up.Client().Transport.(*http.Transport).Clone()
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
			transport.TLSClientConfig.Certificates = []tls.Certificate{rejectionClientCertificate76(t)}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			cfg := WechatV2Config{AppID: "app79", MerchantID: "mch79", APIKey: key, BaseURL: up.URL, HTTPClient: client, RefundClient: client}
			enableWechatV2Sharing79(t, &cfg)
			if mode == "no-cert" || mode == "query-no-cert" {
				cfg.RefundClient = nil
				cfg.HTTPClient = up.Client()
			}
			base, e := NewWechatV2(cfg)
			if e != nil {
				t.Fatal(e)
			}
			p, ok := any(base).(sharingSingleAPI79)
			if !ok {
				t.Fatal("original single V2 sharing and remaining protocol missing")
			}
			r := ChannelSharingRequest{OrderNo: "PS79_ORIGINAL", ProviderTradeNo: "TX79_ORIGINAL", TotalAmount: 1000, UnfreezeUnsplit: true, Receivers: []SharingAllocation{{SharingReceiver: SharingReceiver{Type: "MERCHANT_ID", Account: "receiver79"}, Amount: 100, Description: "local merchant79"}, {SharingReceiver: SharingReceiver{Type: "PERSONAL_OPENID", Account: "openid79"}, Amount: 200, Description: "local person79"}}}
			if mode == "not-single" {
				r.UnfreezeUnsplit = false
			}
			if mode == "remaining" || mode == "wrong-remaining-order" {
				amount, e := p.ChannelSharingRemaining(context.Background(), r.ProviderTradeNo)
				if mode == "remaining" && (e != nil || amount != 0) || mode != "remaining" && e == nil {
					t.Fatal("wrong authenticated remaining amount", amount, e)
				}
			} else {
				var result ChannelSharingResult
				if mode == "query" || mode == "query-no-cert" {
					result, e = p.QueryChannelSharing(context.Background(), r)
				} else {
					result, e = p.SubmitChannelSharing(context.Background(), r)
				}
				valid := mode == "partial" || mode == "query" || mode == "query-no-cert" || mode == "processing"
				if valid {
					want := "partial"
					if mode == "processing" {
						want = "pending"
					}
					if e != nil || result.State != want || result.OrderNo != r.OrderNo || result.ProviderTradeNo != r.ProviderTradeNo || len(result.Receivers) != 2 {
						t.Fatal("FINISHED falsely settled or original allocation lost", result, e)
					}
				} else if e == nil {
					t.Fatal("untrusted original allocation accepted", mode)
				}
				if !valid && mode != "no-cert" && mode != "not-single" {
					var unknown *UnknownOutcome
					if !errors.As(e, &unknown) {
						t.Fatal("ambiguous write did not retain original uncertainty", mode, e)
					}
				}
			}
			wantCalls := int32(1)
			if mode == "no-cert" || mode == "not-single" {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Fatal("invalid allocation performed I/O or implicitly retried", mode, calls.Load())
			}
		})
	}
}
