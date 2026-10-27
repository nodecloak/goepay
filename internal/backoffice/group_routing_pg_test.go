package backoffice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestGroupRoutingWritesRequireActualRuntimeReferencesAndKeepExactIDs(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	const channel int64 = 9007199254740993
	raw := fmt.Sprintf(`{"name":"group-route","info":{"alipay":{"channel":%d,"type":"channel","rate":"99.50"}}}`, channel)
	if _, e := s.Create(ctx, admin, "groups", input(raw)); !errors.Is(e, ErrInvalid) {
		t.Fatal("route accepted with no runtime catalog", e)
	}
	s.GroupChannelAllowed = func(id int64, kind string) bool { return id == channel && kind == "alipay" }
	g, e := s.Create(ctx, admin, "groups", input(raw))
	if e != nil {
		t.Fatal(e)
	}
	check := func(item Item) {
		t.Helper()
		info, ok := item.Fields["info"].(map[string]any)
		if !ok {
			t.Fatal("missing info")
		}
		method, ok := info["alipay"].(map[string]any)
		if !ok || method["channel"] != "9007199254740993" || method["rate"] != "99.50" {
			t.Fatalf("rounded runtime ID: %#v", info)
		}
	}
	check(g)
	read, e := s.Get(ctx, admin, "groups", g.ID)
	if e != nil {
		t.Fatal(e)
	}
	check(read)
	page, e := s.List(ctx, admin, "groups", Query{})
	if e != nil || len(page.Items) != 2 || page.Total != 2 || page.Items[0].ID != 0 || page.Items[1].ID != g.ID {
		t.Fatal(page, e)
	}
	check(page.Items[1])
	for _, info := range []string{`{"alipay":{"channel":"1","type":"channel"}}`, `{"wxpay":{"channel":"9007199254740993","type":"channel"}}`, `{"alipay":{"channel":"99999","type":"roll"}}`} {
		if _, e = s.Update(ctx, admin, "groups", g.ID, input(`{"info":`+info+`}`)); !errors.Is(e, ErrInvalid) {
			t.Fatal("unsupported runtime reference accepted", info, e)
		}
	}
	var roll int64
	body := fmt.Sprintf(`{"name":"real-roll","merchant_id":"0","type":"alipay","strategy":"first","channel_ids":"%d:1","enabled":true}`, channel)
	if e = s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls',$1::jsonb) RETURNING id`, body).Scan(&roll); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, admin, "groups", g.ID, input(fmt.Sprintf(`{"info":{"alipay":{"channel":"%d","type":"roll"}}}`, roll))); e != nil {
		t.Fatal("real globally usable roll rejected", e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false') WHERE id=$1`, roll); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, admin, "groups", g.ID, input(fmt.Sprintf(`{"info":{"alipay":{"channel":"%d","type":"roll"}}}`, roll))); !errors.Is(e, ErrInvalid) {
		t.Fatal("disabled roll accepted", e)
	}
	// An unrelated display-only directory entry must never become a provider.
	var directory int64
	if e = s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('channels','{"name":"metadata","enabled":true}') RETURNING id`).Scan(&directory); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Update(ctx, admin, "groups", g.ID, input(fmt.Sprintf(`{"info":{"alipay":{"channel":"%d","type":"channel"}}}`, directory))); !errors.Is(e, ErrInvalid) {
		t.Fatal("directory ID used as runtime provider", e)
	}
	var persisted string
	if e = s.pool.QueryRow(ctx, `SELECT body::text FROM backoffice_records WHERE id=$1`, g.ID).Scan(&persisted); e != nil {
		t.Fatal(e)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(persisted), &fields) != nil {
		t.Fatal("invalid persisted group")
	}
}

func TestGroupRoutingReadPreservesUnsupportedHistoricalInfoWithoutHidingOtherGroups(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	var id int64
	// -2 is now an implemented subchannel policy. Use a genuinely unsupported
	// historical sentinel so this still exercises the lossless fallback path.
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"historical unsupported","enabled":true,"info":{"alipay":{"channel":-6,"type":"channel","rate":"99.50"},"wxpay":{"channel":9007199254740993,"type":"channel"}}}') RETURNING id`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Create(ctx, admin, "groups", input(`{"name":"supported","info":{"alipay":{"rate":"99"}}}`)); e != nil {
		t.Fatal(e)
	}
	item, e := s.Get(ctx, admin, "groups", id)
	if e != nil {
		t.Fatal("historical record became unreadable", e)
	}
	original, ok := item.Fields["info"].(string)
	if !ok || !strings.Contains(original, "9007199254740993") || !strings.Contains(original, "-6") {
		t.Fatal("unsupported historical bytes rounded or discarded", item.Fields)
	}
	page, e := s.List(ctx, admin, "groups", Query{})
	if e != nil || len(page.Items) != 3 || page.Total != 3 || page.Items[0].ID != 0 {
		t.Fatal("one unsupported group hid the whole directory", page, e)
	}
	if _, e = s.Update(ctx, admin, "groups", id, input(`{"info":{"alipay":{"channel":"-2","type":"channel"}}}`)); e != nil {
		t.Fatal("implemented owned subchannel policy rejected for new configuration", e)
	}
	implemented, e := s.Get(ctx, admin, "groups", id)
	if e != nil {
		t.Fatal(e)
	}
	info, ok := implemented.Fields["info"].(map[string]any)
	if !ok {
		t.Fatal("implemented -2 policy incorrectly treated as unsupported", implemented.Fields)
	}
	alipay, ok := info["alipay"].(map[string]any)
	if !ok || alipay["channel"] != "-2" {
		t.Fatal("implemented subchannel sentinel changed", info)
	}
	if _, e = s.Update(ctx, admin, "groups", id, input(`{"info":{"alipay":{"rate":"99"}}}`)); e != nil {
		t.Fatal("explicit supported replacement rejected", e)
	}
}
