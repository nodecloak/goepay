package server

import (
	"goepay/internal/ledger"
	"net"
	"net/url"
)

// Field names select fixed SQL only. The returned value is always bound as $2;
// the authenticated owner predicate is built independently by the caller.
func consoleOrderSearch(q url.Values, kind, fallback string) (string, any, error) {
	if len(q["q"]) > 1 || len(q["search_field"]) > 1 || len(q.Get("q")) > 256 {
		return "", nil, errListFilter
	}
	raw, field := q.Get("q"), q.Get("search_field")
	if field == "" {
		return "($2::text='' OR strpos(lower(" + fallback + "),lower($2::text))>0)", raw, nil
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
	case "money", "realmoney":
		expression, mode = "o.amount", "money"
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
