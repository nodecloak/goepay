package payment

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestScanCancellation78WechatRequiresOriginalSignedRequestAndActualMutualTLS(t *testing.T) {
	const key = "synthetic-wechat-scan-cancel-key78"
	for _, mode := range []string{"success", "no-recall", "no-nonce", "wrong-mch", "bad-sign", "recall", "wrong-order", "failure", "no-cert", "plain-http", "disabled", "insecure"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, e := io.ReadAll(r.Body)
				if e != nil {
					t.Error(e)
					return
				}
				p, e := decodeXML(raw)
				if e != nil || !VerifyWechatV2(p, key, "MD5", p["sign"]) || p["appid"] != "wx-scan78" || p["mch_id"] != "mch-scan78" || p["out_trade_no"] != "SC78_LOCAL" || p["transaction_id"] != "" || r.URL.Path != "/secapi/pay/reverse" {
					t.Error("wrong signed original cancellation request")
				}
				if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
					t.Error("actual mutual TLS missing")
				}
				out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "wx-scan78", "mch_id": "mch-scan78", "nonce_str": "local-cancel78", "recall": "N"}
				switch mode {
				case "no-recall":
					delete(out, "recall")
				case "no-nonce":
					delete(out, "nonce_str")
				case "wrong-mch":
					out["mch_id"] = "other78"
				case "recall":
					out["recall"] = "Y"
				case "wrong-order":
					out["out_trade_no"] = "other78"
				case "failure":
					out["result_code"] = "FAIL"
					out["err_code"] = "USERPAYING"
				}
				out["sign"], _ = SignWechatV2(out, key, "MD5")
				if mode == "bad-sign" {
					out["sign"] = "wrong78"
				}
				w.Write(encodeXML(out))
			})
			up := httptest.NewUnstartedServer(handler)
			up.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
			up.StartTLS()
			defer up.Close()
			transport := up.Client().Transport.(*http.Transport).Clone()
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
			transport.TLSClientConfig.Certificates = []tls.Certificate{rejectionClientCertificate76(t)}
			if mode == "no-cert" {
				transport.TLSClientConfig.Certificates = nil
			}
			if mode == "insecure" {
				transport.TLSClientConfig.InsecureSkipVerify = true
			}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			endpoint := up.URL
			if mode == "plain-http" {
				plain := httptest.NewServer(handler)
				defer plain.Close()
				endpoint = plain.URL
			}
			base, e := NewWechatV2(WechatV2Config{EnableScanCancellation: mode != "disabled", AppID: "wx-scan78", MerchantID: "mch-scan78", APIKey: key, BaseURL: endpoint, Methods: []string{"scan"}, HTTPClient: client, RefundClient: client})
			if e != nil {
				t.Fatal(e)
			}
			canceller, ok := any(base).(ScanCancellationProvider)
			if !ok {
				t.Fatal("WeChat has no typed scan cancellation")
			}
			result, e := canceller.CancelScan(context.Background(), ScanCancellationRequest{TradeNo: "SC78_LOCAL", ProviderTradeNo: "WX78_LOCAL", Amount: 123})
			if mode == "success" || mode == "no-recall" {
				v, ok := VerifiedScanCancellation(result)
				scope, _ := TransferAccountScope(base)
				if e != nil || !ok || v.Authentication != "wechat_v2_md5_mtls" || v.Product != "/secapi/pay/reverse" || v.AccountScope != scope || v.TradeNo != "SC78_LOCAL" || v.Amount != 123 || result.Action != "close_or_refund" {
					t.Fatal("authenticated original cancellation lost its proof", e)
				}
			} else {
				if e == nil {
					t.Fatal("untrusted or unfinished cancellation accepted")
				}
				if _, ok := VerifiedScanCancellation(result); ok {
					t.Fatal("invalid cancellation gained final proof")
				}
			}
			want := int32(1)
			if mode == "no-cert" || mode == "plain-http" || mode == "disabled" || mode == "insecure" {
				want = 0
			}
			if calls.Load() != want {
				t.Fatal("invalid cancellation sent or retried", calls.Load(), want)
			}
		})
	}
}
