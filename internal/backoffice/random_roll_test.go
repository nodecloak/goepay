package backoffice

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestOriginalRandomRollMultipleNamedGroupsKeepOneLegacyDefault(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	makeGroup := func(name string, defaultRoute *bool) error {
		v := payRollValues()
		v["name"] = name
		v["merchant_id"] = "0"
		if defaultRoute != nil {
			v["default_route"] = *defaultRoute
		}
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.Create(ctx, admin, "pay-rolls", input(string(raw)))
		return e
	}
	no := false
	yes := true
	for _, name := range []string{"named-one", "named-two"} {
		if e := makeGroup(name, &no); e != nil {
			t.Fatal("independent named roll rejected", name, e)
		}
	}
	if e := makeGroup("legacy-default", nil); e != nil {
		t.Fatal(e)
	}
	if e := makeGroup("second-default", &yes); e == nil {
		t.Fatal("duplicate default was permitted")
	}
	var records, audits int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM backoffice_records WHERE resource='pay-rolls'),(SELECT count(*) FROM backoffice_audit WHERE resource='pay-rolls')").Scan(&records, &audits); e != nil || records != 3 || audits != 3 {
		t.Fatal("rejected default leaked record or audit", records, audits, e)
	}
	var named int64
	if e := s.pool.QueryRow(ctx, `SELECT id FROM backoffice_records WHERE resource='pay-rolls' AND body->>'name'='named-one'`).Scan(&named); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Update(ctx, admin, "pay-rolls", named, input(`{"name":"renamed local group"}`)); e != nil {
		t.Fatal("partial edit changed named/default scope", e)
	}
	var defaultRoute bool
	if e := s.pool.QueryRow(ctx, `SELECT (body->>'default_route')::boolean FROM backoffice_records WHERE id=$1`, named).Scan(&defaultRoute); e != nil || defaultRoute {
		t.Fatal("partial edit reenabled default routing", defaultRoute, e)
	}
	if _, e := s.Create(ctx, admin, "groups", input(`{"name":"original random roll","info":{"alipay":{"channel":"-3","type":"roll","rate":"99"}}}`)); e != nil {
		t.Fatal("original random roll could not be configured", e)
	}
	v := payRollValues()
	v["default_route"] = "false"
	if _, e := ParsePayRoll(v); !errors.Is(e, ErrInvalid) {
		t.Fatal("text false bypassed default uniqueness", e)
	}
}
