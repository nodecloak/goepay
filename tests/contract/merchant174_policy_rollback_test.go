package contract_test

import (
	"context"
	"encoding/json"
	"testing"

	"goepay/internal/compat"
)

func TestMerchant174PriorAuthenticatedPolicyRowCannotRollbackCommittedClose(t *testing.T) {
	f, p, o := merchant174Runtime(t)
	ctx := context.Background()
	merchant174Set(t, f, 1, 1, 1)
	var revision int64
	var proof []byte
	if e := f.p.QueryRow(ctx, `SELECT revision,proof_hash FROM merchant_api_switches WHERE merchant_id=2`).Scan(&revision, &proof); e != nil {
		t.Fatal(e)
	}
	merchant174Set(t, f, 0, 0, 0)
	// Ordinary UPDATE restores an old complete, authentic row. It does not alter
	// merchant revision, any audit, proof bytes, table or migration registration.
	if _, e := f.p.Exec(ctx, `UPDATE merchant_api_switches SET keylogin=1,refund=1,transfer=1,revision=$1,proof_hash=$2 WHERE merchant_id=2`, revision, proof); e != nil {
		t.Fatal(e)
	}
	if w := f.request("GET", "api-switches", "", f.csrf); w.Code != 409 {
		t.Errorf("old authentic row was presented as current: GET=%d", w.Code)
	}
	if out := merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "rollback-refund174", "money": "1.00"}); out["code"] != float64(-1) {
		t.Errorf("old authentic policy reopened signed refund: code=%v", out["code"])
	}
	if out := merchant174Invoke(t, f, "/api/transfer/submit", compat.Params{"type": "alipay", "account": "synthetic@example.invalid", "money": "1.00", "out_biz_no": "rollback-transfer174", "channel_id": "7"}); out["code"] != float64(-1) {
		t.Errorf("old authentic policy reopened signed transfer: code=%v", out["code"])
	}
	if w := merchant174KeyLogin(f, "2", "contract-rsa-secret", ""); w.Code == 200 || len(w.Result().Cookies()) != 0 {
		t.Errorf("old authentic policy reopened key login: status=%d", w.Code)
	}
	var intents, audits int
	if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM refunds)+(SELECT count(*) FROM transfers),(SELECT count(*) FROM backoffice_audit WHERE resource='merchant-api-switches' AND resource_id=2 AND operation='merchant-api-switches-update')`).Scan(&intents, &audits); e != nil {
		t.Fatal(e)
	}
	if intents != 0 || p.refundWrites.Load() != 0 || p.transferWrites.Load() != 0 || audits != 2 {
		t.Errorf("rollback admitted funds or altered history: intents=%d refundsend=%d transfersend=%d audits=%d", intents, p.refundWrites.Load(), p.transferWrites.Load(), audits)
	}
}

func TestMerchant174SameValuePolicySavesAndUnrelatedPersonalRevisionRemainValid(t *testing.T) {
	f, p, o := merchant174Runtime(t)
	one := merchant174Set(t, f, 1, 1, 1)
	two := merchant174Set(t, f, 1, 1, 1)
	if one["revision"] != "1" || two["revision"] != "2" || one["fingerprint"] == two["fingerprint"] {
		t.Fatal("same-value save must create a new committed policy revision")
	}
	w := f.request("GET", "personal-settings", "", f.csrf)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	personal := credentialData162(t, w)
	body, _ := json.Marshal(map[string]string{"qq": "1234567", "pay_minmoney": "", "pay_maxmoney": "", "expected_fingerprint": personal["fingerprint"].(string)})
	if w = f.request("PATCH", "personal-settings", string(body), f.csrf); w.Code != 200 {
		t.Fatal("unrelated genuine personal update", w.Code)
	}
	after := apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
	if after["revision"] != "2" || after["fingerprint"] == two["fingerprint"] {
		t.Fatal("unrelated config_revision invalidated policy origin instead of only CAS")
	}
	if out := merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "same-value-valid174", "money": "1.00"}); out["code"] != float64(0) || p.refundWrites.Load() != 1 {
		t.Fatal("valid unchanged policy blocked original refund consumer", out["code"], p.refundWrites.Load())
	}
	if w = merchant174KeyLogin(f, "2", "contract-rsa-secret", ""); w.Code != 200 {
		t.Fatal("valid unchanged policy blocked key login", w.Code)
	}
}
