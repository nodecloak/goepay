package backoffice

import (
	"encoding/json"
	"fmt"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"net/http"
	"strconv"
)

func membershipJSON(v payment.MembershipPurchase) map[string]any {
	return map[string]any{"id": strconv.FormatInt(v.ID, 10), "merchant_id": strconv.FormatInt(v.MerchantID, 10), "group_id": strconv.FormatInt(v.GroupID, 10), "price": ledger.Format(v.Price), "price_cents": strconv.FormatInt(v.Price, 10), "group_name": v.GroupName, "external_no": v.ExternalNo, "duration_days": v.DurationDays, "expires_at": v.ExpiresAt}
}
func depositJSON(v payment.DepositAction) map[string]any {
	return map[string]any{"id": strconv.FormatInt(v.ID, 10), "merchant_id": strconv.FormatInt(v.MerchantID, 10), "amount": ledger.Format(v.Amount), "amount_cents": strconv.FormatInt(v.Amount, 10), "action": v.Action, "external_no": v.ExternalNo, "status": v.Status, "created_at": v.CreatedAt}
}
func stringInput(in map[string]json.RawMessage, key string) (string, error) {
	raw, ok := in[key]
	if !ok {
		return "", nil
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", ErrInvalid
	}
	return s, nil
}
func requestMerchant(p Principal, in map[string]json.RawMessage) (int64, error) {
	raw, e := stringInput(in, "merchant_id")
	if e != nil {
		return 0, e
	}
	if p.Role == "merchant" {
		if raw != "" && raw != strconv.FormatInt(p.SubjectID, 10) {
			return 0, ErrForbidden
		}
		return p.SubjectID, nil
	}
	id, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || id <= 0 {
		return 0, ErrInvalid
	}
	return id, nil
}
func (s *Service) serveBusiness(w http.ResponseWriter, r *http.Request, p Principal, parts []string) bool {
	if len(parts) == 0 || (parts[0] != "memberships" && parts[0] != "deposits" && parts[0] != "recharges") {
		return false
	}
	kind := parts[0]
	actor := fmt.Sprintf("%s:%d:%s", p.Role, p.SubjectID, p.Username)
	if len(parts) == 1 && r.Method == "POST" {
		in, ok := body(w, r)
		if !ok {
			return true
		}
		allowed := map[string]bool{"merchant_id": true, "external_no": true}
		switch kind {
		case "memberships":
			allowed["group_id"] = true
		case "deposits":
			allowed["action"], allowed["amount"] = true, true
		case "recharges":
			for _, k := range []string{"amount", "type", "channel_id", "method", "open_id"} {
				allowed[k] = true
			}
		}
		for k := range in {
			if !allowed[k] {
				writeError(w, ErrInvalid)
				return true
			}
		}
		mid, e := requestMerchant(p, in)
		if e != nil {
			writeError(w, e)
			return true
		}
		externalNo, e := stringInput(in, "external_no")
		if e != nil || externalNo == "" {
			writeError(w, ErrInvalid)
			return true
		}
		switch kind {
		case "memberships":
			raw, e := stringInput(in, "group_id")
			id, parse := strconv.ParseInt(raw, 10, 64)
			if e != nil || parse != nil {
				writeError(w, ErrInvalid)
				return true
			}
			v, e := s.store.PurchaseMembership(r.Context(), mid, id, externalNo, actor)
			if e != nil {
				writeError(w, e)
				return true
			}
			write(w, 201, membershipJSON(v), "membership purchased from available balance")
		case "deposits":
			raw, e := stringInput(in, "amount")
			amount, parse := ledger.Parse(raw)
			action, ae := stringInput(in, "action")
			if e != nil || parse != nil || ae != nil {
				writeError(w, ErrInvalid)
				return true
			}
			v, e := s.store.Deposit(r.Context(), mid, action, externalNo, amount, actor)
			if e != nil {
				writeError(w, e)
				return true
			}
			write(w, 201, depositJSON(v), "internal ledger deposit recorded")
		case "recharges":
			if s.RechargeStart == nil {
				write(w, 503, nil, "no configured recharge payment provider")
				return true
			}
			raw, e := stringInput(in, "amount")
			amount, parse := ledger.Parse(raw)
			typ, te := stringInput(in, "type")
			if e != nil || parse != nil || te != nil || (typ != "alipay" && typ != "wxpay") {
				writeError(w, ErrInvalid)
				return true
			}
			params := map[string]string{"type": typ}
			for _, k := range []string{"channel_id", "method", "open_id"} {
				params[k], e = stringInput(in, k)
				if e != nil {
					writeError(w, e)
					return true
				}
			}
			if params["method"] == "" {
				if typ == "wxpay" {
					params["method"] = "native"
				} else {
					params["method"] = "page"
				}
			}
			if !(typ == "wxpay" && (params["method"] == "native" || params["method"] == "h5") || typ == "alipay" && (params["method"] == "page" || params["method"] == "wap")) {
				writeError(w, ErrInvalid)
				return true
			}
			params["amount"] = ledger.Format(amount)
			params["out_trade_no"] = "recharge:" + externalNo
			if s.RechargeValidate != nil {
				if e = s.RechargeValidate(r.Context(), mid, params); e != nil {
					writeError(w, e)
					return true
				}
			}
			o, e := s.store.CreateRechargeOrder(r.Context(), payment.CreateOrderInput{MerchantID: mid, OutTradeNo: "recharge:" + externalNo, Name: "balance recharge", Type: typ, Amount: amount}, actor)
			if e != nil {
				writeError(w, e)
				return true
			}
			payURL, e := s.RechargeStart(r.Context(), mid, o.TradeNo, params)
			if e != nil {
				write(w, 202, map[string]any{"trade_no": o.TradeNo, "amount": ledger.Format(o.Amount), "status": "requires_query"}, "payment outcome requires query; no balance credited by this endpoint")
				return true
			}
			write(w, 201, map[string]any{"trade_no": o.TradeNo, "amount": ledger.Format(o.Amount), "status": "pending", "pay_url": payURL}, "pay and wait for verified callback before balance credit")
		}
		return true
	}
	if len(parts) == 3 && kind == "deposits" && r.Method == "POST" && (parts[2] == "approve" || parts[2] == "reject") {
		if p.Role != "admin" {
			writeError(w, ErrForbidden)
			return true
		}
		id, e := strconv.ParseInt(parts[1], 10, 64)
		if e != nil {
			writeError(w, ErrInvalid)
			return true
		}
		v, e := s.store.ReviewDeposit(r.Context(), id, parts[2] == "approve", actor)
		if e != nil {
			writeError(w, e)
			return true
		}
		write(w, 200, depositJSON(v), "deposit release reviewed")
		return true
	}
	if len(parts) == 1 && r.Method == "GET" {
		s.listBusiness(w, r, p, kind)
		return true
	}
	write(w, 405, nil, "method not allowed")
	return true
}
func (s *Service) listBusiness(w http.ResponseWriter, r *http.Request, p Principal, kind string) {
	page, limit := 1, 20
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, _ = strconv.Atoi(raw)
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	if page < 1 || page > 100000 || limit < 1 || limit > 100 {
		writeError(w, ErrInvalid)
		return
	}
	mid := p.SubjectID
	if p.Role == "admin" {
		mid = 0
	}
	table, where := "membership_purchases", "($1::bigint=0 OR merchant_id=$1)"
	columns := "id,merchant_id,group_id,price,group_name,external_no,duration_days,expires_at"
	if kind == "deposits" {
		table = "deposit_actions"
		columns = "id,merchant_id,amount,action,external_no,status,created_at"
	}
	if kind == "recharges" {
		table = "recharge_intents i JOIN orders o ON o.id=i.order_id"
		columns = "i.id,o.trade_no,o.amount,o.status,o.created_at"
		where = "($1::bigint=0 OR o.merchant_id=$1)"
	}
	var total int64
	if e := s.pool.QueryRow(r.Context(), "SELECT count(*) FROM "+table+" WHERE "+where, mid).Scan(&total); e != nil {
		writeError(w, e)
		return
	}
	rows, e := s.pool.Query(r.Context(), "SELECT "+columns+" FROM "+table+" WHERE "+where+" ORDER BY 1 DESC LIMIT $2 OFFSET $3", mid, limit, (page-1)*limit)
	if e != nil {
		writeError(w, e)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		switch kind {
		case "memberships":
			var v payment.MembershipPurchase
			e = rows.Scan(&v.ID, &v.MerchantID, &v.GroupID, &v.Price, &v.GroupName, &v.ExternalNo, &v.DurationDays, &v.ExpiresAt)
			if e == nil {
				items = append(items, membershipJSON(v))
			}
		case "deposits":
			var v payment.DepositAction
			e = rows.Scan(&v.ID, &v.MerchantID, &v.Amount, &v.Action, &v.ExternalNo, &v.Status, &v.CreatedAt)
			if e == nil {
				items = append(items, depositJSON(v))
			}
		case "recharges":
			var v payment.Order
			e = rows.Scan(&v.ID, &v.TradeNo, &v.Amount, &v.Status, &v.CreatedAt)
			if e == nil {
				items = append(items, map[string]any{"id": strconv.FormatInt(v.ID, 10), "trade_no": v.TradeNo, "amount": ledger.Format(v.Amount), "amount_cents": strconv.FormatInt(v.Amount, 10), "status": v.Status, "created_at": v.CreatedAt})
			}
		}
		if e != nil {
			writeError(w, e)
			return
		}
	}
	if e = rows.Err(); e != nil {
		writeError(w, e)
		return
	}
	data := map[string]any{"items": items, "total": total, "page": page, "limit": limit}
	if kind == "deposits" && mid > 0 {
		held, pending, e := s.store.DepositBalance(r.Context(), mid)
		if e != nil {
			writeError(w, e)
			return
		}
		data["deposit_money"], data["pending_release_money"] = ledger.Format(held), ledger.Format(pending)
	}
	write(w, 200, data, "")
}
