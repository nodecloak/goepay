package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/cryptopay"
	"goepay/internal/identity"
)

func TestCryptoNetworkConsoleIsAvailableBeforeWalletProvisioning(t *testing.T) {
	f := channel168Local(t)
	f.s.Crypto = nil
	items := readCryptoNetworkControls(t, f)
	seen := map[string]bool{}
	for _, item := range items {
		if item.Enabled || item.EffectiveEnabled || item.ReadyToEnable || item.WalletsConfigured || item.ListenerReady || item.DepositAddressAvailable || item.Revision != 1 {
			t.Fatalf("unconfigured network has a misleading initial state: %+v", item)
		}
		seen[item.Network] = true
	}
	if !seen["tron"] || !seen["ethereum"] {
		t.Fatalf("missing configured network names: %+v", seen)
	}
}

func readCryptoNetworkControls(t *testing.T, f channel168Fixture) []cryptopay.NetworkControl {
	t.Helper()
	w := cryptoChannelHTTP(f, http.MethodGet, "/crypto/networks", "")
	if w.Code != http.StatusOK {
		t.Fatalf("read network controls: HTTP %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("network controls must reflect the current readiness")
	}
	var result struct {
		Code int
		Data struct{ Items []cryptopay.NetworkControl }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Code != 0 || len(result.Data.Items) != 2 {
		t.Fatalf("invalid network list: %s (%v)", w.Body.String(), err)
	}
	return result.Data.Items
}

func TestCryptoNetworkConsoleRequiresExactInputAndOriginalSuperSession(t *testing.T) {
	f := email197Local(t)
	admin := f.channel168Fixture
	raw := `{"enabled":true,"expected_revision":"1","current_password":"test-password"}`
	for _, body := range []string{
		`{"enabled":false,"expected_revision":"1"}`,
		`{"enabled":false,"expected_revision":"1","current_password":null}`,
		strings.Replace(raw, `"enabled":true`, `"enabled":null`, 1),
		strings.Replace(raw, `"enabled":true`, `"enabled":false,"enabled":true`, 1),
		strings.Replace(raw, `"enabled":true`, `"Enabled":true`, 1),
		strings.Replace(raw, `"expected_revision":"1"`, `"expected_revision":"0"`, 1),
		strings.Replace(raw, `"expected_revision":"1"`, `"expected_revision":"01"`, 1),
		strings.Replace(raw, `"expected_revision":"1"`, `"expected_revision":1`, 1),
		strings.Replace(raw, `"enabled":true`, `"enabled":true,"wallet_address":"0x1"`, 1),
		strings.Replace(raw, `"enabled":true`, `"enabled":true,"enable_broadcast":true`, 1),
	} {
		if w := cryptoChannelHTTP(admin, http.MethodPatch, "/crypto/networks/tron", body); w.Code != 400 {
			t.Fatal("network switch accepted invalid body", w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct {
		method, path, allow string
		status              int
	}{
		{http.MethodPatch, "/crypto/networks/unknown", "", 404},
		{http.MethodPatch, "/crypto/networks/tron?enabled=true", "", 400},
		{http.MethodGet, "/crypto/networks?merchant_id=1", "", 400},
		{http.MethodPost, "/crypto/networks", http.MethodGet, 405},
		{http.MethodGet, "/crypto/networks/ethereum", http.MethodPatch, 405},
	} {
		w := cryptoChannelHTTP(admin, tc.method, tc.path, raw)
		if w.Code != tc.status || w.Header().Get("Allow") != tc.allow {
			t.Fatalf("network endpoint accepted unexpected method, path or query: %s %s => %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	withoutCSRF := admin
	withoutCSRF.csrf = "wrong"
	if w := cryptoChannelHTTP(withoutCSRF, http.MethodPatch, "/crypto/networks/tron", raw); w.Code != 403 {
		t.Fatal("network switch accepted missing original CSRF", w.Code, w.Body.String())
	}
	path := "/console/v1/merchant/payment-channels/crypto/networks"
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 403 && w.Code != 404 {
		t.Fatal("merchant read an administrator-only network switch", w.Code, w.Body.String())
	}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class,permissions)
		SELECT 'network-operator','admin',170,'Network operator',password_hash,'operator','[]'::jsonb FROM console_users WHERE role='admin' LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	operator := admin
	var err error
	operator.token, operator.csrf, err = f.s.Sessions.Create(identity.User{ID: 170, Username: "network-operator", Role: "admin", AdminClass: "operator", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		path := "/crypto/networks"
		if method == http.MethodPatch {
			path += "/tron"
		}
		if w := cryptoChannelHTTP(operator, method, path, raw); w.Code != 403 {
			t.Fatal("operator could access network controls", w.Code, w.Body.String())
		}
	}
	var changed int
	if err := f.p.QueryRow(t.Context(), "SELECT count(*) FROM crypto_network_controls WHERE enabled OR revision<>1").Scan(&changed); err != nil || changed != 0 {
		t.Fatalf("rejected requests changed network state: %d %v", changed, err)
	}
	if _, err := f.p.Exec(t.Context(), "ALTER TABLE crypto_listener_health RENAME COLUMN checked_at TO missing_checked_at"); err != nil {
		t.Fatal(err)
	}
	if w := cryptoChannelHTTP(admin, http.MethodGet, "/crypto/networks", ""); w.Code != 503 || !strings.Contains(w.Body.String(), `"data":null`) {
		t.Fatal("incomplete service schema was presented as a verified state", w.Code, w.Body.String())
	}
}

func TestCryptoNetworkConsoleChecksReadinessPasswordAndRevision(t *testing.T) {
	f := channel168Local(t)
	const password = "synthetic-network-admin-password"
	hash, err := identity.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	var accountVersion int64
	if err := f.p.QueryRow(t.Context(), `UPDATE console_users SET password_hash=$1 WHERE username='admin168' RETURNING auth_version`, hash).Scan(&accountVersion); err != nil {
		t.Fatal(err)
	}
	f.token, f.csrf, err = f.s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: accountVersion})
	if err != nil {
		t.Fatal(err)
	}
	cfg := cryptopay.NetworkConfig{Enabled: true, HotAddress: "0x2222222222222222222222222222222222222222", TreasuryAddress: "0x3333333333333333333333333333333333333333"}
	f.s.Crypto = &cryptopay.Service{Pool: f.p, ManageNetworks: true, Networks: map[string]cryptopay.NetworkConfig{"ethereum": cfg}}
	switchNetwork := func(enabled bool, rev string, proof string, code int) *httptest.ResponseRecorder {
		t.Helper()
		raw := fmt.Sprintf(`{"enabled":%t,"expected_revision":%q,"current_password":%q}`, enabled, rev, proof)
		w := cryptoChannelHTTP(f, http.MethodPatch, "/crypto/networks/ethereum", raw)
		if w.Code != code {
			t.Fatalf("network PATCH returned HTTP %d want %d: %s", w.Code, code, w.Body.String())
		}
		return w
	}
	decodeItem := func(w *httptest.ResponseRecorder) cryptopay.NetworkControl {
		t.Helper()
		var result struct {
			Code int
			Data cryptopay.NetworkControl
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Code != 0 {
			t.Fatalf("invalid network mutation envelope: %s (%v)", w.Body.String(), err)
		}
		for _, private := range []string{password, cfg.HotAddress, cfg.TreasuryAddress, "current_password", "fingerprint", "rpc_url"} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatal("network control exposed private input")
			}
		}
		return result.Data
	}
	switchNetwork(true, "1", password, 409)
	if err := f.s.Crypto.AddAddress(t.Context(), "ethereum", "0x1111111111111111111111111111111111111111", "deposit"); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := cryptopay.ListenerFingerprint("ethereum", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO crypto_listener_health(network,fingerprint,checked_at)
		VALUES('ethereum',$1,clock_timestamp()-interval '1 hour')`, fingerprint); err != nil {
		t.Fatal(err)
	}
	switchNetwork(true, "1", password, 409)
	if _, err := f.p.Exec(t.Context(), "UPDATE crypto_listener_health SET checked_at=clock_timestamp() WHERE network='ethereum'"); err != nil {
		t.Fatal(err)
	}
	switchNetwork(true, "1", "wrong-password", 422)
	opened := decodeItem(switchNetwork(true, "1", password, 200))
	if !opened.Enabled || !opened.EffectiveEnabled || !opened.ReadyToEnable || opened.Revision != 2 {
		t.Fatalf("ready network did not open as expected: %+v", opened)
	}
	switchNetwork(false, "1", password, 409)
	rotated := cfg
	rotated.HotAddress = "0x4444444444444444444444444444444444444444"
	f.s.Crypto.Networks["ethereum"] = rotated
	newFingerprint, err := cryptopay.ListenerFingerprint("ethereum", rotated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), "UPDATE crypto_listener_health SET fingerprint=$1,checked_at=clock_timestamp() WHERE network='ethereum'", newFingerprint); err != nil {
		t.Fatal(err)
	}
	var current cryptopay.NetworkControl
	for _, item := range readCryptoNetworkControls(t, f) {
		if item.Network == "ethereum" {
			current = item
		}
	}
	if !current.Enabled || current.EffectiveEnabled || !current.ReadyToEnable || current.Revision != 2 {
		t.Fatalf("rotated wallets inherited old approval or lost pending readiness: %+v", current)
	}
	switchNetwork(true, "2", password, 409)
	if _, err := f.p.Exec(t.Context(), "DELETE FROM crypto_listener_health WHERE network='ethereum'"); err != nil {
		t.Fatal(err)
	}
	closed := decodeItem(switchNetwork(false, "2", password, 200))
	if closed.Enabled || closed.EffectiveEnabled || closed.ListenerReady || closed.ReadyToEnable || closed.Revision != 3 {
		t.Fatalf("an unhealthy listener prevented closing the desired state: %+v", closed)
	}
	switchNetwork(true, "2", password, 409)
	var invoices, payouts, journals, cny, audit int
	if err := f.p.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM crypto_invoices),
		(SELECT count(*) FROM crypto_withdrawals),
		(SELECT count(*) FROM crypto_journals),
		(SELECT count(*) FROM orders),
		(SELECT count(*) FROM crypto_audit WHERE actor_id=168)`).Scan(&invoices, &payouts, &journals, &cny, &audit); err != nil || invoices != 0 || payouts != 0 || journals != 0 || cny != 0 || audit != 2 {
		t.Fatalf("switch effects are not confined to control state and audit: %d %d %d %d %d %v", invoices, payouts, journals, cny, audit, err)
	}
}
