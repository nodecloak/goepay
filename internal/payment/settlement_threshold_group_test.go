package payment

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestSettlementThreshold74GroupOverrideKeepsExplicitZeroAndSkipsClosedBeforeFee(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 7401, Name: "threshold group local", APIKey: "threshold-group-local-only"}); e != nil {
		t.Fatal(e)
	}
	var gid int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"threshold base","settle_open":0}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, "UPDATE merchants SET base_group_id=$1 WHERE id=7401", gid); e != nil {
		t.Fatal(e)
	}
	p := SettlementThresholdPolicy{ThresholdCents: 100, FeeRateBPS: 350, FeeMaximumCents: 200}
	for _, c := range []struct {
		rate     any
		want     int64
		override bool
	}{{nil, 350, false}, {"", 350, false}, {"0", 0, true}, {json.Number("0.00"), 0, true}, {"2.55", 255, true}} {
		raw, _ := json.Marshal(map[string]any{"name": "threshold base", "settle_open": 0, "settle_rate": c.rate, "settle_fee_max": "999999"})
		if _, e := s.pool.Exec(ctx, "UPDATE backoffice_records SET body=$2 WHERE id=$1", gid, raw); e != nil {
			t.Fatal(e)
		}
		got, e := readSettlementThresholdMerchantPolicy(ctx, s.pool, 7401, p, time.Now())
		if e != nil || got.GroupID != gid || got.GroupDisabled || got.Policy.FeeRateBPS != c.want || got.Policy.FeeMaximumCents != 200 || (got.FeeOverrideBPS != nil) != c.override {
			t.Fatal("group inheritance/zero/global maximum changed", got, e)
		}
	}
	if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body='{"name":"closed","settle_open":2,"settle_rate":"invalid ignored when closed"}' WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	got, e := readSettlementThresholdMerchantPolicy(ctx, s.pool, 7401, p, time.Now())
	if e != nil || !got.GroupDisabled {
		t.Fatal("closed group tried to price or execute", got, e)
	}
}

func TestSettlementThreshold74UsesCurrentMembershipGroupAndRestoresBaselineAfterExpiry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 7402, Name: "threshold membership local", APIKey: "threshold-membership-local-only"}); e != nil {
		t.Fatal(e)
	}
	var base, premium int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"base","settle_rate":"1"}') RETURNING id`).Scan(&base); e != nil {
		t.Fatal(e)
	}
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"premium","enabled":true,"price_cents":"0","duration_days":"30","settle_rate":"2"}') RETURNING id`).Scan(&premium); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, "UPDATE merchants SET base_group_id=$1 WHERE id=7402", base); e != nil {
		t.Fatal(e)
	}
	purchase, e := s.PurchaseMembership(ctx, 7402, premium, "threshold74-original-membership", "merchant:7402")
	if e != nil {
		t.Fatal(e)
	}
	p := SettlementThresholdPolicy{ThresholdCents: 100, FeeRateBPS: 350}
	current, e := readSettlementThresholdMerchantPolicy(ctx, s.pool, 7402, p, time.Now())
	if e != nil || current.GroupID != premium || current.Policy.FeeRateBPS != 200 {
		t.Fatal("active membership ignored for threshold group", current, e)
	}
	expired, e := readSettlementThresholdMerchantPolicy(ctx, s.pool, 7402, p, purchase.ExpiresAt.Add(time.Second))
	if e != nil || expired.GroupID != base || expired.Policy.FeeRateBPS != 100 {
		t.Fatal("expired group remained a future payout authority", expired, e)
	}
}
