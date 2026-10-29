package server

import (
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"net/http"
	"time"
)

func (s *Server) paymentQuery(w http.ResponseWriter, r *http.Request, legacy bool, o payment.Order) {
	charge104, chargeError104 := s.Store.AuthenticatedOrderCharge104(r.Context(), o)
	if chargeError104 != nil {
		s.wire(w, legacy, nil, "original payment amount unavailable")
		return
	}
	var upstream, buyer, clientIP, authNo, releaseState string
	var refunded int64
	var incomeHeld bool
	e := s.Pool.QueryRow(r.Context(), `SELECT COALESCE((SELECT provider_trade_no FROM verified_payment_receipts WHERE order_id=o.id),(SELECT NULLIF(provider_trade_no,'') FROM provider_attempts WHERE order_id=o.id),''),COALESCE(NULLIF(m.buyer,''),a.buyer_id,''),COALESCE(m.client_ip,''),(SELECT COALESCE(sum(amount),0) FROM refunds WHERE order_id=o.id AND status='succeeded'),COALESCE(a.auth_no,''),COALESCE(a.release_state,''),EXISTS(SELECT 1 FROM order_income_holds h WHERE h.order_id=o.id AND h.state='held') FROM orders o LEFT JOIN order_reporting m ON m.order_id=o.id LEFT JOIN payment_authorizations a ON a.order_id=o.id WHERE o.id=$1 AND o.merchant_id=$2`, o.ID, o.MerchantID).Scan(&upstream, &buyer, &clientIP, &refunded, &authNo, &releaseState, &incomeHeld)
	if e != nil {
		s.wire(w, legacy, nil, "order metadata unavailable")
		return
	}
	state := 0
	if o.Status == "paid" {
		state = 1
		if refunded == charge104 {
			state = 2
		} else if incomeHeld {
			state = 3
		}
	} else if authNo != "" && releaseState != "succeeded" {
		state = 4
		upstream = authNo
	}
	loc := time.FixedZone("business-UTC8", 8*3600)
	out := map[string]any{"trade_no": o.TradeNo, "out_trade_no": o.OutTradeNo, "api_trade_no": upstream, "pid": o.MerchantID, "name": o.Name, "type": o.Type, "money": ledger.Format(o.Amount), "status": state, "param": o.Param, "buyer": buyer, "clientip": clientIP, "addtime": o.CreatedAt.In(loc).Format("2006-01-02 15:04:05")}
	if authNo != "" {
		out["authorization_release_state"] = releaseState
	}
	if o.PaidAt != nil {
		out["endtime"] = o.PaidAt.In(loc).Format("2006-01-02 15:04:05")
	} else {
		// The original always returns endtime (NULL -> ""); emit it present-but-empty
		// for unpaid orders on both the legacy and v2 query paths.
		out["endtime"] = ""
	}
	if refunded > 0 && refunded < charge104 {
		out["refundmoney"] = ledger.Format(refunded)
	}
	if legacy {
		out["bill_trade_no"] = ""
		out["payurl"] = ""
	}
	s.wire(w, legacy, out, "")
}
