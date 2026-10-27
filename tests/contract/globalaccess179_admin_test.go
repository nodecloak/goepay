package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func platformAdmin179(c adminCredentials162, method, path, body, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/"+path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: c.token})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	c.s.Handler().ServeHTTP(w, r)
	return w
}

func platformState179(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("platform state HTTP=%d cache=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	v := credentialData162(t, w)
	if len(v) != 4 || v["can_update"] != true || v["close_keylogin"] != "0" && v["close_keylogin"] != "1" || v["user_refund"] != "0" && v["user_refund"] != "1" {
		t.Fatal("strict original platform flags", v)
	}
	if fp, ok := v["fingerprint"].(string); !ok || len(fp) != 64 {
		t.Fatal("missing platform CAS")
	}
	return v
}

func platformPatch179(fp, closeKey, refund string) string {
	raw, _ := json.Marshal(map[string]string{"close_keylogin": closeKey, "user_refund": refund, "expected_fingerprint": fp})
	return string(raw)
}

func TestGlobalAccess179OriginalSuperStrictStringsAndSingleWinnerCAS(t *testing.T) {
	c := adminCredentialsFixture162(t)
	state := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
	if state["close_keylogin"] != "0" || state["user_refund"] != "1" {
		t.Fatal("SQL defaults were replaced by loading placeholders", state)
	}
	body := platformPatch179(state["fingerprint"].(string), "1", "0")
	if w := platformAdmin179(c, "PATCH", "settings/merchant-access", body, "wrong"); w.Code != 403 {
		t.Fatal("missing original CSRF accepted", w.Code)
	}
	for _, invalid := range []string{
		strings.Replace(body, `"close_keylogin":"1"`, `"close_keylogin":1`, 1),
		strings.Replace(body, `"user_refund":"0"`, `"user_refund":false`, 1),
		strings.Replace(body, `"close_keylogin":"1"`, `"close_keylogin":"1","close_keylogin":"0"`, 1),
		strings.TrimSuffix(body, "}") + `,"merchant_id":"2"}`,
	} {
		if w := platformAdmin179(c, "PATCH", "settings/merchant-access", invalid, c.csrf); w.Code != 400 {
			t.Fatal("non-strict platform value accepted", w.Code)
		}
	}
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- platformAdmin179(c, "PATCH", "settings/merchant-access", body, c.csrf).Code
		}()
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for status := range results {
		if status == 200 {
			wins++
		} else if status == 409 {
			conflicts++
		} else {
			t.Fatal("CAS returned unexpected state", status)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("platform CAS did not bind both gates", wins, conflicts)
	}
	after := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
	if after["close_keylogin"] != "1" || after["user_refund"] != "0" || after["fingerprint"] == state["fingerprint"] {
		t.Fatal(after)
	}
	var rows, heads, audits int
	if e := c.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM backoffice_records WHERE resource='settings' AND body->>'key' IN('close_keylogin','user_refund')),(SELECT count(*) FROM backoffice_records WHERE resource='settings' AND body->>'key'='__platform_merchant_access179'),(SELECT count(*) FROM backoffice_audit WHERE resource='platform-merchant-access179')`).Scan(&rows, &heads, &audits); e != nil || rows != 2 || heads != 1 || audits != 1 {
		t.Fatal("platform state not one atomic original-row pair", rows, heads, audits, e)
	}
}

func TestGlobalAccess179GenericDeleteCannotRemoveProtectedSetting(t *testing.T) {
	c := adminCredentialsFixture162(t)
	state := platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
	platformState179(t, platformAdmin179(c, "PATCH", "settings/merchant-access", platformPatch179(state["fingerprint"].(string), "1", "0"), c.csrf))
	var id int64
	if e := c.pool.QueryRow(context.Background(), `SELECT id FROM backoffice_records WHERE resource='settings' AND body->>'key'='close_keylogin'`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	w := platformAdmin179(c, "DELETE", fmt.Sprintf("resources/settings/%d", id), "", c.csrf)
	if w.Code != 403 {
		t.Fatal("generic writer removed a live platform gate", w.Code)
	}
	platformState179(t, platformAdmin179(c, "GET", "settings/merchant-access", "", c.csrf))
}
