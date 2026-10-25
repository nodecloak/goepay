package payment

import (
	"context"
	"testing"
)

func TestChannelSharingReturnUnknownRequiresItsOriginalActorResultAudit(t *testing.T) {
	for _, wrongActor := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-result", true: "wrong-actor-name"}[wrongActor], func(t *testing.T) {
			f := newSharingReturnFixture(t)
			ctx := context.Background()
			r, owner, e := f.Flow.claimChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, f.Sharing.Items[0].ReceiverID, "unknown-audit-boundary", 12, channelSharingActor)
			if e != nil || !owner {
				t.Fatal(r, e)
			}
			tx, e := f.Store.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			if _, e = tx.Exec(ctx, "UPDATE channel_sharing_returns SET state='unknown' WHERE id=$1", r.ID); e != nil {
				t.Fatal(e)
			}
			if wrongActor {
				actor := channelSharingActor
				actor.Name = "another-actor-name"
				if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-returns", r.ID, "submit-result", "unknown"); e != nil {
					t.Fatal(e)
				}
			}
			if e = tx.Commit(ctx); e == nil {
				t.Fatal("unobserved return outcome committed without the original actor result audit")
			}
			if f.Sends.Load() != 0 {
				t.Fatal("review setup sent a financial return")
			}
		})
	}
}
