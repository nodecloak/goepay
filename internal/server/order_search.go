package server

import (
	"goepay/internal/ledger"
	"net"
	"net/url"
	"strings"
	"unicode/utf8"
)

// consoleOrderDomainExpr is the 网站域名 of an order: the host of its notify_url,
// else its return_url (the same projection list.go selects as the domain
// column). Shared so the list column and the domain search match the same value.
const consoleOrderDomainExpr = "COALESCE(NULLIF(substring(o.notify_url from '^[a-zA-Z][a-zA-Z0-9+.-]*://([^/?#]+)'),''),NULLIF(substring(o.return_url from '^[a-zA-Z][a-zA-Z0-9+.-]*://([^/?#]+)'),''),'')"

// Field names select fixed SQL only. The returned value is always bound as $2;
// the authenticated owner predicate is built independently by the caller.
func consoleOrderSearch(q url.Values, kind, fallback string) (string, any, error) {
	if len(q["q"]) > 1 || len(q["search_field"]) > 1 || len(q.Get("q")) > 256 {
		return "", nil, errListFilter
	}
	raw, field := q.Get("q"), q.Get("search_field")
	if values, exists := q["trade_no"]; exists && (kind == "ledger" || kind == "refunds") {
		if len(values) != 1 || values[0] == "" || len(values[0]) > 64 || strings.ContainsAny(values[0], "\x00\r\n") || (raw != "" && raw != values[0]) || (field != "" && field != "trade_no") {
			return "", nil, errListFilter
		}
		raw, field = values[0], "trade_no"
	}
	if kind == "ledger" && field == "trade_no" {
		if raw == "" || len(raw) > 64 || strings.ContainsAny(raw, "\x00\r\n") {
			return "", nil, errListFilter
		}
		return consoleOrderLedgerReference, raw, nil
	}
	if kind == "refunds" && strings.ContainsAny(raw, "\x00\r\n") {
		return "", nil, errListFilter
	}
	if kind == "transfers" && (!utf8.ValidString(raw) || strings.ContainsAny(raw, "\x00\r\n")) {
		return "", nil, errListFilter
	}
	if field == "" {
		return "($2::text='' OR strpos(lower(" + fallback + "),lower($2::text))>0)", raw, nil
	}
	if kind == "refunds" {
		return consoleRefundFieldSearch(field, raw)
	}
	if kind == "transfers" {
		expression := ""
		switch field {
		case "biz_no":
			expression = "t.biz_no"
		case "out_biz_no":
			expression = "t.transfer_no"
		case "pay_order_no":
			expression = "COALESCE(p.provider_transfer_no,'')"
		case "money":
			expression = "t.amount"
		default:
			return "", nil, errListFilter
		}
		if raw == "" {
			return "$2::text=''", "", nil
		}
		if field == "money" {
			amount, e := ledger.Parse(raw)
			if e != nil {
				return "", nil, errListFilter
			}
			return expression + "=$2::bigint", amount, nil
		}
		return expression + "=$2::text", raw, nil
	}
	if kind != "orders" {
		return "", nil, errListFilter
	}
	expression, mode := "", "exact"
	switch field {
	case "trade_no":
		expression = "o.trade_no"
	case "out_trade_no":
		expression = "o.out_trade_no"
	case "api_trade_no":
		expression = "COALESCE(console_receipt.provider_trade_no,NULLIF(console_attempt.provider_trade_no,''),'')"
	case "name":
		expression, mode = "o.name", "substring"
	case "param":
		expression, mode = "o.param", "substring"
	case "buyer":
		expression = "COALESCE(NULLIF(console_report.buyer,''),console_authorization.buyer_id,'')"
	case "ip":
		expression, mode = "console_report.client_ip", "ip"
	case "domain":
		expression, mode = consoleOrderDomainExpr, "substring"
	case "money":
		expression, mode = "o.amount", "money"
	case "realmoney":
		expression, mode = "COALESCE((to_jsonb(o)->>'actual_charge_amount104')::bigint,o.amount)", "money"
	case "getmoney":
		expression, mode = "o.merchant_amount", "money"
	default:
		return "", nil, errListFilter
	}
	if raw == "" {
		return "$2::text=''", "", nil
	}
	switch mode {
	case "money":
		amount, e := ledger.Parse(raw)
		if e != nil {
			return "", nil, errListFilter
		}
		return expression + "=$2::bigint", amount, nil
	case "ip":
		ip := net.ParseIP(raw)
		if ip == nil {
			return "", nil, errListFilter
		}
		raw = ip.String()
	case "substring":
		return "strpos(lower(" + expression + "),lower($2::text))>0", raw, nil
	}
	return expression + "=$2::text", raw, nil
}
