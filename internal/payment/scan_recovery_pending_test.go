package payment

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

type scanPendingRecorder interface {
	RecordScanPending(context.Context, int64, string, adapter.ScanRequest, error) error
}

func pendingReceiptFixture78(t *testing.T, s *Store, o Order) (ProviderAttempt, adapter.ScanRequest, error) {
	t.Helper()
	a, r, pending, _ := pendingReceiptProviderFixture78(t, s, o)
	return a, r, pending
}
func pendingReceiptProviderFixture78(t *testing.T, s *Store, o Order) (ProviderAttempt, adapter.ScanRequest, error, *adapter.WechatV2) {
	t.Helper()
	return pendingReceiptProviderStateFixture78(t, s, o, func() string { return "USERPAYING" })
}
func pendingReceiptProviderStateFixture78(t *testing.T, s *Store, o Order, state func() string) (ProviderAttempt, adapter.ScanRequest, error, *adapter.WechatV2) {
	t.Helper()
	return pendingReceiptProviderCodeStateFixture78(t, s, o, "USERPAYING", state)
}
func pendingReceiptProviderCodeStateFixture78(t *testing.T, s *Store, o Order, code string, state func() string) (ProviderAttempt, adapter.ScanRequest, error, *adapter.WechatV2) {
	t.Helper()
	const key = "synthetic-pending-source-api-key78"
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pay/micropay" && r.URL.Path != "/pay/orderquery" {
			t.Error("unexpected upstream path")
			return
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "FAIL", "err_code": code, "appid": "app78", "mch_id": "mch78", "nonce_str": "pending78", "out_trade_no": o.TradeNo, "total_fee": "123"}
		if r.URL.Path == "/pay/orderquery" {
			out["result_code"] = "SUCCESS"
			delete(out, "err_code")
			out["trade_state"] = state()
			out["transaction_id"] = "UP78_LOCAL"
			out["openid"] = "local-buyer78"
		}
		out["sign"], _ = adapter.SignWechatV2(out, key, "MD5")
		var body bytes.Buffer
		body.WriteString("<xml>")
		for field, value := range out {
			fmt.Fprintf(&body, "<%s>", field)
			xml.EscapeText(&body, []byte(value))
			fmt.Fprintf(&body, "</%s>", field)
		}
		body.WriteString("</xml>")
		w.Write(body.Bytes())
	}))
	t.Cleanup(up.Close)
	p, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "app78", MerchantID: "mch78", APIKey: key, Methods: []string{"scan"}, BaseURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	scope, e := adapter.TransferAccountScope(p)
	if e != nil {
		t.Fatal(e)
	}
	a, owner, e := s.BeginRecoverableScanAttempt(context.Background(), o.TradeNo, 7, "app78", "120000000000000078", scope)
	if e != nil || !owner {
		t.Fatal(e)
	}
	request := adapter.ScanRequest{TradeNo: o.TradeNo, Subject: o.Name, Amount: o.Amount, ClientIP: "192.0.2.78", AuthCode: "120000000000000078", NotifyURL: "https://local.invalid/callback"}
	_, pending := p.Scan(context.Background(), request)
	if _, valid := adapter.VerifiedScanPending(pending); !valid {
		t.Fatal("fixture lacks actual signed TLS pending response", pending)
	}
	return a, request, pending, p
}

func TestScanRecovery78PendingEligibilityBindsOriginalRequestAndCommitsOnlyOnce(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	recorder, ok := any(s).(scanPendingRecorder)
	if !ok {
		t.Fatal("authenticated pending response has no durable original eligibility")
	}
	a, request, pending := pendingReceiptFixture78(t, s, o)
	if e := recorder.RecordScanPending(ctx, a.ID, "app78", request, &adapter.ProviderError{Code: "USERPAYING"}); e == nil {
		t.Fatal("bare text granted eligibility")
	}
	for _, changed := range []struct {
		name   string
		change func(*adapter.ScanRequest)
	}{
		{"barcode", func(r *adapter.ScanRequest) { r.AuthCode = "120000000000000079" }},
		{"order", func(r *adapter.ScanRequest) { r.TradeNo = "OTHER78" }},
		{"amount", func(r *adapter.ScanRequest) { r.Amount++ }},
		{"notify", func(r *adapter.ScanRequest) { r.NotifyURL = "https://other.invalid/callback" }},
		{"subject", func(r *adapter.ScanRequest) { r.Subject = "changed78" }},
		{"ip", func(r *adapter.ScanRequest) { r.ClientIP = "192.0.2.79" }},
	} {
		r := request
		changed.change(&r)
		if e := recorder.RecordScanPending(ctx, a.ID, "app78", r, pending); e == nil {
			t.Fatal("pending response detached from original", changed.name)
		}
	}
	if e := recorder.RecordScanPending(ctx, a.ID, "changed-app78", request, pending); !errors.Is(e, ErrConflict) {
		t.Fatal("different application reused original eligibility", e)
	}
	for i := 0; i < 3; i++ {
		if e := recorder.RecordScanPending(ctx, a.ID, "app78", request, pending); e != nil {
			t.Fatal("original response not idempotent", e)
		}
	}
	var eligibility, audits, jobs int
	var raw string
	e := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM scan_recovery_eligibility),(SELECT count(*) FROM backoffice_audit WHERE resource='scan-recovery-eligibility'),(SELECT count(*) FROM jobs WHERE kind='payment.scan-recover'),(SELECT row_to_json(e)::text FROM scan_recovery_eligibility e WHERE attempt_id=$1)`, a.ID).Scan(&eligibility, &audits, &jobs, &raw)
	if e != nil || eligibility != 1 || audits != 1 || jobs != 1 || strings.Contains(raw, request.AuthCode) || strings.Contains(raw, "mch78") || strings.Contains(raw, "spbill_create_ip") {
		t.Fatal("eligibility missing, duplicate, or secrets persisted", eligibility, audits, jobs, e)
	}
	b, e := s.Balance(ctx, 78)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatal("pending response credited funds", b, e)
	}
}

func TestScanRecovery78EligibilityWriteFailureDoesNotConsumePendingResponse(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	recorder, ok := any(s).(scanPendingRecorder)
	if !ok {
		t.Fatal("durable pending eligibility missing")
	}
	a, request, pending := pendingReceiptFixture78(t, s, o)
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION reject_scan_eligibility78() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='scan-recovery-eligibility' THEN RAISE EXCEPTION 'synthetic audit failure78';END IF;RETURN NEW;END $$;CREATE TRIGGER reject_scan_eligibility78 BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_scan_eligibility78()`); e != nil {
		t.Fatal(e)
	}
	if e := recorder.RecordScanPending(ctx, a.ID, "app78", request, pending); e == nil {
		t.Fatal("audit failure accepted pending response")
	}
	var count int
	if e := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM scan_recovery_eligibility)+(SELECT count(*) FROM jobs WHERE kind='payment.scan-recover')`).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial eligibility survived rollback", count, e)
	}
	if _, e := s.pool.Exec(ctx, "DROP TRIGGER reject_scan_eligibility78 ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if e := recorder.RecordScanPending(ctx, a.ID, "app78", request, pending); e != nil {
		t.Fatal("audit rollback consumed original eligibility", e)
	}
}

func TestScanRecovery78EligibilityCorruptionNeverReopensTheOriginalJob(t *testing.T) {
	for _, mode := range []string{"receipt", "source", "eligibility-deleted", "audit-deleted", "job-deleted", "job-payload"} {
		t.Run(mode, func(t *testing.T) {
			s, _, o := recoveryOriginFixture78(t)
			ctx := context.Background()
			recorder, ok := any(s).(scanPendingRecorder)
			if !ok {
				t.Fatal("durable pending eligibility missing")
			}
			a, request, pending := pendingReceiptFixture78(t, s, o)
			if e := recorder.RecordScanPending(ctx, a.ID, "app78", request, pending); e != nil {
				t.Fatal(e)
			}
			if _, e := s.pool.Exec(ctx, `ALTER TABLE scan_recovery_eligibility DISABLE TRIGGER USER;ALTER TABLE backoffice_audit DISABLE TRIGGER USER`); e != nil {
				t.Fatal(e)
			}
			if mode == "eligibility-deleted" {
				_, blocked := s.pool.Exec(ctx, "DELETE FROM scan_recovery_eligibility WHERE attempt_id=$1", a.ID)
				var foreignKey *pgconn.PgError
				if !errors.As(blocked, &foreignKey) || foreignKey.Code != "23503" {
					t.Fatal("query head did not protect its original eligibility", blocked)
				}
				// Only this isolated corruption fixture removes the independent
				// head first. Production FK and immutability rules remain intact.
				if _, e := s.pool.Exec(ctx, "DELETE FROM scan_recovery_query_heads WHERE attempt_id=$1", a.ID); e != nil {
					t.Fatal(e)
				}
			}
			query := map[string]string{
				"receipt":             `UPDATE scan_recovery_eligibility SET receipt=jsonb_set(receipt,'{amount}','"124"') WHERE attempt_id=$1`,
				"source":              `UPDATE scan_recovery_eligibility SET source_hash=decode(repeat('00',32),'hex') WHERE attempt_id=$1`,
				"eligibility-deleted": `DELETE FROM scan_recovery_eligibility WHERE attempt_id=$1`,
				"audit-deleted":       `DELETE FROM backoffice_audit WHERE resource='scan-recovery-eligibility' AND resource_id=$1`,
				"job-deleted":         `DELETE FROM jobs WHERE event_key='scan-recover:'||$1::bigint::text`,
				"job-payload":         `UPDATE jobs SET payload='{"attempt_id":"different78"}' WHERE event_key='scan-recover:'||$1::bigint::text`,
			}[mode]
			if _, e := s.pool.Exec(ctx, query, a.ID); e != nil {
				t.Fatal(e)
			}
			if e := recorder.RecordScanPending(ctx, a.ID, "app78", request, pending); !errors.Is(e, ErrConflict) {
				t.Fatal("corrupt eligibility reused original response", mode, e)
			}
		})
	}
}
