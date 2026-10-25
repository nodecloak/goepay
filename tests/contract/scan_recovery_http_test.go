package contract_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestScanRecovery78HTTPWaitPreservesOriginalV1V2SuccessFields(t *testing.T) {
	s, pool, key := fixture(t)
	var charges, queries atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			return
		}
		params := map[string]string{}
		for name, values := range r.Form {
			if len(values) != 1 {
				t.Error("duplicate original parameter")
				return
			}
			params[name] = values[0]
		}
		digest := sha256.Sum256([]byte(adapter.AlipayRequestCanonical(params)))
		sig, e := base64.StdEncoding.DecodeString(params["sign"])
		if e != nil || rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig) != nil {
			t.Error("invalid original RSA2 request")
			return
		}
		var biz map[string]any
		if e = json.Unmarshal([]byte(params["biz_content"]), &biz); e != nil {
			t.Error(e)
			return
		}
		trade, _ := biz["out_trade_no"].(string)
		out := map[string]string{"code": "10000", "app_id": "app78", "seller_id": "seller78", "out_trade_no": trade, "total_amount": "1.00"}
		field := ""
		switch params["method"] {
		case "alipay.trade.pay":
			charges.Add(1)
			field = "alipay_trade_pay_response"
			out["code"] = "10003"
			if biz["auth_code"] != "280000000000000078" {
				t.Error("original bearer changed")
			}
			var sources int
			if e = pool.QueryRow(context.Background(), `SELECT count(*) FROM scan_recovery_origins WHERE trade_no=$1`, trade).Scan(&sources); e != nil || sources != 1 {
				t.Error("charge preceded committed original source", sources, e)
			}
		case "alipay.trade.query":
			queries.Add(1)
			field = "alipay_trade_query_response"
			out["trade_no"] = "ALI78_" + trade
			out["trade_status"] = "TRADE_SUCCESS"
			out["buyer_user_id"] = "2088000000000078"
			if len(biz) != 1 {
				t.Error("query received bearer or changed identifiers")
			}
		default:
			t.Error("unexpected financial operation", params["method"])
			http.Error(w, "unexpected", 500)
			return
		}
		raw, _ := json.Marshal(out)
		responseDigest := sha256.Sum256(raw)
		signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, responseDigest[:])
		if e != nil {
			t.Error(e)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer up.Close()
	p, e := adapter.NewAlipay(adapter.AlipayConfig{EnableScanCancellation: true, AppID: "app78", SellerID: "seller78", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"page", "scan"}, GatewayURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{8: p}, AllowLive: true, ScanRecoveryEnabled: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{9}, 32)
	s.MerchantChannels = map[int64][]int64{1: {8}, 2: {8}}
	for _, legacy := range []bool{true, false} {
		params := compat.Params{"method": "scan", "auth_code": "280000000000000078", "channel_id": "8", "out_trade_no": fmt.Sprintf("recovery-http78-%t", legacy), "name": "local recovery78", "money": "1.00", "clientip": "192.0.2.78", "notify_url": "https://merchant.example.invalid/notify"}
		invoke := func() map[string]any {
			path := "/api/pay/create"
			if legacy {
				path = "/mapi.php"
				params["pid"] = "1"
				params["sign_type"] = "MD5"
				params["sign"] = compat.SignMD5(params, "contract-secret")
			} else {
				params["pid"] = "2"
				params["sign_type"] = "RSA"
				params["timestamp"] = fmt.Sprint(time.Now().Unix())
				params["sign"], _ = compat.SignRSA(params, key)
			}
			values := url.Values{}
			for name, value := range params {
				values.Set(name, value)
			}
			_, result := call(t, s.Handler(), "POST", path, values)
			return result
		}
		out := invoke()
		want := float64(0)
		if legacy {
			want = 1
		}
		if out["code"] != want {
			t.Fatal("original endpoint returned pending before its authenticated query completed", legacy, out)
		}
		trade, _ := out["trade_no"].(string)
		if trade == "" {
			t.Fatal("missing original trade identifier")
		}
		if legacy {
			if out["payurl"] == nil || out["pay_type"] != nil || out["pay_info"] != nil {
				t.Fatal("legacy recovery changed original response shape", out)
			}
		} else {
			var info map[string]string
			if out["pay_type"] != "scan" || json.Unmarshal([]byte(fmt.Sprint(out["pay_info"])), &info) != nil || info["trade_no"] != trade || info["api_trade_no"] != "ALI78_"+trade || info["buyer"] != "2088000000000078" || info["money"] != "1.00" {
				t.Fatal("V2 recovered scan lost original fields", out)
			}
		}
		if again := invoke(); again["trade_no"] != trade || again["code"] != want {
			t.Fatal("scan recovery replay changed original contract", again)
		}
		admin, csrf, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AdminClass: "super"})
		if e != nil {
			t.Fatal(e)
		}
		detail := lookupHTTP(t, s.Handler(), "GET", "/console/v1/admin/orders/"+trade, "", admin, "admin", csrf)
		view, ok := lookupData(t, detail)["scan_recovery"].(map[string]any)
		if detail.Code != 200 || !ok || view["state"] != "completed" || view["queries"] != float64(1) || view["requires_reconciliation"] != false {
			t.Fatal("console omitted original verified recovery", detail.Code, detail.Body.String())
		}
		merchant, proof, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
		if e != nil {
			t.Fatal(e)
		}
		merchantDetail := lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/orders/"+trade, "", merchant, "merchant", proof)
		if legacy && merchantDetail.Code != 200 || !legacy && merchantDetail.Code != 404 {
			t.Fatal("recovery detail changed merchant ownership", merchantDetail.Code)
		}
	}
	if charges.Load() != 2 || queries.Load() != 2 {
		t.Fatal("HTTP wait repeated barcode charge or query", charges.Load(), queries.Load())
	}
	var sources, eligibility, events, cancellations int
	if e = pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM scan_recovery_origins),(SELECT count(*) FROM scan_recovery_eligibility),(SELECT count(*) FROM scan_recovery_query_events),(SELECT count(*) FROM scan_cancellation_claims)`).Scan(&sources, &eligibility, &events, &cancellations); e != nil || sources != 2 || eligibility != 2 || events != 4 || cancellations != 0 {
		t.Fatal("wrong original HTTP recovery evidence", sources, eligibility, events, cancellations, e)
	}
}
