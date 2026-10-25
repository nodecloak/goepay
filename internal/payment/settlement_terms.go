package payment

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"math"
	"strconv"
	"time"
)

type SettlementPolicy struct {
	MinimumAmount   int64 `json:"minimum_cents,string"`
	MaximumAmount   int64 `json:"maximum_cents,string"`
	FeeRateBPS      int64 `json:"fee_rate_bps,string"`
	FeeMinimumCents int64 `json:"fee_minimum_cents,string"`
	FeeMaximumCents int64 `json:"fee_maximum_cents,string"`
	RetainedCents   int64 `json:"retained_cents,string"`
	DailyLimit      int64 `json:"daily_limit,string"`
}
type SettlementTerms struct {
	GrossAmount     int64 `json:"gross_amount,string"`
	FeeAmount       int64 `json:"fee_amount,string"`
	NetAmount       int64 `json:"net_amount,string"`
	FeeRateBPS      int64 `json:"fee_rate_bps,string"`
	FeeMinimumCents int64 `json:"fee_minimum_cents,string"`
	FeeMaximumCents int64 `json:"fee_maximum_cents,string"`
	RetainedCents   int64 `json:"retained_cents,string"`
	DailyLimit      int64 `json:"daily_limit,string"`
}
type settlementReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

var settlementPolicyKeys = []string{"settlement_minimum_cents", "settlement_maximum_cents", "settlement_fee_rate_bps", "settlement_fee_minimum_cents", "settlement_fee_maximum_cents", "settlement_retained_cents", "settlement_daily_limit"}

func readSettlementPolicy(ctx context.Context, q settlementReader, lock bool) (SettlementPolicy, error) {
	var out SettlementPolicy
	sql := "SELECT body->>'key',COALESCE(body->>'value','') FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1::text[]) ORDER BY body->>'key'"
	if lock {
		sql += " FOR SHARE"
	}
	rows, e := q.Query(ctx, sql, settlementPolicyKeys)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var key, raw string
		if e = rows.Scan(&key, &raw); e != nil {
			return out, e
		}
		value, parse := strconv.ParseInt(raw, 10, 64)
		max := int64(100000000000)
		if key == "settlement_fee_rate_bps" {
			max = 10000
		}
		if key == "settlement_daily_limit" {
			max = 1000000
		}
		if parse != nil || value < 0 || value > max {
			return out, ErrInvalid
		}
		switch key {
		case "settlement_minimum_cents":
			out.MinimumAmount = value
		case "settlement_maximum_cents":
			out.MaximumAmount = value
		case "settlement_fee_rate_bps":
			out.FeeRateBPS = value
		case "settlement_fee_minimum_cents":
			out.FeeMinimumCents = value
		case "settlement_fee_maximum_cents":
			out.FeeMaximumCents = value
		case "settlement_retained_cents":
			out.RetainedCents = value
		case "settlement_daily_limit":
			out.DailyLimit = value
		}
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if out.MaximumAmount > 0 && out.MinimumAmount > out.MaximumAmount || out.FeeMaximumCents > 0 && out.FeeMinimumCents > out.FeeMaximumCents {
		return out, ErrInvalid
	}
	return out, nil
}
func (p SettlementPolicy) Quote(amount int64) (SettlementTerms, error) {
	out := SettlementTerms{GrossAmount: amount, FeeRateBPS: p.FeeRateBPS, FeeMinimumCents: p.FeeMinimumCents, FeeMaximumCents: p.FeeMaximumCents, RetainedCents: p.RetainedCents, DailyLimit: p.DailyLimit}
	for _, value := range []int64{p.MinimumAmount, p.MaximumAmount, p.FeeMinimumCents, p.FeeMaximumCents, p.RetainedCents} {
		if value < 0 || value > 100000000000 {
			return out, ErrInvalid
		}
	}
	if p.FeeRateBPS < 0 || p.FeeRateBPS > 10000 || p.DailyLimit < 0 || p.DailyLimit > 1000000 || p.MaximumAmount > 0 && p.MinimumAmount > p.MaximumAmount || p.FeeMaximumCents > 0 && p.FeeMinimumCents > p.FeeMaximumCents {
		return out, ErrInvalid
	}
	if amount <= 0 || amount < p.MinimumAmount || p.MaximumAmount > 0 && amount > p.MaximumAmount || amount > math.MaxInt64-p.RetainedCents {
		return out, ErrInvalid
	}
	if p.FeeRateBPS > 0 {
		out.FeeAmount = amount/10000*p.FeeRateBPS + (amount%10000*p.FeeRateBPS+5000)/10000
		if out.FeeAmount < p.FeeMinimumCents {
			out.FeeAmount = p.FeeMinimumCents
		}
		if p.FeeMaximumCents > 0 && out.FeeAmount > p.FeeMaximumCents {
			out.FeeAmount = p.FeeMaximumCents
		}
	}
	if out.FeeAmount >= amount {
		return out, ErrInvalid
	}
	out.NetAmount = amount - out.FeeAmount
	return out, nil
}
func (s *Store) SettlementPolicy(ctx context.Context) (SettlementPolicy, error) {
	return readSettlementPolicy(ctx, s.pool, false)
}

// SettlementTermsJSON is a fixed correlated expression for the existing list.
// Values are strings so JavaScript never rounds big integer financial snapshots.
const SettlementTermsJSON = "(SELECT jsonb_build_object('gross_amount',st.gross_amount::text,'fee_amount',st.fee_amount::text,'net_amount',st.net_amount::text,'fee_rate_bps',st.fee_rate_bps::text,'fee_minimum_cents',st.fee_minimum_cents::text,'fee_maximum_cents',st.fee_maximum_cents::text,'retained_cents',st.retained_cents::text,'daily_limit',st.daily_limit::text) FROM settlement_request_terms st WHERE st.request_id=ledger_requests.id)"

func attachSettlementTerms(ctx context.Context, q fundsReader, v LedgerRequest) (LedgerRequest, error) {
	if v.Kind != "settlements" {
		return v, nil
	}
	var raw []byte
	e := q.QueryRow(ctx, "SELECT "+SettlementTermsJSON+" FROM ledger_requests WHERE id=$1", v.ID).Scan(&raw)
	if e != nil {
		return v, e
	}
	if len(raw) > 0 {
		e = json.Unmarshal(raw, &v.Settlement)
	}
	if e != nil {
		return v, e
	}
	return attachSettlementFunding(ctx, q, v)
}
func persistSettlementTerms(ctx context.Context, tx pgx.Tx, id int64, p SettlementTerms) error {
	_, e := tx.Exec(ctx, "INSERT INTO settlement_request_terms(request_id,gross_amount,fee_amount,net_amount,fee_rate_bps,fee_minimum_cents,fee_maximum_cents,retained_cents,daily_limit) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", id, p.GrossAmount, p.FeeAmount, p.NetAmount, p.FeeRateBPS, p.FeeMinimumCents, p.FeeMaximumCents, p.RetainedCents, p.DailyLimit)
	return e
}
func requireSettlementDaily(ctx context.Context, tx pgx.Tx, mid, limit int64, operationAt time.Time) error {
	if limit == 0 {
		return nil
	}
	var count int64
	local := operationAt.In(time.FixedZone("UTC+08:00", 8*3600))
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	e := tx.QueryRow(ctx, "SELECT count(*) FROM ledger_requests WHERE kind='settlements' AND merchant_id=$1 AND created_at>=$2 AND created_at<$3", mid, start, start.AddDate(0, 0, 1)).Scan(&count)
	if e != nil {
		return e
	}
	if count >= limit {
		return ErrState
	}
	return nil
}
