package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
	"goepay/internal/riskpolicy"
	"time"
)

func coreRiskKeyIn(keys []string, key string) bool {
	for _, v := range keys {
		if v == key {
			return true
		}
	}
	return false
}

// Original buyer limits apply when an authenticated prepayment buyer is known.
// The accumulated confirmed gross amount is compared before the new payment;
// this deliberately does not turn the original threshold into an amount cap.
func (s *Store) checkCoreRiskBuyer(ctx context.Context, tx pgx.Tx, settings riskpolicy.Settings, key string, now time.Time) error {
	limit, amount := riskpolicy.Integer(settings, "pay_userlimit"), riskpolicy.Amount(settings, "pay_daymoney")
	if key == "" || limit == 0 && amount == 0 {
		return nil
	}
	rows, e := tx.Query(ctx, `SELECT order_id FROM core_risk_successes WHERE facts->'BuyerKeys'?$1 UNION SELECT order_id FROM core_risk_origins WHERE facts->>'BuyerKey'=$1 UNION SELECT resource_id FROM backoffice_audit WHERE resource IN('core-risk-success','core-risk-origins') AND operation IN('confirm','admit') AND(changes->>'buyer_key'=$1 OR changes->'buyer_keys'?$1)`, key)
	if e != nil {
		return e
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
		if len(ids) > 100000 {
			rows.Close()
			return ErrState
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	facts, e := s.coreRiskAdmissionFactsTx(ctx, tx, ids)
	if e != nil {
		return e
	}
	day := now.In(settlementScheduleZone).Format("2006-01-02")
	var count, total int64
	aliases := map[string]bool{key: true}
	for _, id := range ids {
		fact := facts[id]
		origin, success := fact.Origin, fact.Success
		if success == nil {
			if origin.BuyerKey != key {
				return ErrConflict
			}
			if fact.AttemptState == "processing" || fact.AttemptState == "pending" || fact.AttemptState == "unknown" {
				if amount > 0 {
					return ErrState
				}
				count++
			}
			continue
		}
		if !coreRiskKeyIn(success.BuyerKeys, key) {
			return ErrConflict
		}
		for _, known := range success.BuyerKeys {
			aliases[known] = true
		}
		if success.Day == day {
			count++
			if total > 9223372036854775807-success.Amount {
				return ErrState
			}
			total += success.Amount
		}
	}
	for alias := range aliases {
		legacyCount, legacyAmount, e := s.coreRiskLegacyDaily(ctx, tx, "", alias, now)
		if e != nil {
			return e
		}
		if total > 9223372036854775807-legacyAmount {
			return ErrState
		}
		count += legacyCount
		total += legacyAmount
	}
	if limit > 0 && count >= limit || amount > 0 && total >= amount {
		return ErrState
	}
	return nil
}
