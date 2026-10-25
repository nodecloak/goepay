package payment

import (
	"context"
	"fmt"
	"testing"
)

func TestOriginalGroupPaymentLimitsGlobalBoundsAndHistoricalReplay(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 1401, Name: "local140", APIKey: "test-only-limits-key"}); e != nil {
		t.Fatal(e)
	}
	for _, p := range [][2]string{{"pay_minmoney", "1.00"}, {"pay_maxmoney", "2.00"}} {
		if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, p[0], p[1]); e != nil {
			t.Fatal(e)
		}
	}
	for _, n := range []int64{99, 201} {
		if _, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1401, OutTradeNo: fmt.Sprint(n), Amount: n}); e == nil {
			t.Fatalf("out-of-range new order %d accepted", n)
		}
	}
	in := CreateOrderInput{MerchantID: 1401, OutTradeNo: "original", Amount: 100}
	original, e := s.CreateOrder(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1401, OutTradeNo: "upper", Amount: 200}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"1.50"') WHERE resource='settings' AND body->>'key'='pay_minmoney'`); e != nil {
		t.Fatal(e)
	}
	same, e := s.CreateOrder(ctx, in)
	if e != nil || same.TradeNo != original.TradeNo {
		t.Fatalf("changed policy broke historical replay %+v %v", same, e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1401, OutTradeNo: "new-low", Amount: 100}); e == nil {
		t.Fatal("new limit ignored")
	}
	var total int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE merchant_id=1401`).Scan(&total); e != nil || total != 2 {
		t.Fatalf("rejected new orders persisted: %d %v", total, e)
	}
}

func TestOriginalGroupPaymentLimitsOverrideInheritanceAndExpiry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 1402, 0, "platform")
	var base int64
	if e := s.pool.QueryRow(ctx, `SELECT base_group_id FROM merchants WHERE id=1402`).Scan(&base); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"pay_minmoney","value":"1.00"}'),('settings','{"key":"pay_maxmoney","value":"2.00"}'); UPDATE backoffice_records SET body=body||'{"pay_minmoney":"","pay_maxmoney":"0"}'::jsonb WHERE resource='groups'`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1402, OutTradeNo: "inherit-min", Amount: 99}); e == nil {
		t.Fatal("empty group minimum must inherit")
	}
	if _, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1402, OutTradeNo: "zero-max", Amount: 9007199254740993}); e != nil {
		t.Fatalf("explicit zero must disable maximum without floating conversion: %v", e)
	}
	var vip int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"VIP140","enabled":true,"pay_minmoney":"0.01","pay_maxmoney":"0.50"}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(1402,$1,now()+interval '1 day')`, vip); e != nil {
		t.Fatal(e)
	}
	vipOrder, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1402, OutTradeNo: "VIP", Amount: 50})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1402, OutTradeNo: "vip-too-high", Amount: 51}); e == nil {
		t.Fatal("active membership group maximum ignored")
	}
	if _, e = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=now()-interval '1 second' WHERE merchant_id=1402`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1402, OutTradeNo: "expired-low", Amount: 50}); e == nil {
		t.Fatal("expired membership did not restore base minimum")
	}
	same, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1402, OutTradeNo: "VIP", Amount: 50})
	if e != nil || same.TradeNo != vipOrder.TradeNo {
		t.Fatalf("expired membership broke original replay %v", e)
	}
}

func TestOriginalGroupPaymentLimitsRejectMalformedPersistedConfiguration(t *testing.T) {
	for _, bad := range []string{"-1", "1e2", "NaN", "0.001", "92233720368547758.08"} {
		t.Run(bad, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			if e := s.CreateMerchant(ctx, Merchant{ID: 1403, Name: "local140", APIKey: "test-only-limits-key"}); e != nil {
				t.Fatal(e)
			}
			if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','pay_maxmoney','value',$1::text))`, bad); e != nil {
				t.Fatal(e)
			}
			if _, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1403, OutTradeNo: "malformed", Amount: 100}); e == nil {
				t.Fatal("malformed policy was silently ignored")
			}
		})
	}
}
