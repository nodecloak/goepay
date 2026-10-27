package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/payment"
	"strings"
	"testing"
	"time"
)

func TestOriginalGroupSettlementFeeFundsConfirmationMatchesMerchantQuote(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	if _, e := p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settlement_fee_rate_bps","value":"100"}'); UPDATE backoffice_records SET body=jsonb_set(body,'{settle_rate}','"2.55"') WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	body := json.RawMessage(`{"merchant_id":"1","amount":"10.00","external_no":"group-fee-confirm155"}`)
	target := "/console/v1/admin/workflows/settlements"
	a, summary, e := s.ResolveFundsAction(ctx, p, target, body)
	if e != nil || summary["fee_amount"] != "0.26" || summary["net_amount"] != "9.74" {
		t.Fatal("financial confirmation presented another price than original group", summary, e)
	}
	if _, e = p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{settle_rate}','"0"') WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	b, summary, e := s.ResolveFundsAction(ctx, p, target, body)
	if e != nil || summary["fee_amount"] != "0.00" || summary["net_amount"] != "10.00" || bytes.Equal(a.Digest(), b.Digest()) {
		t.Fatal("explicit zero reused an old fee confirmation", summary, e)
	}
	var journals int
	if e = p.QueryRow(ctx, `SELECT count(*) FROM journals`).Scan(&journals); e != nil || journals != 0 {
		t.Fatal("quotation reserved money", journals, e)
	}
}

func TestOriginalGroupSettlementFeePlanConfirmationShowsAuthorizedMerchantRates(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	zero := int64(0)
	if _, e := p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settlement_fee_rate_bps","value":"100"}'); UPDATE backoffice_records SET body=jsonb_set(body,'{settle_rate}','"2.55"') WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	cfg, e := s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "alipay", Account: "local-plan155@example.invalid", Name: "Local Plan"}, &zero, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "local"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.SetSettlementSchedulePreference(ctx, 1, 0, true, cfg.ConfigID, payment.SettlementPlanActor{Role: "merchant", ID: 1, Name: "local"}); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e = p.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	plan, e := s.Store.CreateSettlementPlan(ctx, payment.SettlementPlanInput{Name: "original fee plan155", Mode: "generate_only", Schedule: payment.SettlementScheduleSpec{Frequency: "hour", Interval: 1, AnchorAt: now.Add(-time.Hour).Truncate(time.Second)}, MerchantIDs: []int64{1}}, payment.SettlementPlanActor{Role: "admin", Class: "super", ID: 1, Name: "local"})
	if e != nil {
		t.Fatal(e)
	}
	body, e := json.Marshal(map[string]string{"expected_revision": "1", "expected_authorization_id": "0", "valid_from": now.Add(-time.Minute).Format(time.RFC3339), "expires_at": now.Add(time.Hour).Format(time.RFC3339), "max_item_gross": "1000", "max_run_gross": "1000", "max_day_gross": "1000", "max_day_items": "1"})
	if e != nil {
		t.Fatal(e)
	}
	_, summary, e := s.ResolveFundsAction(ctx, p, fmt.Sprintf("/console/v1/admin/settlement-plans/%d/authorizations", plan.ID), body)
	if e != nil || summary["fee_rate_bps"] != "255" || !strings.Contains(summary["group_fee_rules"], "商户1：2.55%") {
		t.Fatal("plan confirmation hid original group rate", summary, e)
	}
}
