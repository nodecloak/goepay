package channelconfig

import (
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"strings"
	"testing"
)

func TestParentChannel180FactoryUsesRealConstructorsAndImmutableSnapshot(t *testing.T) {
	origin := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "synthetic180-app", "appmchid": "synthetic180-merchant-A", "appkey": "0123456789abcdef0123456789abcdef"}}
	factory, e := NewParentChannelFactory(origin, ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	origin.Config["appkey"] = "corrupt-external-map"
	original := factory.Bootstrap()
	if original.Config["appkey"] != "0123456789abcdef0123456789abcdef" {
		t.Fatal("factory retained caller-owned config")
	}
	next := factory.Bootstrap()
	next.Config["appmchid"] = "synthetic180-merchant-B"
	execution, raw, e := factory.BuildSnapshot(9007199254740993, next)
	if e != nil {
		t.Fatal(e)
	}
	before, _, e := factory.BuildSnapshot(7, original)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := adapter.TransferAccountScope(before.Payment)
	b, _ := adapter.TransferAccountScope(execution.Payment)
	if a == b || a == "" || b == "" {
		t.Fatal("config did not reach actual provider account")
	}
	restored, e := factory.Restore(9007199254740993, raw)
	if e != nil {
		t.Fatal(e)
	}
	scope, _ := adapter.TransferAccountScope(restored.Payment)
	if scope != b {
		t.Fatal("immutable account was replaced by current factory")
	}
	out, _ := json.Marshal(execution)
	if string(out) != "{}" {
		t.Fatal("execution serialized private provider")
	}
	if _, e = factory.Restore(7, raw); e == nil {
		t.Fatal("snapshot replayed across parent IDs")
	}
}
func TestParentChannel180FactoryRejectsUndeclaredFieldsAndDirectSettlement(t *testing.T) {
	origin := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "synthetic180-app", "appmchid": "synthetic180-merchant", "appkey": "0123456789abcdef0123456789abcdef"}}
	factory, e := NewParentChannelFactory(origin, ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"gateway_url", "provider", "merchant_id", "private_key_path", "payfee_lessthan"} {
		c := factory.Bootstrap()
		c.Config[key] = "http://external.invalid/or-path"
		if _, _, e = factory.BuildSnapshot(9, c); e == nil {
			t.Errorf("unconsumed or unsafe field admitted: %s", key)
		}
	}
	c := factory.Bootstrap()
	one := 1
	c.Mode = &one
	if _, _, e = factory.BuildSnapshot(9, c); e == nil {
		t.Fatal("mode1 executed as platform collection")
	}
	c = factory.Bootstrap()
	c.Certificates = map[string]string{"sslkey_path": "C:/private/imported/real.key"}
	if _, _, e = factory.BuildSnapshot(9, c); e == nil {
		t.Fatal("certificate path treated as contents")
	}
}
func TestParentChannel180FactoryFreshSecretsAreExplicitAndReadProjectionNeverEchoesThem(t *testing.T) {
	origin := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "synthetic180-origin", "appmchid": "synthetic180-old", "appkey": "0123456789abcdef0123456789abcdef"}}
	factory, e := NewParentChannelFactory(origin, ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	products := []string{"1"}
	replace := func(v string) adapter.ParentChannelValueEdit {
		return adapter.ParentChannelValueEdit{Operation: "replace", Value: &v}
	}
	in := adapter.ParentChannelAccountChange{AppTypes: &products, Config: map[string]adapter.ParentChannelValueEdit{"appid": replace("synthetic180-fresh"), "appmchid": replace("synthetic180-new")}}
	if _, _, e = factory.NewSnapshot(9, in); e == nil {
		t.Fatal("fresh parent silently copied template secret")
	}
	in.Config["appkey"] = replace("abcdef0123456789abcdef0123456789")
	execution, raw, e := factory.NewSnapshot(9, in)
	if e != nil {
		t.Fatal(e)
	}
	view, e := factory.Describe(9, raw)
	if e != nil {
		t.Fatal(e)
	}
	public, _ := json.Marshal(view)
	if strings.Contains(string(public), "abcdef0123456789abcdef0123456789") {
		t.Fatal("private API key leaked in projection")
	}
	changed := adapter.ParentChannelAccountChange{Config: map[string]adapter.ParentChannelValueEdit{"appmchid": replace("synthetic180-newer")}}
	next, _, e := factory.ChangeSnapshot(9, raw, changed)
	if e != nil {
		t.Fatal(e)
	}
	oldScope, _ := adapter.TransferAccountScope(execution.Payment)
	newScope, _ := adapter.TransferAccountScope(next.Payment)
	if oldScope == newScope {
		t.Fatal("changed parent ignored account")
	}
	copy, copyRaw, e := factory.CopySnapshot(9, 10, raw, adapter.ParentChannelAccountChange{})
	if e != nil {
		t.Fatal(e)
	}
	copyScope, _ := adapter.TransferAccountScope(copy.Payment)
	if copyScope != oldScope {
		t.Fatal("copy did not preserve original credential server-side")
	}
	if _, e = factory.Restore(9, copyRaw); e == nil {
		t.Fatal("copied account version retained old ID")
	}
}
func TestParentChannel180FactoryRejectsUnknownKeepAndClearOperations(t *testing.T) {
	origin := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "synthetic180-app", "appmchid": "synthetic180-old", "appkey": "0123456789abcdef0123456789abcdef"}}
	factory, e := NewParentChannelFactory(origin, ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	_, raw, e := factory.BootstrapSnapshot(7)
	if e != nil {
		t.Fatal(e)
	}
	for _, op := range []string{"keep", "clear"} {
		if _, _, e = factory.ChangeSnapshot(7, raw, adapter.ParentChannelAccountChange{Config: map[string]adapter.ParentChannelValueEdit{"payfee_lessthan": {Operation: op}}}); e == nil {
			t.Errorf("unknown %s was silently accepted without a consumer", op)
		}
	}
}
