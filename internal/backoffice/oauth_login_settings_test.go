package backoffice

import (
	"context"
	"fmt"
	"testing"
)

func TestOAuthLoginSettingsAreRegisteredAndValidated(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	// Registration: each non-secret key saves once through the settings resource.
	for _, pair := range [][2]string{{"login_qq", "1"}, {"login_qq_appid", "100200300"}, {"login_wx", "0"}, {"login_alipay", "1"}, {"login_appid", "2016app"}, {"login_apiurl", "https://relay.example.net/oauth"}, {"login_tg", "1"}} {
		if _, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); e != nil {
			t.Fatal(pair, e)
		}
	}
	for _, pair := range [][2]string{{"login_qq", "0"}, {"login_wx", "1"}, {"login_tg", "0"}, {"login_qq_appid", "abc_123"}, {"login_apiurl", "http://relay.example.net"}} {
		if known, e := validateOAuthLoginSetting(pair[0], pair[1]); !known || e != nil {
			t.Fatal("rejected", pair, e)
		}
	}
	for _, pair := range [][2]string{{"login_qq", "2"}, {"login_qq", "true"}, {"login_wx", ""}, {"login_qq_appid", ""}, {"login_qq_appid", "has space"}, {"login_apiurl", "ftp://relay.example.net"}, {"login_apiurl", "relay.example.net"}} {
		if known, e := validateOAuthLoginSetting(pair[0], pair[1]); !known || e == nil {
			t.Fatal("accepted", pair)
		}
		if _, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); e == nil {
			t.Fatal("invalid oauth setting accepted via settings resource", pair)
		}
	}
	// The secret keys (appkey / bot token) must NOT be writable as plaintext
	// settings: they are not in the schema's key options at all.
	for _, secret := range []string{"login_qq_appkey", "login_appkey", "login_tg_bot_token"} {
		if _, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":"x"}`, secret))); e == nil {
			t.Fatal("secret oauth key accepted as plaintext setting", secret)
		}
		if known, _ := validateOAuthLoginSetting(secret, "x"); known {
			t.Fatal("oauth validator claimed a secret key", secret)
		}
	}
	if known, _ := validateOAuthLoginSetting("transfer_alipay", "x"); known {
		t.Fatal("claimed an unrelated key")
	}
}
