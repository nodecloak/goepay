package payment

import (
	"errors"
	"testing"
)

func TestManagedWeixinDependenciesPreserveKnownLegacyChildPermissionAnchor(t *testing.T) {
	// This is the same private constructor called only after the authenticated
	// pre103 child route and current parent fee commitment have been verified.
	head := parentChannelHead{id: 7, source: 7, version: 2, kind: "wxpay"}
	known := legacyChildAuthorityBinding(head, 1, func() bool { return false })
	flow := ProviderFlow{parentBindings: map[int64]*parentExecutionBinding{7: known}}
	if ids, e := flow.ManagedWeixinApplicationDependencies("wxpay"); e != nil || len(ids) != 0 {
		t.Errorf("known legacy child authority required invented parent credential history: %v %v", ids, e)
	}
	unknown := &parentExecutionBinding{account: parentChannelAccount{id: 7, source: 7, version: 2, kind: "wxpay"}, merchant: 1}
	flow.parentBindings[7] = unknown
	if _, e := flow.ManagedWeixinApplicationDependencies("wxpay"); !errors.Is(e, ErrConflict) {
		t.Error("an unknown empty parent source was assumed unmanaged", e)
	}
}
