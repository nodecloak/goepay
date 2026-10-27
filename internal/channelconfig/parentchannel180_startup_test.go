package channelconfig

import "testing"

func TestParentChannel180StartupUsesAuthenticatedCurrentAccountBeforeValidatingExpiredBootstrap(t *testing.T) {
	c := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "startup-app", "appmchid": "startup-mch", "appkey": "0123456789abcdef0123456789abcdef"}}
	f, e := NewParentChannelFactory(c, ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	_, raw, e := f.BootstrapSnapshot(7)
	if e != nil {
		t.Fatal(e)
	}
	stale := cloneParentChannel(c)
	stale.Config["appkey"] = ""
	imp := Import{Version: 1, Channels: []Channel{stale}}
	if _, e = BuildWithOptions(imp, BuildOptions{}); e == nil {
		t.Fatal("invalid bootstrap fixture")
	}
	resolved, e := WithParentStartupAccounts(imp, map[int64][]byte{7: raw})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = BuildWithOptions(resolved, BuildOptions{}); e != nil {
		t.Fatal("current account did not replace stale bootstrap before construction", e)
	}
	if imp.Channels[0].Config["appkey"] != "" {
		t.Fatal("caller import mutated")
	}
	for _, bad := range []map[int64][]byte{{8: raw}, {7: append(append([]byte{}, raw...), []byte(" {}")...)}, {7: []byte(`{"version":1,"channel":{"id":7,"plugin":"alipay"}}`)}} {
		if _, e = WithParentStartupAccounts(imp, bad); e == nil {
			t.Fatal("untrusted/cross-family startup source accepted")
		}
	}
}
