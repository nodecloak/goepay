package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestChannelSharingConfigurationGrantIsAtomicAndReplayDoesNotConsumeAgain(t *testing.T) {
	f := newSharingExecutionFixture(t)
	f.paid(t, "sharing-config-grants", false)
	ctx := context.Background()
	rule, e := f.Store.ChannelSharingRule(ctx, 751, 7, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	var kinds, keys []string
	deny := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, key string) error {
		kinds = append(kinds, kind)
		keys = append(keys, key)
		return ErrInvalid
	})
	if _, e = f.Store.DisableChannelSharingRule(deny, 751, 7, rule.ID, channelSharingActor); !errors.Is(e, ErrInvalid) {
		t.Fatal("rule disable bypassed funds authorization", e)
	}
	current, e := f.Store.ChannelSharingRule(ctx, 751, 7, channelSharingActor)
	if e != nil || current.ID != rule.ID || !current.Enabled {
		t.Fatal(current, e)
	}
	if len(kinds) != 1 || kinds[0] != "channel-sharing-disable" || keys[0] != fmt.Sprintf("751:7:%d", rule.ID) {
		t.Fatal(kinds, keys)
	}
	input := rule.ChannelSharingRuleInput
	input.MinimumAmount++
	if _, e = f.Flow.SetChannelSharingRule(deny, 751, 7, input, rule.ID, channelSharingActor); !errors.Is(e, ErrInvalid) {
		t.Fatal("rule update bypassed funds authorization", e)
	}
	alias, e := f.Flow.CreateChannelSharingReceiver(ctx, 751, 7, "grant-alias", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.BindChannelSharingReceiver(deny, 751, alias.ID, "grant-bind", channelSharingActor); !errors.Is(e, ErrInvalid) {
		t.Fatal("reference bind bypassed funds authorization", e)
	}
	view, e := f.Store.ChannelSharingReceiver(ctx, 751, alias.ID, channelSharingActor)
	if e != nil || view.State != "draft" || view.Operation != nil {
		t.Fatal("denied grant left a relationship lease", view, e)
	}
	var consumed int
	allow := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, key string) error {
		if kind != "channel-sharing-bind" || key != fmt.Sprintf("751:%d:grant-bind", alias.ID) {
			return ErrInvalid
		}
		consumed++
		return nil
	})
	if _, e = f.Flow.BindChannelSharingReceiver(allow, 751, alias.ID, "grant-bind", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.BindChannelSharingReceiver(allow, 751, alias.ID, "grant-bind", channelSharingActor); e != nil || consumed != 1 {
		t.Fatal("replay consumed another grant", e, consumed)
	}
}
