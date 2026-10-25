package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func setScanSharing79(t *testing.T, r *ScanRequest) {
	t.Helper()
	field := reflect.ValueOf(r).Elem().FieldByName("ProfitSharing")
	if !field.IsValid() || field.Kind() != reflect.Bool {
		t.Fatal("scan request cannot bind original profit-sharing declaration")
	}
	field.SetBool(true)
}

func TestWechatV2Sharing79OriginalPaymentDeclarationKeepsMD5Contract(t *testing.T) {
	for _, method := range []string{"native", "jsapi", "h5", "app", "scan"} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				in, e := decodeXML(raw)
				if e != nil || in["profit_sharing"] != "Y" || in["out_trade_no"] != "PAY79_ORIGINAL" || in["total_fee"] != "123" || !VerifyWechatV2(in, sharingCompletionKey79, "MD5", in["sign"]) {
					t.Error("sharing declaration changed original MD5 payment contract")
				}
				out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app79", "mch_id": "mch79", "nonce_str": "declaration79", "prepay_id": "PREPAY79", "code_url": "weixin://wxpay/bizpayurl?pr=local79", "mweb_url": "https://local.example.invalid/pay", "out_trade_no": "PAY79_ORIGINAL", "transaction_id": "TX79_ORIGINAL", "total_fee": "123", "trade_type": "MICROPAY", "openid": "buyer79"}
				out["sign"], _ = SignWechatV2(out, sharingCompletionKey79, "MD5")
				w.Write(encodeXML(out))
			}))
			defer up.Close()
			p, e := NewWechatV2(WechatV2Config{AppID: "app79", MerchantID: "mch79", APIKey: sharingCompletionKey79, BaseURL: up.URL, Methods: []string{method}, HTTPClient: up.Client(), H5SiteURL: "https://local.example.invalid", H5SiteName: "local79", EnableChannelSharing: true})
			if e != nil {
				t.Fatal(e)
			}
			if method == "scan" {
				r := ScanRequest{TradeNo: "PAY79_ORIGINAL", Subject: "local79", Amount: 123, ClientIP: "127.0.0.1", AuthCode: "120000000000000079"}
				setScanSharing79(t, &r)
				_, e = p.Scan(context.Background(), r)
			} else {
				_, e = p.Create(context.Background(), CreateRequest{TradeNo: "PAY79_ORIGINAL", Subject: "local79", Method: method, Amount: 123, ClientIP: "127.0.0.1", NotifyURL: "https://local.example.invalid/notify", OpenID: "buyer79", ProfitSharing: true})
			}
			if e != nil || calls != 1 {
				t.Fatal("original payment declaration missing or retried", e, calls)
			}
			p.config.EnableChannelSharing = false
			if method == "scan" {
				r := ScanRequest{TradeNo: "PAY79_ORIGINAL", Subject: "local79", Amount: 123, ClientIP: "127.0.0.1", AuthCode: "120000000000000079"}
				setScanSharing79(t, &r)
				_, e = p.Scan(context.Background(), r)
			} else {
				_, e = p.Create(context.Background(), CreateRequest{ProfitSharing: true})
			}
			if e == nil || calls != 1 {
				t.Fatal("disabled sharing made a declared payment")
			}
		})
	}
}

func TestWechatV2Sharing79ScanPendingHashPreservesOldFalseAndBindsTrue(t *testing.T) {
	r := ScanRequest{TradeNo: "PAY79", Amount: 123, Subject: "local79", ClientIP: "127.0.0.1", NotifyURL: "https://local.example.invalid/notify", AuthCode: "120000000000000079"}
	old, _ := json.Marshal([]any{"wxpay", "/pay/micropay", "scope79", r.TradeNo, r.Amount, r.Subject, r.ClientIP, r.NotifyURL, r.AuthCode})
	sum := sha256.Sum256(old)
	before, e := scanPendingRequestHash("wxpay", "/pay/micropay", "scope79", r)
	if e != nil || before != hex.EncodeToString(sum[:]) {
		t.Fatal("legacy false hash changed", e)
	}
	setScanSharing79(t, &r)
	after, e := scanPendingRequestHash("wxpay", "/pay/micropay", "scope79", r)
	if e != nil || after == before {
		t.Fatal("private pending proof can change original sharing declaration", e)
	}
}
