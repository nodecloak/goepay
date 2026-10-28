package backoffice

import (
	"context"
	"fmt"
	"testing"
)

func TestProxySettingsAreRegisteredAndValidated(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	// Registration: each key saves once through the settings resource.
	for _, pair := range [][2]string{{"proxy", "1"}, {"proxy_server", "proxy.example.net"}, {"proxy_port", "8080"}, {"proxy_user", "agent"}, {"proxy_type", "sock5h"}, {"proxy_apiurl", "https://relay.example.net/sub"}} {
		if _, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); e != nil {
			t.Fatal(pair, e)
		}
	}
	for _, pair := range [][2]string{{"proxy", "0"}, {"proxy", "2"}, {"proxy_type", "http"}, {"proxy_type", "https"}, {"proxy_type", "sock4"}, {"proxy_type", "sock5"}, {"proxy_port", "65535"}} {
		if known, e := validateProxySetting(pair[0], pair[1]); !known || e != nil {
			t.Fatal("rejected", pair, e)
		}
	}
	for _, pair := range [][2]string{{"proxy", "3"}, {"proxy", "true"}, {"proxy_type", "socks5"}, {"proxy_port", "0"}, {"proxy_port", "70000"}, {"proxy_port", "8080a"}, {"proxy_server", "has space"}, {"proxy_server", ""}, {"proxy_apiurl", "ftp://relay.example.net"}, {"proxy_apiurl", "relay.example.net"}} {
		if known, e := validateProxySetting(pair[0], pair[1]); !known || e == nil {
			t.Fatal("accepted", pair)
		}
		if _, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); e == nil {
			t.Fatal("invalid proxy setting accepted via settings resource", pair)
		}
	}
	if known, _ := validateProxySetting("transfer_alipay", "x"); known {
		t.Fatal("claimed an unrelated key")
	}
}
