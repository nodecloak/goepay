package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func oauthLegacyFixture(t *testing.T) channel168Fixture {
	t.Helper()
	f := channel168Local(t)
	for key, value := range map[string]string{
		"login_qq": "1", "login_qq_appid": "legacy-qq-app", "login_qq_appkey": "legacy-qq-secret",
		"login_wx": "1", "login_alipay": "1", "login_appid": "legacy-relay-app", "login_appkey": "legacy-relay-secret",
		"login_apiurl": "http://127.0.0.1:1", "login_tg": "1", "login_tg_bot_token": "123:legacy-telegram-secret",
	} {
		if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, key, value); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func oauthResolvedSecret(f channel168Fixture, provider string) string {
	_, _, secret := f.s.connectLoginConfig(httptest.NewRequest("GET", "/", nil), provider)
	return secret
}

func oauthAssertUnavailable(t *testing.T, f channel168Fixture, provider string) {
	t.Helper()
	if oauthResolvedSecret(f, provider) != "" {
		t.Error("legacy plaintext credential was reactivated", provider)
	}
	options := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(options, httptest.NewRequest("GET", "/console/v1/public/connect/options", nil))
	var data struct {
		Data struct {
			Providers map[string]bool `json:"providers"`
		} `json:"data"`
	}
	if options.Code != 200 || json.Unmarshal(options.Body.Bytes(), &data) != nil || data.Data.Providers[provider] {
		t.Error("disabled vault credential still advertised as available", provider, options.Code)
	}
	// Never follow redirects or call an external OAuth endpoint.
	start := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(start, httptest.NewRequest("GET", "/console/v1/public/connect/"+provider+"/start", nil))
	if start.Code != 503 || start.Header().Get("Location") != "" || len(start.Result().Cookies()) != 0 {
		t.Error("unavailable vault credential still starts OAuth", provider, start.Code)
	}
}

func TestOAuthCredentialsClearDoesNotReactivateLegacy(t *testing.T) {
	f := oauthLegacyFixture(t)
	for _, provider := range []string{"qq", "wx", "alipay", "telegram"} {
		if oauthResolvedSecret(f, provider) == "" {
			t.Fatal("genuinely unmigrated legacy credential unavailable", provider)
		}
	}
	view := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
	for _, provider := range []string{"qq", "wx", "alipay", "telegram"} {
		view = oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"`+provider+`","clear":true,"expected_fingerprint":"`+view.Fingerprint+`"}`))
		t.Run(provider, func(t *testing.T) { oauthAssertUnavailable(t, f, provider) })
	}
	fields := map[string]string{"id": "12345", "auth_date": strconv.FormatInt(time.Now().Unix(), 10)}
	query := url.Values{}
	for key, value := range fields {
		query.Set(key, value)
	}
	query.Set("hash", tgWidgetSign("123:legacy-telegram-secret", fields))
	if _, err := f.s.telegramIdentity(httptest.NewRequest("GET", "/", nil), query); err == nil {
		t.Error("cleared Telegram token still authenticates the old widget signature")
	}
}

func TestOAuthCredentialVaultFailuresNeverUseLegacy(t *testing.T) {
	for _, failure := range []string{"wrong-master", "missing-master", "proof", "ciphertext", "missing-table"} {
		t.Run(failure, func(t *testing.T) {
			f := oauthLegacyFixture(t)
			view := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
			oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"qq","value":"encrypted-qq-secret","expected_fingerprint":"`+view.Fingerprint+`"}`))
			switch failure {
			case "wrong-master":
				f.s.CheckoutKey = append([]byte(nil), f.s.CheckoutKey...)
				f.s.CheckoutKey[0] ^= 1
			case "missing-master":
				f.s.CheckoutKey = nil
			case "proof":
				if _, err := f.p.Exec(t.Context(), `UPDATE connect_login_credentials SET proof_hash=decode(repeat('00',32),'hex')`); err != nil {
					t.Fatal(err)
				}
			case "ciphertext":
				if _, err := f.p.Exec(t.Context(), `UPDATE connect_login_credentials SET qq_cipher=decode('00','hex')`); err != nil {
					t.Fatal(err)
				}
			case "missing-table":
				if _, err := f.p.Exec(t.Context(), `ALTER TABLE connect_login_credentials RENAME TO unavailable_oauth_credentials`); err != nil {
					t.Fatal(err)
				}
			}
			oauthAssertUnavailable(t, f, "qq")
		})
	}
}

func TestOAuthFirstCredentialWriteMigratesOtherLegacySlots(t *testing.T) {
	f := oauthLegacyFixture(t)
	view := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
	updated := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"qq","clear":true,"expected_fingerprint":"`+view.Fingerprint+`"}`))
	if updated.QQSet || !updated.WXSet || !updated.AlipaySet || !updated.TelegramSet {
		t.Error("first clear did not retain the other legacy slots in the encrypted vault")
	}
	store := f.s.connectConfigStore()
	for provider, want := range map[string]string{"qq": "", "wx": "legacy-relay-secret", "alipay": "legacy-relay-secret", "telegram": "123:legacy-telegram-secret"} {
		got, err := store.Resolve(t.Context(), provider)
		if err != nil || got != want {
			t.Error("first write did not migrate the intended encrypted slot", provider, err)
		}
	}
	var raw []byte
	var appid string
	if err := f.p.QueryRow(t.Context(), `SELECT wx_cipher FROM connect_login_credentials`).Scan(&raw); err != nil || len(raw) == 0 || strings.Contains(string(raw), "legacy-relay-secret") {
		t.Error("legacy secret was not encrypted", err)
	}
	if err := f.p.QueryRow(t.Context(), `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='login_appid'`).Scan(&appid); err != nil || appid != "legacy-relay-app" {
		t.Error("migration changed the public appid", err)
	}
}

func TestOAuthFirstCredentialWriteRejectsInvalidLegacySlots(t *testing.T) {
	f := oauthLegacyFixture(t)
	for key, value := range map[string]any{"login_qq_appkey": map[string]string{"invalid": "object"}, "login_appkey": strings.Repeat("x", 513), "login_tg_bot_token": "secret\ncontrol"} {
		raw, _ := json.Marshal(value)
		if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{value}',$2::jsonb) WHERE resource='settings' AND body->>'key'=$1`, key, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	view := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
	updated := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"qq","value":"new-valid-secret","expected_fingerprint":"`+view.Fingerprint+`"}`))
	if !updated.QQSet || updated.WXSet || updated.AlipaySet || updated.TelegramSet {
		t.Fatal("invalid or non-string legacy settings migrated into credential slots")
	}
}

func TestOAuthConcurrentFirstCredentialWriteKeepsOneMigration(t *testing.T) {
	f := oauthLegacyFixture(t)
	view := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, field := range []string{"qq", "telegram"} {
		go func(field string) {
			<-start
			results <- oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"`+field+`","clear":true,"expected_fingerprint":"`+view.Fingerprint+`"}`).Code
		}(field)
	}
	close(start)
	first, second := <-results, <-results
	if !((first == 200 && second == 409) || (first == 409 && second == 200)) {
		t.Fatal("concurrent first writes did not enforce one CAS owner", first, second)
	}
	final := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
	if !final.WXSet || !final.AlipaySet || final.QQSet == final.TelegramSet {
		t.Error("first-write loser changed or lost migrated credentials")
	}
	var revision, audits int
	if err := f.p.QueryRow(t.Context(), `SELECT revision,(SELECT count(*) FROM backoffice_audit WHERE resource='oauth-credentials') FROM connect_login_credentials`).Scan(&revision, &audits); err != nil || revision != 1 || audits != 1 {
		t.Error("first write was not a single atomic migration", revision, audits, err)
	}
}

func TestOAuthLegacyReadWaitsForFirstClearCommit(t *testing.T) {
	f := oauthLegacyFixture(t)
	view := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
	oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"qq","clear":true,"expected_fingerprint":"`+view.Fingerprint+`"}`))
	var revision int64
	var qq, wx, alipay, tg, proof []byte
	if err := f.p.QueryRow(t.Context(), `SELECT revision,qq_cipher,wx_cipher,alipay_cipher,tg_cipher,proof_hash FROM connect_login_credentials`).Scan(&revision, &qq, &wx, &alipay, &tg, &proof); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `DELETE FROM connect_login_credentials`); err != nil {
		t.Fatal(err)
	}
	tx, err := f.p.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err = tx.Exec(t.Context(), `SELECT pg_advisory_xact_lock(hashtextextended('oauth-credential',128))`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), `INSERT INTO connect_login_credentials(id,revision,qq_cipher,wx_cipher,alipay_cipher,tg_cipher,proof_hash) VALUES(true,$1,$2,$3,$4,$5,$6)`, revision, qq, wx, alipay, tg, proof); err != nil {
		t.Fatal(err)
	}
	resolved := make(chan string, 1)
	go func() { resolved <- oauthResolvedSecret(f, "qq") }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case value := <-resolved:
			if value != "" {
				t.Fatal("legacy resolver bypassed the first credential clear transaction")
			}
			t.Fatal("credential resolver did not acquire the migration read lock")
		default:
		}
		var waiting bool
		if err = f.p.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE 'SELECT pg_advisory_xact_lock_shared%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("credential resolver failed to reach the migration lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-resolved:
		if value != "" {
			t.Error("committed first clear still reactivated a legacy credential")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("credential resolution did not finish after clear committed")
	}
}

func TestOAuthTelegramWorksWithOnlyVaultToken(t *testing.T) {
	f := channel168Local(t)
	if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"login_tg","value":"1"}')`); err != nil {
		t.Fatal(err)
	}
	view := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
	oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"telegram","value":"123:vault-only-token","expected_fingerprint":"`+view.Fingerprint+`"}`))
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/console/v1/public/connect/telegram/start", nil))
	if w.Code != 200 || connectCookie(w, connectLoginBrowserCookie) == "" {
		t.Fatal("vault-only Telegram login cannot start without an unrelated plaintext appid", w.Code)
	}
}
