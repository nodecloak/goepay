package contract_test

import (
	"bytes"
	"context"
	"encoding/json"

	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInvitationCodesUXBatchUsageArchive(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	token, csrf, err := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, raw, anti string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/admin"+path, strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", anti)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	body := `{"request_id":"b7d541c7-1e57-43b4-a285-b066799c03e2","count":3}`
	if w := request("POST", "/invitation-codes/generate", body, "bad"); w.Code != 403 {
		t.Fatalf("CSRF %d", w.Code)
	}
	w := request("POST", "/invitation-codes/generate", body, csrf)
	if w.Code != 200 {
		t.Fatalf("batch generate %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			Items    []struct{ ID, Code string }
			Replayed bool
		}
	}
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil || len(out.Data.Items) != 3 {
		t.Fatalf("batch result %s %v", w.Body.String(), err)
	}
	first := out.Data.Items[0]
	replay := request("POST", "/invitation-codes/generate", body, csrf)
	if replay.Code != 200 || !bytes.Contains(replay.Body.Bytes(), []byte(`"replayed":true`)) || !bytes.Contains(replay.Body.Bytes(), []byte(first.Code)) {
		t.Fatalf("replay %s", replay.Body.String())
	}
	if w = request("POST", "/invitation-codes/generate", strings.Replace(body, `"count":3`, `"count":4`, 1), csrf); w.Code != 409 {
		t.Fatalf("changed replay %d", w.Code)
	}
	for _, invalid := range []string{`{"request_id":"x","count":1}`, `{"request_id":"b7d541c7-1e57-43b4-a285-b066799c03e3","count":1001}`, `{"request_id":"b7d541c7-1e57-43b4-a285-b066799c03e3","count":0}`} {
		if w = request("POST", "/invitation-codes/generate", invalid, csrf); w.Code != 400 {
			t.Fatalf("invalid accepted %d", w.Code)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO enrollment_invitation_uses(invitation_id,merchant_id) VALUES($1,1)`, first.ID); err != nil {
		t.Fatal(err)
	}
	w = request("GET", "/invitation-codes?status=used", "", csrf)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"used_by":"1"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"used_at":`)) || !bytes.Contains(w.Body.Bytes(), []byte(first.Code)) {
		t.Fatalf("usage projection %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/invitation-codes?status=unused&search="+first.Code, "", csrf); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"total":0`)) {
		t.Fatalf("unused filter %s", w.Body.String())
	}
	if w = request("GET", "/invitation-codes?status=unexpected", "", csrf); w.Code != 400 {
		t.Fatalf("invalid status accepted %d", w.Code)
	}
	if w = request("POST", "/invitation-codes/clear", `{"request_id":"b7d541c7-1e57-43b4-a285-b066799c03e4","scope":"used","confirm":true}`, csrf); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"archived":1`)) {
		t.Fatalf("clear used %d %s", w.Code, w.Body.String())
	}
	var uses, records int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM enrollment_invitation_uses WHERE invitation_id=$1`, first.ID).Scan(&uses); err != nil || uses != 1 {
		t.Fatalf("registration proof lost %d %v", uses, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_records WHERE resource='invitation-codes' AND body->>'enabled'='false' AND body->>'archived'='true' AND id=$1`, first.ID).Scan(&records); err != nil || records != 1 {
		t.Fatalf("archive state %d %v", records, err)
	}
	if w = request("PUT", "/resources/invitation-codes/"+first.ID, `{"enabled":true}`, csrf); w.Code != 409 && w.Code != 403 {
		t.Fatalf("archived code resurrected %d %s", w.Code, w.Body.String())
	}
	w = request("DELETE", "/resources/invitation-codes/"+out.Data.Items[1].ID, "", csrf)
	if w.Code != 200 {
		t.Fatalf("generic delete did not archive %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/resources/invitation-codes", "", csrf); w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(first.Code)) {
		t.Fatalf("generic list exposed archive %s", w.Body.String())
	}
	if w = request("POST", "/invitation-codes/clear", `{"request_id":"b7d541c7-1e57-43b4-a285-b066799c03e5","scope":"all","confirm":false}`, csrf); w.Code != 400 {
		t.Fatalf("unconfirmed clear accepted %d", w.Code)
	}
	if w = request("POST", "/invitation-codes/clear", `{"request_id":"b7d541c7-1e57-43b4-a285-b066799c03e6","scope":"all","confirm":true}`, csrf); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"archived":1`)) {
		t.Fatalf("clear remaining %d %s", w.Code, w.Body.String())
	}
	var audits int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='invitation-batches' AND operation='generate'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("generation replay duplicated %d %v", audits, err)
	}

}
