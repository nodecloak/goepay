package server

import (
	"context"
	"goepay/internal/payment"
	"strconv"
)

func buyerText104(value any) string { out, _ := value.(string); return out }

func (s *Server) consoleOrderCharges104(ctx context.Context, mid int64, items []map[string]any) error {
	ids := make([]int64, 0, len(items))
	for _, row := range items {
		id, e := strconv.ParseInt(buyerText104(row["id"]), 10, 64)
		if e != nil || id <= 0 {
			return payment.ErrInvalid
		}
		ids = append(ids, id)
		row["charge_verified104"] = false
		row["realmoney"] = ""
		row["remaining_refund_amount"] = ""
	}
	if len(ids) == 0 {
		return nil
	}
	values, e := s.Store.OriginalOrderCharges104(ctx, mid, ids)
	if e != nil {
		return e
	}
	for i, id := range ids {
		charge, ok := values[id]
		if !ok {
			continue
		}
		row := items[i]
		row["charge_verified104"] = true
		row["realmoney"] = strconv.FormatInt(charge, 10)
		used, e := strconv.ParseInt(buyerText104(row["refunded_amount"]), 10, 64)
		if e == nil && used >= 0 && used <= charge {
			row["remaining_refund_amount"] = strconv.FormatInt(charge-used, 10)
		}
	}
	return nil
}
