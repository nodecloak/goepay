package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"testing"
	"time"
)

func testQueue(t *testing.T) Queue {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	nonce := make([]byte, 8)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	schema := "jobs_" + hex.EncodeToString(nonce)
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	migration, e := os.ReadFile("../../../db/migrations/000001_core.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, string(migration)); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close(); _, _ = pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); pool.Close() })
	return Queue{Pool: p}
}
func TestConcurrentClaimAndLeaseFence(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	if _, e := q.Pool.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload) VALUES('a','notify','{}')"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	claimed := make(chan Job, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := q.Claim(ctx, []string{"notify"}, time.Minute)
			if e == nil {
				claimed <- j
			} else if !IsEmpty(e) {
				t.Errorf("claim: %v", e)
			}
		}()
	}
	wg.Wait()
	close(claimed)
	var jobs []Job
	for j := range claimed {
		jobs = append(jobs, j)
	}
	if len(jobs) != 1 {
		t.Fatalf("%d claimed", len(jobs))
	}
	old := jobs[0]
	if _, e := q.Pool.Exec(ctx, "UPDATE jobs SET lease_until=now()-interval '1 second' WHERE id=$1", old.ID); e != nil {
		t.Fatal(e)
	}
	fresh, e := q.Claim(ctx, []string{"notify"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = q.Complete(ctx, old); !errors.Is(e, ErrLeaseLost) {
		t.Fatalf("old worker completed: %v", e)
	}
	if e = q.Complete(ctx, fresh); e != nil {
		t.Fatal(e)
	}
}

func TestChannelNotice76QueueRequiresExplicitKindAndStaysIndependentOfProviderReadRouting(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	if _, e := q.Pool.Exec(ctx, `CREATE TABLE provider_attempts(id bigint,order_id bigint,channel_id bigint);CREATE TABLE provider_transfer_attempts(transfer_id bigint,channel_id bigint);CREATE TABLE provider_attempt_sharing(order_id bigint,channel_id bigint);CREATE TABLE channel_sharing_orders(id bigint,order_id bigint);CREATE TABLE channel_sharing_returns(id bigint,order_id bigint);CREATE TABLE channel_sharing_finishes(id bigint,order_id bigint);INSERT INTO jobs(event_key,kind,payload)VALUES('channel-stop:31','admin.channel-stopped','{"event_id":"31"}')`); e != nil {
		t.Fatal(e)
	}
	if _, e := q.ClaimProviderChannels(ctx, []string{"payment.notify"}, time.Minute, []int64{7}); !IsEmpty(e) {
		t.Fatal("disabled notice worker claimed administrator job", e)
	}
	j, e := q.ClaimProviderChannels(ctx, []string{"admin.channel-stopped"}, time.Minute, []int64{7})
	if e != nil || j.Kind != "admin.channel-stopped" {
		t.Fatal("explicit notice intent hidden by provider read routing", j, e)
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
}
func TestExhaustedCrashedJobBecomesDead(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	if _, e := q.Pool.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,state,attempts,max_attempts,lease_until) VALUES('a','notify','{}','running',1,1,now()-interval '1 second')"); e != nil {
		t.Fatal(e)
	}
	_, e := q.Claim(ctx, []string{"notify"}, time.Minute)
	if !IsEmpty(e) {
		t.Fatalf("claim exhausted %v", e)
	}
	var state string
	if e = q.Pool.QueryRow(ctx, "SELECT state FROM jobs WHERE event_key='a'").Scan(&state); e != nil {
		t.Fatal(e)
	}
	if state != "dead" {
		t.Fatalf("exhausted job stranded %q", state)
	}
}

func TestSharingChannelRoutingUsesOriginalDeclarationAndIgnoresPayloadChannel(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	// Minimal routing projections, independent of financial mutation fixtures.
	_, e := q.Pool.Exec(ctx, `CREATE TABLE provider_attempts(id bigint,order_id bigint,channel_id bigint);CREATE TABLE provider_transfer_attempts(transfer_id bigint,channel_id bigint);CREATE TABLE provider_attempt_sharing(order_id bigint,channel_id bigint);CREATE TABLE channel_sharing_orders(id bigint,order_id bigint);CREATE TABLE channel_sharing_returns(id bigint,order_id bigint);INSERT INTO provider_attempt_sharing VALUES(101,41),(102,42);INSERT INTO channel_sharing_orders VALUES(501,101),(502,102);INSERT INTO channel_sharing_returns VALUES(601,101),(602,102);INSERT INTO jobs(event_key,kind,payload) VALUES('other','channel-sharing.reconcile','{"sharing_id":501,"channel_id":42}'),('original','channel-sharing.reconcile','{"sharing_id":502,"channel_id":41}'),('other-return','channel-sharing-return.reconcile','{"return_id":601,"channel_id":42}'),('original-return','channel-sharing-return.reconcile','{"return_id":602,"channel_id":41}')`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Pool.Exec(ctx, `CREATE TABLE channel_sharing_finishes(id bigint,order_id bigint);INSERT INTO channel_sharing_finishes VALUES(701,101),(702,102);INSERT INTO jobs(event_key,kind,payload) VALUES('other-finish','channel-sharing-finish.reconcile','{"finish_id":701,"channel_id":42}'),('original-finish','channel-sharing-finish.reconcile','{"finish_id":702,"channel_id":41}')`); e != nil {
		t.Fatal(e)
	}
	j, e := q.ClaimProviderChannels(ctx, []string{"channel-sharing.reconcile"}, time.Minute, []int64{42})
	if e != nil || string(j.Payload) != `{"channel_id": 41, "sharing_id": 502}` {
		t.Fatal("queue trusted injected channel or ignored original declaration", j, e)
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
	if _, e = q.ClaimProviderChannels(ctx, []string{"channel-sharing.reconcile"}, time.Minute, []int64{42}); !IsEmpty(e) {
		t.Fatal("another channel's task was claimable", e)
	}
	if _, e = q.ClaimProviderChannels(ctx, []string{"channel-sharing.reconcile"}, time.Minute, []int64{41}); e != nil {
		t.Fatal("original owning channel could not claim", e)
	}
	j, e = q.ClaimProviderChannels(ctx, []string{"channel-sharing-return.reconcile"}, time.Minute, []int64{42})
	if e != nil || string(j.Payload) != `{"return_id": 602, "channel_id": 41}` {
		t.Fatal("return task trusted a payload channel", j, e)
	}
	if _, e = q.ClaimProviderChannels(ctx, []string{"channel-sharing-return.reconcile"}, time.Minute, []int64{42}); !IsEmpty(e) {
		t.Fatal("return task escaped original channel", e)
	}
	j, e = q.ClaimProviderChannels(ctx, []string{"channel-sharing-finish.reconcile"}, time.Minute, []int64{42})
	if e != nil || string(j.Payload) != `{"finish_id": 702, "channel_id": 41}` {
		t.Fatal("finish task trusted a payload channel", j, e)
	}
	if _, e = q.ClaimProviderChannels(ctx, []string{"channel-sharing-finish.reconcile"}, time.Minute, []int64{42}); !IsEmpty(e) {
		t.Fatal("finish task escaped original channel", e)
	}
}
