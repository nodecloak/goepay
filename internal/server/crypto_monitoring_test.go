package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/cryptopay"
	"goepay/internal/identity"
)

const cryptoMonitoringPath = "/crypto/monitoring"

func cryptoMonitorHTTP(s *Server, role, token, method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/"+role+path, nil)
	r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestCryptoMonitoringReportsDailyReservationsAndDepositCapacityWhileClosed(t *testing.T) {
	f := channel168Local(t)
	_, err := f.p.Exec(t.Context(), `
		INSERT INTO crypto_channels (id,name,network,enabled,archived,revision,fee_bps,day_limit) VALUES
			(101,'TRON Capped','tron',true,false,1,0,4000000),
			(102,'Ethereum Unlimited','ethereum',true,false,1,0,0),
			(9007199254740993,'TRON Archived','tron',false,true,1,0,700000);
		INSERT INTO crypto_addresses (network,address,role,used) VALUES
			('tron','private-pending','deposit',true),
			('tron','private-lapsed','deposit',true),
			('tron','private-older','deposit',true),
			('tron','private-next-day','deposit',true),
			('tron','private-paid','deposit',true),
			('tron','private-legacy','deposit',true),
			('tron','private-expired','deposit',true),
			('tron','private-free','deposit',false),
			('tron','private-treasury','treasury',false),
			('ethereum','private-late','deposit',true),
			('ethereum','private-underpaid','deposit',true),
			('ethereum','private-free','deposit',false),
			('ethereum','private-hot','hot',false);
		WITH now AS (SELECT clock_timestamp() AS as_of),
		     start_of_day AS (SELECT date_trunc('day',as_of AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS day_start, as_of FROM now)
		INSERT INTO crypto_invoices(id,merchant_id,out_trade_no,network,address,amount,fee,token_hash,token_cipher,expires_at,created_at,state,
			channel_id,channel_revision,fee_bps,fee_source,channel_snapshot)
		SELECT id,1,id,network,'private-'||id,amount,0,''::bytea,''::bytea,as_of+expires_delta,
		       day_start+day_delta,state,
		       channel_id,CASE WHEN channel_id IS NOT NULL THEN 1 END,CASE WHEN channel_id IS NOT NULL THEN 0 END,
		       CASE WHEN channel_id IS NOT NULL THEN 'channel' ELSE 'legacy_network' END,
		       CASE WHEN channel_id IS NOT NULL THEN '{"snapshot":true}'::jsonb ELSE '{}'::jsonb END
		  FROM start_of_day, (VALUES
		   ('pending','tron',1000001,'pending',101,interval '0 second',interval '10 minute'),
		   ('lapsed','tron',2000002,'underpaid',101,interval '0 second',interval '-10 minute'),
		   ('expired','tron',600000,'expired',9007199254740993,interval '0 second',interval '-10 minute'),
		   ('older','tron',900000000,'paid',101,interval '-1 microsecond',interval '10 minute'),
		   ('next-day','tron',900000000,'paid',101,interval '24 hours',interval '10 minute'),
		   ('paid','tron',3000003,'paid',101,interval '0 second',interval '-10 minute'),
		   ('legacy','tron',5000000,'pending',NULL::bigint,interval '0 second',interval '10 minute'),
		   ('late','ethereum',4000004,'late',102,interval '0 second',interval '-10 minute'),
		   ('underpaid','ethereum',7000001,'underpaid',102,interval '0 second',interval '10 minute')
		 ) AS v(id,network,amount,state,channel_id,day_delta,expires_delta)`)
	if err != nil {
		t.Fatal(err)
	}

	type usage struct {
		ID, Name, Network string
		Enabled, Archived bool
		Limit             string            `json:"day_limit"`
		Used              string            `json:"day_used"`
		Remaining         *string           `json:"day_remaining"`
		ByState           map[string]string `json:"day_usage_by_state"`
	}
	type network struct {
		Network   string
		Enabled   bool
		Total     int `json:"deposit_total"`
		Used      int `json:"deposit_used"`
		Available int `json:"deposit_available"`
	}
	read := func() (map[string]network, map[string]usage) {
		t.Helper()
		w := cryptoMonitorHTTP(f.s, "admin", f.token, "GET", cryptoMonitoringPath)
		var out struct {
			Code int
			Data struct {
				AsOf        time.Time `json:"as_of"`
				WindowStart time.Time `json:"window_start"`
				WindowEnd   time.Time `json:"window_end"`
				Networks    []network
				Channels    []usage
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Code != 0 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("monitoring should be a no-store read in mainnet-off mode: HTTP %d: %s (%v)", w.Code, w.Body.String(), err)
		}
		if strings.Contains(w.Body.String(), "private-") || strings.Contains(w.Body.String(), "token_hash") || strings.Contains(w.Body.String(), "merchant_id") {
			t.Fatalf("monitoring leaked source records: %s", w.Body.String())
		}
		day := out.Data.AsOf.UTC().Truncate(24 * time.Hour)
		if out.Data.AsOf.IsZero() || !out.Data.WindowStart.Equal(day) || !out.Data.WindowEnd.Equal(day.Add(24*time.Hour)) {
			t.Fatalf("wrong UTC accounting window: %+v", out.Data)
		}
		if len(out.Data.Channels) != 3 || len(out.Data.Networks) != 2 {
			t.Fatalf("missing configured channels or known networks: %+v", out.Data)
		}
		pools := map[string]network{}
		channels := map[string]usage{}
		for _, pool := range out.Data.Networks {
			pools[pool.Network] = pool
		}
		for _, c := range out.Data.Channels {
			channels[c.ID] = c
		}
		return pools, channels
	}
	pools, channels := read()
	if tron := pools["tron"]; tron != (network{Network: "tron", Total: 8, Used: 7, Available: 1}) {
		t.Fatalf("TRON counter includes non-deposit or excludes permanently assigned deposits: %+v", tron)
	}
	if eth := pools["ethereum"]; eth != (network{Network: "ethereum", Total: 3, Used: 2, Available: 1}) {
		t.Fatalf("Ethereum counter includes non-deposit or excludes permanently assigned deposits: %+v", eth)
	}
	capped := channels["101"]
	if capped.Name != "TRON Capped" || capped.Network != "tron" || !capped.Enabled || capped.Archived || capped.Limit != "4" || capped.Used != "6.000006" || capped.Remaining == nil || *capped.Remaining != "0" {
		t.Fatalf("day usage omitted reservations or did not clamp reduced limit: %+v", capped)
	}
	if len(capped.ByState) != 6 || capped.ByState["pending"] != "1.000001" || capped.ByState["expired"] != "2.000002" || capped.ByState["paid"] != "3.000003" || capped.ByState["late"] != "0" || capped.ByState["underpaid"] != "0" || capped.ByState["overpaid"] != "0" {
		t.Fatalf("quota breakdown does not match effective states: %+v", capped.ByState)
	}
	if unlimited := channels["102"]; unlimited.Limit != "0" || unlimited.Used != "11.000005" || unlimited.Remaining != nil || unlimited.ByState["late"] != "4.000004" || unlimited.ByState["underpaid"] != "7.000001" {
		t.Fatalf("unlimited usage lost precision or capacity semantics: %+v", unlimited)
	}
	if archived := channels["9007199254740993"]; !archived.Archived || archived.Enabled || archived.Used != "0.6" || archived.Remaining == nil || *archived.Remaining != "0.1" || archived.ByState["expired"] != "0.6" {
		t.Fatalf("archived channel lost historical reservations or full ID: %+v", archived)
	}

	f.s.Crypto = &cryptopay.Service{Networks: map[string]cryptopay.NetworkConfig{"ethereum": {Enabled: true}}}
	pools, _ = read()
	if pools["tron"].Enabled || !pools["ethereum"].Enabled {
		t.Fatalf("runtime network switches conflate configuration and inventory: %+v", pools)
	}
	var invoices, ledgerEntries int
	if err := f.p.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM crypto_invoices),(SELECT count(*) FROM crypto_entries)").Scan(&invoices, &ledgerEntries); err != nil || invoices != 9 || ledgerEntries != 0 {
		t.Fatalf("monitoring unexpectedly changed money records: invoices=%d entries=%d, %v", invoices, ledgerEntries, err)
	}
}

func TestCryptoMonitoringRequiresSuperRoleAndInstalledSchema(t *testing.T) {
	f := email197Local(t)
	f.s.Crypto = nil
	admin := f.channel168Fixture
	registered := cryptoMonitorHTTP(f.s, "admin", admin.token, "GET", cryptoMonitoringPath)
	if registered.Code != 200 || !strings.Contains(registered.Body.String(), `"channels":[]`) || strings.Count(registered.Body.String(), `"deposit_available":0`) != 2 {
		t.Fatalf("installed empty pool should return verified empty inventory: %d %s", registered.Code, registered.Body.String())
	}
	for _, tc := range []struct {
		role, token, method, path string
		code                      int
	}{
		{"admin", "", "GET", cryptoMonitoringPath, 401},
		{"merchant", f.token, "GET", cryptoMonitoringPath, 404},
		{"admin", admin.token, "POST", cryptoMonitoringPath, 403},
		{"admin", admin.token, "GET", cryptoMonitoringPath + "?merchant_id=1", 400},
	} {
		w := cryptoMonitorHTTP(f.s, tc.role, tc.token, tc.method, tc.path)
		if w.Code != tc.code {
			t.Fatalf("%s %s method %s returned %d want %d: %s", tc.role, tc.path, tc.method, w.Code, tc.code, w.Body.String())
		}
	}
	post := httptest.NewRequest(http.MethodPost, "/console/v1/admin"+cryptoMonitoringPath, nil)
	post.Header.Set("X-CSRF-Token", admin.csrf)
	post.AddCookie(&http.Cookie{Name: "goepay_admin", Value: admin.token})
	rejected := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(rejected, post)
	if rejected.Code != http.StatusMethodNotAllowed || rejected.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("authenticated requests must still be read-only: %d %s", rejected.Code, rejected.Body.String())
	}
	if _, err := f.p.Exec(t.Context(), "ALTER TABLE crypto_invoices RENAME COLUMN channel_id TO hidden_channel_id"); err != nil {
		t.Fatal(err)
	}
	w := cryptoMonitorHTTP(f.s, "admin", admin.token, "GET", cryptoMonitoringPath)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"data":null`) || strings.Contains(w.Body.String(), "channel_id") {
		t.Fatalf("missing quota schema must not simulate zero usage: %d %s", w.Code, w.Body.String())
	}
	if _, err := f.p.Exec(t.Context(), "UPDATE console_users SET admin_class='operator' WHERE role='admin' AND subject_id=168"); err != nil {
		t.Fatal(err)
	}
	actor := identity.User{ID: 168, Username: "admin168", Role: "admin", AdminClass: "operator"}
	if err := f.p.QueryRow(t.Context(), "SELECT auth_version FROM console_users WHERE role='admin' AND subject_id=168").Scan(&actor.AuthVersion); err != nil {
		t.Fatal(err)
	}
	token, _, err := f.s.Sessions.Create(actor)
	if err != nil {
		t.Fatal(err)
	}
	w = cryptoMonitorHTTP(f.s, "admin", token, "GET", cryptoMonitoringPath)
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"data":null`) {
		t.Fatalf("operator viewed privileged crypto monitoring: %d %s", w.Code, w.Body.String())
	}
}
