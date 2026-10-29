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

var ledgerSummarySlots = make(chan struct{}, 4)

// ledgerSummary serves GET /ledger/summary: the 资金明细 统计 totals the original
// console showed in its 统计 dialog — 增加金额 (sum of credit entries), 减少金额
// (absolute sum of debit entries) and 总计金额 (net change) over the whole current
// filter, not just the page. It reuses the list's FROM / search / filter builders
// so the aggregate covers exactly the rows the same filters would list, under the
// identical authenticated owner predicate (admin: all; merchant: own account only).
func (s *Server) ledgerSummary(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 3 || parts[1] != "ledger" || parts[2] != "summary" {
		return false
	}
	if (role != "admin" && role != "merchant") || session.User.Role != role || session.User.ID <= 0 {
		response(w, 403, -1, "authenticated ledger scope required", nil)
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
		response(w, 400, -1, "invalid ledger summary query", nil)
		return true
	}
	allowed := map[string]bool{"q": true, "search_field": true, "trade_no": true, "merchant_id": true, "type": true, "from": true, "to": true}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 || strings.ContainsAny(values[0], "\x00\r\n") {
			response(w, 400, -1, "invalid ledger summary filter", nil)
			return true
		}
		if key == "merchant_id" {
			id, e := strconv.ParseInt(values[0], 10, 64)
			if e != nil || id <= 0 || strconv.FormatInt(id, 10) != values[0] {
				response(w, 400, -1, "invalid ledger summary id", nil)
				return true
			}
		}
		if key == "from" || key == "to" {
			at, e := time.Parse("2006-01-02", values[0])
			if e != nil || at.Year() < 1 || at.Format("2006-01-02") != values[0] {
				response(w, 400, -1, "invalid ledger summary date", nil)
				return true
			}
		}
	}
	searchClause, search, err := consoleOrderSearch(q, "ledger", "concat_ws(' ',j.event_key,j.description)")
	if err != nil {
		response(w, 400, -1, "invalid ledger summary search", nil)
		return true
	}
	mid := session.User.ID
	if role == "admin" {
		mid = 0
	}
	filters, args, err := consoleListFilters(q, role, "ledger", "a.owner_id", mid, []any{mid, search})
	if err != nil {
		status := 400
		if err == errListFilterForbidden {
			status = 403
		}
		response(w, status, -1, "invalid ledger summary scope", nil)
		return true
	}
	select {
	case ledgerSummarySlots <- struct{}{}:
		defer func() { <-ledgerSummarySlots }()
	default:
		w.Header().Set("Retry-After", "5")
		response(w, 429, -1, "ledger statistics busy", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	where := " WHERE ($1::bigint=0 OR a.owner_id=$1) AND (" + searchClause + ")" + filters
	// 增加 sums positive entries, 减少 the absolute value of negative entries, and
	// 总计 the net change. Formatting matches the list's integer-minor-unit → yuan
	// projection exactly (no floats).
	money := func(expression string) string {
		return "(COALESCE(sum(" + expression + "),0)::numeric/100)::numeric(38,2)::text"
	}
	query := "SELECT count(*)::text," +
		money("e.amount FILTER(WHERE e.amount>0)") + "," +
		money("-e.amount FILTER(WHERE e.amount<0)") + "," +
		money("e.amount") +
		" FROM " + consoleLedgerFrom + where
	var count, increase, decrease, net string
	if err = s.Pool.QueryRow(ctx, query, args...).Scan(&count, &increase, &decrease, &net); err != nil {
		s.fail(w, err)
		return true
	}
	response(w, 200, 0, "", map[string]string{"totalCount": count, "increaseMoney": increase, "decreaseMoney": decrease, "netMoney": net})
	return true
}
