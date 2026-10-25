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

const consoleRefundFrom = "refunds f JOIN orders o ON o.id=f.order_id"
const consoleRefundMatching = "concat_ws(' ',f.id,f.refund_no,f.order_id,o.trade_no,o.out_trade_no)"

var refundSummarySlots = make(chan struct{}, 4)

func (s *Server) refundSummary(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 3 || parts[1] != "refunds" || parts[2] != "summary" {
		return false
	}
	if (role != "admin" && role != "merchant") || session.User.Role != role || session.User.ID <= 0 {
		response(w, 403, -1, "authenticated refund scope required", nil)
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
	allowed := map[string]bool{"q": true, "search_field": true, "merchant_id": true, "type": true, "status": true, "channel_id": true, "from": true, "to": true}
	if err != nil {
		response(w, 400, -1, "invalid refund summary query", nil)
		return true
	}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 || strings.ContainsAny(values[0], "\x00\r\n") {
			response(w, 400, -1, "invalid refund summary filter", nil)
			return true
		}
		if key == "merchant_id" || key == "channel_id" {
			id, e := strconv.ParseInt(values[0], 10, 64)
			if e != nil || id <= 0 || strconv.FormatInt(id, 10) != values[0] {
				response(w, 400, -1, "invalid refund summary id", nil)
				return true
			}
		}
		if key == "from" || key == "to" {
			at, e := time.Parse("2006-01-02", values[0])
			if e != nil || at.Year() < 1 || at.Format("2006-01-02") != values[0] {
				response(w, 400, -1, "invalid refund summary date", nil)
				return true
			}
		}
	}
	searchClause, search, err := consoleOrderSearch(q, "refunds", consoleRefundMatching)
	if err != nil {
		response(w, 400, -1, "invalid refund summary search", nil)
		return true
	}
	mid := session.User.ID
	if role == "admin" {
		mid = 0
	}
	filters, args, err := consoleListFilters(q, role, "refunds", "f.merchant_id", mid, []any{mid, search})
	if err != nil {
		status := 400
		if err == errListFilterForbidden {
			status = 403
		}
		response(w, status, -1, "invalid refund summary scope", nil)
		return true
	}
	select {
	case refundSummarySlots <- struct{}{}:
		defer func() { <-refundSummarySlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "refund statistics busy", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	where := " WHERE ($1::bigint=0 OR f.merchant_id=$1) AND (" + searchClause + ")" + filters
	money := func(condition string) string {
		suffix := ""
		if condition != "" {
			suffix = " FILTER(WHERE " + condition + ")"
		}
		return "(COALESCE(sum(f.amount::numeric)" + suffix + ",0)/100)::numeric(38,2)::text"
	}
	query := "SELECT count(*)::text," + money("") + ",count(*) FILTER(WHERE f.status='succeeded')::text," + money("f.status='succeeded'") + ",count(*) FILTER(WHERE f.status IN ('pending','unknown'))::text," + money("f.status IN ('pending','unknown')") + " FROM " + consoleRefundFrom + where
	var total, totalMoney, success, successMoney, pending, pendingMoney string
	if err = s.Pool.QueryRow(ctx, query, args...).Scan(&total, &totalMoney, &success, &successMoney, &pending, &pendingMoney); err != nil {
		s.fail(w, err)
		return true
	}
	response(w, 200, 0, "", map[string]string{"totalCount": total, "totalMoney": totalMoney, "successCount": success, "successMoney": successMoney, "pendingCount": pending, "pendingMoney": pendingMoney})
	return true
}
