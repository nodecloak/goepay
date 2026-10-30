package server

import (
	"context"
	"encoding/json"
	"errors"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"net/http"
	"time"
)

// Call only after authenticating the original parameter set. Derived fields
// live in a copy, so signature verification never sees a mutated request.
func normalizeScanParams(p compat.Params) (compat.Params, error) {
	kind, e := provider.ScanPaymentType(p["auth_code"])
	if e != nil || p["type"] != "" && p["type"] != kind {
		return nil, payment.ErrInvalid
	}
	out := compat.Params{}
	for k, v := range p {
		out[k] = v
	}
	out["type"] = kind
	return out, nil
}
func (s *Server) providerScanCreate(w http.ResponseWriter, r *http.Request, legacy bool, mid, channel int64, ip string, amount int64, p compat.Params) {
	if s.ProviderFlow.ScanRecoveryEnabled {
		// Includes the original charge and waiting. Leave time below the HTTP
		// server's30-second write deadline to save a late original cancel ACK.
		ctx, cancel := context.WithTimeout(r.Context(), 23*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
	}
	if e := s.ProviderFlow.ValidateScanRequest(channel, provider.ScanRequest{TradeNo: "preflight", Subject: p["name"], Amount: amount, ClientIP: ip, AuthCode: p["auth_code"]}); e != nil {
		s.wire(w, legacy, nil, "scan parameters or capability unavailable")
		return
	}
	cost, e := s.ProviderFlow.PaymentCostQuote(channel, "scan")
	if e != nil {
		s.wire(w, legacy, nil, "payment cost configuration unavailable")
		return
	}
	o, e := s.Store.CreateOrder(r.Context(), payment.CreateOrderInput{PaymentCost: cost, Version: wireVersion(legacy), MerchantID: mid, OutTradeNo: p["out_trade_no"], Name: p["name"], Type: p["type"], Amount: amount, NotifyURL: p["notify_url"], ReturnURL: p["return_url"], Param: p["param"]})
	if e != nil {
		s.wire(w, legacy, nil, orderRejectedMessage(e))
		return
	}
	if e = s.Store.SetOrderClientIP(r.Context(), o.ID, ip); e != nil {
		s.wire(w, legacy, map[string]any{"trade_no": o.TradeNo}, "order metadata unavailable")
		return
	}
	current, e := s.ProviderFlow.SubmitScan(r.Context(), o.TradeNo, channel, ip, p["auth_code"])
	if s.ProviderFlow.ScanRecoveryEnabled && current.Status != "paid" {
		_, authenticatedPending := provider.VerifiedScanPending(e)
		if authenticatedPending || errors.Is(e, payment.ErrScanPending) {
			current, e = s.ProviderFlow.WaitScanRecovery(r.Context(), mid, o.TradeNo)
		}
	}
	if e != nil || current.Status != "paid" {
		s.wire(w, legacy, map[string]any{"trade_no": o.TradeNo}, "scan unresolved or rejected; query the original order")
		return
	}
	if legacy {
		s.wire(w, true, map[string]any{"trade_no": o.TradeNo, "payurl": s.checkoutURL(o.TradeNo, "checkout")}, "")
		return
	}
	var number, buyer string
	if e = s.Pool.QueryRow(r.Context(), "SELECT r.provider_trade_no,COALESCE(m.buyer,'') FROM verified_payment_receipts r LEFT JOIN order_reporting m ON m.order_id=r.order_id WHERE r.order_id=$1 AND r.channel_id=$2", o.ID, channel).Scan(&number, &buyer); e != nil {
		s.wire(w, false, map[string]any{"trade_no": o.TradeNo}, "verified payment receipt unavailable")
		return
	}
	info, e := json.Marshal(map[string]string{"type": o.Type, "trade_no": o.TradeNo, "api_trade_no": number, "buyer": buyer, "money": ledger.Format(o.Amount)})
	if e != nil {
		s.wire(w, false, nil, "payment result unavailable")
		return
	}
	s.wire(w, false, map[string]any{"trade_no": o.TradeNo, "pay_type": "scan", "pay_info": string(info)}, "")
}
