package channelconfig

import (
	"testing"
)

func TestBuildOAuthExplicitAppBoundAndCertificateTrust(t *testing.T) {
	c := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"2"}, Config: map[string]string{"appid": "wxapp", "oauth_appsecret": "synthetic-secret"}}
	providers, e := BuildOAuth(Import{Version: 1, Channels: []Channel{c}})
	if e != nil || len(providers) != 0 {
		t.Fatal("OAuth silently enabled", providers, e)
	}
	c.Config["enable_web_oauth"] = "true"
	providers, e = BuildOAuth(Import{Version: 1, Channels: []Channel{c}})
	if e != nil || providers[7].ApplicationID() != "wxapp" {
		t.Fatal(providers, e)
	}
	c.Config["oauth_appsecret"] = ""
	if _, e = BuildOAuth(Import{Version: 1, Channels: []Channel{c}}); e == nil {
		t.Fatal("missing OAuth secret accepted")
	}
	cert := certificateChannel(t)
	cert.Config["enable_web_oauth"] = "true"
	cert.AppTypes = append(cert.AppTypes, "4")
	providers, e = BuildOAuth(Import{Version: 1, Channels: []Channel{cert}})
	if e != nil || providers[cert.ID].ApplicationID() != cert.Config["appid"] {
		t.Fatal("certificate OAuth unavailable", providers, e)
	}
	delete(cert.Certificates, "alipay_cert_path")
	if _, e = BuildOAuth(Import{Version: 1, Channels: []Channel{cert}}); e == nil {
		t.Fatal("untrusted certificate OAuth accepted")
	}
}
