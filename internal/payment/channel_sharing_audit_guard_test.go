package payment

import (
	"context"
	"testing"
)

func TestChannelSharingDatabaseUnknownOrUnobservedFailureRequiresOriginalActorResultAudit(t *testing.T) {
	for _, state := range []string{"unknown", "failed"} {
		t.Run(state, func(t *testing.T) {
			f := newSharingExecutionFixture(t)
			ctx := context.Background()
			o := f.paid(t, "sharing-missing-audit-"+state, false)
			r, owner, e := f.Flow.claimChannelSharingOrder(ctx, 751, o.TradeNo, "missing-audit-sharing", channelSharingActor)
			if e != nil || !owner {
				t.Fatal(e, owner)
			}
			var proof []byte
			if state == "failed" {
				proof, e = f.Store.sharingOrderFailureFingerprint(r)
				if e != nil {
					t.Fatal(e)
				}
			}
			tx, e := f.Store.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			if _, e = tx.Exec(ctx, "UPDATE channel_sharing_orders SET state=$2,failure_proof=$3 WHERE id=$1", r.ID, state, proof); e != nil {
				t.Fatal(e)
			}
			if e = tx.Commit(ctx); e == nil {
				t.Fatal("database accepted a result without the original actor audit", state)
			}
		})
	}
}
