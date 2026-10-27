package jobs

import (
	"testing"
	"time"
)

func TestRefundEvent201QueueClaimsOnlyExplicitEventKindWithoutProviderChannel(t *testing.T) {
	for _, kind := range []string{"merchant.refund-event", "merchant.refund-event-closed-notice"} {
		t.Run(kind, func(t *testing.T) {
			q := testQueue(t)
			ctx := t.Context()
			_, err := q.Pool.Exec(ctx, `CREATE TABLE provider_attempts(id bigint,order_id bigint,channel_id bigint);CREATE TABLE provider_transfer_attempts(transfer_id bigint,channel_id bigint);CREATE TABLE provider_attempt_sharing(order_id bigint,channel_id bigint);CREATE TABLE channel_sharing_orders(id bigint,order_id bigint);CREATE TABLE channel_sharing_returns(id bigint,order_id bigint);CREATE TABLE channel_sharing_finishes(id bigint,order_id bigint)`)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = q.Pool.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload)VALUES($1,$2,'{"event_id":"201"}')`, "refund201:"+kind, kind); err != nil {
				t.Fatal(err)
			}
			if _, err = q.ClaimProviderChannels(ctx, []string{"payment.notify"}, time.Minute, []int64{7}); !IsEmpty(err) {
				t.Fatal("worker without an explicit event kind acquired an optional event", err)
			}
			job, err := q.ClaimProviderChannels(ctx, []string{kind}, time.Minute, []int64{7})
			if err != nil || job.Kind != kind || job.Token == "" {
				t.Fatal("explicit platform refund event is incorrectly hidden by provider-channel routing", kind, err)
			}
			if _, err = q.ClaimProviderChannels(ctx, []string{kind}, time.Minute, []int64{7}); !IsEmpty(err) {
				t.Fatal("an active event lease was claimed twice", err)
			}
			if err = q.Complete(ctx, job); err != nil {
				t.Fatal(err)
			}
		})
	}
}
