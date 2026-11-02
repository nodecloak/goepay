package server

import (
	"context"
	"encoding/json"
	"errors"
	"goepay/internal/identity"
	notice "goepay/internal/merchantnotice"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type templateRow230 struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Value string `json:"value"`
}
type templateInput230 struct {
	Expected []templateRow230  `json:"expected"`
	Values   map[string]string `json:"values"`
}

func validTemplate230(key, value string) bool {
	known, e := notice.ValidatePlatformSetting(key, value)
	return strings.HasPrefix(key, "wxnotice_tpl_") && known && e == nil
}
func (s *Server) noticeTemplates230(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "settings" || parts[2] != "notice-templates" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.Issuer != nil || session.User.AdminClass != "super" {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.Method != "PUT" {
		w.Header().Set("Allow", "PUT")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	var rawInput json.RawMessage
	if !decodeFundsJSON(w, r, &rawInput) {
		return true
	}
	if !validTemplateInputJSON230(rawInput) {
		response(w, 400, -1, "explicit template snapshot strings required", nil)
		return true
	}
	var in templateInput230
	if json.Unmarshal(rawInput, &in) != nil {
		response(w, 400, -1, "invalid template snapshot", nil)
		return true
	}
	if in.Expected == nil || len(in.Expected) > 80 || len(in.Values) == 0 || len(in.Values) > 80 {
		response(w, 422, -1, "complete template snapshot required", nil)
		return true
	}
	for k, v := range in.Values {
		if !validTemplate230(k, v) {
			response(w, 422, -1, "invalid template field", nil)
			return true
		}
	}
	rows, e := s.saveNoticeTemplates230(r.Context(), session, token, in)
	if e != nil {
		status := 503
		if errors.Is(e, notice.ErrInvalid) {
			status = 422
		} else if errors.Is(e, notice.ErrConflict) {
			status = 409
		} else if errors.Is(e, identity.ErrAdminSession) {
			status = 403
		}
		response(w, status, -1, "template configuration not confirmed; reload before retrying", nil)
	} else {
		response(w, 200, 0, "", rows)
	}
	return true
}
func (s *Server) saveNoticeTemplates230(ctx context.Context, session identity.Session, token string, in templateInput230) ([]templateRow230, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(7170140001)"); e != nil {
		return nil, e
	}
	check := func() error { return identity.CheckOriginalSuperAdminSessionTx(ctx, tx, session.User, token, false) }
	if e = check(); e != nil {
		return nil, e
	}
	expected := map[string]templateRow230{}
	for _, row := range in.Expected {
		n, e := strconv.ParseInt(row.ID, 10, 64)
		if e != nil || n <= 0 || strconv.FormatInt(n, 10) != row.ID || !validTemplate230(row.Key, row.Value) {
			return nil, notice.ErrInvalid
		}
		if _, ok := expected[row.Key]; ok {
			return nil, notice.ErrInvalid
		}
		expected[row.Key] = row
	}
	current := map[string]templateRow230{}
	result, e := tx.Query(ctx, `SELECT id::text,body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND starts_with(body->>'key','wxnotice_tpl_') ORDER BY id FOR UPDATE`)
	if e != nil {
		return nil, e
	}
	for result.Next() {
		var row templateRow230
		if e = result.Scan(&row.ID, &row.Key, &row.Value); e != nil {
			result.Close()
			return nil, e
		}
		if _, ok := current[row.Key]; ok {
			result.Close()
			return nil, notice.ErrConflict
		}
		current[row.Key] = row
	}
	e = result.Err()
	result.Close()
	if e != nil {
		return nil, e
	}
	if len(current) != len(expected) {
		return nil, notice.ErrConflict
	}
	for key, row := range current {
		if expected[key] != row {
			return nil, notice.ErrConflict
		}
	}
	final := map[string]string{}
	for k, row := range current {
		final[k] = row.Value
	}
	for k, v := range in.Values {
		final[k] = v
	}
	used := map[string]bool{}
	for k, v := range final {
		if !validTemplate230(k, v) {
			return nil, notice.ErrInvalid
		}
		rest := strings.TrimPrefix(k, "wxnotice_tpl_")
		parts := strings.SplitN(rest, "_", 2)
		if len(parts) == 1 || v == "" {
			continue
		}
		slot := parts[0] + "/" + v
		if used[slot] {
			return nil, notice.ErrInvalid
		}
		used[slot] = true
	}
	keys := make([]string, 0, len(in.Values))
	for k := range in.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]templateRow230, 0, len(keys))
	for _, key := range keys {
		row, exists := current[key]
		value := in.Values[key]
		body, _ := json.Marshal(map[string]string{"key": key, "value": value})
		op := "update"
		if exists {
			_, e = tx.Exec(ctx, "UPDATE backoffice_records SET body=$2,updated_at=clock_timestamp() WHERE id=$1", row.ID, body)
		} else {
			op = "create"
			row.Key = key
			e = tx.QueryRow(ctx, "INSERT INTO backoffice_records(resource,body) VALUES('settings',$1) RETURNING id::text", body).Scan(&row.ID)
		}
		if e != nil {
			return nil, e
		}
		row.Value = value
		if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'settings',$3,$4,$5)`, session.User.ID, session.User.Username, row.ID, op, body); e != nil {
			return nil, e
		}
		out = append(out, row)
	}
	if e = check(); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return out, nil
}

// JSON null is not an explicit request to clear a template. Every snapshot
// dimension and value must be a present string before typed decoding.
func validTemplateInputJSON230(raw json.RawMessage) bool {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil || len(top) != 2 {
		return false
	}
	var rows []map[string]json.RawMessage
	var values map[string]json.RawMessage
	if string(top["expected"]) == "null" || json.Unmarshal(top["expected"], &rows) != nil || rows == nil || json.Unmarshal(top["values"], &values) != nil || values == nil {
		return false
	}
	text := func(raw json.RawMessage) bool {
		var s string
		return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &s) == nil
	}
	for _, row := range rows {
		if len(row) != 3 || !text(row["id"]) || !text(row["key"]) || !text(row["value"]) {
			return false
		}
	}
	for _, value := range values {
		if !text(value) {
			return false
		}
	}
	return true
}
