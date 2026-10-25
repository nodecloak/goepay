package jobs

import (
	"context"
	"testing"
	"time"
)

func TestMerchantNotice80QueueRequiresExplicitKindIndependentlyOfProviderChannels(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	if _, err := q.Pool.Exec(ctx, `CREATE TABLE provider_attempts(id bigint,order_id bigint,channel_id bigint);CREATE TABLE provider_transfer_attempts(transfer_id bigint,channel_id bigint);CREATE TABLE provider_attempt_sharing(order_id bigint,channel_id bigint);CREATE TABLE channel_sharing_orders(id bigint,order_id bigint);CREATE TABLE channel_sharing_returns(id bigint,order_id bigint);CREATE TABLE channel_sharing_finishes(id bigint,order_id bigint);INSERT INTO jobs(event_key,kind,payload)VALUES('merchant-notice:80','merchant.notice','{"event_id":"80"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ClaimProviderChannels(ctx, []string{"payment.notify"}, time.Minute, []int64{7}); !IsEmpty(err) {
		t.Fatal("default worker claimed optional merchant messaging", err)
	}
	j, err := q.ClaimProviderChannels(ctx, []string{"merchant.notice"}, time.Minute, []int64{7})
	if err != nil || j.Kind != "merchant.notice" {
		t.Fatal("explicit messaging hidden by channel routing", j, err)
	}
}
