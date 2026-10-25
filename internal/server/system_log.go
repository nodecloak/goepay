package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
)

var systemLogSlots = make(chan struct{}, 4)

func (s *Server) systemLogConsole(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string) {
	if session.User.Role != "admin" || session.User.ID <= 0 || session.Issuer != nil || (session.User.AdminClass != "super" && session.User.AdminClass != "operator") {
		response(w, 403, -1, "own administrator session required", nil)
		return
	}
	if len(parts) != 3 {
		response(w, 404, -1, "system log route not found", nil)
		return
	}
	if r.Method != "GET" {
		response(w, 405, -1, "GET required", nil)
		return
	}
	invalid := func() { response(w, 400, -1, "invalid system log filter", nil) }
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		invalid()
		return
	}
	allowed := map[string]bool{"column": true, "value": true, "uid": true, "page": true, "limit": true}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 || !utf8.ValidString(values[0]) || strings.ContainsAny(values[0], "\x00\r\n") {
			invalid()
			return
		}
	}
	if uid, exists := q["uid"]; exists {
		if _, set := q["column"]; set {
			invalid()
			return
		}
		if _, set := q["value"]; set {
			invalid()
			return
		}
		q.Set("column", "uid")
		q.Set("value", uid[0])
	}
	column, value := q.Get("column"), q.Get("value")
	if column == "" {
		column = "uid"
	}
	if column != "uid" && column != "ip" {
		invalid()
		return
	}
	if len(value) > 253 {
		invalid()
		return
	}
	if column == "uid" && value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id < 0 || strconv.FormatInt(id, 10) != value {
			invalid()
			return
		}
	}
	page, limit := 1, 30
	for key, target := range map[string]*int{"page": &page, "limit": &limit} {
		if raw, exists := q[key]; exists {
			n, err := strconv.Atoi(raw[0])
			if err != nil || n < 1 || strconv.Itoa(n) != raw[0] {
				invalid()
				return
			}
			*target = n
		}
	}
	if page > 100000 || limit > 100 {
		invalid()
		return
	}
	select {
	case systemLogSlots <- struct{}{}:
		defer func() { <-systemLogSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "system logs busy", nil)
		return
	}
	tenants, globals := []string{}, []string{}
	titles := map[string]string{}
	for _, scope := range backoffice.AuditResourceScopes() {
		titles[scope.Name] = scope.Title
		if scope.Tenant {
			tenants = append(tenants, scope.Name)
		} else {
			globals = append(globals, scope.Name)
		}
	}
	// Only registered, validated create/update snapshots establish tenant
	// ownership. Go's original owner parser accepts leading zero/plus; regex and
	// bigint validation constrain PostgreSQL to those same integer semantics.
	snapshot := `CASE WHEN a.changes->>'merchant_id' ~ '^\+?[0-9]+$' AND pg_input_is_valid(a.changes->>'merchant_id','bigint') THEN CASE WHEN (a.changes->>'merchant_id')::bigint>0 THEN (a.changes->>'merchant_id')::bigint::text END END`
	subject := `CASE WHEN a.actor_role='merchant' AND a.actor_id>0 THEN a.actor_id::text
 WHEN a.actor_role='admin' AND a.resource='merchants' AND a.resource_id>0 THEN a.resource_id::text
 WHEN a.actor_role='admin' AND a.resource='orders' THEN o.merchant_id::text
 WHEN a.actor_role='admin' AND a.resource='refunds' THEN f.merchant_id::text
 WHEN a.actor_role='admin' AND a.resource='transfers' THEN t.merchant_id::text
 WHEN a.actor_role='admin' AND a.resource IN ('settlements','internal-transfers','profitsharing') THEN lr.merchant_id::text
 WHEN a.actor_role='admin' AND a.resource=ANY($1::text[]) AND a.operation IN ('create','update') THEN ` + snapshot + `
 WHEN a.actor_role='admin' AND a.resource=ANY($1::text[]) AND a.operation IN ('delete','archive') THEN prior.uid
 WHEN a.actor_role='admin' AND (a.resource=ANY($2::text[]) OR a.operation='export-csv') THEN '0' END`
	loginFilter := `($3::text='' OR (l.role='admin' AND $3='0') OR (l.role='merchant' AND l.success AND l.subject_id=NULLIF($3,'')::bigint))`
	auditFilter := `($3::text='' OR subject.uid=$3)`
	if column == "ip" {
		loginFilter = `($3::text='' OR l.ip=$3)`
		auditFilter = `$3::text=''`
	}
	base := `WITH events AS (
 SELECT 'login'::text AS source,l.id::text AS id,l.id AS sequence_id,CASE WHEN l.role='admin' THEN '0' WHEN l.role='merchant' AND l.success THEN l.subject_id::text END AS uid,l.role AS actor_role,CASE WHEN l.success THEN 'login-success' ELSE 'login-failure' END AS operation,'sessions'::text AS resource,l.ip,l.created_at AS occurred_at
 FROM console_login_audit l WHERE ` + loginFilter + `
 UNION ALL
 SELECT 'business'::text,a.id::text,a.id,subject.uid,a.actor_role,a.operation,a.resource,''::text,a.created_at
 FROM backoffice_audit a
 LEFT JOIN orders o ON a.resource='orders' AND o.id=a.resource_id
 LEFT JOIN refunds f ON a.resource='refunds' AND f.id=a.resource_id
 LEFT JOIN transfers t ON a.resource='transfers' AND t.id=a.resource_id
 LEFT JOIN ledger_requests lr ON a.resource=lr.kind AND lr.id=a.resource_id
 LEFT JOIN LATERAL (
 SELECT CASE WHEN h.actor_role='merchant' AND h.actor_id>0 THEN h.actor_id::text
 WHEN h.actor_role='admin' THEN ` + strings.ReplaceAll(snapshot, "a.changes", "h.changes") + ` END AS uid
 FROM backoffice_audit h
 WHERE a.actor_role='admin' AND a.resource=ANY($1::text[]) AND a.operation IN ('delete','archive')
 AND h.resource=a.resource AND h.resource_id=a.resource_id AND h.id<a.id
 AND h.operation IN ('create','update')
 ORDER BY h.id DESC LIMIT 1
 ) prior ON true
 CROSS JOIN LATERAL(SELECT ` + subject + ` AS uid) subject
 WHERE a.actor_role IN ('admin','merchant') AND ` + auditFilter + `) `
	args := []any{tenants, globals, value}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		s.fail(w, e)
		return
	}
	defer tx.Rollback(ctx)
	var total int64
	if e = tx.QueryRow(ctx, base+`SELECT count(*) FROM events`, args...).Scan(&total); e != nil {
		s.fail(w, e)
		return
	}
	rows, e := tx.Query(ctx, base+`SELECT source,id,uid,actor_role,operation,resource,ip,occurred_at FROM events ORDER BY occurred_at DESC,source ASC,sequence_id DESC LIMIT $4 OFFSET $5`, append(args, limit, (page-1)*limit)...)
	if e != nil {
		s.fail(w, e)
		return
	}
	items := []map[string]any{}
	for rows.Next() {
		var source, id, role, operation, resource, ip string
		var uid *string
		var at time.Time
		if e = rows.Scan(&source, &id, &uid, &role, &operation, &resource, &ip, &at); e != nil {
			rows.Close()
			s.fail(w, e)
			return
		}
		kind := "unknown"
		if uid != nil {
			n, err := strconv.ParseInt(*uid, 10, 64)
			if err != nil || n < 0 || strconv.FormatInt(n, 10) != *uid {
				uid = nil
			} else if n == 0 {
				kind = "admin"
			} else {
				kind = "merchant"
			}
		}
		eventType := systemLogType(source, operation, resource, titles)
		items = append(items, map[string]any{"source": source, "id": id, "uid": uid, "subject_kind": kind, "actor_role": role, "type": eventType, "ip": ip, "date": at})
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
	response(w, 200, 0, "recorded sources only; unverified subjects remain unknown", map[string]any{"items": items, "total": total, "page": page, "limit": limit})
}

func systemLogType(source, operation, resource string, titles map[string]string) string {
	if source == "login" {
		if operation == "login-success" {
			return "登录成功"
		}
		return "登录失败"
	}
	actions := map[string]string{"create": "新增", "update": "修改", "delete": "删除", "archive": "归档", "approve": "审核通过", "reject": "审核拒绝", "export-csv": "导出CSV"}
	action := actions[operation]
	if action == "" {
		action = operation
	}
	title := titles[resource]
	if title == "" {
		title = resource
	}
	return action + " · " + title
}
