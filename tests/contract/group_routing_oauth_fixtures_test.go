package contract_test

import (
	"context"
	"encoding/json"
	"goepay/internal/server"
	"testing"
)

// Run the complete existing OAuth/mini assertions with an explicit group
// binding and an unusable unrelated default roll. Original baseline variants
// still run without this fixture, preserving their independent coverage.
func installGroupRoutingEntrypointFixture152(t *testing.T, s *server.Server, channels map[string]string) {
	t.Helper()
	ctx := context.Background()
	info := map[string]map[string]string{}
	for kind, channel := range channels {
		info[kind] = map[string]string{"channel": channel, "type": "channel"}
	}
	body, e := json.Marshal(map[string]any{"enabled": true, "info": info})
	if e != nil {
		t.Fatal(e)
	}
	var gid int64
	if e = s.Pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups',$1::jsonb) RETURNING id`, body).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id IN(1,2)`, gid); e != nil {
		t.Fatal(e)
	}
	for kind := range channels {
		body, e = json.Marshal(map[string]any{"name": "Unrelated unavailable default", "merchant_id": "0", "type": kind, "strategy": "first", "channel_ids": "9999:1", "enabled": true})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls',$1::jsonb)`, body); e != nil {
			t.Fatal(e)
		}
	}
}
