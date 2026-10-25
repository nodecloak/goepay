package migration

import (
	"encoding/json"
	"strings"
	"testing"
)

const snapshot = `{"schema_version":1,"export_id":"sanitized-fixture-1","source":"epay_pro","exported_at":"2026-10-23T12:00:00+08:00","merchants":[{"id":9001,"name":"Imported demo","available_money":"12.34","reserved_money":"0.50"}],"orders":[{"trade_no":"legacy-t1","merchant_id":9001,"out_trade_no":"legacy-o1","name":"Historical","type":"wxpay","money":"5.00","status":"paid","created_at":"2026-10-20T12:00:00+08:00","paid_at":"2026-10-20T12:01:00+08:00"}],"refunds":[{"merchant_id":9001,"trade_no":"legacy-t1","refund_no":"legacy-r1","money":"0.50","status":"unknown"}],"transfers":[]}`

func TestValidateSnapshotAndRejectAmbiguity(t *testing.T) {
	plan, report, e := Validate([]byte(snapshot))
	if e != nil || len(plan.Merchants) != 1 || report.AvailableTotal != "12.34" || report.ReservedTotal != "0.50" {
		t.Fatalf("valid snapshot %v %v", report, e)
	}
	for _, raw := range []string{strings.Replace(snapshot, `"reserved_money":"0.50"`, `"reserved_money":"0.49"`, 1), strings.Replace(snapshot, `"merchant_id":9001`, `"merchant_id":9999`, 1), strings.Replace(snapshot, `"money":"5.00"`, `"money":"0.001"`, 1), strings.Replace(snapshot, `"export_id":"sanitized-fixture-1"`, `"export_id":"first","export_id":"second"`, 1), strings.Replace(snapshot, `"created_at":"2026-10-20T12:00:00+08:00"`, `"created_at":"0000-00-00 00:00:00"`, 1)} {
		if _, r, e := Validate([]byte(raw)); e == nil || len(r.Issues) == 0 {
			t.Fatal("invalid snapshot accepted")
		}
	}
}
func TestRejectOverRefundAndDuplicateBusinessKey(t *testing.T) {
	var data map[string]any
	json.Unmarshal([]byte(snapshot), &data)
	refund := data["refunds"].([]any)[0].(map[string]any)
	refund["money"] = "6.00"
	b, _ := json.Marshal(data)
	if _, _, e := Validate(b); e == nil {
		t.Fatal("refund above order accepted")
	}
	json.Unmarshal([]byte(snapshot), &data)
	data["orders"] = append(data["orders"].([]any), data["orders"].([]any)[0])
	b, _ = json.Marshal(data)
	if _, _, e := Validate(b); e == nil {
		t.Fatal("duplicate business order accepted")
	}
}
