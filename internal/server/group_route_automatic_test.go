package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
)

func automaticGroup153(t *testing.T, sentinel string) (*Server, int64) {
	t.Helper()
	s, pool := groupRoutingFixture152(t)
	var gid int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO backoffice_records(resource,body) VALUES('groups',jsonb_build_object('enabled',true,'info',jsonb_build_object('alipay',jsonb_build_object('channel',$1::text,'type','channel'),'wxpay',jsonb_build_object('channel',$1::text,'type','channel')))) RETURNING id`, sentinel).Scan(&gid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE merchants SET base_group_id=$1 WHERE id=1`, gid); err != nil {
		t.Fatal(err)
	}
	s.MerchantChannels[1] = []int64{8, 7}
	return s, gid
}

func automaticParams153(no, kind string) compat.Params {
	return compat.Params{"type": kind, "method": "native", "name": "automatic original group", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": no}
}

func TestOriginalGroupSequentialGlobalParentUseAcrossTypesAndReplayRollback(t *testing.T) {
	s, gid := automaticGroup153(t, "-4")
	ctx := context.Background()
	wx, err := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "local-test", MerchantID: "mch", APIKey: "0123456789abcdef0123456789abcdef", Methods: []string{"native"}})
	if err != nil {
		t.Fatal(err)
	}
	s.ProviderFlow.Providers[9] = wx
	s.ProviderFlow.Providers[10] = wx
	s.MerchantChannels[1] = []int64{10, 8, 9, 7}
	for i, c := range []struct {
		kind string
		want int64
	}{{"alipay", 7}, {"wxpay", 9}, {"alipay", 8}} {
		p := automaticParams153(fmt.Sprintf("shared-%d", i), c.kind)
		for retry := 0; retry < 2; retry++ {
			got, _, e := s.providerRoutedRequest(ctx, 1, p)
			if e != nil || got != c.want {
				t.Fatalf("step %d replay %d: got %d want %d: %v", i, retry, got, c.want, e)
			}
		}
	}
	// Pro Channel.php uses each parent's global usetime, not Release's group index.
	var count int
	if e := s.Pool.QueryRow(ctx, `SELECT count(*) FROM provider_channel_last_used`).Scan(&count); e != nil || count != 3 {
		t.Fatal("parent use/replay", count, e)
	}
	p := automaticParams153("explicit-must-not-advance", "alipay")
	p["channel_id"] = "8"
	if _, _, e := s.providerRoutedRequest(ctx, 1, p); e == nil {
		t.Fatal("explicit input skipped next channel")
	}
	if got, _, e := s.providerRoutedRequest(ctx, 1, automaticParams153("after-rejected", "alipay")); e != nil || got != 7 {
		t.Fatal("failed attempt consumed sequence", got, e)
	}
	if e := s.Pool.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments`).Scan(&count); e != nil || count != 4 {
		t.Fatal("failed request changed original assignment count", count, e)
	}
	if _, e := s.Pool.Exec(ctx, `DELETE FROM backoffice_records WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	if e := s.Pool.QueryRow(ctx, `SELECT count(*) FROM group_route_counters WHERE group_id=$1`, gid).Scan(&count); e != nil || count != 0 {
		t.Fatal("deleted group retained sequence", count, e)
	}
	if e := s.Pool.QueryRow(ctx, `SELECT count(*) FROM provider_channel_last_used`).Scan(&count); e != nil || count != 3 {
		t.Fatal("deleting a group erased global parent history", count, e)
	}
}

func TestOriginalGroupSequentialConcurrentSameOrderConsumesOnce(t *testing.T) {
	s, _ := automaticGroup153(t, "-4")
	ctx := context.Background()
	var wg sync.WaitGroup
	type result struct {
		id  int64
		err error
	}
	results := make(chan result, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, _, e := s.providerRoutedRequest(ctx, 1, automaticParams153("same-concurrent", "alipay"))
			results <- result{id, e}
		}()
	}
	wg.Wait()
	close(results)
	for r := range results {
		if r.err != nil || r.id != 7 {
			t.Fatal("concurrent replay", r)
		}
	}
	var assigned int64
	if e := s.Pool.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments`).Scan(&assigned); e != nil || assigned != 1 {
		t.Fatal("same order consumed more than once", assigned, e)
	}
	results = make(chan result, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, _, e := s.providerRoutedRequest(ctx, 1, automaticParams153(fmt.Sprintf("unique-%02d", i), "alipay"))
			results <- result{id, e}
		}(i)
	}
	wg.Wait()
	close(results)
	counts := map[int64]int{}
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		counts[r.id]++
	}
	// Exact millisecond ties use numeric ID, so equal-time fairness is not
	// invented. Every unique order must still bind one eligible parent once.
	if counts[7]+counts[8] != 12 || counts[7] == 0 || counts[8] == 0 {
		t.Fatal("lost concurrent parent selections", counts)
	}
	if e := s.Pool.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments`).Scan(&assigned); e != nil || assigned != 13 {
		t.Fatal("unique orders not counted once", assigned, e)
	}
}

func TestOriginalGroupRandomPersistsChosenEligibleRouteAndPolicySnapshot(t *testing.T) {
	s, gid := automaticGroup153(t, "-1")
	ctx := context.Background()
	p := automaticParams153("random-sticky", "alipay")
	first, _, e := s.providerRoutedRequest(ctx, 1, p)
	if e != nil || (first != 7 && first != 8) {
		t.Fatal("random eligible", first, e)
	}
	var snapshot []byte
	if e = s.Pool.QueryRow(ctx, `SELECT rule_snapshot FROM provider_route_assignments WHERE merchant_id=1 AND out_trade_no=$1`, p["out_trade_no"]).Scan(&snapshot); e != nil {
		t.Fatal(e)
	}
	var value struct {
		Group string `json:"group_id"`
		Info  map[string]struct {
			Channel string `json:"channel"`
		} `json:"payment_info"`
	}
	if json.Unmarshal(snapshot, &value) != nil || value.Group != fmt.Sprint(gid) || value.Info["alipay"].Channel != "-1" {
		t.Fatalf("missing original random policy snapshot: %s", snapshot)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info,alipay,channel}','"0"') WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 5; i++ {
		if got, _, e := s.providerRoutedRequest(ctx, 1, p); e != nil || got != first {
			t.Fatal("random replay changed after closure", got, first, e)
		}
	}
	if _, _, e := s.providerRoutedRequest(ctx, 1, automaticParams153("random-closed-new", "alipay")); e == nil {
		t.Fatal("closed random group accepted new route")
	}
}
