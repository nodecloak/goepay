package payment

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func enableScanRecovery78(t *testing.T, flow *ProviderFlow) {
	t.Helper()
	field := reflect.ValueOf(flow).Elem().FieldByName("ScanRecoveryEnabled")
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.Bool {
		t.Fatal("scan recovery has no independent default-off execution policy")
	}
	field.SetBool(true)
}

func TestScanRecovery78SubmitCreatesOriginalSourceBeforeSingleTLSCharge(t *testing.T) {
	for _, mode := range []string{"pending", "disabled", "unauthenticated", "paid", "legacy-paid"} {
		t.Run(mode, func(t *testing.T) {
			legacy := mode == "disabled" || mode == "legacy-paid"
			paid := mode == "paid" || mode == "legacy-paid"
			s, _, o := recoveryOriginFixture78(t)
			ctx := context.Background()
			const key = "synthetic-flow-scan-key78"
			var scans atomic.Int32
			up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/pay/micropay" {
					t.Error("submit performed an implicit query or cancellation", r.URL.Path)
					return
				}
				scans.Add(1)
				var sourceCount int
				if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM scan_recovery_origins").Scan(&sourceCount); e != nil || !legacy && sourceCount != 1 || legacy && sourceCount != 0 {
					t.Error("recovery authority was not committed before original charge", sourceCount, e)
				}
				var payload struct {
					Entries []struct {
						XMLName xml.Name
						Value   string `xml:",chardata"`
					} `xml:",any"`
				}
				if e := xml.NewDecoder(r.Body).Decode(&payload); e != nil {
					t.Error(e)
					return
				}
				fields := map[string]string{}
				for _, item := range payload.Entries {
					fields[item.XMLName.Local] = item.Value
				}
				if fields["auth_code"] != "120000000000000078" || fields["out_trade_no"] != o.TradeNo || fields["total_fee"] != "123" || !adapter.VerifyWechatV2(fields, key, "MD5", fields["sign"]) {
					t.Error("original signed charge request changed")
				}
				out := map[string]string{"return_code": "SUCCESS", "result_code": "FAIL", "err_code": "USERPAYING", "appid": "app78", "mch_id": "mch78", "nonce_str": "flow78", "out_trade_no": o.TradeNo, "total_fee": "123"}
				if paid {
					out["result_code"] = "SUCCESS"
					delete(out, "err_code")
					out["transaction_id"] = "UP78_LOCAL"
					out["openid"] = "flow-buyer78"
					out["trade_type"] = "MICROPAY"
				}
				if mode == "unauthenticated" {
					out["mch_id"] = "other-mch78"
				}
				writeScanWechat78(w, out, key)
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
			flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://local.invalid"}
			if !legacy {
				enableScanRecovery78(t, &flow)
			}
			current, e := flow.SubmitScan(ctx, o.TradeNo, 7, "192.0.2.78", "120000000000000078")
			if paid {
				if e != nil || current.Status != "paid" {
					t.Fatal("immediate authenticated scan did not settle", current.Status, e)
				}
			} else if e == nil || current.Status != "unknown" {
				t.Fatal("pending charge reported success", current.Status, e)
			}
			var origins, eligible, jobs int
			var persisted string
			if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM scan_recovery_origins),(SELECT count(*) FROM scan_recovery_eligibility),(SELECT count(*) FROM jobs WHERE kind='payment.scan-recover'),(SELECT row_to_json(a)::text FROM provider_attempts a WHERE order_id=$1)`, o.ID).Scan(&origins, &eligible, &jobs, &persisted); e != nil {
				t.Fatal(e)
			}
			wantOrigins, wantPending := 1, 0
			if legacy {
				wantOrigins = 0
			}
			if mode == "pending" {
				wantPending = 1
			}
			if origins != wantOrigins || eligible != wantPending || jobs != wantPending || strings.Contains(persisted, "120000000000000078") {
				t.Fatal("wrong durable recovery or leaked bearer", origins, eligible, jobs)
			}
			flow.SubmitScan(ctx, o.TradeNo, 7, "192.0.2.78", "120000000000000078")
			if scans.Load() != 1 {
				t.Fatal("repeated submit resent original barcode", scans.Load())
			}
			if legacy {
				flow.ScanRecoveryEnabled = true
				current, e = flow.SubmitScan(ctx, o.TradeNo, 7, "192.0.2.78", "120000000000000078")
				if paid && (e != nil || current.Status != "paid") || !paid && !errors.Is(e, ErrScanPending) || scans.Load() != 1 {
					t.Fatal("enabling recovery changed or reauthorized a legacy original scan", e, scans.Load())
				}
				var count int
				if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM scan_recovery_origins").Scan(&count); e != nil || count != 0 {
					t.Fatal("new policy backfilled an old unknown charge", count, e)
				}
			}
			balance, e := s.Balance(ctx, 78)
			wantBalance := int64(0)
			if paid {
				wantBalance = 123
			}
			if e != nil || balance.Available != wantBalance || balance.Reserved != 0 {
				t.Fatal("incorrect scan funds", balance, e)
			}
			if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != 0 {
				t.Fatal("valid original flow has invalid financial source", invalid, unknown)
			}
			if mode == "pending" {
				if _, e = s.pool.Exec(ctx, `ALTER TABLE provider_attempt_accounts DISABLE TRIGGER USER;UPDATE provider_attempt_accounts SET recovery_version=0,recovery_source_hash=NULL`); e != nil {
					t.Fatal(e)
				}
				if _, e = flow.SubmitScan(ctx, o.TradeNo, 7, "192.0.2.78", "120000000000000078"); !errors.Is(e, ErrConflict) || scans.Load() != 1 {
					t.Fatal("V1 downgrade reused legacy replay path", e, scans.Load())
				}
			}
		})
	}
}

func TestScanRecovery78PreflightRejectsMissingRecoveryCapabilitiesWithoutCharge(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	p := &scanFlowProvider{}
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://local.invalid"}
	enableScanRecovery78(t, &flow)
	if e := flow.ValidateScanRequest(7, adapter.ScanRequest{TradeNo: o.TradeNo, Subject: o.Name, Amount: o.Amount, ClientIP: "192.0.2.78", AuthCode: "120000000000000078"}); e == nil {
		t.Fatal("preflight admitted scan without cancellation and observation capability")
	}
	if _, e := flow.SubmitScan(context.Background(), o.TradeNo, 7, "192.0.2.78", "120000000000000078"); e == nil || p.scans.Load() != 0 {
		t.Fatal("unsupported recovery sent charge", e)
	}
	if _, e := s.ProviderAttempt(context.Background(), o.TradeNo); !errors.Is(e, ErrNotFound) {
		t.Fatal("unsupported recovery allocated intent", e)
	}
}
