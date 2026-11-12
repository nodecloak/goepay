package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOAuthLegacyMetadataReflectsActiveSecretsWithoutMigrating(t *testing.T) {
	f := oauthLegacyFixture(t)
	w := oauthCredCall(f, f.token, f.csrf, "GET", "")
	view := oauthCredView(t, w)
	if !view.QQSet || !view.WXSet || !view.AlipaySet || !view.TelegramSet {
		t.Error("active legacy-only credentials are invisible to the clear controls")
	}
	for _, secret := range []string{"legacy-qq-secret", "legacy-relay-secret", "123:legacy-telegram-secret"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Error("metadata response leaked a legacy secret")
		}
	}
	next := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
	if view.Fingerprint != next.Fingerprint {
		t.Error("unchanged legacy snapshot has an unstable fingerprint")
	}
	var vaults, audits int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM connect_login_credentials),(SELECT count(*) FROM backoffice_audit WHERE resource='oauth-credentials')`).Scan(&vaults, &audits); err != nil || vaults != 0 || audits != 0 {
		t.Error("metadata GET mutated credential state", vaults, audits, err)
	}
}

func TestOAuthLegacyMetadataRejectsStaleFirstWrite(t *testing.T) {
	for _, change := range []struct{ name, key, value string }{
		{"target-replaced", "login_qq_appkey", "newer-qq-secret"},
		{"other-provider-replaced", "login_tg_bot_token", "123:newer-telegram-secret"},
		{"shared-provider-cleared", "login_appkey", ""},
	} {
		t.Run(change.name, func(t *testing.T) {
			f := oauthLegacyFixture(t)
			view := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
			if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{value}',to_jsonb($2::text)) WHERE resource='settings' AND body->>'key'=$1`, change.key, change.value); err != nil {
				t.Fatal(err)
			}
			w := oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"qq","clear":true,"expected_fingerprint":"`+view.Fingerprint+`"}`)
			if w.Code != 409 {
				t.Error("old metadata cleared a changed legacy snapshot", w.Code)
			}
			var vaults, audits int
			if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM connect_login_credentials),(SELECT count(*) FROM backoffice_audit WHERE resource='oauth-credentials')`).Scan(&vaults, &audits); err != nil || vaults != 0 || audits != 0 {
				t.Error("stale first write migrated or audited credentials", vaults, audits, err)
			}
			wantQQ := "legacy-qq-secret"
			if change.key == "login_qq_appkey" {
				wantQQ = change.value
			}
			if oauthResolvedSecret(f, "qq") != wantQQ {
				t.Error("stale first clear replaced the current effective credential")
			}
			fresh := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
			if fresh.Fingerprint == view.Fingerprint {
				t.Error("legacy snapshot change did not change the metadata fingerprint")
			}
		})
	}
}

func TestOAuthLegacyMetadataSnapshotAllowsSelectiveClear(t *testing.T) {
	f := oauthLegacyFixture(t)
	view := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "GET", ""))
	if !view.WXSet {
		t.Error("active WeChat legacy secret has no UI clear action")
	}
	next := oauthCredView(t, oauthCredCall(f, f.token, f.csrf, "PATCH", `{"field":"wx","clear":true,"expected_fingerprint":"`+view.Fingerprint+`"}`))
	if next.WXSet || !next.QQSet || !next.AlipaySet || !next.TelegramSet || next.Fingerprint == view.Fingerprint {
		t.Error("first selective clear changed the wrong slots or retained its fingerprint")
	}
	if oauthResolvedSecret(f, "wx") != "" || oauthResolvedSecret(f, "alipay") != "legacy-relay-secret" {
		t.Error("shared legacy key was not separated into authoritative provider slots")
	}
}

func TestOAuthLegacyMetadataWaitsForFirstClearCommit(t *testing.T) {
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
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- oauthCredCall(f, f.token, f.csrf, "GET", "") }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-done:
			t.Fatal("metadata GET bypassed the uncommitted first credential clear")
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
			t.Fatal("metadata GET did not reach the migration read lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-done:
		current := oauthCredView(t, w)
		if current.QQSet || !current.WXSet || !current.AlipaySet || !current.TelegramSet || current.Fingerprint == view.Fingerprint {
			t.Error("metadata GET did not return the committed authoritative vault")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("metadata GET did not finish after clear committed")
	}
}
