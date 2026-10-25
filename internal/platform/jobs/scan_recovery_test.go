package jobs

import (
	"context"
	"testing"
	"time"
)

func TestScanRecovery78QueueRoutesOnlyOriginalAttemptAndExplicitKind(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	_, e := q.Pool.Exec(ctx, `CREATE TABLE provider_attempts(id bigint,order_id bigint,channel_id bigint);CREATE TABLE provider_transfer_attempts(transfer_id bigint,channel_id bigint);CREATE TABLE provider_attempt_sharing(order_id bigint,channel_id bigint);CREATE TABLE channel_sharing_orders(id bigint,order_id bigint);CREATE TABLE channel_sharing_returns(id bigint,order_id bigint);CREATE TABLE channel_sharing_finishes(id bigint,order_id bigint);INSERT INTO provider_attempts VALUES(7801,101,41),(7802,102,42);INSERT INTO jobs(event_key,kind,payload)VALUES('scan-recover:7801','payment.scan-recover','{"attempt_id":"7801","channel_id":42}'),('scan-recover:7802','payment.scan-recover','{"attempt_id":"7802"}'),('bad-scan','payment.scan-recover','{"attempt_id":"bad"}')`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.ClaimProviderChannels(ctx, []string{"payment.reconcile"}, time.Minute, []int64{42}); !IsEmpty(e) {
		t.Fatal("read-only worker claimed cancellation-capable task", e)
	}
	j, e := q.ClaimProviderChannels(ctx, []string{"payment.scan-recover"}, time.Minute, []int64{42})
	if e != nil || string(j.Payload) != `{"attempt_id": "7802"}` {
		t.Fatal("original scan routing unavailable or trusted injected channel", string(j.Payload), e)
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
	if _, e = q.ClaimProviderChannels(ctx, []string{"payment.scan-recover"}, time.Minute, []int64{42}); !IsEmpty(e) {
		t.Fatal("another channel or malformed source became claimable", e)
	}
}
