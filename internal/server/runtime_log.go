package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

var runtimeLogWriteSlots = make(chan struct{}, 4)
var runtimeSQLState = regexp.MustCompile(`^[A-Z0-9]{5}$`)
var runtimeRequestNo = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

func safeRuntimeFailure(err error) (level, code, content string) {
	level, code, content = "error", "INTERNAL", "应用处理失败，请根据记录时间核对服务状态"
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "warning", "TIMEOUT", "请求处理超时"
	case errors.Is(err, context.Canceled):
		return "notice", "CANCELED", "请求处理已取消"
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) && runtimeSQLState.MatchString(state.SQLState()) {
		code = state.SQLState()
		content = "数据库操作失败（SQLSTATE " + code + "）"
	}
	return
}
func (s *Server) recordRuntimeFailure(err error) {
	if s.Pool == nil {
		return
	}
	select {
	case runtimeLogWriteSlots <- struct{}{}:
		defer func() { <-runtimeLogWriteSlots }()
	default:
		return
	}
	level, code, content := safeRuntimeFailure(err)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, e := s.Pool.Exec(ctx, `INSERT INTO runtime_events(level,error_code,content) VALUES($1,$2,$3)`, level, code, content); e != nil {
		slog.Error("runtime event persistence unavailable")
	}
}
func runtimeDay(now time.Time) time.Time {
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	at := now.In(zone)
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, zone)
}
func runtimeDays(raw string) (int, bool) {
	days, e := strconv.Atoi(raw)
	return days, e == nil && days >= 1 && days <= 9999 && strconv.Itoa(days) == raw
}

func (s *Server) runtimeLogConsole(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string) {
	if session.User.Role != "admin" || session.User.ID <= 0 || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "own super administrator session required", nil)
		return
	}
	if len(parts) == 4 && parts[3] == "cleanup" {
		s.runtimeLogCleanup(w, r, session)
		return
	}
	if len(parts) != 3 {
		response(w, 404, -1, "runtime log route not found", nil)
		return
	}
	if r.Method != "GET" {
		response(w, 405, -1, "GET required", nil)
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		response(w, 400, -1, "invalid runtime log query", nil)
		return
	}
	for key, values := range q {
		if (key != "date" && key != "page" && key != "limit") || len(values) != 1 {
			response(w, 400, -1, "invalid runtime log filter", nil)
			return
		}
	}
	day := runtimeDay(time.Now())
	if raw, exists := q["date"]; exists {
		day, e = time.ParseInLocation("2006-01-02", raw[0], day.Location())
		if e != nil || day.Year() < 1 || day.Format("2006-01-02") != raw[0] {
			response(w, 400, -1, "invalid runtime log date", nil)
			return
		}
	}
	page, limit := 1, 30
	for key, target := range map[string]*int{"page": &page, "limit": &limit} {
		if raw, exists := q[key]; exists {
			n, err := strconv.Atoi(raw[0])
			if err != nil || n < 1 || strconv.Itoa(n) != raw[0] {
				response(w, 400, -1, "invalid runtime log pagination", nil)
				return
			}
			*target = n
		}
	}
	if limit > 100 || page > 100000 {
		response(w, 400, -1, "invalid runtime log pagination", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		s.fail(w, e)
		return
	}
	defer tx.Rollback(ctx)
	var total int64
	until := day.AddDate(0, 0, 1)
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM runtime_events WHERE archived_at IS NULL AND created_at >= $1 AND created_at < $2`, day, until).Scan(&total); e != nil {
		s.fail(w, e)
		return
	}
	rows, e := tx.Query(ctx, `SELECT id::text,created_at,level,content,error_code FROM runtime_events WHERE archived_at IS NULL AND created_at >= $1 AND created_at < $2 ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, day, until, limit, (page-1)*limit)
	if e != nil {
		s.fail(w, e)
		return
	}
	items := []map[string]any{}
	for rows.Next() {
		var id, level, content, code string
		var at time.Time
		if e = rows.Scan(&id, &at, &level, &content, &code); e != nil {
			rows.Close()
			s.fail(w, e)
			return
		}
		items = append(items, map[string]any{"id": id, "time": at, "type": level, "content": content, "error_code": code})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		s.fail(w, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		s.fail(w, e)
		return
	}
	response(w, 200, 0, "", map[string]any{"items": items, "total": total, "page": page, "limit": limit, "date": day.Format("2006-01-02"), "timezone": "Asia/Shanghai"})
}

func (s *Server) runtimeLogCleanup(w http.ResponseWriter, r *http.Request, session identity.Session) {
	if r.Method == "GET" {
		q, e := url.ParseQuery(r.URL.RawQuery)
		values, exists := q["days"]
		if e != nil || len(q) != 1 || !exists || len(values) != 1 {
			response(w, 400, -1, "single cleanup days required", nil)
			return
		}
		days, valid := runtimeDays(values[0])
		if !valid {
			response(w, 400, -1, "cleanup days must be 1..9999", nil)
			return
		}
		cutoff := runtimeDay(time.Now()).AddDate(0, 0, -days)
		var count string
		if e = s.Pool.QueryRow(r.Context(), `SELECT count(*)::text FROM runtime_events WHERE archived_at IS NULL AND created_at<$1`, cutoff).Scan(&count); e != nil {
			s.fail(w, e)
			return
		}
		response(w, 200, 0, "", map[string]any{"days": days, "cutoff": cutoff.Format(time.RFC3339), "count": count})
		return
	}
	if r.Method != "POST" || r.URL.RawQuery != "" {
		response(w, 405, -1, "cleanup POST without query required", nil)
		return
	}
	var in struct {
		Days       int    `json:"days"`
		Cutoff     string `json:"cutoff"`
		ExternalNo string `json:"external_no"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Days < 1 || in.Days > 9999 || !runtimeRequestNo.MatchString(in.ExternalNo) {
		response(w, 422, -1, "invalid runtime cleanup request", nil)
		return
	}
	raw, _ := json.Marshal(in)
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		s.fail(w, e)
		return
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/runtime-cleanup/%d/%s", session.User.ID, in.ExternalNo)); e != nil {
		s.fail(w, e)
		return
	}
	var prior []byte
	e = tx.QueryRow(ctx, `SELECT changes FROM backoffice_audit WHERE actor_role='admin' AND actor_id=$1 AND resource='runtime-log' AND operation='archive' AND changes->>'external_no'=$2 ORDER BY id LIMIT 1`, session.User.ID, in.ExternalNo).Scan(&prior)
	if e == nil {
		var saved struct {
			Hash  string `json:"request_hash"`
			Count string `json:"count"`
		}
		if json.Unmarshal(prior, &saved) != nil || saved.Hash != hash {
			response(w, 409, -1, "original runtime cleanup request changed", nil)
			return
		}
		if e = tx.Commit(ctx); e != nil {
			s.fail(w, e)
			return
		}
		response(w, 200, 0, "", map[string]any{"count": saved.Count, "replayed": true, "cutoff": in.Cutoff})
		return
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		s.fail(w, e)
		return
	}
	cutoff := runtimeDay(time.Now()).AddDate(0, 0, -in.Days)
	if in.Cutoff != cutoff.Format(time.RFC3339) {
		response(w, 409, -1, "runtime cleanup preview expired; read again", nil)
		return
	}
	var count string
	if e = tx.QueryRow(ctx, `WITH changed AS(UPDATE runtime_events SET archived_at=now() WHERE archived_at IS NULL AND created_at<$1 RETURNING id) SELECT count(*)::text FROM changed`, cutoff).Scan(&count); e != nil {
		s.fail(w, e)
		return
	}
	changes, _ := json.Marshal(map[string]any{"request_hash": hash, "external_no": in.ExternalNo, "days": in.Days, "cutoff": in.Cutoff, "count": count})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'runtime-log',0,'archive',$3)`, session.User.ID, session.User.Username, changes); e != nil {
		s.fail(w, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		s.fail(w, e)
		return
	}
	response(w, 200, 0, "", map[string]any{"count": count, "replayed": false, "cutoff": in.Cutoff})
}
