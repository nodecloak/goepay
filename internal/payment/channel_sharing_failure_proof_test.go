package payment

import (
	"context"
	"errors"
	"testing"
)

func TestChannelSharingUnobservedFailureMustBeKeyedBeforeItCanPermitFinish(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	o := f.paid(t, "sharing-failure-proof", false)
	r, owner, e := f.Flow.claimChannelSharingOrder(ctx, 751, o.TradeNo, "failure-proof-sharing", channelSharingActor)
	if e != nil || !owner {
		t.Fatal(r, e, owner)
	}
	// Also permits running this regression against the previous schema50 body.
	if _, e = f.Store.pool.Exec(ctx, "ALTER TABLE channel_sharing_orders ADD COLUMN IF NOT EXISTS failure_proof bytea"); e != nil {
		t.Fatal(e)
	}
	tx, e := f.Store.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_orders SET state='failed',failure_proof=decode(repeat('00',32),'hex') WHERE id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-orders", r.ID, "submit-result", "failed"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Store.readSharingOrder(ctx, 751, "failure-proof-sharing", channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("untrusted failure could authorize a new financial operation", e)
	}
	if f.Sends.Load() != 0 {
		t.Fatal("test unexpectedly sent a financial request")
	}
}
