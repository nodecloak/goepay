package backoffice

import (
	"context"
	"encoding/csv"
	"net/http/httptest"
	"strings"
	"testing"
)

// A6-08: the merchant directory exports to CSV like Pro user/export, through the
// same bounded pattern, never leaking a secret field, honouring the directory
// filter, neutralising spreadsheet formulas, and writing an audit row.
func TestMerchantDirectoryCSVExport(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	// A name beginning with '=' must be neutralised; email/phone present.
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET name='=cmd|evil', enabled=true,
 profile=profile||'{"email":"csv@example.invalid","phone":"13000000009","api_key":"TOPSECRETKEY"}'::jsonb WHERE id=101;
 UPDATE merchants SET enabled=false WHERE id=102`); err != nil {
		t.Fatal(err)
	}

	do := func(rawQuery string, principal Principal, wantStatus int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/resources/merchants?"+rawQuery, nil)
		s.Serve(w, r, principal, []string{"merchants"})
		if w.Code != wantStatus {
			t.Fatalf("query %q: status=%d want %d body=%s", rawQuery, w.Code, wantStatus, w.Body.String())
		}
		return w
	}

	// A merchant principal must never reach the admin merchant export.
	do("format=csv", Principal{Role: "merchant", SubjectID: 101, Username: "m"}, 403)

	// Bad format value rejected.
	do("format=xlsx", admin, 400)

	w := do("format=csv", admin, 200)
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content-type=%q", ct)
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, "\xef\xbb\xbf") {
		t.Fatal("missing UTF-8 BOM")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\xef\xbb\xbf")))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) < 2 {
		t.Fatalf("expected header + >=1 row, got %d", len(records))
	}
	header := strings.Join(records[0], ",")
	for _, secret := range []string{"api_key", "key", "rsa", "RSA", "account", "secret", "password"} {
		if strings.Contains(strings.ToLower(header), strings.ToLower(secret)) {
			t.Fatalf("secret-looking header %q present: %s", secret, header)
		}
	}
	for _, want := range []string{"商户ID", "商户名称", "账户余额（分）", "邮箱", "手机"} {
		if !strings.Contains(header, want) {
			t.Fatalf("header missing %q: %s", want, header)
		}
	}
	// Row for merchant 101: formula-neutralised name, email, no secret value.
	var found bool
	for _, row := range records[1:] {
		line := strings.Join(row, "|")
		if strings.Contains(line, "csv@example.invalid") {
			found = true
			if strings.Contains(line, "TOPSECRETKEY") {
				t.Fatalf("api_key value leaked into CSV: %s", line)
			}
			if !strings.Contains(line, "'=cmd|evil") {
				t.Fatalf("formula not neutralised: %s", line)
			}
		}
	}
	if !found {
		t.Fatal("merchant 101 row not found in export")
	}

	// Disabled-only filter returns merchant 102 (enabled=false) but not 101.
	w2 := do("format=csv&dstatus=status_0", admin, 200)
	b2 := strings.TrimPrefix(w2.Body.String(), "\xef\xbb\xbf")
	if strings.Contains(b2, "csv@example.invalid") {
		t.Fatal("disabled filter leaked enabled merchant 101")
	}

	// Audit row written for the export.
	var audits int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE resource='merchants' AND operation='export-csv'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits < 2 {
		t.Fatalf("expected >=2 export-csv audit rows, got %d", audits)
	}
}
