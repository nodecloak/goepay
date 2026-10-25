package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestChannelSharingForgedDatabaseObservationCannotBeAdoptedAsTrustedCompletion(t *testing.T) {
	f := newSharingExecutionFixture(t)
	o := f.paid(t, "sharing-forged-observation", false)
	ctx := context.Background()
	r, owner, e := f.Flow.claimChannelSharingOrder(ctx, 751, o.TradeNo, "forged-sharing", channelSharingActor)
	if e != nil || !owner {
		t.Fatal(e, owner)
	}
	tx, e := f.Store.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	items := []ChannelSharingOrderItem{{ReceiverID: r.Payment.Items[0].ReceiverID, Amount: r.Payment.Items[0].Amount, State: "succeeded", ProviderDetailNo: "FORGED_DETAIL"}}
	raw, _ := json.Marshal(items)
	var obs, clearing int64
	if e = tx.QueryRow(ctx, `INSERT INTO channel_sharing_observations(sharing_id,fingerprint,source,provider_order_no,state,items,actor_role,actor_id,actor_name) VALUES($1,decode(repeat('00',32),'hex'),'query','FORGED_PS','succeeded',$2,'merchant',751,'sharing-owner') RETURNING id`, r.ID, raw).Scan(&obs); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0").Scan(&clearing); e != nil {
		t.Fatal(e)
	}
	key := fmt.Sprintf("channel-sharing:%d:receiver:%d:paid", r.ID, items[0].ReceiverID)
	if e = postByAccountIDs(ctx, tx, key, "test untrusted database observation", r.FundingAccount, clearing, items[0].Amount); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE channel_sharing_order_items SET state='succeeded',provider_detail_no='FORGED_DETAIL',last_observation_id=$2,paid_journal_id=(SELECT id FROM journals WHERE event_key=$3) WHERE sharing_id=$1`, r.ID, obs, key); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE channel_sharing_orders SET state='succeeded',provider_order_no='FORGED_PS',completed=true,last_observation_id=$2 WHERE id=$1`, r.ID, obs); e != nil {
		t.Fatal(e)
	}
	if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-orders", r.ID, "query-result", "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	view, e := f.Flow.QueryChannelSharing(ctx, 751, "forged-sharing", channelSharingActor)
	if !errors.Is(e, ErrConflict) || view.FundsReleased || f.Sends.Load() != 0 || f.Queries.Load() != 0 {
		t.Fatal("untrusted database fact was signed as a trusted release", view, e, f.Sends.Load(), f.Queries.Load())
	}
	var completions int
	if e = f.Store.pool.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_completions").Scan(&completions); e != nil || completions != 0 {
		t.Fatal(completions, e)
	}
	report, e := f.Store.CheckLedger(ctx)
	if e != nil || report.InvalidChannelSharingFundings != 1 {
		t.Fatal("ledger checker missed untrusted sharing evidence", report, e)
	}
}
