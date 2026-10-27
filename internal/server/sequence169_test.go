package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

func sequence169Fixture(t *testing.T) (*Server, *pgxpool.Pool, []int64) {
	t.Helper()
	s, p := groupRoutingFixture152(t)
	ctx := context.Background()
	if e := s.Store.CreateMerchant(ctx, payment.Merchant{ID: 2, Name: "synthetic-sequence169", APIKey: "synthetic-sequence169-key"}); e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow.Providers[9] = s.ProviderFlow.Providers[7]
	s.MerchantChannels[1] = []int64{9, 8, 7}
	s.MerchantChannels[2] = []int64{9, 8, 7}
	groups := []int64{}
	for _, mid := range []int64{1, 2} {
		var gid int64
		if e := p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"-4","type":"channel"}}}') RETURNING id`).Scan(&gid); e != nil {
			t.Fatal(e)
		}
		if _, e := p.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=$2`, gid, mid); e != nil {
			t.Fatal(e)
		}
		groups = append(groups, gid)
	}
	return s, p, groups
}

func sequence169Params(no string) compat.Params {
	return compat.Params{"type": "alipay", "method": "native", "name": "synthetic169", "money": "1.00", "clientip": "192.0.2.169", "out_trade_no": no}
}

func sequence169Route(t *testing.T, s *Server, mid int64, no string, want int64) {
	t.Helper()
	got, method, e := s.providerRoutedRequest(context.Background(), mid, sequence169Params(no))
	if e != nil || got != want || method != "native" {
		t.Fatalf("global parent usetime selection: merchant=%d no=%s got=%d want=%d method=%s err=%v", mid, no, got, want, method, e)
	}
}

func sequence169Digest(t *testing.T, p *pgxpool.Pool) string {
	t.Helper()
	var digest string
	if e := p.QueryRow(context.Background(), `SELECT md5(COALESCE((SELECT jsonb_agg(to_jsonb(x) ORDER BY channel_id)::text FROM provider_channel_last_used x),'[]')||COALESCE((SELECT jsonb_agg(to_jsonb(x) ORDER BY merchant_id,out_trade_no)::text FROM provider_route_assignments x),'[]')||COALESCE((SELECT jsonb_agg(to_jsonb(x) ORDER BY group_id)::text FROM group_route_counters x),'[]'))`).Scan(&digest); e != nil {
		t.Fatal(e)
	}
	return digest
}

func TestSequence169DifferentGroupsShareGlobalParentUseAndReplayDoesNotAdvance(t *testing.T) {
	s, p, groups := sequence169Fixture(t)
	ctx := context.Background()
	// An old per-group counter remains evidence, but must not select new -4.
	if _, e := p.Exec(ctx, `INSERT INTO group_route_counters(group_id,group_record_id,cursor,assigned) VALUES($1,$1,0,17)`, groups[0]); e != nil {
		t.Fatal(e)
	}
	sequence169Route(t, s, 1, "sequence169-group-a", 7)
	sequence169Route(t, s, 2, "sequence169-group-b", 8)
	before := sequence169Digest(t, p)
	sequence169Route(t, s, 1, "sequence169-group-a", 7)
	if got := sequence169Digest(t, p); got != before {
		t.Fatal("replay advanced last-used, route or old counter")
	}
	var cursor, assigned, rows int64
	if e := p.QueryRow(ctx, `SELECT cursor,assigned FROM group_route_counters WHERE group_id=$1`, groups[0]).Scan(&cursor, &assigned); e != nil || cursor != 0 || assigned != 17 {
		t.Fatal("old group counter changed", cursor, assigned, e)
	}
	if e := p.QueryRow(ctx, `SELECT count(*) FROM group_route_counters`).Scan(&rows); e != nil || rows != 1 {
		t.Fatal("new sequential group counter was invented", rows, e)
	}
	if e := p.QueryRow(ctx, `SELECT count(*) FROM provider_attempts`).Scan(&rows); e != nil || rows != 0 {
		t.Fatal("routing created upstream attempt", rows, e)
	}
}

func TestSequence169CandidateRemovalUsesLeastUsedRatherThanCursorModulo(t *testing.T) {
	s, _, _ := sequence169Fixture(t)
	sequence169Route(t, s, 1, "sequence169-before-removal-a", 7)
	sequence169Route(t, s, 1, "sequence169-before-removal-b", 8)
	s.MerchantChannels[1] = []int64{9, 8}
	sequence169Route(t, s, 1, "sequence169-after-removal", 9)
}

func TestSequence169FailureRollbackNoEligibleAndCommittedHistoryStayUnchanged(t *testing.T) {
	s, p, _ := sequence169Fixture(t)
	ctx := context.Background()
	sequence169Route(t, s, 1, "sequence169-prior", 7)
	before := sequence169Digest(t, p)
	conflict := sequence169Params("sequence169-explicit-conflict")
	conflict["channel_id"] = "7"
	if _, _, e := s.providerRoutedRequest(ctx, 1, conflict); e == nil {
		t.Fatal("explicit channel bypassed global least-used selection")
	}
	s.MerchantChannels[1] = nil
	if _, _, e := s.providerRoutedRequest(ctx, 1, sequence169Params("sequence169-empty")); e == nil {
		t.Fatal("no eligible channel accepted")
	}
	s.MerchantChannels[1] = []int64{7, 8, 9}
	// Commit rejection happens after both assignment and last-used writes.
	if _, e := p.Exec(ctx, `CREATE FUNCTION sequence169_reject_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.out_trade_no='sequence169-commit-fails' THEN RAISE EXCEPTION 'synthetic169 commit rejected'; END IF; RETURN NEW; END $$; CREATE CONSTRAINT TRIGGER sequence169_reject_commit AFTER INSERT ON provider_route_assignments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION sequence169_reject_commit()`); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.providerRoutedRequest(ctx, 1, sequence169Params("sequence169-commit-fails")); e == nil {
		t.Fatal("synthetic commit failure was ignored")
	}
	if got := sequence169Digest(t, p); got != before {
		t.Fatal("failed route advanced durable head, route or historical counter")
	}
	// Original assignment remains authoritative even after a policy change.
	if _, e := p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info,alipay,channel}','"-5"') WHERE id=(SELECT base_group_id FROM merchants WHERE id=1)`); e != nil {
		t.Fatal(e)
	}
	sequence169Route(t, s, 1, "sequence169-prior", 7)
	if got := sequence169Digest(t, p); got != before {
		t.Fatal("historical assignment was re-priced or re-owned")
	}
}

func TestSequence169ConcurrentGroupsSerializeOverlappingParentSet(t *testing.T) {
	s, p, _ := sequence169Fixture(t)
	s.MerchantChannels[1] = []int64{8, 7}
	s.MerchantChannels[2] = []int64{7, 8}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan int64, 2)
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	for _, mid := range []int64{1, 2} {
		wg.Add(1)
		go func(mid int64) {
			defer wg.Done()
			<-start
			id, _, e := s.providerRoutedRequest(ctx, mid, sequence169Params(fmt.Sprintf("sequence169-concurrent-%d", mid)))
			results <- id
			errors <- e
		}(mid)
	}
	close(start)
	wg.Wait()
	for range 2 {
		if e := <-errors; e != nil {
			t.Fatal(e)
		}
	}
	a, b := <-results, <-results
	if a == b || (a != 7 && a != 8) || (b != 7 && b != 8) {
		t.Fatal("overlapping groups concurrently chose same never-used parent", a, b)
	}
	var count int
	if e := p.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments`).Scan(&count); e != nil || count != 2 {
		t.Fatal("route commit count", count, e)
	}
}

func TestSequence169EqualMillisecondsUseExactLargeIDsAndLegacyReplayNeverBackfills(t *testing.T) {
	s, p, _ := sequence169Fixture(t)
	ctx := context.Background()
	const lower, upper int64 = 9007199254740993, 9007199254740995
	s.ProviderFlow.Providers[lower] = s.ProviderFlow.Providers[7]
	s.ProviderFlow.Providers[upper] = s.ProviderFlow.Providers[7]
	s.MerchantChannels[1] = []int64{upper, lower}
	for i, channel := range []int64{lower, upper} {
		params := sequence169Params(fmt.Sprintf("sequence169-tie-source-%d", i))
		// Different microseconds in the same millisecond deliberately tie.
		at := fmt.Sprintf("2026-10-25T02:00:00.123%d00Z", 1+i)
		if _, e := p.Exec(ctx, `INSERT INTO provider_route_assignments(merchant_id,out_trade_no,channel_id,method,pay_type,amount,request_hash,rule_snapshot,created_at) VALUES(1,$1,$2,'native','alipay',100,$3,'{"payment_info":{"alipay":{"channel":"-4","type":"channel"}}}',$4::timestamptz)`, params["out_trade_no"], channel, s.routeRequestHash(1, params, channel, "native", 100), at); e != nil {
			t.Fatal(e)
		}
		if _, e := p.Exec(ctx, `INSERT INTO provider_channel_last_used(channel_id,merchant_id,out_trade_no,last_used_millis) SELECT channel_id,merchant_id,out_trade_no,floor(extract(epoch FROM created_at)*1000)::bigint FROM provider_route_assignments WHERE merchant_id=1 AND out_trade_no=$1`, params["out_trade_no"]); e != nil {
			t.Fatal(e)
		}
	}
	sequence169Route(t, s, 1, "sequence169-exact-tie", lower)
	// A pre-rollout route has no new head. Its old identity is not backfilled.
	legacy := sequence169Params("sequence169-legacy-route")
	// A genuine legacy order was already born before this historical route
	// is replayed. An assignment alone is not permission to create an order
	// without the new sealed fee quote (168A deliberately fails that closed).
	if _, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: legacy["out_trade_no"], Name: legacy["name"], Type: legacy["type"], Amount: 100}); e != nil {
		t.Fatal("original legacy order birth", e)
	}
	if _, e := p.Exec(ctx, `INSERT INTO provider_route_assignments(merchant_id,out_trade_no,channel_id,method,pay_type,amount,request_hash) VALUES(1,$1,7,'native','alipay',100,$2)`, legacy["out_trade_no"], s.routeRequestHash(1, legacy, 7, "native", 100)); e != nil {
		t.Fatal(e)
	}
	s.MerchantChannels[1] = append(s.MerchantChannels[1], 7)
	before := sequence169Digest(t, p)
	sequence169Route(t, s, 1, legacy["out_trade_no"], 7)
	if got := sequence169Digest(t, p); got != before {
		t.Fatal("legacy replay backfilled global use fact")
	}
	for _, sql := range []string{
		`UPDATE provider_channel_last_used SET last_used_millis=last_used_millis+1`,
		`UPDATE provider_channel_last_used SET channel_id=6 WHERE channel_id=9007199254740993`,
		`DELETE FROM provider_channel_last_used`,
	} {
		if _, e := p.Exec(ctx, sql); e == nil {
			t.Fatal("unproven head rewrite accepted", sql)
		}
	}
	if got := sequence169Digest(t, p); got != before {
		t.Fatal("rejected fact rewrite changed source")
	}
}
