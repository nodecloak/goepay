package server

import (
	"context"
	"encoding/json"
	"goepay/internal/compat"
	"testing"
)

func randomRollParams161(no string) compat.Params {
	return compat.Params{"type": "alipay", "method": "native", "name": "local random group", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": no}
}

func TestOriginalRandomRollChoosesOnlyGlobalMatchingGroupAndKeepsOriginalReplay(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx := context.Background()
	add := func(mid, kind, channels string) int64 {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"name": "local roll", "merchant_id": mid, "type": kind, "strategy": "first", "channel_ids": channels, "enabled": true, "default_route": false})
		var id int64
		if e := pool.QueryRow(ctx, "INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls',$1::jsonb) RETURNING id", string(body)).Scan(&id); e != nil {
			t.Fatal(e)
		}
		return id
	}
	add("2", "alipay", "8:1")
	add("0", "wxpay", "8:1")
	original := add("0", "alipay", "7:1")
	var gid int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"original random roll","enabled":true,"info":{"alipay":{"channel":"-3","type":"roll"}}}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, "UPDATE merchants SET base_group_id=$1 WHERE id=1", gid); e != nil {
		t.Fatal(e)
	}
	p := randomRollParams161("original-random161")
	got, method, e := s.providerRoutedRequest(ctx, 1, p)
	if e != nil || got != 7 || method != "native" {
		t.Fatal("random group escaped owner/type scope or failed original route", got, method, e)
	}
	add("0", "alipay", "8:1")
	for i := 0; i < 4; i++ {
		again, _, e := s.providerRoutedRequest(ctx, 1, p)
		if e != nil || again != 7 {
			t.Fatal("same intent redrew its roll", again, e)
		}
	}
	var rule, count, assigned int64
	if e = pool.QueryRow(ctx, "SELECT rule_id,(SELECT count(*) FROM provider_route_assignments),(SELECT assigned FROM provider_route_counters WHERE rule_id=$1) FROM provider_route_assignments WHERE out_trade_no='original-random161'", original).Scan(&rule, &count, &assigned); e != nil || rule != original || count != 1 || assigned != 1 {
		t.Fatal("original rule/quota changed on replay", rule, count, assigned, e)
	}
	if _, e = pool.Exec(ctx, "UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false'::jsonb) WHERE id=$1", original); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.providerRoutedRequest(ctx, 1, p); e == nil {
		t.Fatal("unsent original selected a replacement after its group closed")
	}
}

func TestOriginalRandomRollNamedGroupIsNotAnInheritedDefault(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls','{"name":"named only","merchant_id":"0","type":"alipay","strategy":"first","channel_ids":"8:1","enabled":true,"default_route":false}')`); e != nil {
		t.Fatal(e)
	}
	got, _, e := s.providerRoutedRequest(ctx, 1, randomRollParams161("inherit-named161"))
	if e != nil || got != 7 {
		t.Fatal("named roll silently became every merchant's default", got, e)
	}
}

func TestOriginalRandomRollEmptyUnavailableAndQuotaDoNotAllocateAnotherIntent(t *testing.T) {
	for _, scenario := range []string{"empty", "unavailable", "quota"} {
		t.Run(scenario, func(t *testing.T) {
			s, pool := groupRoutingFixture152(t)
			ctx := context.Background()
			if _, e := pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info}','{"alipay":{"channel":"-3","type":"roll"}}') WHERE id=0 AND resource='groups'`); e != nil {
				t.Fatal(e)
			}
			if scenario != "empty" {
				if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls','{"name":"one limited group","merchant_id":"0","type":"alipay","strategy":"first","channel_ids":"7:1","enabled":true,"default_route":false,"limits":"{\"daily_count\":1}"}')`); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "unavailable" {
				s.MerchantChannels[1] = []int64{8}
			}
			want := 0
			if scenario == "quota" {
				if _, _, e := s.providerRoutedRequest(ctx, 1, randomRollParams161("quota-original161")); e != nil {
					t.Fatal(e)
				}
				want = 1
			}
			if _, _, e := s.providerRoutedRequest(ctx, 1, randomRollParams161("denied-"+scenario)); e == nil {
				t.Fatal("unavailable or exhausted group accepted a new order")
			}
			var routes, attempts, assigned int
			if e := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM provider_attempts),(SELECT COALESCE(sum(assigned),0) FROM provider_route_counters)").Scan(&routes, &attempts, &assigned); e != nil || routes != want || attempts != 0 || assigned != want {
				t.Fatal("denied candidate consumed intent/quota or called provider", routes, attempts, assigned, e)
			}
		})
	}
}
