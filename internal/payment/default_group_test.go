package payment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func defaultGroupFixture154(t *testing.T) (*Store, context.Context) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 15401, Name: "default group", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(id,resource,body) VALUES(0,'groups','{"name":"默认用户组","enabled":true,"rate_bps":100,"info":{}}') ON CONFLICT(id) DO UPDATE SET body=EXCLUDED.body`); e != nil {
		t.Fatal(e)
	}
	return s, ctx
}

func TestOriginalDefaultGroupZeroOrdinaryTransferPermissionIsEffective(t *testing.T) {
	s, ctx := defaultGroupFixture154(t)
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"false"}'); UPDATE backoffice_records SET body=body||'{"user_transfer":"1"}'::jsonb WHERE id=0`); e != nil {
		t.Fatal(e)
	}
	p, a, e := s.ManualTransferPolicy(ctx, 15401)
	if e != nil || !p.Enabled || !a.Enabled || a.GroupID != 0 || a.Source != "group" {
		t.Fatal("default transfer override ignored", p, a, e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false') WHERE id=0`); e != nil {
		t.Fatal(e)
	}
	p, a, e = s.ManualTransferPolicy(ctx, 15401)
	if e != nil || p.Enabled || a.Enabled || a.Source != "group_disabled" {
		t.Fatal("disabled default permitted ordinary transfer", p, a, e)
	}
}

func TestOriginalDefaultGroupZeroSettlementModeAndThresholdRate(t *testing.T) {
	s, ctx := defaultGroupFixture154(t)
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settle_open","value":"3"}'); UPDATE backoffice_records SET body=body||'{"settle_open":"2","settle_rate":"0"}'::jsonb WHERE id=0`); e != nil {
		t.Fatal(e)
	}
	a, e := readSettlementAccess(ctx, s.pool, 15401, false, false)
	if e != nil || a.Mode != "2" || a.AutomaticAllowed || !a.ManualAllowed || a.Source != "group" {
		t.Fatal("default manual-only mode ignored", a, e)
	}
	global := SettlementThresholdPolicy{ThresholdCents: 100, FeeRateBPS: 350, FeeMaximumCents: 200}
	g, e := readSettlementThresholdMerchantPolicy(ctx, s.pool, 15401, global, time.Now())
	if e != nil || !g.GroupDisabled {
		t.Fatal("default manual-only group entered threshold payout", g, e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{settle_open}','"3"') WHERE id=0`); e != nil {
		t.Fatal(e)
	}
	g, e = readSettlementThresholdMerchantPolicy(ctx, s.pool, 15401, global, time.Now())
	if e != nil || g.GroupDisabled || g.Policy.FeeRateBPS != 0 || g.Policy.FeeMaximumCents != 200 || g.FeeOverrideBPS == nil || len(g.GroupBody) == 0 {
		t.Fatal("default threshold explicit zero/signed body ignored", g, e)
	}
}

func TestOriginalDefaultGroupZeroPaymentClosureAndOriginalReplay(t *testing.T) {
	s, ctx := defaultGroupFixture154(t)
	in := CreateOrderInput{MerchantID: 15401, OutTradeNo: "before-default-close", Name: "original", Type: "alipay", Amount: 10000}
	original, e := s.CreateOrder(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info}','{"alipay":{"channel":"0","type":"","rate":"0"}}') WHERE id=0`); e != nil {
		t.Fatal(e)
	}
	in.OutTradeNo = "default-closed-new"
	if _, e = s.CreateOrder(ctx, in); !errors.Is(e, ErrState) {
		t.Fatal("default group channel closure ignored", e)
	}
	in.OutTradeNo = "before-default-close"
	if replay, e := s.CreateOrder(ctx, in); e != nil || replay.ID != original.ID || replay.Amount != original.Amount {
		t.Fatal("default closure rewrote original", replay, e)
	}
}

func TestOriginalDefaultGroupZeroFeeAndAmountAreNewOrderPolicies(t *testing.T) {
	s, ctx := defaultGroupFixture154(t)
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"pay_minmoney":"2.00","pay_maxmoney":"100.00","info":{"alipay":{"rate":"98.50"}}}'::jsonb WHERE id=0`); e != nil {
		t.Fatal(e)
	}
	in := CreateOrderInput{MerchantID: 15401, OutTradeNo: "default-fee", Name: "original", Type: "alipay", Amount: 10000}
	o, e := s.CreateOrder(ctx, in)
	if e != nil || o.PlatformFee != 150 || o.FeeGroupID != 0 {
		t.Fatal("default fee ignored", o, e)
	}
	in.OutTradeNo = "below-default-min"
	in.Amount = 100
	if _, e = s.CreateOrder(ctx, in); !errors.Is(e, ErrInvalid) {
		t.Fatal("default amount boundary ignored", e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info,alipay,rate}','"90"') WHERE id=0`); e != nil {
		t.Fatal(e)
	}
	in.OutTradeNo = "default-fee"
	in.Amount = 10000
	if replay, e := s.CreateOrder(ctx, in); e != nil || replay.ID != o.ID || replay.PlatformFee != 150 {
		t.Fatal("default fee change repriced original", replay, e)
	}
}
