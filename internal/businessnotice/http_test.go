package businessnotice_test

import (
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/server"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminBusiness83ActualHTTPReturnsRedactedOriginalHistory(t *testing.T) {
	s, p, _ := delivery83(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('operator83','admin',83,'Local','synthetic-only83','operator')`); err != nil {
		t.Fatal(err)
	}
	sessions := identity.NewPersistentSessions(p, false)
	token, _, err := sessions.Create(identity.User{ID: 83, Username: "operator83", Role: "admin", AdminClass: "operator", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	app := (&server.Server{Pool: p, Sessions: sessions, BusinessNotice: s}).Handler()
	req := httptest.NewRequest("GET", "/console/v1/admin/business-notices?limit=20", nil)
	req.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	out := httptest.NewRecorder()
	app.ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	for _, private := range []string{"original83.example.test", "owner8301", "admin83@example.test", "payload_cipher", "proof_hash"} {
		if strings.Contains(out.Body.String(), private) {
			t.Fatal("private notification source exposed", private)
		}
	}
	var body struct {
		Code int
		Data struct {
			Items  []map[string]any `json:"items"`
			Cursor string           `json:"next_cursor"`
		}
	}
	if json.Unmarshal(out.Body.Bytes(), &body) != nil || body.Code != 0 || len(body.Data.Items) != 1 || body.Data.Items[0]["merchant_id"] != "8301" || body.Data.Items[0]["state"] != "queued" {
		t.Fatal("actual contract mismatch", out.Body.String())
	}
}
