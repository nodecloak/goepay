package server

import (
	"context"
	"errors"
	"goepay/internal/identity"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

var groupStatisticsSlots = make(chan struct{}, 4)

// The original group dialog reports payment methods, not provider channel IDs.
// Keep this aggregate independent of provider attempts, costs and refunds: each
// order contributes once, including when it has several associated operations.
func (s *Server) groupStatistics(w http.ResponseWriter, r *http.Request, role string, session identity.Session) {
	if role != "admin" || session.User.Role != "admin" || session.User.ID <= 0 {
		response(w, 403, -1, "administrator group statistics required", nil)
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || len(q) != 1 || len(q["gid"]) != 1 {
		response(w, 400, -1, "invalid group statistics filter", nil)
		return
	}
	raw := q.Get("gid")
	gid, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || gid < 0 || strconv.FormatInt(gid, 10) != raw {
		response(w, 400, -1, "invalid group statistics filter", nil)
		return
	}
	select {
	case groupStatisticsSlots <- struct{}{}:
		defer func() { <-groupStatisticsSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "group statistics busy", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		s.fail(w, e)
		return
	}
	defer tx.Rollback(ctx)
	name := "默认用户组"
	if gid >= 0 {
		e = tx.QueryRow(ctx, `SELECT COALESCE(body->>'name','') FROM backoffice_records WHERE resource='groups' AND id=$1`, gid).Scan(&name)
		if errors.Is(e, pgx.ErrNoRows) {
			response(w, 404, -1, "group not found", nil)
			return
		}
		if e != nil {
			s.fail(w, e)
			return
		}
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	yesterday := today.AddDate(0, 0, -1)
	rows, e := tx.Query(ctx, `SELECT CASE WHEN o.created_at >= $2 THEN 'today' ELSE 'yesterday' END,o.type,
 COALESCE(sum(o.amount::numeric) FILTER(WHERE o.status='paid'),0)::text,count(*)::text,count(*) FILTER(WHERE o.status='paid')::text
 FROM orders o JOIN merchants m ON m.id=o.merchant_id LEFT JOIN memberships v ON v.merchant_id=m.id
 WHERE o.created_at >= $1 AND o.created_at < $3
 AND CASE WHEN v.expires_at > CURRENT_TIMESTAMP THEN v.group_id WHEN m.group_baseline_unresolved THEN NULL::bigint ELSE m.base_group_id END=$4
 GROUP BY 1,2 ORDER BY 1,2`, yesterday, today, today.AddDate(0, 0, 1), gid)
	if e != nil {
		s.fail(w, e)
		return
	}
	methods := paymentStatisticsMethods
	groups := map[string]map[string]*originalStatisticsGroup{}
	totals := map[string]*originalStatisticsGroup{}
	for _, day := range []string{"today", "yesterday"} {
		groups[day] = map[string]*originalStatisticsGroup{}
		totals[day] = &originalStatisticsGroup{}
		for _, m := range methods {
			groups[day][m.kind] = &originalStatisticsGroup{}
		}
	}
	for rows.Next() {
		var day, kind, money, count, success string
		if e = rows.Scan(&day, &kind, &money, &count, &success); e != nil {
			break
		}
		g := groups[day][kind]
		if g == nil {
			rows.Close()
			response(w, 422, -1, "group statistics contains an unsupported payment method", nil)
			return
		}
		if e = g.add(money, "0", "0", count, success, "0", "0"); e != nil {
			break
		}
		if e = totals[day].add(money, "0", "0", count, success, "0", "0"); e != nil {
			break
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		s.fail(w, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		s.fail(w, e)
		return
	}
	days := map[string]any{}
	for _, day := range []string{"today", "yesterday"} {
		at := today
		if day == "yesterday" {
			at = yesterday
		}
		days[day] = paymentStatisticsDay(at, groups[day], totals[day])
	}
	response(w, 200, 0, "", map[string]any{"group_id": raw, "group_name": name, "timezone": "Asia/Shanghai", "cohort": "current_effective_group", "columns": []reportColumn{{"showname", "支付方式"}, {"money", "订单金额"}, {"count", "订单数量"}, {"rate", "成功率"}}, "days": days})
}
