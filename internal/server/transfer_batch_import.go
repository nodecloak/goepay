package server

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/transferimport"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"
)

const importRequestLimit = 704 * 1024

// This is a pure parser under the ordinary console session middleware. No
// payment flow/store method, durable batch, financial grant or outbox is touched.
// draft_items is sensitive: only the same authenticated browser may hold it in
// transient memory. It is intentionally distinct from the masked display rows.
func (s *Server) transferBatchImport(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "transfer-batch-imports" {
		return false
	}
	if len(parts) != 3 || parts[2] != "preview" {
		response(w, 404, -1, "import operation unavailable", nil)
		return true
	}
	if session.User.ID <= 0 || session.User.Username == "" || session.User.Role != role || (role != "merchant" && role != "admin") || role == "admin" && (session.User.AdminClass != "super" && session.User.AdminClass != "operator") {
		response(w, 403, -1, "authenticated business role required", nil)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "POST required", nil)
		return true
	}
	if session.CSRF == "" || subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		response(w, 415, -1, "application/json required", nil)
		return true
	}
	if !s.allowLogin(r.RemoteAddr, "transfer-import:"+role, fmt.Sprint(session.User.ID), time.Now()) {
		w.Header().Set("Retry-After", "900")
		response(w, 429, -1, "import rate limited", nil)
		return true
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, importRequestLimit+1))
	if len(raw) > importRequestLimit {
		response(w, 413, -1, "import request too large", nil)
		return true
	}
	if e != nil || !utf8.Valid(raw) {
		response(w, 400, -1, "invalid import request", nil)
		return true
	}
	fields, e := importJSON(raw)
	if e != nil {
		response(w, 400, -1, "invalid or ambiguous import fields", nil)
		return true
	}
	mid := session.User.ID
	owner := fields["merchant_id"]
	if role == "admin" {
		mid, e = strconv.ParseInt(owner, 10, 64)
		if e != nil || mid <= 0 || fmt.Sprint(mid) != owner {
			response(w, 422, -1, "canonical merchant_id required", nil)
			return true
		}
	} else if owner != "" && owner != fmt.Sprint(mid) {
		response(w, 403, -1, "merchant ownership required", nil)
		return true
	}
	encoded := fields["file_base64"]
	if len(encoded) > base64.StdEncoding.EncodedLen(transferimport.MaxFileBytes) {
		response(w, 413, -1, "maximum file size is 512 KiB", nil)
		return true
	}
	file, e := base64.StdEncoding.Strict().DecodeString(encoded)
	if e != nil || base64.StdEncoding.EncodeToString(file) != encoded {
		response(w, 400, -1, "canonical base64 file required", nil)
		return true
	}
	draft, e := transferimport.Parse(fields["format"], file)
	clear(file)
	if e != nil {
		var problem *transferimport.Error
		if errors.As(e, &problem) {
			response(w, 422, -1, problem.Error(), map[string]any{"error": problem.Code, "source_row": problem.Row})
		} else {
			response(w, 422, -1, "import validation failed", nil)
		}
		return true
	}
	rows := make([]map[string]any, 0, len(draft.Rows))
	for _, v := range draft.Rows {
		rows = append(rows, map[string]any{"source_row": v.SourceRow, "serial": v.Serial, "account_masked": importMask(v.Account), "name_masked": importMask(v.Name), "amount": v.Amount, "has_remark": v.Remark != ""})
	}
	response(w, 200, 0, "preview only; apply locally, then use the existing batch review and funds authorization", map[string]any{"merchant_id": fmt.Sprint(mid), "format": fields["format"], "count": len(rows), "total_cents": fmt.Sprint(draft.TotalCents), "rows": rows, "draft_items": draft.Rows})
	return true
}
func importMask(raw string) string {
	v := []rune(raw)
	if len(v) == 0 {
		return ""
	}
	if len(v) <= 4 {
		return "***"
	}
	return string(v[:1]) + "***" + string(v[len(v)-2:])
}

// The upload schema is deliberately flat and string-only. Decode tokens so
// duplicate keys, nulls, objects, unknown fields and trailing JSON are rejected.
func importJSON(raw []byte) (map[string]string, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, errors.New("object required")
	}
	out := map[string]string{}
	for d.More() {
		t, e = d.Token()
		key, ok := t.(string)
		if e != nil || !ok {
			return nil, errors.New("field required")
		}
		if key != "merchant_id" && key != "format" && key != "file_base64" {
			return nil, errors.New("unknown field")
		}
		if _, ok = out[key]; ok {
			return nil, errors.New("duplicate field")
		}
		t, e = d.Token()
		value, ok := t.(string)
		if e != nil || !ok {
			return nil, errors.New("string required")
		}
		out[key] = value
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') {
		return nil, errors.New("invalid end")
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	return out, nil
}
