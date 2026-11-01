package server

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"goepay/internal/payment"
)

func TestGroupSettlementPolicyDigestBindsCycleAndMinimum(t *testing.T) {
	for _, field := range []string{"settle_type", "settle_minmoney"} {
		t.Run(field, func(t *testing.T) {
			f := channel168Local(t)
			ctx := t.Context()
			if err := f.s.Store.CreateMerchant(ctx, payment.Merchant{ID: 2, Name: "synthetic recipient", APIKey: "test-only"}); err != nil {
				t.Fatal(err)
			}
			target := "/console/v1/admin/workflows/internal-transfers"
			body := json.RawMessage(`{"merchant_id":"1","recipient_id":"2","amount":"1.00","external_no":"group-cycle-digest"}`)
			before, _, err := f.s.ResolveFundsAction(ctx, f.p, target, body)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,ARRAY[$1]::text[],to_jsonb($2::text)) WHERE resource='groups' AND id=0`, field, "1"); err != nil {
				t.Fatal(err)
			}
			after, _, err := f.s.ResolveFundsAction(ctx, f.p, target, body)
			if err != nil || before == after {
				t.Fatal("group settlement policy change left confirmation digest unchanged", field, err)
			}
		})
	}
}

func TestGroupSettlementPolicyDigestBindsBatchFeature(t *testing.T) {
	for _, source := range []string{"groups", "settings"} {
		t.Run(source, func(t *testing.T) {
			f := channel168Local(t)
			ctx := t.Context()
			if err := f.s.Store.CreateMerchant(ctx, payment.Merchant{ID: 2, Name: "synthetic recipient", APIKey: "test-only"}); err != nil {
				t.Fatal(err)
			}
			target := "/console/v1/admin/workflows/internal-transfers"
			body := json.RawMessage(`{"merchant_id":"1","recipient_id":"2","amount":"1.00","external_no":"group-batch-digest"}`)
			before, _, err := f.s.ResolveFundsAction(ctx, f.p, target, body)
			if err != nil {
				t.Fatal(err)
			}
			if source == "groups" {
				_, err = f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"user_transfer_batch":"0"}'::jsonb WHERE resource='groups' AND id=0`)
			} else {
				_, err = f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"user_transfer_batch","value":"0"}')`)
			}
			if err != nil {
				t.Fatal(err)
			}
			after, _, err := f.s.ResolveFundsAction(ctx, f.p, target, body)
			if err != nil || before == after {
				t.Fatal("batch feature change left confirmation digest unchanged", source, err)
			}
		})
	}
}

func TestGroupSettlementPolicyDigestBindsDepositFields(t *testing.T) {
	testGroupSettlementPolicyDigestFields(t, []string{"user_deposit", "user_deposit_min", "user_deposit_day"})
}

func TestGroupSettlementPolicyDigestBindsRecipientTypes(t *testing.T) {
	testGroupSettlementPolicyDigestFields(t, []string{"settle_alipay", "settle_wxpay", "settle_bank"})
}

func TestGroupSettlementPolicyDigestBindsRedpackFeature(t *testing.T) {
	testGroupSettlementPolicyDigestFields(t, []string{"user_transfer_red"})
}

func TestGroupTransferFeeFundsSummaryAndNoOverrideCompatibility(t *testing.T) {
	f, provider, _ := redpackFixture(t)
	ctx := t.Context()
	target := "/console/v1/admin/transfers"
	body := json.RawMessage(`{"merchant_id":"1","channel_id":"7","type":"alipay","account":"synthetic@example.test","name":"Synthetic","amount":"1.01","external_no":"group-fee-summary"}`)
	legacy, summary, err := f.s.ResolveFundsAction(ctx, f.p, target, body)
	if err != nil || summary["service_fee"] != "0.00" {
		t.Fatal("legacy fee summary", summary, err)
	}
	for _, value := range []string{`null`, `""`, `"_"`} {
		if _, err = f.p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{transfer_rate}',$1::jsonb)WHERE resource='groups' AND id=0`, value); err != nil {
			t.Fatal(err)
		}
		current, _, err := f.s.ResolveFundsAction(ctx, f.p, target, body)
		if err != nil || current != legacy {
			t.Fatal("non-override transfer fee changed original authorization bytes", value, err)
		}
	}
	if _, err = f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"transfer_rate":"2.50"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	current, summary, err := f.s.ResolveFundsAction(ctx, f.p, target, body)
	if err != nil || current == legacy || summary["service_fee"] != "0.03" || summary["balance_debit"] != "1.04" {
		t.Error("confirmation did not use effective group fee", summary, err)
	}
	batch := json.RawMessage(`{"merchant_id":"1","external_no":"group-fee-batch-summary","items":[{"channel_id":"7","type":"alipay","account":"synthetic-1@example.test","name":"Synthetic","amount":"0.20"},{"channel_id":"7","type":"alipay","account":"synthetic-2@example.test","name":"Synthetic","amount":"0.20"}]}`)
	_, summary, err = f.s.ResolveFundsAction(ctx, f.p, "/console/v1/admin/transfer-batches", batch)
	if err != nil || summary["service_fee"] != "0.02" || summary["balance_debit"] != "0.42" {
		t.Error("batch confirmation lost per-item original rounding", summary, err)
	}
	if _, err = f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"transfer_rate":"0","settle_rate":"0.00"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	inherit, summary, err := f.s.ResolveFundsAction(ctx, f.p, target, body)
	if err != nil || summary["service_fee"] != "0.00" {
		t.Fatal("fallback zero fee summary", summary, err)
	}
	if _, err = f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"transfer_rate":"0.00"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	free, summary, err := f.s.ResolveFundsAction(ctx, f.p, target, body)
	if err != nil || free == inherit || summary["service_fee"] != "0.00" {
		t.Fatal("zero spellings lost their distinct bound policy source", summary, err)
	}
	if provider.sent.Load() != 0 {
		t.Fatal("fee confirmation sent an upstream transfer")
	}
}

func testGroupSettlementPolicyDigestFields(t *testing.T, fields []string) {
	t.Helper()
	for _, source := range []string{"groups", "settings"} {
		t.Run(source, func(t *testing.T) {
			f := channel168Local(t)
			ctx := t.Context()
			if err := f.s.Store.CreateMerchant(ctx, payment.Merchant{ID: 2, Name: "synthetic recipient", APIKey: "test-only"}); err != nil {
				t.Fatal(err)
			}
			for _, field := range fields {
				t.Run(field, func(t *testing.T) {
					target := "/console/v1/admin/workflows/internal-transfers"
					body := json.RawMessage(`{"merchant_id":"1","recipient_id":"2","amount":"1.00","external_no":"group-deposit-digest"}`)
					before, _, err := f.s.ResolveFundsAction(ctx, f.p, target, body)
					if err != nil {
						t.Fatal(err)
					}
					if source == "groups" {
						_, err = f.p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,ARRAY[$1]::text[],to_jsonb($2::text)) WHERE resource='groups' AND id=0`, field, "1")
					} else {
						_, err = f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value','1'))`, field)
					}
					if err != nil {
						t.Fatal(err)
					}
					after, _, err := f.s.ResolveFundsAction(ctx, f.p, target, body)
					if err != nil || before == after {
						t.Fatal("financial group policy change left confirmation digest unchanged", source, field, err)
					}
				})
			}
		})
	}
}

func TestGroupSettlementPlanSummaryUsesEachMerchantsBoundCycleAndMinimum(t *testing.T) {
	f := channel168Local(t)
	ctx := t.Context()
	if err := f.s.Store.CreateMerchant(ctx, payment.Merchant{ID: 2, Name: "synthetic second merchant", APIKey: "test-only"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"settle_type":"1","settle_minmoney":"1.23"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	var second int64
	if err := f.p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"second group","enabled":true,"settle_type":"0","settle_minmoney":"4.56"}') RETURNING id`).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=2`, second); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='settlement_minimum_cents'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settlement_minimum_cents","value":"999"}')`); err != nil {
		t.Fatal(err)
	}
	for _, mid := range []int64{1, 2} {
		zero := int64(0)
		cfg, err := f.s.Store.SetSettlementRecipient(ctx, mid, payment.SettlementRecipientInput{Type: "alipay", Account: "local-payee@example.test", Name: "Synthetic Payee"}, &zero, payment.SettlementRecipientActor{Role: "merchant", ID: mid, Name: "synthetic-owner"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.s.Store.SetSettlementSchedulePreference(ctx, mid, 0, true, cfg.ConfigID, payment.SettlementPlanActor{Role: "merchant", ID: mid, Name: "synthetic-owner"}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	plan, err := f.s.Store.CreateSettlementPlan(ctx, payment.SettlementPlanInput{Name: "synthetic group summary", Mode: "generate_only", MerchantIDs: []int64{1, 2}, Schedule: payment.SettlementScheduleSpec{Frequency: "hour", Interval: 1, AnchorAt: now}}, payment.SettlementPlanActor{Role: "admin", Class: "super", ID: 168, Name: "admin168"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"expected_revision": fmt.Sprint(plan.Revision), "expected_authorization_id": "0", "valid_from": now.Add(-time.Minute).Format(time.RFC3339), "expires_at": now.Add(time.Hour).Format(time.RFC3339), "max_item_gross": "1000", "max_run_gross": "2000", "max_day_gross": "2000", "max_day_items": "2"})
	definition, ok := settlementPlanFundsTarget([]string{"settlement-plans", fmt.Sprint(plan.ID), "authorizations"})
	if !ok {
		t.Fatal("plan target missing")
	}
	fields, _, err := normalizeSettlementPlanFundsBody(raw, definition)
	if err != nil {
		t.Fatal(err)
	}
	summary := map[string]string{}
	if _, err = f.s.fundsSettlementPlan(ctx, f.p, definition, fields, summary); err != nil {
		t.Fatal(err)
	}
	if summary["group_maturity_rules"] != "商户1：D+1; 商户2：D+0" || summary["maturity"] != "逐商户，见结算周期规则" {
		t.Fatal("summary borrowed global cycle", summary)
	}
	if summary["group_minimum_rules"] != "商户1：1.23元; 商户2：4.56元" || summary["minimum_money"] != "逐商户，见最低金额规则" {
		t.Fatal("summary borrowed global minimum", summary)
	}
	if _, err = f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"settle_type":"1","settle_minmoney":"1.23"}'::jsonb WHERE resource='groups' AND id=$1`, second); err != nil {
		t.Fatal(err)
	}
	common := map[string]string{}
	if _, err = f.s.fundsSettlementPlan(ctx, f.p, definition, fields, common); err != nil || common["maturity"] != "D+1（UTC+8 00:00）" || common["minimum_money"] != "1.23" {
		t.Fatal("common group policy still displayed unrelated global values", common, err)
	}
	var created int
	if err := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM settlement_plan_authorizations)+(SELECT count(*) FROM settlement_auto_authorizations)`).Scan(&created); err != nil || created != 0 {
		t.Fatal("summary created future financial authority", created, err)
	}
}
