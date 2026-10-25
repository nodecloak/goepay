package payment

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestChannelSharingRulesRequireBoundRecipientsAndImmutableVersionCAS(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	receiver, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "rule-receiver", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	input := ChannelSharingRuleInput{Enabled: true, MinimumAmount: 100, Methods: []string{"native"}, Items: []ChannelSharingRuleItem{{ReceiverID: receiver.ID, Mode: "rate", RateBPS: 1000}}}
	if _, e = g.SetChannelSharingRule(ctx, 751, 7, input, 0, channelSharingActor); !errors.Is(e, ErrState) {
		t.Fatal("unbound recipient enabled a rule", e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, receiver.ID, "rule-bind", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	first, e := g.SetChannelSharingRule(ctx, 751, 7, input, 0, channelSharingActor)
	if e != nil || first.ID <= 0 || first.Revision != 1 || !first.Enabled || first.RefundMode != "merchant_funded" || !first.AutoFinish {
		t.Fatal(first, e)
	}
	same, e := g.SetChannelSharingRule(ctx, 751, 7, input, 0, channelSharingActor)
	if e != nil || same.ID != first.ID {
		t.Fatal("lost rule response created another version", same, e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, minimum := range []int64{101, 102} {
		wg.Add(1)
		go func(minimum int64) {
			defer wg.Done()
			changed := input
			changed.MinimumAmount = minimum
			_, e := g.SetChannelSharingRule(ctx, 751, 7, changed, first.ID, channelSharingActor)
			results <- e
		}(minimum)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("concurrent configuration lost a CAS", success, conflicts)
	}
	original, e := s.ChannelSharingRuleVersion(ctx, 751, first.ID, channelSharingActor)
	if e != nil || original.MinimumAmount != 100 || original.Revision != 1 {
		t.Fatal("current policy overwrote original version", original, e)
	}
	current, e := s.ChannelSharingRule(ctx, 751, 7, channelSharingActor)
	if e != nil || current.Revision != 2 || current.ID == first.ID || calls.Load() != 1 {
		t.Fatal(current, e, calls.Load())
	}
	if _, e = s.pool.Exec(ctx, "UPDATE channel_sharing_rule_items SET rate_bps=5000 WHERE rule_id=$1", first.ID); e == nil {
		t.Fatal("old recipient percentage was mutable")
	}
	if _, e = s.ChannelSharingRule(ctx, 751, 7, TransferBatchActor{Role: "merchant", ID: 752, Name: "other"}); e == nil {
		t.Fatal("other merchant read source policy")
	}
}
func TestChannelSharingActiveRuleBlocksRemoteUnbindUntilVersionedDisable(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	receiver, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "active-rule-receiver", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, receiver.ID, "active-rule-bind", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	rule, e := g.SetChannelSharingRule(ctx, 751, 7, ChannelSharingRuleInput{Enabled: true, Methods: []string{"native"}, Items: []ChannelSharingRuleItem{{ReceiverID: receiver.ID, Mode: "rate", RateBPS: 1000}}}, 0, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, receiver.ID, "blocked-rule-unbind", channelSharingActor); !errors.Is(e, ErrState) || calls.Load() != 1 {
		t.Fatal("enabled rule's remote relation was removed", e, calls.Load())
	}
	disabled, e := s.DisableChannelSharingRule(ctx, 751, 7, rule.ID, channelSharingActor)
	if e != nil || disabled.Enabled || disabled.Revision != 2 {
		t.Fatal(disabled, e)
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, receiver.ID, "allowed-rule-unbind", channelSharingActor); e != nil || calls.Load() != 2 {
		t.Fatal(e, calls.Load())
	}
	if _, e = g.SetChannelSharingRule(ctx, 751, 7, ChannelSharingRuleInput{Enabled: true, Methods: []string{"native"}, Items: rule.Items}, disabled.ID, channelSharingActor); !errors.Is(e, ErrState) {
		t.Fatal("unbound relation was re-enabled", e)
	}
}

func TestChannelSharingRuleDatabaseCompletenessAndAuditRollback(t *testing.T) {
	s, g, _, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	receiver, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "guard-rule-receiver", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, receiver.ID, "guard-rule-bind", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	input := ChannelSharingRuleInput{Enabled: true, Methods: []string{"native"}, Items: []ChannelSharingRuleItem{{ReceiverID: receiver.ID, Mode: "rate", RateBPS: 1000}}}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION reject_rule_audit_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='channel-sharing-rules' THEN RAISE EXCEPTION 'injected rule audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_rule_audit_test BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_rule_audit_test()`); e != nil {
		t.Fatal(e)
	}
	if _, e = g.SetChannelSharingRule(ctx, 751, 7, input, 0, channelSharingActor); e == nil {
		t.Fatal("rule committed without audit")
	}
	var versions, pointers int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM channel_sharing_rules),(SELECT count(*) FROM channel_sharing_rule_current)").Scan(&versions, &pointers); e != nil || versions != 0 || pointers != 0 {
		t.Fatal(versions, pointers, e)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER reject_rule_audit_test ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	rule, e := g.SetChannelSharingRule(ctx, 751, 7, input, 0, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	// A new immutable parent cannot be committed without all original children.
	if _, e = s.pool.Exec(ctx, `INSERT INTO channel_sharing_rules(merchant_id,channel_id,revision,provider,app_id,product,account_hash,input_hash,enabled,minimum_amount,methods,refund_mode,auto_finish,receiver_count) SELECT merchant_id,channel_id,revision+1,provider,app_id,product,account_hash,input_hash,enabled,minimum_amount,methods,refund_mode,auto_finish,receiver_count FROM channel_sharing_rules WHERE id=$1`, rule.ID); e == nil {
		t.Fatal("incomplete rule committed")
	}
	if _, e = s.pool.Exec(ctx, "DELETE FROM channel_sharing_rule_current WHERE merchant_id=751"); e == nil {
		t.Fatal("current rule deletion erased history")
	}
	// Disabling remains available after the runtime channel has disappeared.
	delete(g.Providers, 7)
	disabled, e := s.DisableChannelSharingRule(ctx, 751, 7, rule.ID, channelSharingActor)
	if e != nil || disabled.Enabled {
		t.Fatal(disabled, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE channel_sharing_rule_current SET rule_id=$1 WHERE merchant_id=751", rule.ID); e == nil {
		t.Fatal("current pointer moved back to an enabled old version")
	}
}
