package server

import (
	"errors"
	"fmt"
	provider "goepay/internal/adapters/payment"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var errListFilter = errors.New("invalid list filter")
var errListFilterForbidden = errors.New("list filter owner forbidden")

// Fixed SQL expressions are selected by endpoint; every filter value is bound.
// This augments, and can never replace, the authenticated tenant predicate.
func consoleListFilters(q url.Values, role, kind, tenant string, mid int64, args []any) (string, []any, error) {
	if kind == "transfers" {
		if e := validateTransferListFilters(q); e != nil {
			return "", nil, e
		}
	}
	var clauses []string
	if len(q["include_archived"]) > 1 {
		return "", nil, errListFilter
	}
	if raw := q.Get("include_archived"); raw != "" {
		if kind != "orders" {
			return "", nil, errListFilter
		}
		if role != "admin" {
			return "", nil, errListFilterForbidden
		}
		if raw != "true" && raw != "false" {
			return "", nil, errListFilter
		}
	}
	bind := func(expression string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(expression, len(args)))
	}
	for _, key := range []string{"merchant_id", "type", "status", "income_status", "notification_state", "channel_id", "from", "to", "buyer", "client_ip", "enabled"} {
		if len(q[key]) > 1 {
			return "", nil, errListFilter
		}
	}
	if raw := q.Get("notification_state"); raw != "" {
		if kind != "orders" {
			return "", nil, errListFilter
		}
		switch raw {
		case "none", "pending", "running", "succeeded", "dead":
			bind("COALESCE(console_notification.state,'none')=$%d", raw)
		default:
			return "", nil, errListFilter
		}
	}
	if raw := q.Get("income_status"); raw != "" {
		if kind != "orders" {
			return "", nil, errListFilter
		}
		switch raw {
		case "held":
			clauses = append(clauses, "EXISTS(SELECT 1 FROM order_income_holds income WHERE income.order_id=o.id AND income.state='held')")
		case "released":
			clauses = append(clauses, "EXISTS(SELECT 1 FROM order_income_holds income WHERE income.order_id=o.id) AND NOT EXISTS(SELECT 1 FROM order_income_holds income WHERE income.order_id=o.id AND income.state='held')")
		case "none":
			clauses = append(clauses, "NOT EXISTS(SELECT 1 FROM order_income_holds income WHERE income.order_id=o.id)")
		default:
			return "", nil, errListFilter
		}
	}
	if raw := q.Get("merchant_id"); raw != "" {
		owner, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || owner <= 0 {
			return "", nil, errListFilter
		}
		if role != "admin" && owner != mid {
			return "", nil, errListFilterForbidden
		}
		if tenant == "0::bigint" {
			bind("m.id=$%d", owner)
		} else {
			bind(tenant+"=$%d", owner)
		}
	}
	if raw := q.Get("status"); raw != "" {
		expression, allowed := "", ""
		switch kind {
		case "orders":
			expression, allowed = "o.status", "created,submitting,pending,paid,closed,failed,unknown"
			if role == "admin" {
				expression = consoleAdminStatusExpression
			}
		case "refunds":
			expression, allowed = "f.status", "pending,unknown,succeeded,failed"
		case "transfers":
			expression, allowed = transferEffectiveStatus, "pending,unknown,succeeded,failed,reversed,reversal_pending,unsent,cancelled"
		}
		validStatus := false
		for _, candidate := range strings.Split(allowed, ",") {
			if candidate == raw {
				validStatus = true
			}
		}
		if expression == "" || !validStatus {
			return "", nil, errListFilter
		}
		bind(expression+"=$%d", raw)
	}
	if raw := q.Get("type"); raw != "" {
		if (kind != "orders" && kind != "refunds") || (!provider.CatalogPayType(raw) && raw != "sandbox") {
			return "", nil, errListFilter
		}
		bind("o.type=$%d", raw)
	}
	for _, key := range []string{"subchannel", "subchannel_id"} {
		if len(q[key]) > 1 || len(q[key]) == 1 && q.Get(key) == "" {
			return "", nil, errListFilter
		}
	}
	childID := q.Get("subchannel_id")
	if legacy := q.Get("subchannel"); legacy != "" {
		if childID != "" && childID != legacy {
			return "", nil, errListFilter
		}
		childID = legacy
	}
	if childID != "" {
		if kind != "orders" {
			return "", nil, errListFilter
		}
		child, e := strconv.ParseInt(childID, 10, 64)
		if e != nil || child <= 0 || strconv.FormatInt(child, 10) != childID {
			return "", nil, errListFilter
		}
		bind("EXISTS(SELECT 1 FROM payment_subchannel_routes child_route WHERE child_route.merchant_id=o.merchant_id AND child_route.out_trade_no=o.out_trade_no AND child_route.subchannel_id=$%d)", child)
	}
	if raw := q.Get("channel_id"); raw != "" {
		channel, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || channel <= 0 {
			return "", nil, errListFilter
		}
		switch kind {
		case "orders", "refunds":
			bind("EXISTS(SELECT 1 FROM provider_attempts filter_attempt WHERE filter_attempt.order_id=o.id AND filter_attempt.channel_id=$%d)", channel)
		case "transfers":
			bind("p.channel_id=$%d", channel)
		default:
			return "", nil, errListFilter
		}
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	dateColumn := map[string]string{"orders": "o.created_at", "refunds": "f.created_at", "transfers": "t.created_at", "ledger": "j.created_at", "merchants": "m.created_at"}[kind]
	var from, to time.Time
	for _, key := range []string{"from", "to"} {
		if raw := q.Get(key); raw != "" {
			d, e := time.ParseInLocation("2006-01-02", raw, zone)
			if e != nil || dateColumn == "" {
				return "", nil, errListFilter
			}
			if key == "from" {
				from = d
				bind(dateColumn+">=$%d", d)
			} else {
				to = d
				bind(dateColumn+"<$%d", d.AddDate(0, 0, 1))
			}
		}
	}
	if !from.IsZero() && !to.IsZero() && (from.After(to) || to.Sub(from) > 366*24*time.Hour) {
		return "", nil, errListFilter
	}
	for _, key := range []string{"buyer", "client_ip"} {
		if raw := q.Get(key); raw != "" {
			if kind != "orders" || len(raw) > 256 {
				return "", nil, errListFilter
			}
			if key == "client_ip" {
				ip := net.ParseIP(raw)
				if ip == nil {
					return "", nil, errListFilter
				}
				raw = ip.String()
			}
			if key == "buyer" {
				// Match the displayed buyer, including the original preauthorization
				// when no ordinary payment reporting account has been stored.
				bind("COALESCE((SELECT NULLIF(filter_report.buyer,'') FROM order_reporting filter_report WHERE filter_report.order_id=o.id),(SELECT filter_auth.buyer_id FROM payment_authorizations filter_auth WHERE filter_auth.order_id=o.id),'')=$%d", raw)
				continue
			}
			bind("EXISTS(SELECT 1 FROM order_reporting filter_report WHERE filter_report.order_id=o.id AND filter_report."+key+"=$%d)", raw)
		}
	}
	if raw := q.Get("enabled"); raw != "" {
		if kind != "merchants" || (raw != "true" && raw != "false") {
			return "", nil, errListFilter
		}
		bind("m.enabled=$%d", raw == "true")
	}
	if len(clauses) == 0 {
		return "", args, nil
	}
	return " AND " + strings.Join(clauses, " AND "), args, nil
}
