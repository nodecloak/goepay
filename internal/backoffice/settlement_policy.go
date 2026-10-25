package backoffice

import (
	"goepay/internal/ledger"
	"net/http"
	"strconv"
)

func init() {
	d := definitions["settings"]
	labels := map[string]string{"settlement_maximum_cents": "结算申请上限（分，0不限）", "settlement_fee_rate_bps": "结算手续费率（万分比）", "settlement_fee_minimum_cents": "最低结算手续费（分）", "settlement_fee_maximum_cents": "最高手续费（分，0不限）", "settlement_retained_cents": "结算后留存可出账余额（分）", "settlement_daily_limit": "每日结算申请次数（0不限）"}
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			for _, key := range []string{"settlement_maximum_cents", "settlement_fee_rate_bps", "settlement_fee_minimum_cents", "settlement_fee_maximum_cents", "settlement_retained_cents", "settlement_daily_limit"} {
				d.fields[i].Options = append(d.fields[i].Options, Option{Label: labels[key], Value: key})
			}
		}
	}
	d.description += " settlement_*金额为整数分，fee_rate_bps为0..10000基点，daily_limit为0..1000000（0不限制）。仅新申请使用并固定快照；审批不出款、不提前确认费用。"
	definitions["settings"] = d
}
func (s *Service) serveSettlementQuote(w http.ResponseWriter, r *http.Request, p Principal, parts []string) bool {
	if len(parts) != 1 || parts[0] != "settlement-policy" {
		return false
	}
	if r.Method != "GET" {
		writeError(w, ErrInvalid)
		return true
	}
	q := r.URL.Query()
	if len(q["merchant_id"]) > 1 || len(q["amount"]) > 1 {
		writeError(w, ErrInvalid)
		return true
	}
	mid := p.SubjectID
	raw := q.Get("merchant_id")
	if p.Role == "admin" {
		var e error
		mid, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || mid <= 0 {
			writeError(w, ErrInvalid)
			return true
		}
	} else if raw != "" && raw != strconv.FormatInt(mid, 10) {
		writeError(w, ErrForbidden)
		return true
	}
	var amount int64
	if raw = q.Get("amount"); raw != "" {
		var e error
		amount, e = ledger.Parse(raw)
		if e != nil || amount <= 0 {
			writeError(w, ErrInvalid)
			return true
		}
	}
	quote, e := s.store.SettlementQuote(r.Context(), mid, amount)
	if e != nil {
		writeError(w, e)
		return true
	}
	write(w, 200, map[string]any{"merchant_id": strconv.FormatInt(mid, 10), "policy": quote.Policy, "terms": quote.Terms, "daily_used": strconv.FormatInt(quote.DailyUsed, 10), "can_apply": quote.CanApply, "available_money": ledger.Format(quote.Funds.Available), "ledger_available_money": ledger.Format(quote.Funds.LedgerAvailable), "today_blocked_money": ledger.Format(quote.Funds.Blocked), "settle_type": quote.Funds.SettleType, "external_payout": false}, "estimate only; submit revalidates policy and funds")
	return true
}
