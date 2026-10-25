package payment

import (
	"context"
	"testing"
)

func TestChannelStop76CheckerFindsOrphanIndependentAnchors(t *testing.T) {
	for _, mode := range []string{"stop-audit", "notice-job", "notice-audit"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			sql := map[string]string{
				"stop-audit":   `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'settlement-auto','channel-stops',31,'pause','{"channel_id":"7","request_id":"1","proof":"00"}')`,
				"notice-job":   `INSERT INTO jobs(event_key,kind,payload)VALUES('channel-stop:31','admin.channel-stopped','{"event_id":"31"}')`,
				"notice-audit": `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'channel-notice','channel-stop-notices',31,'attempt','{}')`,
			}[mode]
			if _, e := s.pool.Exec(ctx, sql); e != nil {
				t.Fatal(e)
			}
			report, e := s.CheckLedger(ctx)
			if e != nil || report.InvalidChannelStops != 1 {
				t.Fatal("orphan channel stop/notice source omitted", mode, report.InvalidChannelStops, e)
			}
		})
	}
}
