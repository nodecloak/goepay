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
	"testing"
)

func TestAlipayRelationRequestNumberRejectsOverlengthAndFinancialOnlySymbolsBeforeIO(t *testing.T) {
	calls := 0
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer up.Close()
	key := testKey(t)
	p, e := NewAlipay(AlipayConfig{AppID: "local-app", SellerID: "2088000000000099", PrivateKey: key, PublicKey: &key.PublicKey, EnableChannelSharing: true, GatewayURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	for _, no := range []string{"PB" + strings.Repeat("a", 32), "bad-no", "bad|no"} {
		if e = p.BindChannelSharingReceiver(context.Background(), no, SharingReceiver{Type: "userId", Account: "2088000000000001"}); !errors.Is(e, ErrInvalid) || calls != 0 {
			t.Fatal("invalid relation request reached upstream", no, e, calls)
		}
	}
}

func TestAlipayChannelSharingTLSAsyncPartialAndExplicitReturnEvidence(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	mode, calls := "partial", 0
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		r.ParseForm()
		method := r.Form.Get("method")
		var input map[string]json.RawMessage
		if json.Unmarshal([]byte(r.Form.Get("biz_content")), &input) != nil {
			t.Error("bad business JSON")
		}
		result := map[string]any{"code": "10000"}
		switch method {
		case "alipay.trade.royalty.relation.bind", "alipay.trade.royalty.relation.unbind":
			if v3String(input, "out_request_no") != "BIND_1" {
				t.Error("relation request number was regenerated")
			}
			result["result_code"] = "SUCCESS"
		case "alipay.trade.order.settle":
			if v3String(input, "out_request_no") != "SHARE_1" || v3String(input, "trade_no") != "ALI_PAYMENT" || v3String(input, "royalty_mode") != "async" {
				t.Error("sharing identity or execution mode changed")
			}
			result["trade_no"] = "ALI_PAYMENT"
			result["settle_no"] = "ALI_SHARE"
		case "alipay.trade.order.settle.query":
			if v3String(input, "out_request_no") != "SHARE_1" || v3String(input, "trade_no") != "ALI_PAYMENT" {
				t.Error("query lost original no/trade")
			}
			details := []map[string]any{{"trans_in_type": "userId", "trans_in": "receiver-one", "amount": "0.10", "state": "SUCCESS", "detail_id": "ALI_DETAIL_1", "operation_type": "transfer"}, {"trans_in_type": "loginName", "trans_in": "two@example.invalid", "amount": "0.20", "state": "FAIL", "error_code": "NO_AUTH", "operation_type": "transfer"}}
			if mode == "wrong-amount" {
				details[1]["amount"] = "0.21"
			}
			if mode == "duplicate" {
				details[1] = details[0]
			}
			result["out_request_no"] = "SHARE_1"
			result["royalty_detail_list"] = details
		case "alipay.trade.refund":
			if v3String(input, "refund_amount") != "0.00" || v3String(input, "out_request_no") != "RETURN_1" || v3String(input, "trade_no") != "ALI_PAYMENT" {
				t.Error("sharing return became a buyer refund or changed its stable request")
			}
			result["trade_no"] = "ALI_PAYMENT"
			result["refund_fee"] = "0.00"
		case "alipay.trade.fastpay.refund.query":
			result["trade_no"] = "ALI_PAYMENT"
			result["out_request_no"] = "RETURN_1"
			result["refund_amount"] = "0.00"
			result["refund_status"] = "REFUND_SUCCESS"
			if mode != "missing-return-details" {
				amount := "0.10"
				if mode == "wrong-return" {
					amount = "0.11"
				}
				result["refund_royaltys"] = []map[string]any{{"royalty_type": "transfer", "trans_out": "receiver-one", "ori_trans_out": "receiver-one", "refund_amount": amount, "result_code": "SUCCESS"}}
			}
		default:
			t.Error("unexpected Alipay method", method)
			w.WriteHeader(500)
			return
		}
		node, _ := json.Marshal(result)
		digest := sha256.Sum256(node)
		signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		envelope := map[string]any{strings.ReplaceAll(method, ".", "_") + "_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(signature)}
		json.NewEncoder(w).Encode(envelope)
	}))
	defer up.Close()
	p, e := NewAlipay(AlipayConfig{AppID: "local-app", SellerID: "2088000000000099", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client(), EnableChannelSharing: true, EnableChannelSharingReturn: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = p.BindChannelSharingReceiver(context.Background(), "BIND_1", SharingReceiver{Type: "userId", Account: "receiver-one"}); e != nil {
		t.Fatal(e)
	}
	request := ChannelSharingRequest{OrderNo: "SHARE_1", ProviderTradeNo: "ALI_PAYMENT", TotalAmount: 101, Receivers: []SharingAllocation{{SharingReceiver: SharingReceiver{Type: "userId", Account: "receiver-one"}, Amount: 10, Description: "本地分账"}, {SharingReceiver: SharingReceiver{Type: "loginName", Account: "two@example.invalid"}, Amount: 20, Description: "本地分账"}}}
	result, e := p.SubmitChannelSharing(context.Background(), request)
	if e != nil || result.State != "pending" {
		t.Fatal("async accepted is not settled", result, e)
	}
	result, e = p.QueryChannelSharing(context.Background(), request)
	if e != nil || result.State != "partial" || len(result.Receivers) != 2 {
		t.Fatal("must check all Alipay receivers", result, e)
	}
	for _, bad := range []string{"wrong-amount", "duplicate"} {
		mode = bad
		if _, e = p.QueryChannelSharing(context.Background(), request); e == nil {
			t.Fatal("bad signed receiver evidence accepted", bad)
		}
	}
	mode = "partial"
	ret := ChannelSharingReturnRequest{OrderNo: "SHARE_1", ProviderOrderNo: "ALI_SHARE", ProviderTradeNo: "ALI_PAYMENT", ReturnNo: "RETURN_1", ReceiverType: "userId", ReceiverAccount: "receiver-one", Amount: 10, Description: "本地回退"}
	if r, e := p.ReturnChannelSharing(context.Background(), ret); e != nil || r.State != "pending" {
		t.Fatal("accepted zero refund does not prove returned royalty", r, e)
	}
	if r, e := p.QueryChannelSharingReturn(context.Background(), ret); e != nil || r.State != "succeeded" || r.Amount != 10 {
		t.Fatal(r, e)
	}
	for _, bad := range []string{"missing-return-details", "wrong-return"} {
		mode = bad
		if _, e = p.QueryChannelSharingReturn(context.Background(), ret); e == nil {
			t.Fatal("zero buyer refund alone proved a royalty return", bad)
		}
	}
	before := calls
	p.config.EnableChannelSharingReturn = false
	if _, e = p.ReturnChannelSharing(context.Background(), ret); !errors.Is(e, ErrUnsupported) || calls != before {
		t.Fatal("manual royalty return did not require separate product evidence enablement", e)
	}
}
