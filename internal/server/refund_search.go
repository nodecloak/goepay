package server

import "strconv"

// The public refund_no is the decimal platform ID. refunds.refund_no stores
// out_refund_no; RF{id} is an upstream namespace and is not a display alias.
func consoleRefundFieldSearch(field, raw string) (string, any, error) {
	expression := ""
	switch field {
	case "refund_no":
		if raw == "" {
			return "$2::text=''", "", nil
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
			return "", nil, errListFilter
		}
		return "f.id=$2::bigint", id, nil
	case "out_refund_no":
		expression = "f.refund_no"
	case "trade_no":
		expression = "o.trade_no"
	default:
		return "", nil, errListFilter
	}
	if raw == "" {
		return "$2::text=''", "", nil
	}
	return expression + "=$2::text", raw, nil
}
