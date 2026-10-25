package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type applicationAuthorizationAttemptTransport struct {
	t       *testing.T
	inner   http.RoundTripper
	checked bool
}

func (a *applicationAuthorizationAttemptTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	a.checked = true
	if r.GetBody != nil || r.Header.Get("Idempotency-Key") != "" || r.Header.Get("X-Idempotency-Key") != "" {
		a.t.Error("application authorization body can be replayed inside HTTP transport")
	}
	return a.inner.RoundTrip(r)
}
func TestApplicationAuthorizationDisablesTransportBodyReplay(t *testing.T) {
	key := testKey(t)
	calls := 0
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{"alipay_open_auth_token_app_response": json.RawMessage(appAuthorizationResponse), "sign": independentSignature(t, appAuthorizationResponse, key)})
	}))
	defer up.Close()
	transport := &applicationAuthorizationAttemptTransport{t: t, inner: oauthTLSRoundTripper{t, up, "openapi.alipay.com"}}
	a, err := NewAlipayApplicationAuthorization(AlipayConfig{AppID: appAuthorizationIssuer, PrivateKey: key, PublicKey: &key.PublicKey, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.ExchangeApplicationAuthorization(context.Background(), ApplicationAuthorizationExchange{Code: "one-original-app-code", ExpectedAuthAppID: appAuthorizationTarget}); err != nil || !transport.checked || calls != 1 {
		t.Fatal("expected one protected transport attempt", err, transport.checked, calls)
	}
}
