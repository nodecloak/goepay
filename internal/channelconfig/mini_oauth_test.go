package channelconfig

import "testing"

func TestBuildMiniOAuthRequiresExplicitProductSecretAndTrustedPath(t *testing.T) {
	c := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"2"}, Config: map[string]string{"appid": "wxapp", "mini_secret": "synthetic-mini-secret", "mini_path": "pages/pay/pay"}}
	p, e := BuildMiniOAuth(Import{Version: 1, Channels: []Channel{c}})
	if e != nil || len(p) != 0 {
		t.Fatal("mini enabled by default", e)
	}
	c.Config["enable_mini_launch"] = "true"
	p, e = BuildMiniOAuth(Import{Version: 1, Channels: []Channel{c}})
	if e != nil || p[7].ApplicationID() != "wxapp" {
		t.Fatal(e)
	}
	for _, field := range []string{"mini_secret", "mini_path"} {
		old := c.Config[field]
		c.Config[field] = ""
		if _, e = BuildMiniOAuth(Import{Version: 1, Channels: []Channel{c}}); e == nil {
			t.Fatal("missing mini config accepted", field)
		}
		c.Config[field] = old
	}
	c.Config["mini_appid"] = "different"
	if _, e = BuildMiniOAuth(Import{Version: 1, Channels: []Channel{c}}); e == nil {
		t.Fatal("foreign application accepted")
	}
	delete(c.Config, "mini_appid")
	c.AppTypes = []string{"1"}
	if _, e = BuildMiniOAuth(Import{Version: 1, Channels: []Channel{c}}); e == nil {
		t.Fatal("native-only mini exchange accepted")
	}
	cert := certificateChannel(t)
	cert.Config["enable_mini_launch"] = "true"
	cert.Config["mini_path"] = "pages/pay/pay"
	cert.AppTypes = []string{"7"}
	p, e = BuildMiniOAuth(Import{Version: 1, Channels: []Channel{cert}})
	if e != nil || p[cert.ID].ApplicationID() != cert.Config["appid"] {
		t.Fatal("certificate mini exchange unavailable", e)
	}
	cert.AppTypes = []string{"10"}
	if _, e = BuildMiniOAuth(Import{Version: 1, Channels: []Channel{cert}}); e == nil {
		t.Fatal("preauth mini exchange accepted")
	}
}
