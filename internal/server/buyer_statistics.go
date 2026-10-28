package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var buyerStatisticsSlots = make(chan struct{}, 4)

func (s *Server) originalBuyerStatistics(w http.ResponseWriter, r *http.Request, role string, session identity.Session) {
	if role != "admin" || session.User.Role != "admin" || session.User.ID <= 0 {
		response(w, 403, -1, "administrator buyer statistics required", nil)
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	allowed := map[string]bool{"view": true, "from": true, "to": true, "method": true, "type": true, "user": true, "sort": true, "direction": true, "page": true, "limit": true, "format": true}
	invalid := func() { response(w, 400, -1, "invalid buyer statistics filter", nil) }
	if err != nil {
		invalid()
		return
	}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 || !utf8.ValidString(values[0]) || strings.ContainsAny(values[0], "\x00\r\n") {
			invalid()
			return
		}
	}
	method := q.Get("method")
	if method == "" {
		method = "0"
	}
	if method != "0" && method != "1" && method != "2" {
		invalid()
		return
	}
	kind := q.Get("type")
	if kind != "" && !provider.CatalogPayType(kind) && kind != "sandbox" {
		invalid()
		return
	}
	user := q.Get("user")
	if len(user) > 253 {
		invalid()
		return
	}
	sort := q.Get("sort")
	if sort == "" {
		sort = "order_count"
	}
	if sort != "order_count" && sort != "pay_money" {
		invalid()
		return
	}
	direction := q.Get("direction")
	if direction == "" {
		direction = "desc"
	}
	if direction != "asc" && direction != "desc" {
		invalid()
		return
	}
	page, limit := 1, 20
	for key, target := range map[string]*int{"page": &page, "limit": &limit} {
		if raw, exists := q[key]; exists {
			n, e := strconv.Atoi(raw[0])
			if e != nil || n < 1 || strconv.Itoa(n) != raw[0] {
				invalid()
				return
			}
			*target = n
		}
	}
	if limit > 100 || page > 100000 {
		invalid()
		return
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	today := time.Now().In(zone).Format("2006-01-02")
	dates := []time.Time{}
	for _, key := range []string{"from", "to"} {
		raw := q.Get(key)
		if raw == "" {
			raw = today
		}
		at, e := time.ParseInLocation("2006-01-02", raw, zone)
		if e != nil || at.Year() < 1 || at.Format("2006-01-02") != raw {
			invalid()
			return
		}
		dates = append(dates, at)
	}
	if dates[0].After(dates[1]) || dates[1].Sub(dates[0]) > 366*24*time.Hour {
		invalid()
		return
	}
	export, e := csvRequested(r)
	if e != nil {
		invalid()
		return
	}
	columns := []reportColumn{{"user", "支付账号"}, {"order_count", "付款笔数"}, {"pay_money", "付款金额（元）"}, {"trade_no", "最新订单号"}, {"is_black", "是否拉黑"}}
	dimension, blackKind := "COALESCE(NULLIF(d.buyer,''),a.buyer_id,'')", "buyer"
	if method == "1" {
		dimension = "COALESCE(d.client_ip,'')"
		blackKind = "ip"
		columns[0].Label = "支付IP"
	}
	if method == "2" {
		if export {
			response(w, 422, -1, "该统计维度暂无可信手机号来源，无法导出", nil)
			return
		}
		columns[0].Label = "手机号"
		response(w, 200, 0, "", map[string]any{"columns": columns, "items": []any{}, "total": 0, "page": page, "limit": limit, "available": false, "unavailable_reason": "未记录可信支付手机号，暂不可按手机号统计", "timezone": "Asia/Shanghai"})
		return
	}
	select {
	case buyerStatisticsSlots <- struct{}{}:
		defer func() { <-buyerStatisticsSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "buyer statistics busy", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	// Every identifier/expression below is fixed. User input is exclusively bound.
	source := `WITH source AS (SELECT o.trade_no,o.amount,` + dimension + ` AS user_value FROM orders o LEFT JOIN order_reporting d ON d.order_id=o.id LEFT JOIN payment_authorizations a ON a.order_id=o.id WHERE o.status='paid' AND o.created_at >= $1 AND o.created_at < $2 AND ($3::text='' OR o.type=$3)), grouped AS (SELECT user_value,count(*) AS count_value,sum(amount::numeric) AS money_value,max(trade_no) AS trade_no FROM source WHERE user_value<>'' AND ($4::text='' OR user_value=$4) GROUP BY user_value) `
	query := source + `SELECT g.user_value AS "user",g.count_value::text AS order_count,(g.money_value/100)::numeric(38,2)::text AS pay_money,g.trade_no FROM grouped g`
	sorting := "g.count_value"
	if sort == "pay_money" {
		sorting = "g.money_value"
	}
	order := " ORDER BY " + sorting + " " + strings.ToUpper(direction) + ",g.user_value ASC"
	args := []any{dates[0], dates[1].AddDate(0, 0, 1), kind, user}
	if export {
		select {
		case csvExportSlots <- struct{}{}:
			defer func() { <-csvExportSlots }()
		default:
			w.Header().Set("Retry-After", "5")
			response(w, 429, -1, "CSV exports busy", nil)
			return
		}
	}
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		s.fail(w, e)
		return
	}
	defer tx.Rollback(ctx)
	// Read the same enabled rules and reuse payment admission's exact Go parser;
	// PostgreSQL's permissive date/IP parser must not weaken historical rules.
	ruleRows, e := tx.Query(ctx, `SELECT body FROM backoffice_records WHERE resource='blacklist' AND body->>'enabled'='true' AND body->>'kind'=$1`, blackKind)
	if e != nil {
		s.fail(w, e)
		return
	}
	rules := []map[string]any{}
	for ruleRows.Next() {
		var raw []byte
		var rule map[string]any
		if e = ruleRows.Scan(&raw); e != nil {
			ruleRows.Close()
			s.fail(w, e)
			return
		}
		if e = json.Unmarshal(raw, &rule); e != nil {
			ruleRows.Close()
			s.fail(w, e)
			return
		}
		rules = append(rules, rule)
	}
	e = ruleRows.Err()
	ruleRows.Close()
	if e != nil {
		s.fail(w, e)
		return
	}
	var total int64
	if e = tx.QueryRow(ctx, source+"SELECT count(*) FROM grouped", args...).Scan(&total); e != nil {
		s.fail(w, e)
		return
	}
	readQuery := query + order
	if export {
		readQuery += " LIMIT 50001"
	} else {
		readQuery += " LIMIT $5 OFFSET $6"
		args = append(args, limit, (page-1)*limit)
	}
	rows, e := tx.Query(ctx, readQuery, args...)
	if e != nil {
		s.fail(w, e)
		return
	}
	items := []map[string]any{}
	b := &csvBoundedBuffer{}
	writer := csv.NewWriter(b)
	count := 0
	now := time.Now()
	if export {
		_, _ = b.Write([]byte("\xef\xbb\xbf"))
		if e = writer.Write([]string{"user", "order_count", "pay_money", "trade_no", "is_black"}); e != nil {
			rows.Close()
			s.fail(w, e)
			return
		}
	}
	for rows.Next() {
		if e = ctx.Err(); e != nil {
			rows.Close()
			s.fail(w, e)
			return
		}
		var user, count, money, trade string
		var black bool
		if e = rows.Scan(&user, &count, &money, &trade); e != nil {
			rows.Close()
			s.fail(w, e)
			return
		}
		p := backoffice.PaymentContext{Buyer: user}
		if method == "1" {
			p = backoffice.PaymentContext{ClientIP: user}
		}
		for _, rule := range rules {
			if backoffice.ActiveBlacklistReason(rule, p, now) != "" {
				black = true
				break
			}
		}
		items = append(items, map[string]any{"user": user, "order_count": count, "pay_money": money, "trade_no": trade, "is_black": black})
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
	if export {
		if len(items) > csvExportMaxRows {
			response(w, 422, -1, errCSVExportTooLarge.Error(), nil)
			return
		}
		for _, item := range items {
			record := []string{}
			for _, key := range []string{"user", "order_count", "pay_money", "trade_no", "is_black"} {
				record = append(record, csvExportCell(item[key]))
			}
			if e = writer.Write(record); e != nil {
				break
			}
			count++
		}
		writer.Flush()
		if e == nil {
			e = writer.Error()
		}
		if e != nil {
			response(w, 422, -1, errCSVExportTooLarge.Error(), nil)
			return
		}
		s.finishCSVExport(ctx, w, session, "reports/buyers", b, count)
		return
	}
	response(w, 200, 0, "", map[string]any{"columns": columns, "items": items, "total": total, "page": page, "limit": limit, "available": true, "timezone": "Asia/Shanghai", "method": method})
}
