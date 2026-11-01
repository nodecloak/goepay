package server

import (
	"context"
	"encoding/json"
	"goepay/internal/backoffice"
	"testing"
)

func TestPayRollDraftCreationAndChannelConfigurationStayOutsideLiveRouting(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx := context.Background()
	bo := backoffice.New(pool, s.Store)
	bo.GroupChannelAllowed = func(id int64, kind string) bool { return kind == "alipay" && (id == 7 || id == 8) }
	p := backoffice.Principal{Role: "admin", SubjectID: 1, Username: "synthetic"}
	decode := func(value string) map[string]json.RawMessage {
		var fields map[string]json.RawMessage
		if e := json.Unmarshal([]byte(value), &fields); e != nil {
			t.Fatal(e)
		}
		return fields
	}
	draft, e := bo.CreatePayRollDraft(ctx, p, decode(`{"creation_mode":"draft","name":"inert weighted draft","type":"alipay","strategy":"weighted"}`))
	if e != nil {
		t.Fatal(e)
	}
	assertNotRandom := func() {
		t.Helper()
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		if _, _, _, e = readRandomGroupRoll(ctx, tx, "alipay"); e == nil {
			t.Fatal("inert draft entered random group pool")
		}
	}
	assertNotRandom()
	snapshot, e := bo.PayRollChannelSnapshot(ctx, p, draft.ID)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"expected_revision": snapshot.Revision, "channel_ids": "8:1", "confirm_enabled": false})
	configured, e := bo.UpdatePayRollChannels(ctx, p, draft.ID, decode(string(raw)))
	if e != nil {
		t.Fatal(e)
	}
	if configured.Fields["enabled"] != false || configured.Fields["default_route"] != false {
		t.Fatal("channel save activated draft", configured.Fields)
	}
	assertNotRandom()
	channel, _, e := s.providerRoutedRequest(ctx, 1, randomRollParams161("draft-inherit-before-enable"))
	if e != nil || channel != 7 {
		t.Fatal("configured inert draft changed inherited routing", channel, e)
	}
	if _, e = bo.Update(ctx, p, "pay-rolls", draft.ID, decode(`{"enabled":true}`)); e != nil {
		t.Fatal("explicit activation rejected configured draft", e)
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	id, _, roll, e := readRandomGroupRoll(ctx, tx, "alipay")
	_ = tx.Rollback(ctx)
	if e != nil || id != draft.ID || len(roll.Channels) != 1 || roll.Channels[0].ID != 8 {
		t.Fatal("explicitly enabled named roll unavailable", id, roll, e)
	}
	channel, _, e = s.providerRoutedRequest(ctx, 1, randomRollParams161("draft-inherit-after-enable"))
	if e != nil || channel != 7 {
		t.Fatal("explicit activation silently claimed global default routing", channel, e)
	}
}
