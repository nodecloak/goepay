package payment

import (
	"context"
	"crypto/hmac"
	"github.com/jackc/pgx/v5"
	"time"
)

type coreRiskAdmissionFact struct {
	Origin                    coreRiskOrigin
	OriginHash                []byte
	Success                   *coreRiskSuccess
	AttemptState, OrderStatus string
	Receipt                   bool
}

// Candidate rows are selected through both original private sources and
// independent audit witnesses. This single read then authenticates every
// selected origin/success and its original attempt/order/receipt binding.
func (s *Store) coreRiskAdmissionFactsTx(ctx context.Context, tx pgx.Tx, ids []int64) (map[int64]coreRiskAdmissionFact, error) {
	out := map[int64]coreRiskAdmissionFact{}
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > 100000 {
		return nil, ErrState
	}
	charges, e := s.orderChargesTx104(ctx, tx, 0, ids)
	if e != nil {
		return nil, e
	}
	if len(charges) != len(ids) {
		return nil, ErrConflict
	}
	rows, e := tx.Query(ctx, `SELECT i.id,COALESCE(k.attempt_id,0),COALESCE(k.ips,'{}'::text[]),COALESCE(k.admitted_at,'epoch'::timestamptz),k.facts,k.proof_hash,
 COALESCE(a.id,0),COALESCE(a.order_id,0),COALESCE(a.channel_id,0),COALESCE(a.method,''),COALESCE(a.core_risk_required,false),COALESCE(a.state,''),COALESCE(o.merchant_id,0),COALESCE(o.type,''),COALESCE(o.amount,0),o.paid_at,COALESCE(o.status,''),v.order_id IS NOT NULL,COALESCE(v.channel_id,0),COALESCE(v.provider_trade_no,''),COALESCE(c.day::text,''),c.facts,c.proof_hash,
 EXISTS(SELECT 1 FROM backoffice_audit u WHERE u.resource='core-risk-origins' AND u.resource_id=i.id AND u.operation='admit' AND (u.actor_role,u.actor_id,u.actor_name)=('system',0,'core-risk') AND u.changes->>'attempt_id'=k.attempt_id::text AND u.changes->>'source_hash'=encode(k.proof_hash,'hex') AND u.changes->>'admitted_at'=to_char(k.admitted_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US')||'000Z'),
 EXISTS(SELECT 1 FROM backoffice_audit u WHERE u.resource='core-risk-success' AND u.resource_id=i.id AND u.operation='confirm' AND (u.actor_role,u.actor_id,u.actor_name)=('system',0,'core-risk') AND u.changes->>'source_hash'=encode(c.proof_hash,'hex') AND u.changes->>'day'=c.day::text)
 FROM unnest($1::bigint[])i(id) LEFT JOIN core_risk_origins k ON k.order_id=i.id LEFT JOIN provider_attempts a ON a.id=k.attempt_id LEFT JOIN orders o ON o.id=i.id LEFT JOIN verified_payment_receipts v ON v.order_id=i.id LEFT JOIN core_risk_successes c ON c.order_id=i.id`, ids)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var id, attempt, actualAttempt, attemptOrder, channel, merchant, amount, receiptChannel int64
		var ips []string
		var at time.Time
		var paidAt *time.Time
		var raw, proof, successRaw, successProof []byte
		var method, state, kind, status, receiptTrade, day string
		var required, receipt, originAudit, successAudit bool
		if e = rows.Scan(&id, &attempt, &ips, &at, &raw, &proof, &actualAttempt, &attemptOrder, &channel, &method, &required, &state, &merchant, &kind, &amount, &paidAt, &status, &receipt, &receiptChannel, &receiptTrade, &day, &successRaw, &successProof, &originAudit, &successAudit); e != nil {
			return nil, e
		}
		if _, duplicate := out[id]; duplicate {
			return nil, ErrConflict
		}
		origin, e := s.decodeCoreRiskOriginFact(raw, proof, id, attempt, ips, at)
		if e != nil {
			return nil, e
		}
		if !originAudit || !required || origin.AttemptID != actualAttempt || origin.OrderID != attemptOrder || origin.MerchantID != merchant || origin.Type != kind || origin.Amount != amount || origin.ChannelID != channel || origin.Method != method {
			return nil, ErrConflict
		}
		charge, ok := charges[id]
		if !ok {
			return nil, ErrConflict
		}
		if origin.CustomerCharge104 == nil {
			if len(charge.SourceProof) != 0 {
				return nil, ErrConflict
			}
		} else if origin.CustomerCharge104.Charge != charge.Charge || !hmac.Equal(origin.CustomerCharge104.SourceProof, charge.SourceProof) || len(charge.SourceProof) != 32 {
			return nil, ErrConflict
		}
		fact := coreRiskAdmissionFact{Origin: origin, OriginHash: proof, AttemptState: state, OrderStatus: status, Receipt: receipt}
		if successRaw != nil {
			fact.Success, e = s.decodeCoreRiskSuccessFact(successRaw, successProof, proof, id, day)
			if e != nil {
				return nil, e
			}
			success := fact.Success
			if !successAudit || !receipt || paidAt == nil || status != "paid" || success.Amount != charge.Charge || !success.PaidAt.Equal(*paidAt) || success.ChannelID != receiptChannel || success.ProviderTradeNo != receiptTrade {
				return nil, ErrConflict
			}
		} else if receipt && status == "paid" {
			return nil, ErrConflict
		}
		out[id] = fact
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if len(out) != len(ids) {
		return nil, ErrConflict
	}
	return out, nil
}

func coreRiskBusinessDayBounds(now time.Time) (time.Time, time.Time, string) {
	day := now.In(settlementScheduleZone)
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, settlementScheduleZone)
	return start, start.AddDate(0, 0, 1), day.Format("2006-01-02")
}

func (s *Store) coreRiskCurrentIPCandidatesTx(ctx context.Context, tx pgx.Tx, ip string, now time.Time, limit, interval int64) ([]int64, error) {
	start, end, day := coreRiskBusinessDayBounds(now)
	cutoff := now.Add(-time.Duration(interval) * time.Second)
	key, e := s.coreRiskIPKey(ip)
	if e != nil {
		return nil, e
	}
	// A paid terminal source outside this day/interval cannot occupy a current
	// slot. Any still-unresolved source remains selected regardless of age.
	var lost bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE a.core_risk_required AND NOT EXISTS(SELECT 1 FROM core_risk_origins k WHERE k.attempt_id=a.id) AND ((a.state IN('processing','pending','unknown') AND NOT(o.status='paid' AND EXISTS(SELECT 1 FROM verified_payment_receipts paid_v WHERE paid_v.order_id=o.id))) OR o.paid_at >=$1 AND o.paid_at<$2 OR a.created_at >=$3))`, start, end, cutoff).Scan(&lost); e != nil {
		return nil, e
	}
	if lost {
		return nil, ErrConflict
	}
	rows, e := tx.Query(ctx, `SELECT k.order_id FROM core_risk_origins k JOIN provider_attempts a ON a.id=k.attempt_id JOIN orders o ON o.id=k.order_id LEFT JOIN core_risk_successes c ON c.order_id=k.order_id
 WHERE k.ips @> ARRAY[$1]::text[] AND (($2::bigint>0 AND k.admitted_at >=$3) OR ($4::bigint>0 AND ((o.paid_at >=$5 AND o.paid_at<$6) OR c.day=$7::date OR c.facts->>'Day'=$7::text OR (a.state IN('processing','pending','unknown') AND NOT(o.status='paid' AND EXISTS(SELECT 1 FROM verified_payment_receipts paid_v WHERE paid_v.order_id=o.id))))))
 UNION SELECT u.resource_id FROM backoffice_audit u JOIN orders o ON o.id=u.resource_id LEFT JOIN provider_attempts a ON a.order_id=o.id LEFT JOIN core_risk_successes c ON c.order_id=o.id
 WHERE u.resource='core-risk-origins' AND u.operation='admit' AND (u.actor_role,u.actor_id,u.actor_name)=('system',0,'core-risk') AND u.changes->'subject_keys'?$8 AND (($2::bigint>0 AND (u.changes->>'admitted_at'>=$9 OR u.created_at >=$3)) OR ($4::bigint>0 AND ((o.paid_at >=$5 AND o.paid_at<$6) OR c.day=$7::date OR c.facts->>'Day'=$7::text OR (a.state IN('processing','pending','unknown') AND NOT(o.status='paid' AND EXISTS(SELECT 1 FROM verified_payment_receipts paid_v WHERE paid_v.order_id=o.id))) OR EXISTS(SELECT 1 FROM backoffice_audit x WHERE x.resource='core-risk-success' AND x.resource_id=o.id AND x.operation='confirm' AND x.changes->>'day'=$7::text)))) LIMIT 100001`, ip, interval, cutoff, limit, start, end, day, key, coreRiskTimeKey(cutoff))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
		if len(ids) > 100000 {
			return nil, ErrState
		}
	}
	return ids, rows.Err()
}
