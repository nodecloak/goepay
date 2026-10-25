package contract_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestTransferBatchImportAuthenticatedHTTPNoFinancialSideEffects(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	// A preview must work even with no provider flow configured. Its only input
	// is the caller's own upload, not another merchant's stored recipients.
	if s.ProviderFlow != nil {
		t.Fatal("preview fixture must have no provider")
	}
	token, csrf, e := s.Sessions.Create(identity.User{Role: "merchant", Username: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	admin, adminCSRF, e := s.Sessions.Create(identity.User{Role: "admin", Username: "admin", AdminClass: "super", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	snapshot := func() string {
		t.Helper()
		var out string
		e := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT count(*) FROM transfer_batches),(SELECT count(*) FROM transfers),(SELECT count(*) FROM journals),(SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM jobs),(SELECT count(*) FROM backoffice_audit),(SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb) FROM accounts a))::text`).Scan(&out)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	before := snapshot()
	request := func(role, cookie, csrf, body string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("POST", "/console/v1/"+role+"/transfer-batch-imports/preview", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: cookie})
		}
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private import response cacheable")
		}
		var v map[string]any
		if json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("invalid envelope")
		}
		return w.Code, v
	}
	csv := "序号,收款账号,收款人姓名,转账金额（元）,转账备注\n1,0000123456789012345678,Private Recipient,0.01,local-only\n"
	body := func(owner, format string, file []byte) string {
		b, _ := json.Marshal(map[string]string{"merchant_id": owner, "format": format, "file_base64": base64.StdEncoding.EncodeToString(file)})
		return string(b)
	}
	valid := body("", "csv", []byte(csv))
	for _, tc := range []struct {
		role, token, csrf, body string
		want                    int
	}{{"merchant", "", csrf, valid, 401}, {"merchant", token, "", valid, 403}, {"merchant", token, csrf, body("2", "csv", []byte(csv)), 403}, {"admin", admin, adminCSRF, body("", "csv", []byte(csv)), 422}, {"merchant", token, csrf, valid, 200}, {"admin", admin, adminCSRF, body("2", "csv", []byte(csv)), 200}, {"merchant", token, csrf, body("", "csv", []byte(strings.Replace(csv, "0.01", "1.001", 1))), 422}} {
		code, v := request(tc.role, tc.token, tc.csrf, tc.body)
		if code != tc.want {
			t.Fatalf("HTTP import boundary status=%d want=%d", code, tc.want)
		}
		if code == 200 {
			data := v["data"].(map[string]any)
			masked, _ := json.Marshal(data["rows"])
			if strings.Contains(string(masked), "Private Recipient") || strings.Contains(string(masked), "0000123456789012345678") {
				t.Fatal("public masked preview leaked private rows")
			}
			draft := data["draft_items"].([]any)[0].(map[string]any)
			if draft["account"] != "0000123456789012345678" || data["total_cents"] != "1" {
				t.Fatal("private draft lost exact input")
			}
		}
	}
	// Read the same original XLSX template used by both PHP versions. It has
	// only sequence placeholders, so this must be an honest empty-file error.
	xlsx, e := os.ReadFile("../../internal/transferimport/testdata/original-transfer.xlsx")
	if e != nil {
		t.Fatal(e)
	}
	if code, _ := request("merchant", token, csrf, body("", "xlsx", xlsx)); code != 422 {
		t.Fatal("empty original template imported")
	}
	var packageBytes bytes.Buffer
	writer := zip.NewWriter(&packageBytes)
	for name, content := range map[string]string{
		"[Content_Types].xml":        `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/></Types>`,
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet r:id="r1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="r1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/custom.xml"/></Relationships>`,
		"xl/worksheets/custom.xml":   `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>序号</t></is></c><c r="B1" t="inlineStr"><is><t>收款账号</t></is></c><c r="C1" t="inlineStr"><is><t>收款人姓名</t></is></c><c r="D1" t="inlineStr"><is><t>转账金额（元）</t></is></c><c r="E1" t="inlineStr"><is><t>转账备注</t></is></c></row><row r="2"><c r="A2"><v>1</v></c><c r="B2" t="inlineStr"><is><t>0000123456789012345678</t></is></c><c r="C2" t="inlineStr"><is><t>Private Recipient</t></is></c><c r="D2"><v>0.01</v></c></row></sheetData></worksheet>`,
	} {
		entry, e := writer.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = entry.Write([]byte(content)); e != nil {
			t.Fatal(e)
		}
	}
	if e = writer.Close(); e != nil {
		t.Fatal(e)
	}
	if code, _ := request("merchant", token, csrf, body("", "xlsx", packageBytes.Bytes())); code != 200 {
		t.Fatal("first worksheet relationship XLSX rejected", code)
	}
	if after := snapshot(); after != before {
		t.Fatal("preview created financial state, jobs, or stored uploaded PII")
	}
	if _, e = pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	if code, _ := request("merchant", token, csrf, valid); code != 401 {
		t.Fatal("disabled owner retained upload access")
	}
}
