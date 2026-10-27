package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

// Historical confirmed payments retain their original receipt/reporting facts.
// Reading them is not a backfill of a new admission or a fabricated HMAC.
func (s *Store) coreRiskLegacyDaily(ctx context.Context, tx pgx.Tx, ip, buyerKey string, now time.Time) (int64, int64, error) {
	day := now.In(settlementScheduleZone)
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, settlementScheduleZone)
	end := start.AddDate(0, 0, 1)
	rows, e := tx.Query(ctx, `SELECT o.id,o.type,o.amount,COALESCE(r.client_ip,''),COALESCE(r.buyer,'') FROM orders o JOIN verified_payment_receipts v ON v.order_id=o.id JOIN provider_attempts a ON a.order_id=o.id AND a.channel_id=v.channel_id LEFT JOIN order_reporting r ON r.order_id=o.id WHERE o.paid_at >= $1 AND o.paid_at < $2 AND ($3='' OR COALESCE(r.client_ip,'')=$3) AND NOT a.core_risk_required AND NOT EXISTS(SELECT 1 FROM core_risk_origins k WHERE k.order_id=o.id) AND NOT EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='core-risk-origins' AND b.operation='admit' AND b.resource_id=o.id) ORDER BY o.id LIMIT 100001`, start, end, ip)
	if e != nil {
		return 0, 0, e
	}
	defer rows.Close()
	type candidate struct {
		id              int64
		kind, ip, buyer string
		amount          int64
	}
	candidates := []candidate{}
	ids := []int64{}
	var count, total, n int64
	for rows.Next() {
		n++
		if n > 100000 {
			return 0, 0, ErrState
		}
		var kind, oldIP, buyer string
		var id, amount int64
		if e = rows.Scan(&id, &kind, &amount, &oldIP, &buyer); e != nil {
			return 0, 0, e
		}
		candidates = append(candidates, candidate{id, kind, oldIP, buyer, amount})
		ids = append(ids, id)
	}
	if e = rows.Err(); e != nil {
		return 0, 0, e
	}
	rows.Close()
	charges, e := s.orderChargesTx104(ctx, tx, 0, ids)
	if e != nil {
		return 0, 0, e
	}
	if len(charges) != len(ids) {
		return 0, 0, ErrConflict
	}
	for _, c := range candidates {
		kind, oldIP, buyer, amount := c.kind, c.ip, c.buyer, charges[c.id].Charge
		match := ip != "" && oldIP == ip
		if buyerKey != "" {
			key, e := s.coreRiskBuyerKey(kind, buyer)
			if e != nil {
				return 0, 0, e
			}
			match = key == buyerKey
		}
		if match {
			if amount <= 0 || total > 9223372036854775807-amount {
				return 0, 0, ErrConflict
			}
			count++
			total += amount
		}
	}
	return count, total, nil
}
