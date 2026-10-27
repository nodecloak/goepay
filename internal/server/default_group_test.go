package server

import (
	"context"
	"testing"
)

func TestOriginalDefaultGroupZeroRouteAndGlobalParentUsePreserveOriginalIdentity(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx := context.Background()
	s.MerchantChannels[1] = []int64{8, 7}
	set := func(channel string) {
		t.Helper()
		if _, e := pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info}',jsonb_build_object('alipay',jsonb_build_object('channel',$1::text,'type','channel'))) WHERE id=0`, channel); e != nil {
			t.Fatal(e)
		}
	}
	set("8")
	original := automaticParams153("default-original", "alipay")
	if got, _, e := s.providerRoutedRequest(ctx, 1, original); e != nil || got != 8 {
		t.Fatal("configured real default ignored", got, e)
	}
	set("-4")
	first := automaticParams153("default-sequence-first", "alipay")
	for i := 0; i < 2; i++ {
		if got, _, e := s.providerRoutedRequest(ctx, 1, first); e != nil || got != 7 {
			t.Fatal("default sequence/replay", got, e)
		}
	}
	if got, _, e := s.providerRoutedRequest(ctx, 1, automaticParams153("default-sequence-second", "alipay")); e != nil || got != 8 {
		t.Fatal("default sequence did not advance", got, e)
	}
	var assigned int64
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM provider_channel_last_used`).Scan(&assigned); e != nil || assigned != 2 {
		t.Fatal("default sequence/replay parent facts", assigned, e)
	}
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM group_route_counters`).Scan(&assigned); e != nil || assigned != 0 {
		t.Fatal("new default sequence invented old group counter", assigned, e)
	}
	set("0")
	if _, _, e := s.providerRoutedRequest(ctx, 1, automaticParams153("default-closed-route", "alipay")); e == nil {
		t.Fatal("default closure ignored")
	}
	if got, _, e := s.providerRoutedRequest(ctx, 1, original); e != nil || got != 8 {
		t.Fatal("default edit changed original route", got, e)
	}
	if s.MerchantChannels[1][0] != 8 {
		t.Fatal("selection mutated runtime allowlist")
	}
}
