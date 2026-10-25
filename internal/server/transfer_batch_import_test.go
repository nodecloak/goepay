package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/transferimport"
	"net/http/httptest"
	"strings"
	"testing"
)

func importBody(mid, format, raw string) string {
	b, _ := json.Marshal(map[string]string{"merchant_id": mid, "format": format, "file_base64": base64.StdEncoding.EncodeToString([]byte(raw))})
	return string(b)
}

const privateImportCSV = "序号,收款账号,收款人姓名,转账金额（元）,转账备注\n1,000123456789012345678,Private Name,0.10,private-note\n"

func TestTransferBatchImportPrivateDraftIsPureAndScoped(t *testing.T) {
	s := &Server{}
	session := identity.Session{User: identity.User{Role: "merchant", ID: 7, Username: "local"}, CSRF: "csrf"}
	r := httptest.NewRequest("POST", "/console/v1/merchant/transfer-batch-imports/preview", strings.NewReader(importBody("", "csv", privateImportCSV)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", "csrf")
	w := httptest.NewRecorder()
	if !s.transferBatchImport(w, r, "merchant", session, []string{"merchant", "transfer-batch-imports", "preview"}) || w.Code != 200 {
		t.Fatalf("pure preview missing status=%d", w.Code)
	}
	var out struct {
		Data struct {
			Merchant string               `json:"merchant_id"`
			Count    int                  `json:"count"`
			Total    string               `json:"total_cents"`
			Rows     []map[string]any     `json:"rows"`
			Draft    []transferimport.Row `json:"draft_items"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.Merchant != "7" || out.Data.Total != "10" || out.Data.Count != 1 || len(out.Data.Draft) != 1 || out.Data.Draft[0].Account != "000123456789012345678" {
		t.Fatal("draft did not preserve original exact data")
	}
	masked, _ := json.Marshal(out.Data.Rows)
	for _, secret := range []string{"000123456789012345678", "Private Name", "private-note"} {
		if strings.Contains(string(masked), secret) {
			t.Fatal("masked preview contains private text")
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("PII response cacheable")
	}
}
func TestTransferBatchImportRejectsCrossOwnerCSRFMethodsAndAmbiguousPII(t *testing.T) {
	for _, tc := range []struct {
		body, csrf, role, class, method, query string
		want                                   int
	}{
		{importBody("8", "csv", privateImportCSV), "csrf", "merchant", "", "POST", "", 403},
		{importBody("", "csv", privateImportCSV), "", "merchant", "", "POST", "", 403},
		{importBody("", "csv", privateImportCSV), "csrf", "merchant", "", "GET", "", 405},
		{importBody("", "csv", privateImportCSV), "csrf", "admin", "unknown", "POST", "", 403},
		{importBody("", "csv", privateImportCSV), "csrf", "admin", "operator", "POST", "", 422},
		{`{"format":"csv","format":"xlsx","file_base64":"private-account"}`, "csrf", "merchant", "", "POST", "", 400},
		{`{"format":"csv","file_base64":"private-account","unknown":"Private Name"}`, "csrf", "merchant", "", "POST", "", 400},
		{importBody("", "xls", privateImportCSV), "csrf", "merchant", "", "POST", "", 422},
		{importBody("", "csv", strings.Replace(privateImportCSV, "0.10", "=private-account", 1)), "csrf", "merchant", "", "POST", "", 422},
		{importBody("", "csv", privateImportCSV), "csrf", "merchant", "", "POST", "?merchant_id=8", 400},
	} {
		s := &Server{}
		session := identity.Session{User: identity.User{ID: 7, Role: tc.role, Username: "local", AdminClass: tc.class}, CSRF: "csrf"}
		r := httptest.NewRequest(tc.method, "/console/v1/"+tc.role+"/transfer-batch-imports/preview"+tc.query, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", tc.csrf)
		w := httptest.NewRecorder()
		if !s.transferBatchImport(w, r, tc.role, session, []string{tc.role, "transfer-batch-imports", "preview"}) || w.Code != tc.want {
			t.Fatalf("boundary status=%d want=%d", w.Code, tc.want)
		}
		for _, secret := range []string{"private-account", "Private Name", "000123456789012345678"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("PII echoed on rejection")
			}
		}
	}
}
func TestTransferBatchImportSizeAndRateLimit(t *testing.T) {
	s := &Server{}
	session := identity.Session{User: identity.User{Role: "merchant", ID: 7, Username: "local"}, CSRF: "csrf"}
	invoke := func(body string) int {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", "csrf")
		w := httptest.NewRecorder()
		s.transferBatchImport(w, r, "merchant", session, []string{"merchant", "transfer-batch-imports", "preview"})
		return w.Code
	}
	for i := 0; i < 10; i++ {
		if code := invoke(importBody("", "csv", privateImportCSV)); code != 200 {
			t.Fatalf("rate rejected valid request %d: %d", i, code)
		}
	}
	if code := invoke(importBody("", "csv", privateImportCSV)); code != 429 {
		t.Fatalf("unbounded import CPU: %d", code)
	}
	s = &Server{}
	if code := invoke(fmt.Sprintf(`{"format":"csv","file_base64":"%s"}`, strings.Repeat("A", 720897))); code != 413 {
		t.Fatalf("request limit=%d", code)
	}
}
