package payment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAlipayChannelBalanceAndTransferProof(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	mode := "ok"
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		r.ParseForm()
		p := map[string]string{}
		for k, v := range r.Form {
			p[k] = v[0]
		}
		if !verifyRSA([]byte(AlipayRequestCanonical(p)), p["sign"], &merchant.PublicKey) {
			t.Error("bad signature")
		}
		biz := map[string]any{}
		json.Unmarshal([]byte(p["biz_content"]), &biz)
		out := map[string]any{"code": "10000"}
		switch p["method"] {
		case "alipay.fund.account.query":
			if biz["alipay_user_id"] != "2088000000000000" || biz["account_type"] != "ACCTRANS_ACCOUNT" {
				t.Error(biz)
			}
			out["available_amount"] = "12.34"
			out["freeze_amount"] = "0.00"
		case "alipay.fund.quota.query":
			if biz["product_code"] != "TRANS_ACCOUNT_NO_PWD" || biz["biz_scene"] != "DIRECT_TRANSFER" {
				t.Error(biz)
			}
			out["new_quota_single_max"] = "100.00"
			out["new_quota_daily_remain"] = "200.00"
		case "alipay.data.bill.ereceipt.apply":
			if biz["type"] != "FUND_DETAIL" || biz["key"] != "ali42" {
				t.Error(biz)
			}
			out["file_id"] = "file42"
		case "alipay.data.bill.ereceipt.query":
			if biz["file_id"] != "file42" {
				t.Error(biz)
			}
			out["file_id"] = "file42"
			out["status"] = "SUCCESS"
			out["download_url"] = "https://receipt.example.invalid/42"
			if mode == "bad-url" {
				out["download_url"] = "javascript:alert(1)"
			}
			if mode == "wrong-file" {
				out["file_id"] = "file43"
			}
		default:
			t.Error(p["method"])
		}
		raw, _ := json.Marshal(out)
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(p["method"], ".", "_") + "_response": json.RawMessage(raw), "sign": independentSignature(t, string(raw), platform)})
	}))
	defer upstream.Close()
	p, _ := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	ctx := context.Background()
	b, e := p.ChannelBalance(ctx, "2088000000000000")
	if e != nil || b.Available != 1234 || b.Frozen != 0 || b.Quota["new_quota_single_max"] != "100.00" {
		t.Fatal(b, e)
	}
	proof, e := p.ApplyTransferProof(ctx, "ali42")
	if e != nil || proof.FileID != "file42" || proof.State != "PROCESSING" || proof.DownloadURL != "" {
		t.Fatal(proof, e)
	}
	proof, e = p.QueryTransferProof(ctx, proof.FileID)
	if e != nil || proof.State != "SUCCESS" || proof.DownloadURL != "https://receipt.example.invalid/42" {
		t.Fatal(proof, e)
	}
	for _, m := range []string{"bad-url", "wrong-file"} {
		mode = m
		if _, e = p.QueryTransferProof(ctx, "file42"); !errors.Is(e, ErrAuthentication) {
			t.Fatal(m, e)
		}
	}
	before := calls
	if _, e = p.ApplyTransferProof(ctx, ""); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e = p.ChannelBalance(ctx, ""); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if calls != before {
		t.Fatal("invalid input reached upstream")
	}
}
