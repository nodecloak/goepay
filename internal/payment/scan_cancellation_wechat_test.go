package payment

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
)

func scanClientCertificate78(t *testing.T) tls.Certificate {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(78), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
func writeScanWechat78(w http.ResponseWriter, fields map[string]string, key string) {
	fields["sign"], _ = adapter.SignWechatV2(fields, key, "MD5")
	var body bytes.Buffer
	body.WriteString("<xml>")
	for field, value := range fields {
		fmt.Fprintf(&body, "<%s>", field)
		xml.EscapeText(&body, []byte(value))
		fmt.Fprintf(&body, "</%s>", field)
	}
	body.WriteString("</xml>")
	w.Write(body.Bytes())
}

func TestScanCancellation78WechatActualMutualTLSFinalOrRecallKeepsOriginalBarrier(t *testing.T) {
	for _, mode := range []string{"known", "recall"} {
		t.Run(mode, func(t *testing.T) {
			s, _, o := recoveryOriginFixture78(t)
			ctx := context.Background()
			const key = "synthetic-pending-source-api-key78"
			var reversals atomic.Int32
			up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
					t.Error("original cancellation test lost real client certificate")
					return
				}
				raw, _ := io.ReadAll(r.Body)
				var envelope struct {
					Entries []struct {
						XMLName xml.Name
						Value   string `xml:",chardata"`
					} `xml:",any"`
				}
				if xml.Unmarshal(raw, &envelope) != nil {
					t.Error("invalid local signed request")
					return
				}
				request := map[string]string{}
				for _, entry := range envelope.Entries {
					if _, duplicate := request[entry.XMLName.Local]; duplicate {
						t.Error("duplicate original field")
						return
					}
					request[entry.XMLName.Local] = entry.Value
				}
				if !adapter.VerifyWechatV2(request, key, "MD5", request["sign"]) || request["out_trade_no"] != o.TradeNo || request["appid"] != "app78" || request["mch_id"] != "mch78" {
					t.Error("wrong original signed request")
					return
				}
				fields := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app78", "mch_id": "mch78", "nonce_str": "original78", "out_trade_no": o.TradeNo, "transaction_id": "UP78_LOCAL"}
				switch r.URL.Path {
				case "/pay/micropay":
					if request["auth_code"] != "120000000000000078" || request["total_fee"] != "123" {
						t.Error("original barcode changed")
					}
					fields["result_code"] = "FAIL"
					fields["err_code"] = "USERPAYING"
					fields["total_fee"] = "123"
				case "/pay/orderquery":
					fields["trade_state"] = "USERPAYING"
					fields["total_fee"] = "123"
					fields["openid"] = "local-buyer78"
				case "/secapi/pay/reverse":
					reversals.Add(1)
					if request["auth_code"] != "" || request["transaction_id"] != "" {
						t.Error("reverse resent barcode or changed original business key")
					}
					var claims, audits int
					if e := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM scan_cancellation_claims),(SELECT count(*) FROM backoffice_audit WHERE resource='scan-cancellations')`).Scan(&claims, &audits); e != nil || claims != 1 || audits != 1 {
						t.Error("reverse I/O preceded its committed barrier", claims, audits, e)
					}
					var unlocked int64
					if e := s.pool.QueryRow(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE NOWAIT", o.ID).Scan(&unlocked); e != nil {
						t.Error("database order lock held during reverse network I/O", e)
					}
					fields["recall"] = "N"
					if mode == "recall" {
						fields["recall"] = "Y"
					}
				default:
					t.Error("unexpected original payment endpoint", r.URL.Path)
					http.Error(w, "unexpected78", 500)
					return
				}
				writeScanWechat78(w, fields, key)
			}))
			up.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
			up.StartTLS()
			defer up.Close()
			transport := up.Client().Transport.(*http.Transport).Clone()
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
			transport.TLSClientConfig.Certificates = []tls.Certificate{scanClientCertificate78(t)}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			p, e := adapter.NewWechatV2(adapter.WechatV2Config{EnableScanCancellation: true, AppID: "app78", MerchantID: "mch78", APIKey: key, Methods: []string{"scan"}, BaseURL: up.URL, HTTPClient: client, RefundClient: client})
			if e != nil {
				t.Fatal(e)
			}
			scope, _ := adapter.TransferAccountScope(p)
			a, owner, e := s.BeginRecoverableScanAttempt(ctx, o.TradeNo, 7, "app78", "120000000000000078", scope)
			if e != nil || !owner {
				t.Fatal(e)
			}
			request := adapter.ScanRequest{TradeNo: o.TradeNo, Subject: o.Name, Amount: 123, ClientIP: "192.0.2.78", AuthCode: "120000000000000078", NotifyURL: "https://local.invalid/callback"}
			_, pending := p.Scan(ctx, request)
			if _, valid := adapter.VerifiedScanPending(pending); !valid {
				t.Fatal("mutual TLS original pending response invalid", pending)
			}
			if e = s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
				t.Fatal(e)
			}
			if e = s.RecordScanPending(ctx, a.ID, "app78", request, pending); e != nil {
				t.Fatal(e)
			}
			job, _ := claimScanFixtureJob78(t, s)
			sixPendingScanQueries78(t, s, a, p, job)
			claim, owner, e := s.ClaimScanCancellation(ctx, job.ID, job.Token, scope)
			if e != nil || !owner {
				t.Fatal(e)
			}
			result, cancelErr := p.CancelScan(ctx, adapter.ScanCancellationRequest{TradeNo: claim.TradeNo, ProviderTradeNo: claim.ProviderTradeNo, Amount: claim.Amount})
			if mode == "known" && cancelErr != nil || mode == "recall" && cancelErr == nil {
				t.Fatal("wrong original reverse finality", cancelErr)
			}
			if e = s.RecordScanCancellation(ctx, a.ID, job.Token, result, cancelErr); e != nil {
				t.Fatal(e)
			}
			if _, owner, e = s.ClaimScanCancellation(ctx, job.ID, job.Token, scope); e != nil || owner || reversals.Load() != 1 {
				t.Fatal("original reverse was reopened", owner, reversals.Load(), e)
			}
			want := ErrScanCancelled
			if mode == "recall" {
				want = ErrScanCancellationPending
			}
			flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}}
			if e = flow.HandleNotification(ctx, 7, o.TradeNo, nil, scanPaidCallback78(o)); !errors.Is(e, want) {
				t.Fatal("late success bypassed actual reverse", e)
			}
			b, e := s.Balance(ctx, 78)
			if e != nil || b.Available != 0 || b.Reserved != 0 {
				t.Fatal("reverse acknowledgement created incoming funds", b, e)
			}
			wantUnknown := int64(0)
			if mode == "recall" {
				wantUnknown = 1
			}
			if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != wantUnknown {
				t.Fatal("reverse finality did not match ledger diagnostics", invalid, unknown)
			}
		})
	}
}
