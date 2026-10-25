package payment

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

const sharingCompletionKey79 = "0123456789abcdef0123456789abcdef"

func sharingCompletionProvider79(t *testing.T, mode string, handler func(*testing.T, string, map[string]string) map[string]string) (*WechatV2, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		fields, e := decodeXML(raw)
		if e != nil || fields["mch_id"] != "mch79" || fields["sign_type"] != "HMAC-SHA256" || !VerifyWechatV2(fields, sharingCompletionKey79, "HMAC-SHA256", fields["sign"]) {
			t.Error("completion lost authenticated original merchant request")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/secapi/") && (r.TLS == nil || len(r.TLS.PeerCertificates) != 1) {
			t.Error("financial completion sent without client certificate")
			return
		}
		out := handler(t, r.URL.Path, fields)
		out["return_code"], out["mch_id"], out["nonce_str"] = "SUCCESS", "mch79", "completion79"
		if fields["appid"] != "" {
			out["appid"] = "app79"
		}
		out["sign"], _ = SignWechatV2(out, sharingCompletionKey79, "HMAC-SHA256")
		w.Write(encodeXML(out))
	}))
	up.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequestClientCert}
	up.StartTLS()
	t.Cleanup(up.Close)
	transport := up.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.Certificates = []tls.Certificate{rejectionClientCertificate76(t)}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport}
	cfg := WechatV2Config{AppID: "app79", MerchantID: "mch79", APIKey: sharingCompletionKey79, BaseURL: up.URL, HTTPClient: client, RefundClient: client}
	enableWechatV2Sharing79(t, &cfg)
	if mode == "no-cert" || mode == "query-no-cert" {
		cfg.HTTPClient, cfg.RefundClient = up.Client(), nil
	}
	p, e := NewWechatV2(cfg)
	if e != nil {
		t.Fatal(e)
	}
	return p, &calls
}

func TestWechatV2Sharing79ReturnUsesOriginalAllocationAndActualReturnContract(t *testing.T) {
	for _, mode := range []string{"success", "processing", "failed", "query-no-cert", "wrong-order", "wrong-return", "wrong-provider", "wrong-account", "wrong-type", "wrong-amount", "missing-number", "unknown-state", "business-failure", "personal", "no-cert"} {
		t.Run(mode, func(t *testing.T) {
			p, calls := sharingCompletionProvider79(t, mode, func(t *testing.T, path string, f map[string]string) map[string]string {
				if path != "/secapi/pay/profitsharingreturn" && path != "/pay/profitsharingreturnquery" || f["appid"] != "app79" || f["out_order_no"] != "PS79_ORIGINAL" || f["out_return_no"] != "RETURN79_ORIGINAL" || f["order_id"] != "" {
					t.Error("return changed original request identity")
				}
				if path == "/secapi/pay/profitsharingreturn" && (f["return_account_type"] != "MERCHANT_ID" || f["return_account"] != "receiver79" || f["return_amount"] != "100" || f["description"] != "local return79") {
					t.Error("return changed original receiver or amount")
				}
				out := map[string]string{"out_order_no": "PS79_ORIGINAL", "order_id": "WECHAT_PS79", "out_return_no": "RETURN79_ORIGINAL", "return_no": "WX_RETURN79", "return_account_type": "MERCHANT_ID", "return_account": "receiver79", "return_amount": "100", "result": "SUCCESS"}
				switch mode {
				case "processing":
					out["result"] = "PROCESSING"
				case "failed":
					out["result"] = "FAILED"
				case "wrong-order":
					out["out_order_no"] = "OTHER79"
				case "wrong-return":
					out["out_return_no"] = "OTHER79"
				case "wrong-provider":
					out["order_id"] = "OTHER79"
				case "wrong-account":
					out["return_account"] = "other79"
				case "wrong-type":
					out["return_account_type"] = "PERSONAL_OPENID"
				case "wrong-amount":
					out["return_amount"] = "101"
				case "missing-number":
					delete(out, "return_no")
				case "unknown-state":
					out["result"] = "FINISHED"
				case "business-failure":
					out["result_code"] = "FAIL"
				}
				// V2 return and return-query have no result_code success field.
				return out
			})
			provider, ok := any(p).(ChannelSharingProvider)
			if !ok {
				t.Fatal("V2 sharing return protocol missing")
			}
			r := ChannelSharingReturnRequest{OrderNo: "PS79_ORIGINAL", ProviderOrderNo: "WECHAT_PS79", ReturnNo: "RETURN79_ORIGINAL", ReceiverType: "MERCHANT_ID", ReceiverAccount: "receiver79", Amount: 100, Description: "local return79"}
			if mode == "personal" {
				r.ReceiverType = "PERSONAL_OPENID"
			}
			var got ChannelSharingReturnResult
			var e error
			if mode == "query-no-cert" {
				got, e = provider.QueryChannelSharingReturn(context.Background(), r)
			} else {
				got, e = provider.ReturnChannelSharing(context.Background(), r)
			}
			valid := mode == "success" || mode == "processing" || mode == "failed" || mode == "query-no-cert"
			if valid {
				want := map[string]string{"success": "succeeded", "processing": "pending", "failed": "failed", "query-no-cert": "succeeded"}[mode]
				if e != nil || got.State != want || got.Amount != 100 || got.ReturnNo != r.ReturnNo || got.ProviderReturnNo != "WX_RETURN79" {
					t.Fatal("incorrect trusted original return", got, e)
				}
			} else if e == nil {
				t.Fatal("untrusted return accepted")
			}
			if !valid && mode != "personal" && mode != "no-cert" {
				var unknown *UnknownOutcome
				if !errors.As(e, &unknown) {
					t.Fatal("ambiguous return write lost uncertainty", e)
				}
			}
			wantCalls := int32(1)
			if mode == "personal" || mode == "no-cert" {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Fatal("return retried or invalid request sent", calls.Load())
			}
		})
	}
}

func TestWechatV2Sharing79FinishAcceptanceRequiresOriginalExplicitAmountQuery(t *testing.T) {
	for _, mode := range []string{"accepted", "query-success", "query-pending", "query-closed", "query-no-cert", "query-wrong-amount", "query-wrong-account", "query-missing", "query-duplicate", "query-pending-finished", "wrong-order", "no-cert"} {
		t.Run(mode, func(t *testing.T) {
			query := strings.HasPrefix(mode, "query-")
			p, calls := sharingCompletionProvider79(t, mode, func(t *testing.T, path string, f map[string]string) map[string]string {
				if f["transaction_id"] != "TX79_ORIGINAL" || f["out_order_no"] != "FINISH79_ORIGINAL" {
					t.Error("finish lost original payment or operation")
				}
				out := map[string]string{"result_code": "SUCCESS", "transaction_id": "TX79_ORIGINAL", "out_order_no": "FINISH79_ORIGINAL", "order_id": "WX_FINISH79"}
				if query {
					if path != "/pay/profitsharingquery" {
						t.Error("finish query used write endpoint")
					}
					out["status"] = "FINISHED"
					row := map[string]any{"type": "MERCHANT_ID", "account": "mch79", "amount": 700, "result": "SUCCESS"}
					switch mode {
					case "query-pending":
						out["status"] = "PROCESSING"
						row["result"] = "PENDING"
					case "query-closed":
						row["result"] = "CLOSED"
					case "query-wrong-amount":
						row["amount"] = 701
					case "query-wrong-account":
						row["account"] = "other79"
					case "query-pending-finished":
						row["result"] = "PENDING"
					}
					rows := []map[string]any{row}
					if mode == "query-missing" {
						rows = nil
					}
					if mode == "query-duplicate" {
						rows = append(rows, row)
					}
					raw, _ := json.Marshal(rows)
					out["receivers"] = string(raw)
				} else if path != "/secapi/pay/profitsharingfinish" || f["appid"] != "app79" || f["description"] != "local finish79" {
					t.Error("wrong original finish payload")
				}
				if mode == "wrong-order" {
					out["out_order_no"] = "OTHER79"
				}
				return out
			})
			provider, ok := any(p).(ChannelSharingFinisher)
			if !ok {
				t.Fatal("V2 explicit finish protocol missing")
			}
			r := ChannelSharingFinishRequest{OrderNo: "FINISH79_ORIGINAL", ProviderTradeNo: "TX79_ORIGINAL", Description: "local finish79", ExpectedUnsplit: 700}
			if query {
				r.ProviderOrderNo = "WX_FINISH79"
			}
			var got ChannelSharingFinishResult
			var e error
			if query {
				got, e = provider.QueryChannelSharingFinish(context.Background(), r)
			} else {
				got, e = provider.FinishChannelSharing(context.Background(), r)
			}
			valid := mode == "accepted" || mode == "query-success" || mode == "query-pending" || mode == "query-closed" || mode == "query-no-cert"
			if valid {
				want, amount := "pending", int64(0)
				if mode == "query-success" || mode == "query-no-cert" {
					want, amount = "succeeded", 700
				}
				if mode == "query-closed" {
					want = "failed"
				}
				if e != nil || got.State != want || got.UnsplitAmount != amount || got.OrderNo != r.OrderNo {
					t.Fatal("finish settled without explicit matching evidence", got, e)
				}
			} else if e == nil {
				t.Fatal("invalid finish amount or original identity accepted")
			}
			wantCalls := int32(1)
			if mode == "no-cert" {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Fatal("finish retried or invalid write sent", strconv.Itoa(int(calls.Load())))
			}
		})
	}
}
