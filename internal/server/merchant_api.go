package server

import (
	"encoding/json"
	"fmt"
	"goepay/internal/compat"
	"goepay/internal/ledger"
	"goepay/internal/merchantpolicy"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) merchantInfo(w http.ResponseWriter, r *http.Request, legacy bool, id int64) {
	balance, e := s.Store.Balance(r.Context(), id)
	if e != nil {
		s.wire(w, legacy, nil, "balance unavailable")
		return
	}
	var profileRaw []byte
	var payStatus int
	if e = s.Pool.QueryRow(r.Context(), "SELECT profile,COALESCE((to_jsonb(merchants)->>'pay')::int,1) FROM merchants WHERE id=$1", id).Scan(&profileRaw, &payStatus); e != nil {
		s.wire(w, legacy, nil, "merchant unavailable")
		return
	}
	profile := map[string]any{}
	settleStatus := 0
	if !legacy {
		allowed, err := merchantpolicy.ResolveSettlementPermission(profileRaw)
		if err != nil {
			s.wire(w, false, nil, "merchant permission unavailable")
			return
		}
		if allowed {
			settleStatus = 1
		}
	}
	if e = json.Unmarshal(profileRaw, &profile); e != nil {
		s.wire(w, legacy, nil, "merchant unavailable")
		return
	}
	stringValue := func(key string) string {
		v := profile[key]
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
	intValue := func(key string, defaultValue int) int {
		raw := stringValue(key)
		if raw == "" {
			return defaultValue
		}
		n, e := strconv.Atoi(raw)
		if e != nil {
			return defaultValue
		}
		return n
	}
	loc := time.FixedZone("business-UTC8", 8*3600)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterday := today.AddDate(0, 0, -1)
	var total, todayCount, yesterdayCount, todayPaid, yesterdayPaid, todayMoney, yesterdayMoney int64
	e = s.Pool.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE created_at>=$2),count(*) FILTER(WHERE created_at>=$3 AND created_at<$2),count(*) FILTER(WHERE status='paid' AND created_at>=$2),count(*) FILTER(WHERE status='paid' AND created_at>=$3 AND created_at<$2),COALESCE(sum(amount) FILTER(WHERE status='paid' AND created_at>=$2),0),COALESCE(sum(amount) FILTER(WHERE status='paid' AND created_at>=$3 AND created_at<$2),0) FROM orders WHERE merchant_id=$1`, id, today, yesterday).Scan(&total, &todayCount, &yesterdayCount, &todayPaid, &yesterdayPaid, &todayMoney, &yesterdayMoney)
	if e != nil {
		s.wire(w, legacy, nil, "statistics unavailable")
		return
	}
	if legacy {
		m, e := s.Store.Merchant(r.Context(), id)
		if e != nil {
			s.wire(w, legacy, nil, "merchant unavailable")
			return
		}
		out := map[string]any{"pid": id, "key": m.APIKey, "active": 1, "money": ledger.Format(balance.Available), "type": intValue("settle_type", 0), "account": stringValue("settle_account"), "username": stringValue("settle_name"), "orders": total, "order_today": todayPaid, "order_lastday": yesterdayPaid}
		if stringValue("compat_profile") == "release-legacy" {
			delete(out, "order_today")
			delete(out, "order_lastday")
			out["orders_today"] = todayPaid
			out["orders_lastday"] = yesterdayPaid
		}
		s.wire(w, true, out, "")
		return
	}
	s.wire(w, false, map[string]any{"pid": id, "status": 1, "pay_status": payStatus, "settle_status": settleStatus, "money": ledger.Format(balance.Available), "settle_type": intValue("settle_type", 0), "settle_account": stringValue("settle_account"), "settle_name": stringValue("settle_name"), "order_num": total, "order_num_today": todayCount, "order_num_lastday": yesterdayCount, "order_money_today": ledger.Format(todayMoney), "order_money_lastday": ledger.Format(yesterdayMoney)}, "")
}
func (s *Server) merchantOrders(w http.ResponseWriter, r *http.Request, legacy bool, id int64, p compat.Params) {
	limit, offset := 20, 0
	releaseProfile := false
	if legacy {
		var profile string
		if e := s.Pool.QueryRow(r.Context(), "SELECT COALESCE(profile->>'compat_profile','pro-v1') FROM merchants WHERE id=$1", id).Scan(&profile); e != nil {
			s.wire(w, legacy, nil, "merchant unavailable")
			return
		}
		releaseProfile = profile == "release-legacy"
		if releaseProfile {
			limit = 10
		}
	}
	parse := func(raw string, max int) (int, bool) {
		n, e := strconv.Atoi(raw)
		return n, e == nil && n >= 0 && n <= max
	}
	if raw := p["limit"]; raw != "" {
		n, ok := parse(raw, 1000000)
		if !ok || n < 1 {
			s.wire(w, legacy, nil, "invalid limit")
			return
		}
		if n > 50 {
			n = 50
		}
		limit = n
	}
	if p["offset"] != "" {
		n, ok := parse(p["offset"], 10000000)
		if !ok {
			s.wire(w, legacy, nil, "invalid offset")
			return
		}
		offset = n
	}
	if legacy && p["page"] != "" {
		if p["offset"] != "" {
			s.wire(w, legacy, nil, "ambiguous pagination")
			return
		}
		n, ok := parse(p["page"], 1000000)
		if !ok || n < 1 {
			s.wire(w, legacy, nil, "invalid page")
			return
		}
		offset = (n - 1) * limit
	}
	status := p["status"]
	if status != "" && status != "0" && status != "1" && status != "2" && status != "3" && status != "4" {
		s.wire(w, legacy, nil, "invalid status")
		return
	}
	rows, e := s.Pool.Query(r.Context(), `SELECT o.id,o.trade_no,o.out_trade_no,o.type,o.name,o.amount,o.status,o.param,o.created_at,o.paid_at,COALESCE(NULLIF(b.buyer,''),z.buyer_id,''),COALESCE(v.provider_trade_no,NULLIF(a.provider_trade_no,''),''),f.refunded,CASE WHEN z.release_state='succeeded' THEN '' ELSE COALESCE(z.auth_no,'') END,i.held FROM orders o LEFT JOIN order_reporting b ON b.order_id=o.id LEFT JOIN verified_payment_receipts v ON v.order_id=o.id LEFT JOIN provider_attempts a ON a.order_id=o.id LEFT JOIN payment_authorizations z ON z.order_id=o.id LEFT JOIN LATERAL(SELECT COALESCE(sum(amount),0) AS refunded FROM refunds WHERE order_id=o.id AND status='succeeded') f ON true CROSS JOIN LATERAL(SELECT EXISTS(SELECT 1 FROM order_income_holds h WHERE h.order_id=o.id AND h.state='held') AS held) i WHERE o.merchant_id=$1 AND ($2::text='' OR ($2='1' AND o.status='paid' AND f.refunded<COALESCE((to_jsonb(o)->>'actual_charge_amount104')::bigint,o.amount) AND NOT i.held) OR ($2='2' AND o.status='paid' AND f.refunded=COALESCE((to_jsonb(o)->>'actual_charge_amount104')::bigint,o.amount)) OR ($2='3' AND o.status='paid' AND i.held AND f.refunded<COALESCE((to_jsonb(o)->>'actual_charge_amount104')::bigint,o.amount)) OR ($2='4' AND o.status<>'paid' AND z.order_id IS NOT NULL AND z.release_state<>'succeeded') OR ($2='0' AND o.status<>'paid' AND (z.order_id IS NULL OR z.release_state='succeeded'))) ORDER BY o.id DESC LIMIT $3 OFFSET $4`, id, status, limit, offset)
	if e != nil {
		s.wire(w, legacy, nil, "order list unavailable")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	ids := []int64{}
	for rows.Next() {
		var trade, external, kind, name, state, param, buyer, upstream, authNo string
		var orderID, amount, refunded int64
		var created time.Time
		var paid *time.Time
		var incomeHeld bool
		if e := rows.Scan(&orderID, &trade, &external, &kind, &name, &amount, &state, &param, &created, &paid, &buyer, &upstream, &refunded, &authNo, &incomeHeld); e != nil {
			s.wire(w, legacy, nil, "order list unavailable")
			return
		}
		paidStatus := 0
		if state == "paid" {
			paidStatus = 1
			if refunded == amount {
				paidStatus = 2
			} else if incomeHeld {
				paidStatus = 3
			}
		} else if authNo != "" {
			paidStatus = 4
			upstream = authNo
		}
		end := ""
		if paid != nil {
			end = paid.In(time.FixedZone("business-UTC8", 8*3600)).Format("2006-01-02 15:04:05")
		}
		item := map[string]any{"trade_no": trade, "out_trade_no": external, "type": kind, "pid": id, "addtime": created.In(time.FixedZone("business-UTC8", 8*3600)).Format("2006-01-02 15:04:05"), "endtime": end, "name": name, "money": ledger.Format(amount), "status": paidStatus, "param": param, "buyer": buyer, "api_trade_no": upstream}
		if refunded > 0 && refunded < amount {
			item["refundmoney"] = ledger.Format(refunded)
		}
		item["_id104"] = orderID
		item["_refunded104"] = refunded
		item["_state104"] = state
		item["_held104"] = incomeHeld
		ids = append(ids, orderID)
		items = append(items, item)
	}
	if rows.Err() != nil {
		s.wire(w, legacy, nil, "order list unavailable")
		return
	}
	rows.Close()
	charges, e := s.Store.OriginalOrderCharges104(r.Context(), id, ids)
	if e != nil || len(charges) != len(ids) {
		s.wire(w, legacy, nil, "original payment amounts unavailable")
		return
	}
	for _, item := range items {
		charge, ok := charges[item["_id104"].(int64)]
		if !ok {
			s.wire(w, legacy, nil, "original payment amount unavailable")
			return
		}
		refunded := item["_refunded104"].(int64)
		if refunded < 0 || refunded > charge {
			s.wire(w, legacy, nil, "original refund amount unavailable")
			return
		}
		if item["_state104"] == "paid" {
			item["status"] = 1
			if refunded == charge {
				item["status"] = 2
			} else if item["_held104"] == true {
				item["status"] = 3
			}
		}
		delete(item, "refundmoney")
		if refunded > 0 && refunded < charge {
			item["refundmoney"] = ledger.Format(refunded)
		}
		for _, key := range []string{"_id104", "_refunded104", "_state104", "_held104"} {
			delete(item, key)
		}
	}
	out := map[string]any{"data": items}
	if legacy && releaseProfile {
		out["count"] = len(items)
	}
	s.wire(w, legacy, out, "")
}
func (s *Server) transferRead(w http.ResponseWriter, r *http.Request, id int64, p compat.Params, route string) {
	if route == "/api/transfer/submit" || route == "/api/transfer/proof" {
		s.wire(w, false, nil, "no transfer provider configured")
		return
	}
	if route == "/api/transfer/balance" {
		balance, e := s.Store.FundsAvailability(r.Context(), id)
		if e != nil {
			s.wire(w, false, nil, "balance unavailable")
			return
		}
		var rate string
		e = s.Pool.QueryRow(r.Context(), "SELECT COALESCE(profile->>'transfer_rate','0') FROM merchants WHERE id=$1", id).Scan(&rate)
		if e != nil {
			s.wire(w, false, nil, "transfer configuration unavailable")
			return
		}
		s.wire(w, false, map[string]any{"available_money": ledger.Format(balance.Available), "ledger_available_money": ledger.Format(balance.LedgerAvailable), "today_blocked_money": ledger.Format(balance.Blocked), "settle_type": balance.SettleType, "transfer_rate": rate}, "")
		return
	}
	number := p["out_biz_no"]
	if number == "" && p["biz_no"] != "" {
		n, e := strconv.ParseInt(p["biz_no"], 10, 64)
		if e != nil {
			s.wire(w, false, nil, "invalid transfer number")
			return
		}
		if e = s.Pool.QueryRow(r.Context(), "SELECT transfer_no FROM transfers WHERE id=$1 AND merchant_id=$2", n, id).Scan(&number); e != nil {
			s.wire(w, false, nil, "transfer not found")
			return
		}
	}
	transfer, e := s.Store.Transfer(r.Context(), id, number)
	if e != nil {
		s.wire(w, false, nil, "transfer not found")
		return
	}
	status := 0
	if transfer.Status == "succeeded" {
		status = 1
	} else if transfer.Status == "failed" {
		status = 2
	}
	out := map[string]any{"status": status, "biz_no": strconv.FormatInt(transfer.ID, 10), "out_biz_no": transfer.TransferNo, "orderid": "", "paydate": "", "amount": ledger.Format(transfer.Amount), "cost_money": ledger.Format(transfer.Amount), "remark": ""}
	if status == 2 {
		out["errmsg"] = "transfer failed"
	}
	s.wire(w, false, out, "")
}
