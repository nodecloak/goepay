package payment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWechatScanVerifiesIdentityAmountAndUnknown(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	mode := "success"
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		p, e := decodeXML(raw)
		if e != nil || !VerifyWechatV2(p, key, "MD5", p["sign"]) || p["auth_code"] != "120000000000000001" || p["out_trade_no"] != "42" || p["spbill_create_ip"] != "192.0.2.1" || r.URL.Path != "/pay/micropay" {
			t.Error("bad micropay request", e)
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "out_trade_no": "42", "transaction_id": "wx42", "trade_type": "MICROPAY", "total_fee": "100", "openid": "synthetic-buyer"}
		switch mode {
		case "unknown":
			out["result_code"] = "FAIL"
			out["err_code"] = "USERPAYING"
		case "wrong-amount":
			out["total_fee"] = "101"
		case "wrong-order":
			out["out_trade_no"] = "99"
		case "wrong-app":
			out["appid"] = "other"
		case "wrong-type":
			out["trade_type"] = "NATIVE"
		}
		out["sign"], _ = SignWechatV2(out, key, "MD5")
		if mode == "bad-sign" {
			out["sign"] = "invalid"
		}
		w.Write(encodeXML(out))
	}))
	defer upstream.Close()
	p, e := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, Methods: []string{"native"}, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	scan, ok := any(p).(ScanProvider)
	if !ok {
		t.Fatal("wechat scan protocol missing")
	}
	r := ScanRequest{TradeNo: "42", Subject: "test", Amount: 100, AuthCode: "120000000000000001", ClientIP: "192.0.2.1"}
	if _, e = scan.Scan(context.Background(), r); !errors.Is(e, ErrUnsupported) || calls != 0 {
		t.Fatal("default scan enabled", e, calls)
	}
	p.config.Methods = append(p.config.Methods, "scan")
	event, e := scan.Scan(context.Background(), r)
	if e != nil || event.State != "SUCCESS" || event.Amount != 100 || event.BuyerID != "synthetic-buyer" || event.ProviderTradeNo != "wx42" {
		t.Fatal(event, e)
	}
	mode = "unknown"
	if _, e = scan.Scan(context.Background(), r); e == nil {
		t.Fatal("USERPAYING declared paid")
	}
	for _, bad := range []string{"wrong-amount", "wrong-order", "wrong-app", "wrong-type", "bad-sign"} {
		mode = bad
		var unknown *UnknownOutcome
		if _, e = scan.Scan(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("invalid response not uncertain", bad, e)
		}
	}
	before := calls
	r.AuthCode = "280000000000000001"
	if _, e = scan.Scan(context.Background(), r); !errors.Is(e, ErrInvalid) || calls != before {
		t.Fatal("other provider code submitted", e)
	}
}

func TestAlipayScanUsesBarCodeAndExactSignedResult(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	mode := "success"
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		r.ParseForm()
		p := map[string]string{}
		for k, v := range r.Form {
			p[k] = v[0]
		}
		var biz map[string]any
		json.Unmarshal([]byte(p["biz_content"]), &biz)
		if !verifyRSA([]byte(AlipayRequestCanonical(p)), p["sign"], &merchant.PublicKey) || p["method"] != "alipay.trade.pay" || p["notify_url"] != "https://pay.example.invalid/notify" || biz["auth_code"] != "280000000000000001" || biz["scene"] != "bar_code" || biz["total_amount"] != "1.00" {
			t.Error("bad alipay scan request")
		}
		out := map[string]string{"code": "10000", "out_trade_no": "42", "trade_no": "ali42", "total_amount": "1.00", "buyer_user_id": "2088000000000000"}
		switch mode {
		case "unknown":
			out["code"] = "10003"
		case "wrong-order":
			out["out_trade_no"] = "99"
		case "wrong-amount":
			out["total_amount"] = "1.01"
		case "wrong-currency":
			out["trans_currency"] = "USD"
		}
		node, _ := json.Marshal(out)
		signature, _ := signRSA(node, platform)
		if mode == "bad-sign" {
			signature = "bad"
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_pay_response": json.RawMessage(node), "sign": signature})
	}))
	defer upstream.Close()
	p, e := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	scan, ok := any(p).(ScanProvider)
	if !ok {
		t.Fatal("alipay scan protocol missing")
	}
	r := ScanRequest{TradeNo: "42", Subject: "test", Amount: 100, AuthCode: "280000000000000001", ClientIP: "192.0.2.1", NotifyURL: "https://pay.example.invalid/notify"}
	if _, e = scan.Scan(context.Background(), r); !errors.Is(e, ErrUnsupported) || calls != 0 {
		t.Fatal("default scan enabled", e)
	}
	p.config.Methods = append(p.config.Methods, "scan")
	event, e := scan.Scan(context.Background(), r)
	if e != nil || event.ProviderTradeNo != "ali42" || event.BuyerID != "2088000000000000" || event.Amount != 100 {
		t.Fatal(event, e)
	}
	for _, bad := range []string{"unknown", "wrong-order", "wrong-amount", "wrong-currency", "bad-sign"} {
		mode = bad
		if _, e = scan.Scan(context.Background(), r); e == nil {
			t.Fatal("unverified scan paid", bad)
		}
	}
	before := calls
	r.AuthCode = strings.Repeat("2", 128)
	if _, e = scan.Scan(context.Background(), r); !errors.Is(e, ErrInvalid) || calls != before {
		t.Fatal("invalid payer code submitted", e)
	}
}

func TestScanRecoveryQueryRejectsDifferentCurrency(t *testing.T) {
	key := testKey(t)
	const apiKey = "0123456789abcdef0123456789abcdef"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pay/orderquery" {
			p := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "out_trade_no": "42", "transaction_id": "wx42", "total_fee": "100", "fee_type": "USD", "trade_state": "SUCCESS"}
			p["sign"], _ = SignWechatV2(p, apiKey, "MD5")
			w.Write(encodeXML(p))
			return
		}
		node := []byte(`{"code":"10000","out_trade_no":"42","trade_no":"ali42","total_amount":"1.00","trans_currency":"USD","trade_status":"TRADE_SUCCESS"}`)
		sign, _ := signRSA(node, key)
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_query_response": json.RawMessage(node), "sign": sign})
	}))
	defer upstream.Close()
	wx, _ := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: apiKey, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	ali, _ := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	for _, p := range []Provider{wx, ali} {
		if _, e := p.Query(context.Background(), QueryRequest{TradeNo: "42"}); !errors.Is(e, ErrAuthentication) {
			t.Fatal("foreign currency mapped to CNY", p.ID(), e)
		}
	}
}

func TestAlipayLateScanNotificationRejectsDifferentCurrency(t *testing.T) {
	key := testKey(t)
	a, e := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey})
	if e != nil {
		t.Fatal(e)
	}
	p := map[string]string{"app_id": "app", "out_trade_no": "42", "trade_no": "ali42", "total_amount": "1.00", "trade_status": "TRADE_SUCCESS", "trans_currency": "USD", "sign_type": "RSA2"}
	p["sign"], _ = signRSA([]byte(AlipayNotificationCanonical(p)), key)
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	if _, e = a.VerifyNotification(context.Background(), nil, []byte(v.Encode()), ExpectedPayment{TradeNo: "42", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("foreign currency notification credited as CNY", e)
	}
}
