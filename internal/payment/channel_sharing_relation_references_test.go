package payment

import (
	"context"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"strings"
	"sync"
	"testing"
)

func TestChannelSharingRelationReferencesShareBoundProofAndUnbindOnlyLastLease(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 752, Name: "reference-owner", APIKey: "local-reference"}); e != nil {
		t.Fatal(e)
	}
	actorB := TransferBatchActor{Role: "merchant", ID: 752, Name: "reference-owner"}
	g.Providers[8] = g.Providers[7]
	a, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "reference-a", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	b, e := g.CreateChannelSharingReceiver(ctx, 752, 8, "reference-b", channelSharingReceiverInput, actorB)
	if e != nil {
		t.Fatal("another owner's draft must not be blocked by a draft", e)
	}
	boundA, e := g.BindChannelSharingReceiver(ctx, 751, a.ID, "reference-bind-a", channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	boundB, e := g.BindChannelSharingReceiver(ctx, 752, b.ID, "reference-bind-b", actorB)
	if e != nil || boundB.State != "bound" || calls.Load() != 1 {
		t.Fatal("same trusted relation should gain a local lease without I/O", boundB, e, calls.Load())
	}
	if boundA.Operation == nil || boundA.Operation.Source != "remote" || boundB.Operation == nil || boundB.Operation.Source != "reference" || boundB.Operation.ReferenceOperationID != boundA.Operation.ID {
		t.Fatal("reference did not retain the original remote proof", boundA, boundB)
	}
	rule, e := g.SetChannelSharingRule(ctx, 751, 7, ChannelSharingRuleInput{Enabled: true, Methods: []string{"native"}, Items: []ChannelSharingRuleItem{{ReceiverID: a.ID, Mode: "rate", RateBPS: 1000}}}, 0, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 752, b.ID, "reference-unbind-b", actorB); e != nil || calls.Load() != 1 {
		t.Fatal("another owner's local release deleted the shared relation", e, calls.Load())
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, a.ID, "reference-protected-a", channelSharingActor); !errors.Is(e, ErrState) || calls.Load() != 1 {
		t.Fatal("active original policy lost its lease", e, calls.Load())
	}
	if _, e = s.DisableChannelSharingRule(ctx, 751, 7, rule.ID, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, a.ID, "reference-last-a", channelSharingActor); e != nil || calls.Load() != 2 {
		t.Fatal("last release must perform exactly one upstream delete", e, calls.Load())
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, a.ID, "reference-last-a", channelSharingActor); e != nil || calls.Load() != 2 {
		t.Fatal("replay deleted twice", e, calls.Load())
	}
}

func TestChannelSharingRelationReferencesConcurrentAliasesBindAndDeleteOnce(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	const n = 12
	ids := make([]int64, n)
	for i := range ids {
		v, e := g.CreateChannelSharingReceiver(ctx, 751, 7, fmt.Sprintf("lease-concurrent-%d", i), channelSharingReceiverInput, channelSharingActor)
		if e != nil {
			t.Fatal(e)
		}
		ids[i] = v.ID
	}
	var registries int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_relations").Scan(&registries); e != nil || registries != 0 {
		t.Fatal("drafts claimed a global relationship", registries, e)
	}
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id int64) {
			defer wg.Done()
			_, e := g.BindChannelSharingReceiver(ctx, 751, id, fmt.Sprintf("lease-bind-%d", i), channelSharingActor)
			if e != nil && !errors.Is(e, ErrState) {
				t.Error(e)
			}
		}(i, id)
	}
	wg.Wait()
	// Requests that encountered the original in-flight send were not claimed.
	// Explicitly repeating them after its successful proof can only add leases.
	for i, id := range ids {
		if _, e := g.BindChannelSharingReceiver(ctx, 751, id, fmt.Sprintf("lease-bind-%d", i), channelSharingActor); e != nil {
			t.Fatal(e)
		}
	}
	var live, remote int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM channel_sharing_relation_leases WHERE released_by_operation_id IS NULL),(SELECT count(*) FROM channel_sharing_receiver_operations WHERE source='remote')").Scan(&live, &remote); e != nil || live != n || remote != 1 || calls.Load() != 1 {
		t.Fatal(live, remote, calls.Load(), e)
	}
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id int64) {
			defer wg.Done()
			if _, e := g.UnbindChannelSharingReceiver(ctx, 751, id, fmt.Sprintf("lease-release-%d", i), channelSharingActor); e != nil {
				t.Error(e)
			}
		}(i, id)
	}
	wg.Wait()
	for i, id := range ids {
		if _, e := g.UnbindChannelSharingReceiver(ctx, 751, id, fmt.Sprintf("lease-release-%d", i), channelSharingActor); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM channel_sharing_relation_leases WHERE released_by_operation_id IS NULL),(SELECT count(*) FROM channel_sharing_receiver_operations WHERE source='remote')").Scan(&live, &remote); e != nil || live != 0 || remote != 2 || calls.Load() != 2 {
		t.Fatal("concurrent aliases deleted the shared relation more than once", live, remote, calls.Load(), e)
	}
}

func TestChannelSharingRelationReferencesFailedBindDoesNotReserveAnotherOwnersDraft(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	bad := channelSharingReceiverInput
	bad.Name = "Wrong Legal Name"
	a, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "lease-bad-name", bad, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	b, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "lease-correct-name", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	original := g.Providers[7].(*adapter.WechatV3)
	g.Providers[7] = &sharingReceiverErrorProvider{WechatV3: original, failure: &adapter.ProviderError{Code: "NO_AUTH"}}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, a.ID, "lease-bad-bind", channelSharingActor); e == nil {
		t.Fatal("definite provider failure accepted")
	}
	g.Providers[7] = original
	if _, e = g.BindChannelSharingReceiver(ctx, 751, b.ID, "lease-good-bind", channelSharingActor); e != nil || calls.Load() != 1 {
		t.Fatal("a rejected draft permanently occupied the physical relation", e, calls.Load())
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, a.ID, "lease-bad-name-alias", channelSharingActor); !errors.Is(e, ErrConflict) || calls.Load() != 1 {
		t.Fatal("reference pretended to validate a different name", e, calls.Load())
	}
	var source int64
	if e = s.pool.QueryRow(ctx, "SELECT source_receiver_id FROM channel_sharing_relations").Scan(&source); e != nil || source != b.ID {
		t.Fatal(source, e)
	}
}

func TestChannelSharingRelationReferencesUnknownAndLostAuditBlockEveryAlias(t *testing.T) {
	for _, mode := range []string{"unknown", "lost-result-audit"} {
		t.Run(mode, func(t *testing.T) {
			s, g, calls, lost := sharingReceiverFlowFixture(t)
			ctx := context.Background()
			a, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "lease-barrier-a", channelSharingReceiverInput, channelSharingActor)
			if e != nil {
				t.Fatal(e)
			}
			b, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "lease-barrier-b", channelSharingReceiverInput, channelSharingActor)
			if e != nil {
				t.Fatal(e)
			}
			original := g.Providers[7].(*adapter.WechatV3)
			if mode == "unknown" {
				lost.Store(true)
			} else {
				g.Providers[7] = &sharingReceiverErrorProvider{WechatV3: original, before: func() {
					_, e := s.pool.Exec(ctx, `CREATE FUNCTION reject_lease_result_audit_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='channel-sharing-receiver-operations' AND NEW.changes->>'state'='succeeded' THEN RAISE EXCEPTION 'injected lease result audit failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER reject_lease_result_audit_test BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_lease_result_audit_test()`)
					if e != nil {
						t.Error(e)
					}
				}}
			}
			if _, e = g.BindChannelSharingReceiver(ctx, 751, a.ID, "lease-barrier-original", channelSharingActor); e == nil || calls.Load() != 1 {
				t.Fatal(e, calls.Load())
			}
			lost.Store(false)
			g.Providers[7] = original
			for _, no := range []string{"lease-barrier-alias", "lease-barrier-another"} {
				if _, e = g.BindChannelSharingReceiver(ctx, 751, b.ID, no, channelSharingActor); !errors.Is(e, ErrState) || calls.Load() != 1 {
					t.Fatal("alias rearmed a globally uncertain send", e, calls.Load())
				}
			}
			if _, e = g.BindChannelSharingReceiver(ctx, 751, a.ID, "lease-barrier-original", channelSharingActor); e == nil || calls.Load() != 1 {
				t.Fatal("original unknown replay resent", e, calls.Load())
			}
			var state string
			if e = s.pool.QueryRow(ctx, "SELECT state FROM channel_sharing_relations").Scan(&state); e != nil || mode == "unknown" && state != "unknown" || mode != "unknown" && state != "binding" {
				t.Fatal(state, e)
			}
		})
	}
}

func TestChannelSharingRelationReferencesAuditFailureRollsBackLocalLease(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	a, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "lease-audit-a", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	b, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "lease-audit-b", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, a.ID, "lease-audit-bind-a", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION reject_local_lease_audit_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='channel-sharing-receiver-operations' AND NEW.changes->>'state'='succeeded' AND EXISTS(SELECT 1 FROM channel_sharing_receiver_operations op WHERE op.id=NEW.resource_id AND op.source='reference') THEN RAISE EXCEPTION 'injected local lease audit failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER reject_local_lease_audit_test BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_local_lease_audit_test()`); e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, b.ID, "lease-audit-bind-b", channelSharingActor); e == nil || calls.Load() != 1 {
		t.Fatal(e, calls.Load())
	}
	current, e := s.ChannelSharingReceiver(ctx, 751, b.ID, channelSharingActor)
	if e != nil || current.State != "draft" || current.Operation != nil {
		t.Fatal("local audit failure left a partial operation", current, e)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER reject_local_lease_audit_test ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, b.ID, "lease-audit-bind-b", channelSharingActor); e != nil || calls.Load() != 1 {
		t.Fatal("rolled-back local operation could not safely repeat", e, calls.Load())
	}
	var leases int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_relation_leases WHERE released_by_operation_id IS NULL").Scan(&leases); e != nil || leases != 2 {
		t.Fatal(leases, e)
	}
	if _, e = s.pool.Exec(ctx, "CREATE TRIGGER reject_local_lease_audit_test BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_local_lease_audit_test()"); e != nil {
		t.Fatal(e)
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, b.ID, "lease-audit-release-b", channelSharingActor); e == nil || calls.Load() != 1 {
		t.Fatal("unaudited local release committed", e, calls.Load())
	}
	current, e = s.ChannelSharingReceiver(ctx, 751, b.ID, channelSharingActor)
	if e != nil || current.State != "bound" || current.Operation == nil || current.Operation.Action != "bind" {
		t.Fatal("local release audit failure lost its live lease", current, e)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER reject_local_lease_audit_test ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, b.ID, "lease-audit-release-b", channelSharingActor); e != nil || calls.Load() != 1 {
		t.Fatal("rolled-back release was not safely repeatable", e, calls.Load())
	}
}

func TestChannelSharingRelationReferencesUnknownLastDeleteKeepsGlobalLeaseBarrier(t *testing.T) {
	s, g, calls, lost := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	a, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "lease-delete-a", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	b, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "lease-delete-b", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, a.ID, "lease-delete-bind-a", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, b.ID, "lease-delete-bind-b", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, b.ID, "lease-delete-release-b", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	lost.Store(true)
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, a.ID, "lease-delete-last-a", channelSharingActor); e == nil || calls.Load() != 2 {
		t.Fatal(e, calls.Load())
	}
	lost.Store(false)
	c, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "lease-delete-c", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, c.ID, "lease-delete-bypass-c", channelSharingActor); !errors.Is(e, ErrState) || calls.Load() != 2 {
		t.Fatal("uncertain delete allowed another alias to rebind", e, calls.Load())
	}
	var state string
	var leases int
	if e = s.pool.QueryRow(ctx, "SELECT state,(SELECT count(*) FROM channel_sharing_relation_leases WHERE released_by_operation_id IS NULL) FROM channel_sharing_relations").Scan(&state, &leases); e != nil || state != "unknown" || leases != 1 {
		t.Fatal("uncertain delete lost its original live lease", state, leases, e)
	}
}

func TestChannelSharingRelationReferencesDatabaseRejectsUnauditedReference(t *testing.T) {
	s, g, _, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	a, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "lease-sql-a", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	b, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "lease-sql-b", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	a, e = g.BindChannelSharingReceiver(ctx, 751, a.ID, "lease-sql-bind-a", channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE channel_sharing_receivers SET state='bound' WHERE id=$1", b.ID); e == nil {
		t.Fatal("bare SQL bound a receiver without an original operation")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var relation, op int64
	if e = tx.QueryRow(ctx, "SELECT id FROM channel_sharing_relations").Scan(&relation); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, "INSERT INTO channel_sharing_receiver_operations(receiver_id,action,out_no,external_no,request_hash,actor_role,actor_id,actor_name,relation_id,source,reference_operation_id) VALUES($1,'bind','PB000000000000000000000000000000','lease-forged-reference',decode(repeat('00',32),'hex'),'merchant',751,'sharing-owner',$2,'reference',$3) RETURNING id", b.ID, relation, a.Operation.ID).Scan(&op); e != nil {
		t.Fatal(e)
	}
	for _, command := range []string{
		"UPDATE channel_sharing_receiver_operations SET state='claimed',claimed_at=now() WHERE id=$1",
		"UPDATE channel_sharing_receivers SET state='binding',last_operation_id=$1 WHERE id=" + fmt.Sprint(b.ID),
		"UPDATE channel_sharing_receiver_operations SET state='succeeded' WHERE id=$1",
		"UPDATE channel_sharing_receivers SET state='bound' WHERE last_operation_id=$1",
	} {
		if _, e = tx.Exec(ctx, command, op); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = tx.Exec(ctx, "INSERT INTO channel_sharing_relation_leases(relation_id,receiver_id,bind_operation_id) VALUES($1,$2,$3)", relation, b.ID, op); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e == nil || !strings.Contains(e.Error(), "original actor audit") {
		t.Fatal("SQL manufactured an unaudited successful local reference", e)
	}
}
