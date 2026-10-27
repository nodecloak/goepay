package server

import (
	"context"
	"goepay/internal/compat"
	"testing"
)

func TestOriginalGroupRoutingCannotTreatUnverifiableChannelStopHistoryAsAvailable(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx := context.Background()
	var gid int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"-5","type":"channel"}}}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=1`, gid); e != nil {
		t.Fatal(e)
	}
	// An orphan audit is an integrity failure, never a fabricated valid pause.
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'local-negative-fixture','channel-stops',999,'pause','{"channel_id":"7"}')`); e != nil {
		t.Fatal(e)
	}
	_, _, e := s.providerRoutedRequest(ctx, 1, compat.Params{"type": "alipay", "method": "native", "name": "item", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "unverifiable-stop"})
	if e == nil {
		t.Fatal("unverifiable stop history became available group route")
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments WHERE out_trade_no='unverifiable-stop'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("unverifiable channel allocated route", count, e)
	}
}
