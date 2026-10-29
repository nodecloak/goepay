package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goepay/internal/identity"
)

// consoleOrderSummaryFrom mirrors the orders list FROM so the overview counts
// exactly the rows the current filter would list. It reuses the same reporting
// joins; it confers no authority over order state.
const consoleOrderSummaryFrom = "orders o" + consoleOrderReportingJoins + consoleOrderControlJoin
const consoleOrderSummaryMatching = "concat_ws(' ',o.trade_no,o.out_trade_no,o.name)"

var orderSummarySlots = make(chan struct{}, 4)

// orderSummary answers GET /console/v1/<role>/orders/summary with Pro's
// 订单统计概况 over the current order filter: total/paid/unpaid/refunded counts,
// the success rate, and order / paid / merchant-share money (yuan strings). It
// is read-only and bounded (slot-limited, 10s, no pagination).
func (s *Server) orderSummary(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 3 || parts[1] != "orders" || parts[2] != "summary" {
		return false
	}
	if (role != "admin" && role != "merchant") || session.User.Role != role || session.User.ID <= 0 {
		response(w, 403, -1, "authenticated order scope required", nil)
		return true
	}
	if len(parts) != 3 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if r.Method != "GET" {
		response(w, 405, -1, "GET required", nil)
		return true
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		response(w, 400, -1, "invalid order summary query", nil)
		return true
	}
	allowed := map[string]bool{"q": true, "search_field": true, "merchant_id": true, "type": true, "status": true, "channel_id": true, "subchannel_id": true, "from": true, "to": true, "buyer": true, "client_ip": true, "notification_state": true, "income_status": true, "include_archived": true}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 || strings.ContainsAny(values[0], "\x00\r\n") {
			response(w, 400, -1, "invalid order summary filter", nil)
			return true
		}
		if key == "merchant_id" || key == "channel_id" || key == "subchannel_id" {
			id, e := strconv.ParseInt(values[0], 10, 64)
			if e != nil || id <= 0 || strconv.FormatInt(id, 10) != values[0] {
				response(w, 400, -1, "invalid order summary id", nil)
				return true
			}
		}
		if key == "from" || key == "to" {
			at, e := time.Parse("2006-01-02", values[0])
			if e != nil || at.Year() < 1 || at.Format("2006-01-02") != values[0] {
				response(w, 400, -1, "invalid order summary date", nil)
				return true
			}
		}
	}
	searchClause, search, err := consoleOrderSearch(q, "orders", consoleOrderSummaryMatching)
	if err != nil {
		response(w, 400, -1, "invalid order summary search", nil)
		return true
	}
	mid := session.User.ID
	if role == "admin" {
		mid = 0
	}
	filters, args, err := consoleListFilters(q, role, "orders", "o.merchant_id", mid, []any{mid, search})
	if err != nil {
		status := 400
		if err == errListFilterForbidden {
			status = 403
		}
		response(w, status, -1, "invalid order summary scope", nil)
		return true
	}
	select {
	case orderSummarySlots <- struct{}{}:
		defer func() { <-orderSummarySlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "order statistics busy", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	where := " WHERE ($1::bigint=0 OR o.merchant_id=$1) AND (" + searchClause + ")" + filters
	if q.Get("include_archived") != "true" {
		where += " AND COALESCE((console_control.changes->>'archived')='true',false)=false"
	}
	// Order amount is the gross order amount; paid/refunded money use the
	// actual charged amount and the succeeded refund sum, matching the list
	// cells. Money is yuan with two decimals (minor units / 100).
	charged := "COALESCE((to_jsonb(o)->>'actual_charge_amount104')::bigint,o.amount)::numeric"
	refundedSum := "COALESCE((SELECT sum(r.amount) FROM refunds r WHERE r.order_id=o.id AND r.status='succeeded'),0)::numeric"
	money := func(expr, condition string) string {
		suffix := ""
		if condition != "" {
			suffix = " FILTER(WHERE " + condition + ")"
		}
		return "(COALESCE(sum(" + expr + ")" + suffix + ",0)/100)::numeric(38,2)::text"
	}
	query := "SELECT count(*)::text," +
		"count(*) FILTER(WHERE o.status='paid')::text," +
		"count(*) FILTER(WHERE o.status<>'paid')::text," +
		"count(*) FILTER(WHERE EXISTS(SELECT 1 FROM refunds r WHERE r.order_id=o.id AND r.status='succeeded'))::text," +
		money("o.amount::numeric", "") + "," +
		money(charged, "o.status='paid'") + "," +
		money("o.merchant_amount::numeric", "o.status='paid'") + "," +
		money(refundedSum, "") +
		" FROM " + consoleOrderSummaryFrom + where
	var total, paid, unpaid, refundedCount, orderMoney, paidMoney, merchantMoney, refundedMoney string
	if err = s.Pool.QueryRow(ctx, query, args...).Scan(&total, &paid, &unpaid, &refundedCount, &orderMoney, &paidMoney, &merchantMoney, &refundedMoney); err != nil {
		s.fail(w, err)
		return true
	}
	rate := "—"
	if t, e1 := strconv.ParseInt(total, 10, 64); e1 == nil && t > 0 {
		if p, e2 := strconv.ParseInt(paid, 10, 64); e2 == nil && p >= 0 && p <= t {
			scaled := (p*10000 + t/2) / t
			rate = strconv.FormatInt(scaled/100, 10) + "." + leftPad2(scaled%100) + "%"
		}
	}
	response(w, 200, 0, "", map[string]string{
		"totalCount":    total,
		"paidCount":     paid,
		"unpaidCount":   unpaid,
		"refundedCount": refundedCount,
		"successRate":   rate,
		"orderMoney":    orderMoney,
		"paidMoney":     paidMoney,
		"merchantMoney": merchantMoney,
		"refundedMoney": refundedMoney,
	})
	return true
}

func leftPad2(v int64) string {
	s := strconv.FormatInt(v, 10)
	if len(s) < 2 {
		return "0" + s
	}
	return s
}
