package server

import (
	"testing"

	"goepay/internal/proxyconfig"
)

func proxySet(t *testing.T, f channel168Fixture, key, value string) {
	t.Helper()
	tag, err := f.p.Exec(t.Context(), "UPDATE backoffice_records SET body=jsonb_build_object('key',$1::text,'value',$2::text) WHERE resource='settings' AND body->>'key'=$1", key, value)
	if err != nil {
		t.Fatal(err)
	}
	if tag.RowsAffected() > 0 {
		return
	}
	if _, err = f.p.Exec(t.Context(), "INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))", key, value); err != nil {
		t.Fatal(err)
	}
}

func TestProxyPlanReadsSettingsAndFailsClosed(t *testing.T) {
	f := channel168Local(t)
	store, err := f.s.proxyCredentialStore()
	if err != nil {
		t.Fatal(err)
	}
	// Mode 0 / unset: a direct plan, no secrets needed.
	plan, err := store.Plan(t.Context())
	if err != nil || plan.Mode != 0 {
		t.Fatalf("unset proxy should be direct: %+v %v", plan, err)
	}
	// Mode 1 but no server yet -> fail closed, never silent direct.
	proxySet(t, f, "proxy", "1")
	if _, err = store.Plan(t.Context()); err != proxyconfig.ErrIncomplete {
		t.Fatalf("mode 1 without a server must fail closed: %v", err)
	}
	proxySet(t, f, "proxy_type", "sock5")
	proxySet(t, f, "proxy_server", "proxy.example.net")
	proxySet(t, f, "proxy_port", "1080")
	proxySet(t, f, "proxy_user", "agent")
	// Store the password through the console and expect Plan to resolve it.
	empty := proxyCredView(t, proxyCredCall(f, f.token, f.csrf, "GET", ""))
	proxyCredView(t, proxyCredCall(f, f.token, f.csrf, "PATCH", `{"password":"relay-pwd","expected_fingerprint":"`+empty.Fingerprint+`"}`))
	plan, err = store.Plan(t.Context())
	if err != nil || plan.Mode != 1 || plan.Type != "sock5" || plan.Server != "proxy.example.net" || plan.Port != "1080" || plan.User != "agent" || plan.Password != "relay-pwd" {
		t.Fatalf("mode 1 plan wrong: %+v %v", plan, err)
	}
	// Mode 2 without an API url/key -> fail closed.
	proxySet(t, f, "proxy", "2")
	if _, err = store.Plan(t.Context()); err != proxyconfig.ErrIncomplete {
		t.Fatalf("mode 2 without api config must fail closed: %v", err)
	}
	proxySet(t, f, "proxy_apiurl", "https://relay.example.net/sub")
	view := proxyCredView(t, proxyCredCall(f, f.token, f.csrf, "GET", ""))
	proxyCredView(t, proxyCredCall(f, f.token, f.csrf, "PATCH", `{"apikey":"AbCdEf2345xyzKMNPQ78","expected_fingerprint":"`+view.Fingerprint+`"}`))
	plan, err = store.Plan(t.Context())
	if err != nil || plan.Mode != 2 || plan.APIURL != "https://relay.example.net/sub" || plan.APIKey != "AbCdEf2345xyzKMNPQ78" {
		t.Fatalf("mode 2 plan wrong: %+v %v", plan, err)
	}
}
