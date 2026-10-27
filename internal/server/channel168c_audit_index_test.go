package server

import (
	"context"
	"strings"
	"testing"
)

func TestChannel168CSuccessAuditLookupUsesParentAndDayIndex(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	// Synthetic unrelated parents/days expose a resource-only historical scan.
	// This fixture does not create paid orders or claim financial success.
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) SELECT 'system',0,'synthetic-index-source','runtime-channel-success',i,'runtime-channel-success',jsonb_build_object('channel_id',(i%50+1)::text,'day','2025-01-01','realmoney','100','revision','1') FROM generate_series(1,25000) i;ANALYZE backoffice_audit`); e != nil {
		t.Fatal(e)
	}
	var plan []byte
	if e := f.p.QueryRow(ctx, `EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) SELECT count(*) FROM backoffice_audit WHERE resource='runtime-channel-success' AND operation='runtime-channel-success' AND changes->>'channel_id'=$1::bigint::text AND changes->>'day'=$2`, int64(7), "2026-10-26").Scan(&plan); e != nil {
		t.Fatal(e)
	}
	t.Logf("actual isolated PG plan: %s", plan)
	if !strings.Contains(string(plan), "runtime_channel_success_audit_day") {
		t.Fatal("daily parent lookup scanned historical audit resource instead of its exact parent/day index")
	}
}
