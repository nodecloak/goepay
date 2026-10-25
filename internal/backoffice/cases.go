package backoffice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Service) serveCase(w http.ResponseWriter, r *http.Request, p Principal, parts []string) bool {
	if len(parts) != 3 || (parts[0] != "complaints" && parts[0] != "onboarding-applications") {
		return false
	}
	resource := parts[0]
	id, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil {
		writeError(w, ErrInvalid)
		return true
	}
	current, e := s.Get(r.Context(), p, resource, id)
	if e != nil {
		writeError(w, e)
		return true
	}
	action := parts[2]
	if action == "history" && r.Method == "GET" {
		rows, e := s.pool.Query(r.Context(), "SELECT id,actor_role,actor_name,content,created_at FROM case_comments WHERE record_id=$1 ORDER BY id", id)
		if e != nil {
			writeError(w, e)
			return true
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var cid int64
			var role, name, content string
			var created time.Time
			if e = rows.Scan(&cid, &role, &name, &content, &created); e != nil {
				writeError(w, e)
				return true
			}
			items = append(items, map[string]any{"id": strconv.FormatInt(cid, 10), "actor_role": role, "actor_name": name, "content": content, "created_at": created})
		}
		if e = rows.Err(); e != nil {
			writeError(w, e)
			return true
		}
		rows.Close()
		audits, e := s.pool.Query(r.Context(), "SELECT id,operation,actor_role,actor_name,changes,created_at FROM backoffice_audit WHERE resource=$1 AND resource_id=$2 ORDER BY id", resource, id)
		if e != nil {
			writeError(w, e)
			return true
		}
		defer audits.Close()
		events := []map[string]any{}
		for audits.Next() {
			var aid int64
			var operation, role, name string
			var changes []byte
			var created time.Time
			if e = audits.Scan(&aid, &operation, &role, &name, &changes, &created); e != nil {
				writeError(w, e)
				return true
			}
			events = append(events, map[string]any{"id": strconv.FormatInt(aid, 10), "operation": operation, "actor_role": role, "actor_name": name, "changes": decodeFields(changes), "created_at": created})
		}
		if e = audits.Err(); e != nil {
			writeError(w, e)
			return true
		}
		write(w, 200, map[string]any{"items": items, "events": events}, "")
		return true
	}
	if r.Method != "POST" {
		write(w, 405, nil, "method not allowed")
		return true
	}
	in, ok := body(w, r)
	if !ok {
		return true
	}
	switch action {
	case "comments":
		if len(in) != 1 {
			writeError(w, ErrInvalid)
			return true
		}
		content, e := stringInput(in, "content")
		if e != nil || len(strings.TrimSpace(content)) < 1 || len(content) > 10000 {
			writeError(w, ErrInvalid)
			return true
		}
		if e = s.appendCaseComment(r.Context(), p, resource, id, content); e != nil {
			writeError(w, e)
			return true
		}
		write(w, 201, map[string]bool{"recorded": true}, "local case comment recorded; no upstream action")
	case "submit":
		if resource != "onboarding-applications" || len(in) != 0 {
			writeError(w, ErrInvalid)
			return true
		}
		state := fmt.Sprint(current.Fields["status"])
		if state != "draft" && state != "action_required" && state != "under_review" {
			writeError(w, ErrInvalid)
			return true
		}
		if e = s.caseState(r.Context(), p, resource, id, "under_review"); e != nil {
			writeError(w, e)
			return true
		}
		write(w, 200, map[string]string{"status": "under_review"}, "submitted for local review; no provider onboarding submission")
	case "review":
		if p.Role != "admin" {
			writeError(w, ErrForbidden)
			return true
		}
		if len(in) > 2 {
			writeError(w, ErrInvalid)
			return true
		}
		state, e := stringInput(in, "status")
		if e != nil {
			writeError(w, ErrInvalid)
			return true
		}
		valid := false
		for _, v := range []string{"approved", "rejected", "action_required"} {
			if resource == "onboarding-applications" && state == v {
				valid = true
			}
		}
		for _, v := range []string{"processing", "resolved", "closed"} {
			if resource == "complaints" && state == v {
				valid = true
			}
		}
		if !valid {
			writeError(w, ErrInvalid)
			return true
		}
		comment, ce := stringInput(in, "comment")
		if ce != nil || len(comment) > 10000 {
			writeError(w, ErrInvalid)
			return true
		}
		for k := range in {
			if k != "status" && k != "comment" {
				writeError(w, ErrInvalid)
				return true
			}
		}
		if e = s.caseStateWithComment(r.Context(), p, resource, id, state, comment); e != nil {
			writeError(w, e)
			return true
		}
		write(w, 200, map[string]string{"status": state}, "local review recorded; no upstream refund or onboarding action")
	default:
		write(w, 404, nil, "not found")
	}
	return true
}
func (s *Service) appendCaseComment(ctx context.Context, p Principal, resource string, id int64, content string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var exists int64
	e = tx.QueryRow(ctx, "SELECT id FROM backoffice_records WHERE id=$1 AND resource=$2 AND ($3::text='admin' OR owner_id=$4) FOR UPDATE", id, resource, p.Role, p.SubjectID).Scan(&exists)
	if e != nil {
		return businessError(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO case_comments(record_id,actor_role,actor_id,actor_name,content) VALUES($1,$2,$3,$4,$5)", id, p.Role, p.SubjectID, p.Username, content); e != nil {
		return e
	}
	if e = audit(ctx, tx, p, resource, id, "comment", map[string]any{"content": content}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Service) caseState(ctx context.Context, p Principal, resource string, id int64, state string) error {
	return s.caseStateWithComment(ctx, p, resource, id, state, "")
}
func (s *Service) caseStateWithComment(ctx context.Context, p Principal, resource string, id int64, state, comment string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var raw []byte
	e = tx.QueryRow(ctx, "SELECT body FROM backoffice_records WHERE id=$1 AND resource=$2 AND ($3::text='admin' OR owner_id=$4) FOR UPDATE", id, resource, p.Role, p.SubjectID).Scan(&raw)
	if e != nil {
		return businessError(e)
	}
	v := decodeFields(raw)
	old := fmt.Sprint(v["status"])
	if old == state && comment == "" {
		return nil
	}
	if p.Role != "admin" && (old != "draft" && old != "action_required" || state != "under_review") {
		return ErrForbidden
	}
	v["status"] = state
	raw, e = json.Marshal(v)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE backoffice_records SET body=$2::jsonb,updated_at=now() WHERE id=$1", id, string(raw)); e != nil {
		return e
	}
	if e = audit(ctx, tx, p, resource, id, "state", map[string]any{"from_status": old, "to_status": state}); e != nil {
		return e
	}
	if comment != "" {
		if _, e = tx.Exec(ctx, "INSERT INTO case_comments(record_id,actor_role,actor_id,actor_name,content) VALUES($1,$2,$3,$4,$5)", id, p.Role, p.SubjectID, p.Username, comment); e != nil {
			return e
		}
		if e = audit(ctx, tx, p, resource, id, "comment", map[string]any{"content": comment}); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
