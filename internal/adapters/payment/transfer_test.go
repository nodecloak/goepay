package payment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWechatWalletTransferAndQueryOfficialFields(t *testing.T) {
	key := "synthetic-api-key-32-byte-secret"
	ctx := context.Background()
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		raw, _ := io.ReadAll(r.Body)
		p, e := decodeXML(raw)
		if e != nil || !VerifyWechatV2(p, key, "MD5", p["sign"]) {
			t.Error("bad signature", e)
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "partner_trade_no": "TF42"}
		switch r.URL.Path {
		case "/mmpaymkttransfers/promotion/transfers":
			if p["mch_appid"] != "app" || p["mchid"] != "mch" || p["appid"] != "" || p["mch_id"] != "" || p["openid"] != "openid" || p["amount"] != "101" || p["check_name"] != "FORCE_CHECK" || p["re_user_name"] != "张三" {
				t.Error("wrong transfer request", p)
			}
			out["mch_appid"] = "app"
			out["mchid"] = "mch"
			out["payment_no"] = "wx42"
			out["payment_time"] = "2026-10-23 12:00:00"
		case "/mmpaymkttransfers/gettransferinfo":
			if p["appid"] != "app" || p["mch_id"] != "mch" || p["mch_appid"] != "" {
				t.Error("wrong queryrequest", p)
			}
			out["appid"] = "app"
			out["mch_id"] = "mch"
			out["payment_amount"] = "101"
			out["openid"] = "openid"
			out["detail_id"] = "wx42"
			out["status"] = "SUCCESS"
		default:
			t.Error(r.URL.Path)
		}
		w.Write(encodeXML(out))
	}))
	defer upstream.Close()
	p, _ := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, BaseURL: upstream.URL, HTTPClient: upstream.Client(), RefundClient: upstream.Client()})
	r := TransferRequest{TransferNo: "TF42", Type: "wxpay", Account: "openid", Name: "张三", Amount: 101, Remark: "test"}
	a, e := p.Transfer(ctx, r)
	if e != nil || a.TransferNo != "TF42" || a.ProviderTransferNo != "wx42" || a.State != "SUCCESS" || a.Amount != 101 {
		t.Fatal(a, e)
	}
	a, e = p.QueryTransfer(ctx, r)
	if e != nil || a.State != "SUCCESS" || a.Amount != 101 {
		t.Fatal(a, e)
	}
	noCert, _ := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if _, e = noCert.Transfer(ctx, r); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	if _, e = noCert.QueryTransfer(ctx, r); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	if requests != 2 {
		t.Fatal("unexpected insecure request", requests)
	}
}
func TestWechatTransferMismatchAndUnknownNeverSucceed(t *testing.T) {
	key := "synthetic-api-key-32-byte-secret"
	mode := "mismatch"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "partner_trade_no": "TF42", "payment_amount": "102", "openid": "other", "detail_id": "wx42", "status": "SUCCESS"}
		if mode == "unknown" {
			out["result_code"] = "FAIL"
			out["err_code"] = "NOT_FOUND"
		}
		w.Write(encodeXML(out))
	}))
	defer upstream.Close()
	p, _ := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, BaseURL: upstream.URL, HTTPClient: upstream.Client(), RefundClient: upstream.Client()})
	r := TransferRequest{TransferNo: "TF42", Type: "wxpay", Account: "openid", Amount: 101}
	if _, e := p.QueryTransfer(context.Background(), r); !errors.Is(e, ErrAuthentication) {
		t.Fatal(e)
	}
	mode = "unknown"
	if _, e := p.QueryTransfer(context.Background(), r); e == nil {
		t.Fatal("NOT_FOUND became success")
	}
	if _, e := p.Transfer(context.Background(), r); e == nil {
		t.Fatal("unverified write became success")
	}
}
func TestAlipayKeyModeTransferAndQueryExactSignedNode(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	mode := "success"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		params := map[string]string{}
		for k, v := range r.Form {
			params[k] = v[0]
		}
		if !verifyRSA([]byte(AlipayRequestCanonical(params)), params["sign"], &merchant.PublicKey) {
			t.Error("bad signature")
		}
		var biz map[string]any
		json.Unmarshal([]byte(params["biz_content"]), &biz)
		if biz["out_biz_no"] != "TF42" {
			t.Error(biz)
		}
		node := map[string]any{"code": "10000", "out_biz_no": "TF42", "order_id": "ali42", "pay_date": "2026-10-23 12:00:00"}
		switch params["method"] {
		case "alipay.fund.trans.toaccount.transfer":
			if biz["payee_type"] != "ALIPAY_USERID" || biz["payee_account"] != "2088000000000000" || biz["amount"] != "1.01" || biz["payer_show_name"] != "test" || biz["remark"] != "remark" {
				t.Error(biz)
			}
		case "alipay.fund.trans.common.query":
			if biz["product_code"] != "TRANS_ACCOUNT_NO_PWD" || biz["biz_scene"] != "DIRECT_TRANSFER" {
				t.Error(biz)
			}
			node["trans_amount"] = "1.01"
			node["status"] = "SUCCESS"
			if mode == "mismatch" {
				node["out_biz_no"] = "TF43"
			}
		default:
			t.Error(params["method"])
		}
		raw, _ := json.Marshal(node)
		sign := independentSignature(t, string(raw), platform)
		if mode == "forged" {
			sign = "bad"
		}
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(params["method"], ".", "_") + "_response": json.RawMessage(raw), "sign": sign})
	}))
	defer upstream.Close()
	p, _ := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	r := TransferRequest{TransferNo: "TF42", Type: "alipay", Account: "2088000000000000", Amount: 101, Title: "test", Remark: "remark"}
	a, e := p.Transfer(context.Background(), r)
	if e != nil || a.TransferNo != "TF42" || a.ProviderTransferNo != "ali42" || a.State != "SUCCESS" {
		t.Fatal(a, e)
	}
	a, e = p.QueryTransfer(context.Background(), r)
	if e != nil || a.Amount != 101 || a.State != "SUCCESS" {
		t.Fatal(a, e)
	}
	mode = "mismatch"
	if _, e = p.QueryTransfer(context.Background(), r); !errors.Is(e, ErrAuthentication) {
		t.Fatal(e)
	}
	mode = "forged"
	var unknown *UnknownOutcome
	if _, e = p.Transfer(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal(e)
	}
	r.Type = "bank"
	if _, e = p.Transfer(context.Background(), r); !errors.Is(e, ErrUnsupported) {
		t.Fatal("unconfigured bank transfer enabled", e)
	}
}
