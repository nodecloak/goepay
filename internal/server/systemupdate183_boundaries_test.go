package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSystemUpdate183StrictHTTPAndCSRFNeverAcceptAnUpdate(t *testing.T) {
	f := update183Local(t)
	v := update183Data(t, f.update("GET", "", ""))
	fp := v["fingerprint"].(string)
	for _, tc := range []struct {
		path, body, csrf string
		code             int
	}{
		{"/prepare", `{"intent":"` + fp + `","expected_fingerprint":"` + fp + `","path":"outside"}`, f.csrf, 400},
		{"/prepare", `{"intent":"` + fp + `","intent":"` + fp + `","expected_fingerprint":"` + fp + `"}`, f.csrf, 400},
		{"/prepare?", `{"intent":"` + fp + `","expected_fingerprint":"` + fp + `"}`, f.csrf, 400},
		{"/prepare", `{"intent":"` + fp + `","expected_fingerprint":"` + fp + `"}`, "wrong-csrf", 403},
	} {
		r := httptest.NewRequest("POST", "/console/v1/admin/system-update"+tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", tc.csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
	}
	var count int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*) FROM backoffice_audit WHERE resource='system-update183'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("rejected request started an update", count, e)
	}
}

func TestSystemUpdate183ConcurrentSameIntentHasOneAcceptedPreparation(t *testing.T) {
	f := update183Local(t)
	v := update183Data(t, f.update("GET", "", ""))
	fp := v["fingerprint"].(string)
	body, _ := json.Marshal(map[string]string{"intent": fp, "expected_fingerprint": fp})
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- f.update("POST", "/prepare", string(body)).Code }()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 && code != 202 {
			t.Fatal(code)
		}
	}
	waitUpdate183(t, f, fp, "prepared")
	var accepted, prepared int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*) FILTER(WHERE operation='prepare-accepted'),count(*) FILTER(WHERE operation='prepare-prepared') FROM backoffice_audit WHERE resource='system-update183'`).Scan(&accepted, &prepared); e != nil || accepted != 1 || prepared != 1 {
		t.Fatal(accepted, prepared, e)
	}
}

func TestSystemUpdate183LastAuditWriteCannotOutliveOriginalSession(t *testing.T) {
	for _, operation := range []string{"accepted", "prepared", "cancelled"} {
		t.Run(operation, func(t *testing.T) {
			f := update183Local(t)
			var fp string
			var prepared map[string]any
			if operation == "cancelled" {
				view := update183Data(t, f.update("GET", "", ""))
				fp = view["fingerprint"].(string)
				raw, _ := json.Marshal(map[string]string{"intent": fp, "expected_fingerprint": fp})
				if w := f.update("POST", "/prepare", string(raw)); w.Code != 202 {
					t.Fatal(w.Code, w.Body.String())
				}
				prepared = waitUpdate183(t, f, fp, "prepared")
			}
			base := time.Now().UTC().Truncate(time.Microsecond)
			f.channel168Fixture = channel168CClock(t, f.channel168Fixture, base)
			t.Cleanup(f.s.Store.StopSystemUpdates)
			if _, e := f.p.Exec(t.Context(), `UPDATE console_sessions SET expires_at=$1 WHERE role='admin'`, base.Add(24*time.Hour)); e != nil {
				t.Fatal(e)
			}
			if _, e := f.p.Exec(t.Context(), `CREATE FUNCTION expire_update183_actor() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='system-update183' AND NEW.operation='prepare-`+operation+`' THEN UPDATE channel168c_test_clock SET instant=instant+interval '48 hours';END IF;RETURN NEW;END$$;CREATE TRIGGER expire_update183_actor AFTER INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION expire_update183_actor()`); e != nil {
				t.Fatal(e)
			}
			if operation == "cancelled" {
				raw, _ := json.Marshal(map[string]string{"expected_state": prepared["state_fingerprint"].(string)})
				if w := f.update("POST", "/preparations/"+fp+"/cancel", string(raw)); w.Code != 403 {
					t.Fatal("expired cancel committed", w.Code, w.Body.String())
				}
			} else {
				view := update183Data(t, f.update("GET", "", ""))
				fp = view["fingerprint"].(string)
				raw, _ := json.Marshal(map[string]string{"intent": fp, "expected_fingerprint": fp})
				w := f.update("POST", "/prepare", string(raw))
				if operation == "accepted" && w.Code != 403 {
					t.Fatal("expired accepted audit committed", w.Code, w.Body.String())
				}
				if operation == "prepared" {
					if w.Code != 202 {
						t.Fatal(w.Code, w.Body.String())
					}
					waitUpdate183(t, f, fp, "failed")
				}
			}
			var bad int
			if e := f.p.QueryRow(t.Context(), `SELECT count(*) FROM backoffice_audit WHERE resource='system-update183' AND operation=$1`, "prepare-"+operation).Scan(&bad); e != nil || bad != 0 {
				t.Fatal("last expired audit survived", operation, bad, e)
			}
			if operation != "cancelled" {
				if _, e := os.Stat(filepath.Join(f.cfg.StagingDir, fp)); !os.IsNotExist(e) {
					t.Fatal("expired actor published a prepared artifact", e)
				}
			}
		})
	}
}
