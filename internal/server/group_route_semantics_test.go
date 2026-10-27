package server

import (
	"context"
	"goepay/internal/compat"
	"testing"
)

func TestOriginalGroupChannelZeroDeniesNewRoutingAndKeepsRateOnlyLegacySeparate(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx := context.Background()
	var gid int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"0","type":"","rate":"0"}}}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=1`, gid); e != nil {
		t.Fatal(e)
	}
	params := func(no string) compat.Params {
		return compat.Params{"type": "alipay", "method": "native", "name": "original zero", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": no}
	}
	if _, _, e := s.providerRoutedRequest(ctx, 1, params("explicit-closed")); e == nil {
		t.Fatal("original closed-zero channel became an inherited payable route")
	}
	var count int
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments`).Scan(&count); e != nil || count != 0 {
		t.Fatal("closed route allocated intent", count, e)
	}
	if _, e := pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info}','{"alipay":{"rate":"0"}}') WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	if got, _, e := s.providerRoutedRequest(ctx, 1, params("legacy-rate-only")); e != nil || got != 7 {
		t.Fatal("legacy rate-zero inheritance changed", got, e)
	}
}

func TestOriginalGroupFirstUsesAscendingActualChannelIDs(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx := context.Background()
	s.MerchantChannels[1] = []int64{8, 7}
	var gid int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"-5","type":"channel"}}}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=1`, gid); e != nil {
		t.Fatal(e)
	}
	p := compat.Params{"type": "alipay", "method": "native", "name": "original first", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "numeric-first"}
	if got, _, e := s.providerRoutedRequest(ctx, 1, p); e != nil || got != 7 {
		t.Fatal("original first did not use numeric ID ascending order", got, e)
	}
}
