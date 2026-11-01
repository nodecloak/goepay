package server

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"

	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
)

var fundsFinancialSettingKeys = []string{"admin_funds_authorization_enabled", "settle_type", "settle_open", "settle_alipay", "settle_wxpay", "settle_bank", "settlement_minimum_cents", "settlement_maximum_cents", "settlement_fee_rate_bps", "settlement_fee_minimum_cents", "settlement_fee_maximum_cents", "settlement_retained_cents", "settlement_daily_limit", "auto_settle_money", "transfer_alipay", "transfer_enabled", "user_transfer", "user_transfer_batch", "user_transfer_red", "user_deposit", "user_deposit_min", "user_deposit_day", "transfer_minmoney", "transfer_maxmoney", "transfer_maxlimit", "transfer_desc", "transfer_name", "payment_fee_enabled", "refund_fee_payer", "enforce_domains"}

// All settings are selected by exact financial keys; target groups/rules are
// bound separately. Other merchants' history can never push a policy outside a
// global prefix. The per-target history bound is explicit and fail-closed.
func fundsRelevantPolicies(ctx context.Context, q identity.FundsQuerier, mids map[int64]bool) ([]json.RawMessage, error) {
	ids := []int64{}
	for id := range mids {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	rows, e := q.Query(ctx, `SELECT jsonb_build_object('id',b.id,'resource',b.resource,'body',
 CASE WHEN b.resource='groups' THEN jsonb_strip_nulls(jsonb_build_object(
 'enabled',b.body->'enabled','settle_open',b.body->'settle_open',
 'settle_rate',b.body->'settle_rate','settle_type',b.body->'settle_type',
 'settle_alipay',b.body->'settle_alipay','settle_wxpay',b.body->'settle_wxpay','settle_bank',b.body->'settle_bank',
 'settle_minmoney',b.body->'settle_minmoney','user_transfer',b.body->'user_transfer',
 'user_transfer_batch',b.body->'user_transfer_batch','user_deposit',b.body->'user_deposit',
 'user_deposit_min',b.body->'user_deposit_min','user_deposit_day',b.body->'user_deposit_day',
 'transfer_rate',CASE WHEN COALESCE(b.body->>'transfer_rate','') IN ('','_') THEN NULL ELSE b.body->'transfer_rate' END,
 'user_transfer_red',CASE WHEN COALESCE(b.body->>'user_transfer_red','') IN ('','_') THEN NULL ELSE b.body->'user_transfer_red' END)) ELSE b.body END)
 FROM backoffice_records b WHERE
 (b.resource='settings' AND b.body->>'key'=ANY($1::text[])) OR
 (b.resource IN ('merchant-risk','settlement-settings') AND b.owner_id=ANY($2::bigint[])) OR
 (b.resource='groups' AND b.id IN (SELECT CASE WHEN v.expires_at>statement_timestamp() THEN v.group_id
 WHEN m.group_baseline_unresolved THEN NULL::bigint ELSE m.base_group_id END
 FROM merchants m LEFT JOIN memberships v ON v.merchant_id=m.id WHERE m.id=ANY($2::bigint[])))
 ORDER BY b.id LIMIT 10001`, fundsFinancialSettingKeys, ids)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		out = append(out, json.RawMessage(raw))
		if len(out) > 10000 {
			return nil, payment.ErrInvalid
		}
	}
	return out, rows.Err()
}
func fundsMaskAccount(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= 4 {
		return "***"
	}
	return "***" + string(r[len(r)-4:])
}
func fundsMaskName(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) == 0 {
		return "***"
	}
	return string(r[:1]) + "***"
}

func fundsActionSummary(summary map[string]string, d fundsTargetDefinition, fields map[string]json.RawMessage, snapshot map[string]any, merchants map[string]json.RawMessage, policies []json.RawMessage) error {
	if d.id != "" {
		summary["resource_id"] = d.id
	}
	for _, key := range []string{"order", "funds_request", "deposit"} {
		if raw, ok := snapshot[key].(json.RawMessage); ok {
			var value struct{ Amount int64 }
			if json.Unmarshal(raw, &value) == nil && value.Amount > 0 {
				summary["original_amount"] = ledger.Format(value.Amount)
			}
		}
	}
	if raw, ok := snapshot["settlement_terms"].(json.RawMessage); ok && string(raw) != "null" {
		var terms struct {
			Gross int64 `json:"gross_amount"`
			Fee   int64 `json:"fee_amount"`
			Net   int64 `json:"net_amount"`
		}
		if json.Unmarshal(raw, &terms) != nil {
			return payment.ErrInvalid
		}
		summary["gross_amount"] = ledger.Format(terms.Gross)
		summary["fee_amount"] = ledger.Format(terms.Fee)
		summary["net_amount"] = ledger.Format(terms.Net)
		summary["external_payout"] = "false"
	}
	if selected, ok := snapshot["selected"].([]json.RawMessage); ok {
		gross, cost := int64(0), int64(0)
		for _, raw := range selected {
			var item struct {
				Amount int64 `json:"amount"`
				Cost   int64 `json:"cost_amount"`
			}
			if json.Unmarshal(raw, &item) != nil || item.Amount < 0 || item.Cost < item.Amount || gross > math.MaxInt64-item.Amount || cost > math.MaxInt64-item.Cost {
				return payment.ErrInvalid
			}
			gross += item.Amount
			cost += item.Cost
		}
		summary["gross_amount"] = ledger.Format(gross)
		summary["service_fee"] = ledger.Format(cost - gross)
		summary["balance_debit"] = ledger.Format(cost)
	}
	settings := map[string]string{}
	for _, raw := range policies {
		var row struct {
			Resource string
			Body     struct{ Key, Value string }
		}
		if json.Unmarshal(raw, &row) != nil {
			return payment.ErrInvalid
		}
		if row.Resource == "settings" {
			settings[row.Body.Key] = row.Body.Value
		}
	}
	if d.category == "transfer" {
		summary["recipient"] = fundsMaskAccount(fundsText(fields, "account"))
		summary["recipient_name"] = fundsMaskName(fundsText(fields, "name"))
	}
	if d.category == "transfer" || d.category == "batch-prepare" {
		var merchant struct{ Profile map[string]json.RawMessage }
		if json.Unmarshal(merchants[fundsText(fields, "merchant_id")], &merchant) != nil {
			return payment.ErrInvalid
		}
		rate := int64(0)
		var e error
		if selected, ok := snapshot["transfer_fee_policy"].(payment.MerchantTransferFeePolicy); ok {
			rate = selected.RateBPS
			if !selected.GroupOverride || rate < 0 || rate > 10000 {
				return payment.ErrInvalid
			}
		} else {
			rateText := ""
			if raw := merchant.Profile["transfer_rate"]; raw != nil {
				json.Unmarshal(raw, &rateText)
			}
			if rateText != "" {
				rate, e = ledger.Parse(rateText)
			}
			if e != nil || rate < 0 || rate > 10000 {
				return payment.ErrInvalid
			}
		}
		values := []string{fundsText(fields, "amount")}
		if d.category == "batch-prepare" {
			var items []batchConsoleItem
			if json.Unmarshal(fields["items"], &items) != nil {
				return payment.ErrInvalid
			}
			values = nil
			for _, item := range items {
				values = append(values, item.Amount)
			}
		}
		gross, fee := int64(0), int64(0)
		for _, v := range values {
			n, e := ledger.Parse(v)
			if e != nil || n <= 0 || n > math.MaxInt64/2 {
				return payment.ErrInvalid
			}
			f := n/10000*rate + (n%10000*rate+5000)/10000
			if gross > math.MaxInt64-n || fee > math.MaxInt64-f {
				return payment.ErrInvalid
			}
			gross += n
			fee += f
		}
		if gross > math.MaxInt64-fee {
			return payment.ErrInvalid
		}
		summary["gross_amount"] = ledger.Format(gross)
		summary["service_fee"] = ledger.Format(fee)
		summary["balance_debit"] = ledger.Format(gross + fee)
		summary["provider_bank_fee"] = "not included; subject to verified provider evidence"
	}
	if d.category == "settlements" && d.id == "" {
		p := payment.SettlementPolicy{}
		for key, dest := range map[string]*int64{"settlement_minimum_cents": &p.MinimumAmount, "settlement_maximum_cents": &p.MaximumAmount, "settlement_fee_rate_bps": &p.FeeRateBPS, "settlement_fee_minimum_cents": &p.FeeMinimumCents, "settlement_fee_maximum_cents": &p.FeeMaximumCents, "settlement_retained_cents": &p.RetainedCents, "settlement_daily_limit": &p.DailyLimit} {
			if raw := settings[key]; raw != "" {
				n, e := strconv.ParseInt(raw, 10, 64)
				if e != nil {
					return payment.ErrInvalid
				}
				*dest = n
			}
		}
		if current, ok := snapshot["settlement_policy"].(payment.SettlementPolicy); ok {
			p = current
		}
		amount, e := ledger.Parse(fundsText(fields, "amount"))
		if e != nil {
			return e
		}
		terms, e := p.Quote(amount)
		if e != nil {
			return e
		}
		summary["gross_amount"] = ledger.Format(terms.GrossAmount)
		summary["fee_amount"] = ledger.Format(terms.FeeAmount)
		summary["net_amount"] = ledger.Format(terms.NetAmount)
		summary["retained_money"] = ledger.Format(terms.RetainedCents)
		summary["external_payout"] = "false"
	}
	return nil
}
