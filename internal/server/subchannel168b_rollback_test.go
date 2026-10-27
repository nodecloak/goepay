package server

import (
	"context"
	"encoding/json"
	"goepay/internal/compat"
	"testing"
)

func TestSubchannel168BLegalOldChildHeadCannotReactivateOrSelectOldAccount(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	row := child168BCreate(t, f, "rollback-original", true)
	if out := f.pay(t, "rollback-original-order", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `CREATE TABLE subchannel168b_old_head AS SELECT * FROM payment_subchannels`); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": row["fingerprint"], "enabled": false, "values": map[string]any{"app": map[string]string{"operation": "replace", "value": "rollback-replacement"}}})
	w := f.childHTTP("PATCH", "/"+row["id"].(string), string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e := f.p.Exec(ctx, `UPDATE payment_subchannels c SET (parent_id,pay_type,name,enabled,archived,weight,min_amount,max_amount,revision,account_version,proof_hash,created_at,updated_at)=(SELECT parent_id,pay_type,name,enabled,archived,weight,min_amount,max_amount,revision,account_version,proof_hash,created_at,updated_at FROM subchannel168b_old_head p WHERE p.id=c.id)`); e != nil {
		t.Fatal(e)
	}
	w = f.childHTTP("GET", "/"+row["id"].(string), "")
	if w.Code != 409 {
		t.Error("valid old MAC revived stale child head", w.Code, w.Body.String())
	}
	if out := f.pay(t, "rollback-new-order", "1.00", 7); out["code"] == float64(1) {
		t.Error("stale enabled account routed", out)
	}
	o, e := f.s.Store.Order(ctx, 1, "rollback-original-order")
	if e != nil {
		t.Fatal(e)
	}
	original, _, e := f.s.ProviderFlow.ForPaymentIntent(ctx, 1, o.OutTradeNo, 7)
	if e != nil || original.ValidatePaymentAccount(ctx, 1, o.TradeNo) != nil {
		t.Fatal("old immutable original source was mistaken for a mutable head", e)
	}
	if factory.providers["rollback-original"].sends.Load() != 1 {
		t.Fatal("rollback resent old payment")
	}
}
func TestSubchannel168BLegalOldParentPolicyCannotReactivateOrReprice(t *testing.T) {
	f := channel168Local(t)
	first := f.configure(t, 7, "99.00")
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `CREATE TABLE subchannel168b_old_parent AS SELECT * FROM runtime_channel_policies WHERE channel_id=7`); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"rate": "98.00", "mode": 0, "enabled": false, "expected_fingerprint": first["fingerprint"]})
	if w := f.admin("PATCH", "/7", string(body), f.csrf); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e := f.p.Exec(ctx, `UPDATE runtime_channel_policies c SET (pay_type,identity_hash,retained_bps,mode,enabled,revision,proof_hash)=(SELECT pay_type,identity_hash,retained_bps,mode,enabled,revision,proof_hash FROM subchannel168b_old_parent p WHERE p.channel_id=c.channel_id) WHERE channel_id=7`); e != nil {
		t.Fatal(e)
	}
	w := f.admin("GET", "/7", "", f.csrf)
	if w.Code != 409 {
		t.Error("valid old MAC revived parent fee/enable policy", w.Code, w.Body.String())
	}
	if _, _, e := f.s.providerRoutedRequest(ctx, 1, compat.Params{"type": "alipay", "method": "native", "channel_id": "7", "money": "1.00", "name": "Synthetic", "clientip": "192.0.2.1", "out_trade_no": "rollback-parent-new"}); e == nil {
		t.Error("old parent enabled policy admitted new route")
	}
}
func TestSubchannel168BLegalOldMerchantRollPolicyCannotRestoreOldSelection(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	child168BCreate(t, f, "roll-rollback-child", true)
	w := f.childHTTP("GET", "/policy?merchant_id=1", "")
	var row struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &row)
	write := func(roll int) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"roll_type": roll, "expected_fingerprint": row.Data["fingerprint"]})
		w = f.childHTTP("PATCH", "/policy?merchant_id=1", string(b))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		json.Unmarshal(w.Body.Bytes(), &row)
	}
	write(1)
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `CREATE TABLE subchannel168b_old_roll AS SELECT * FROM payment_subchannel_merchant_policy`); e != nil {
		t.Fatal(e)
	}
	write(3)
	if _, e := f.p.Exec(ctx, `UPDATE payment_subchannel_merchant_policy c SET (roll_type,revision,proof_hash)=(SELECT roll_type,revision,proof_hash FROM subchannel168b_old_roll p WHERE p.merchant_id=c.merchant_id)`); e != nil {
		t.Fatal(e)
	}
	w = f.childHTTP("GET", "/policy?merchant_id=1", "")
	if w.Code != 409 {
		t.Error("valid old roll head accepted", w.Code, w.Body.String())
	}
	if out := f.pay(t, "roll-rollback-new", "1.00", 7); out["code"] == float64(1) {
		t.Error("old roll policy admitted route", out)
	}
}
func TestSubchannel168BRevisionAnchorsAllowSameValueSavesAndUnrelatedAudit(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	row := child168BCreate(t, f, "same-value-child", true)
	for i := 0; i < 2; i++ {
		b, _ := json.Marshal(map[string]any{"expected_fingerprint": row["fingerprint"], "weight": 1})
		w := f.childHTTP("PATCH", "/"+row["id"].(string), string(b))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var data struct{ Data map[string]any }
		json.Unmarshal(w.Body.Bytes(), &data)
		row = data.Data
	}
	f.configure(t, 7, "99.00")
	f.configure(t, 7, "99.00")
	if _, e := f.p.Exec(context.Background(), `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',168,'admin168','unrelated-settings',7,'update','{}')`); e != nil {
		t.Fatal(e)
	}
	if w := f.childHTTP("GET", "/"+row["id"].(string), ""); w.Code != 200 {
		t.Fatal("legitimate save blocked", w.Code)
	}
	if out := f.pay(t, "same-value-policy-accepted", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("same-value or unrelated audit changed authority", out)
	}
}
