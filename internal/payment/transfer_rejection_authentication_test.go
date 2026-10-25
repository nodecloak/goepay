package payment

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func TestTransferRejection78ConflictingAccountCannotReleaseOriginalReserve(t *testing.T) {
	for _, mode := range []string{"wrong-account", "plaintext", "authenticated"} {
		t.Run(mode, func(t *testing.T) {
			s, flow, _ := transferFixture(t)
			ctx := context.Background()
			const key = "synthetic-transfer-rejection-key78"
			var calls atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				io.Copy(io.Discard, r.Body)
				if r.URL.Path != "/mmpaymkttransfers/promotion/transfers" || (mode != "plaintext" && (r.TLS == nil || len(r.TLS.PeerCertificates) != 1)) {
					t.Error("original transfer did not use actual mutual TLS")
					return
				}
				fields := map[string]string{"return_code": "SUCCESS", "result_code": "FAIL", "err_code": "NOTENOUGH", "mchid": "mch78", "mch_appid": "app78", "nonce_str": "negative78"}
				if mode == "wrong-account" {
					fields["mchid"] = "other-mch78"
				}
				writeScanWechat78(w, fields, key)
			})
			up := httptest.NewUnstartedServer(handler)
			up.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
			up.StartTLS()
			defer up.Close()
			transport := up.Client().Transport.(*http.Transport).Clone()
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
			transport.TLSClientConfig.Certificates = []tls.Certificate{scanClientCertificate78(t)}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			endpoint := up.URL
			if mode == "plaintext" {
				plain := httptest.NewServer(handler)
				defer plain.Close()
				endpoint = plain.URL
			}
			p, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "app78", MerchantID: "mch78", APIKey: key, BaseURL: endpoint, HTTPClient: client, RefundClient: client})
			if e != nil {
				t.Fatal(e)
			}
			flow.Providers[7] = p
			request := adapter.TransferRequest{Type: "wxpay", Account: "synthetic-payee78", Name: "Local Payee", Amount: 100, Remark: "local only"}
			transfer, e := flow.Transfer(ctx, 501, 7, "rejection-origin78", request)
			if e == nil || calls.Load() != 1 {
				t.Fatal("test did not reach original local provider rejection", calls.Load(), e)
			}
			balance, e := s.Balance(ctx, 501)
			if e != nil {
				t.Fatal(e)
			}
			if mode != "authenticated" {
				if transfer.Status != "unknown" || balance.Available != 890 || balance.Reserved != 110 {
					t.Fatal("untrusted account echo released original funds", transfer.Status, balance)
				}
			} else if transfer.Status != "failed" || balance.Available != 1000 || balance.Reserved != 0 {
				t.Fatal("authenticated definitive failure lost its reserve release", transfer.Status, balance)
			}
			flow.Transfer(ctx, 501, 7, "rejection-origin78", request)
			if calls.Load() != 1 {
				t.Fatal("rejected original transfer sent again")
			}
		})
	}
}
