package server

import (
	"encoding/json"
	"goepay/internal/ordersubject"
	"goepay/internal/payment"
	"strconv"
	"strings"
	"testing"
)

func TestParentChannel180OriginalSubjectAdvancedUsesExistingFirstSendSnapshot(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := t.Context()
	path := "/" + setup.id + "/subject"
	w := parent180HTTP(f, "GET", path, "")
	if w.Code != 200 {
		t.Fatal("missing original ordername advanced fields", w.Code)
	}
	var dto struct {
		Data struct {
			Channel string `json:"channel_id"`
			Name    string `json:"ordername"`
			Rules   string `json:"ordername_rules"`
			FP      string `json:"fingerprint"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &dto) != nil || dto.Data.Channel != setup.id || dto.Data.FP == "" {
		t.Fatal("invalid original subject state")
	}
	body, _ := json.Marshal(map[string]string{"ordername": "原通道-[name]-[outorder]", "ordername_rules": "[{\"name\":\"金额-[name]\",\"min\":\"100.00\",\"max\":\"100.00\"}]", "expected_fingerprint": dto.Data.FP})
	w = parent180HTTP(f, "PATCH", path, string(body))
	if w.Code != 200 {
		t.Fatal("cannot save original title fields", w.Code)
	}
	if json.Unmarshal(w.Body.Bytes(), &dto) != nil || !strings.Contains(dto.Data.Name, "[name]") {
		t.Fatal("saved title absent")
	}
	if w = parent180HTTP(f, "PATCH", path, string(body)); w.Code != 409 {
		t.Fatal("stale title overwritten", w.Code)
	}
	o, e := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "subject-180", Name: "合成测试", Type: "wxpay", Amount: 10000})
	if e != nil {
		t.Fatal(e)
	}
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	resolver := ordersubject.New(f.p)
	title, e := resolver.Resolve(ctx, o, id)
	if e != nil || title != "金额-合成测试" {
		t.Fatal("original amount rule not consumed", title, e)
	}
	body, _ = json.Marshal(map[string]string{"ordername": "新模板", "ordername_rules": "[]", "expected_fingerprint": dto.Data.FP})
	if w = parent180HTTP(f, "PATCH", path, string(body)); w.Code != 200 {
		t.Fatal("cannot update subject", w.Code)
	}
	title, e = resolver.Resolve(ctx, o, id)
	if e != nil || title != "金额-合成测试" {
		t.Fatal("old first-send snapshot rewritten", title, e)
	}
	bad := `{"ordername":"x","ordername_rules":"[{\"name\":\"bad\",\"min\":\"2\",\"max\":\"1\"}]","expected_fingerprint":"` + dto.Data.FP + `"}`
	if w = parent180HTTP(f, "PATCH", path, bad); w.Code != 422 {
		t.Fatal("invalid rule accepted", w.Code)
	}
}

func TestParentChannel180SubjectFinalExpiredSessionRollsBackPolicyAndAudit(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := t.Context()
	path := "/" + setup.id + "/subject"
	w := parent180HTTP(f, "GET", path, "")
	var dto struct{ Data map[string]string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &dto) != nil {
		t.Fatal("original state unavailable", w.Code)
	}
	if _, e := f.p.Exec(ctx, `CREATE FUNCTION subject180_wait() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='order-subject-policies' THEN PERFORM pg_sleep(1.3);END IF;RETURN NEW;END$$;CREATE TRIGGER subject180_wait BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION subject180_wait();UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE username='admin168'`); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]string{"ordername": "must rollback", "ordername_rules": "", "expected_fingerprint": dto.Data["fingerprint"]})
	w = parent180HTTP(f, "PATCH", path, string(body))
	if w.Code != 403 {
		t.Fatal("expired subject write committed", w.Code)
	}
	var n int
	if e := f.p.QueryRow(ctx, `SELECT(SELECT count(*)FROM backoffice_records WHERE resource='order-subject-policies')+(SELECT count(*)FROM backoffice_audit WHERE resource='order-subject-policies')`).Scan(&n); e != nil || n != 0 {
		t.Fatal("expired subject write left state", n, e)
	}
}
