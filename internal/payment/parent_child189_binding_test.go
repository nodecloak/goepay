package payment

import (
	"bytes"
	adapter "goepay/internal/adapters/payment"
	"testing"
)

func TestParentChild189ExecutionKeepsChildAndParentFeeAuthoritySeparate(t *testing.T) {
	child, parent := &flowProvider{}, &flowProvider{}
	fee := RuntimeChannel{id: 7, kind: "wxpay", identity: bytes.Repeat([]byte{1}, 32), child: &ChildExecutionBinding{child: 9, parent: 7}}
	binding := &parentExecutionBinding{fee: RuntimeChannel{id: 7, kind: "wxpay", identity: bytes.Repeat([]byte{2}, 32)}, account: parentChannelAccount{id: 7, kind: "wxpay"}}
	g := ProviderFlow{Providers: map[int64]adapter.Provider{7: child}, boundParents: map[int64]RuntimeChannel{7: fee}, parentBindings: map[int64]*parentExecutionBinding{7: binding}}
	b, err := g.RuntimeChannel(7)
	if err != nil || b.child == nil || b.parent != binding || !bytes.Equal(b.identity, fee.identity) {
		t.Fatal("parent authority replaced original child fee commitment", err)
	}
	restored := g.withParentExecution(binding, adapter.ParentChannelExecution{Payment: parent})
	if restored.Providers[7] != child {
		t.Fatal("parent restoration replaced original child SDK")
	}
	selected, err := restored.SelectParentPayment(1, 7, "native", false)
	if err != nil || selected.Providers[7] != child {
		t.Fatal("parent selection replaced original child SDK", err)
	}
}
