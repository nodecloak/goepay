package server

import (
	"encoding/json"
	"errors"
	"goepay/internal/backoffice"
	"goepay/internal/compat"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"net"
	"net/http"
	"strconv"
)

func (s *Server) subchannelPaymentTest(w http.ResponseWriter, r *http.Request, a payment.SubchannelActor, id int64) {
	if r.Method == "GET" {
		s.subchannelTestLookup(w, r, a, id)
		return
	}
	if r.Method != "POST" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 405, -1, "method not allowed", nil)
		return
	}
	c, e := s.Store.Subchannel(r.Context(), a, id)
	if e != nil {
		childConsoleError(w, e)
		return
	}
	fields, e := childConsoleBody(w, r)
	var money, intent string
	if e != nil || len(fields) != 2 || json.Unmarshal(fields["amount"], &money) != nil || json.Unmarshal(fields["intent"], &intent) != nil || len(intent) != 32 {
		childConsoleError(w, payment.ErrInvalid)
		return
	}
	for _, ch := range intent {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			childConsoleError(w, payment.ErrInvalid)
			return
		}
	}
	amount, e := ledger.Parse(money)
	if e != nil || amount < 1 || amount > 100000000 {
		childConsoleError(w, payment.ErrInvalid)
		return
	}
	if s.ProviderFlow == nil || len(s.CheckoutKey) < 32 {
		childConsoleError(w, payment.ErrState)
		return
	}
	no := "subchannel-test:" + strconv.FormatInt(id, 10) + ":" + intent
	name := "子通道支付测试"
	param := "subchannel-test:" + strconv.FormatInt(id, 10)
	if old, err := s.Store.Order(r.Context(), c.MerchantID, no); err == nil {
		if old.Amount != amount || old.Type != c.PayType || old.Name != name || old.Param != param {
			childConsoleError(w, payment.ErrConflict)
			return
		}
		if attempt, err := s.Store.ProviderAttempt(r.Context(), old.TradeNo); err == nil {
			original, e := s.paymentIntentServer(r.Context(), c.MerchantID, no, attempt.ChannelID)
			if e != nil || original.ProviderFlow.SubchannelID() != id {
				childConsoleError(w, payment.ErrConflict)
				return
			}
			if e = original.ProviderFlow.ValidatePaymentAccount(r.Context(), c.MerchantID, old.TradeNo); e != nil {
				childConsoleError(w, e)
				return
			}
			s.childTestResponse(w, old, attempt)
			return
		} else if !errors.Is(err, payment.ErrNotFound) {
			childConsoleError(w, err)
			return
		}
	} else if !errors.Is(err, payment.ErrNotFound) {
		childConsoleError(w, err)
		return
	}
	view, e := s.withParentRuntime(r.Context(), c.MerchantID, "")
	if e != nil {
		childConsoleError(w, e)
		return
	}
	s = view
	if !s.ProviderFlow.AllowLive {
		childConsoleError(w, payment.ErrState)
		return
	}
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	if net.ParseIP(ip) == nil {
		childConsoleError(w, payment.ErrInvalid)
		return
	}
	if e = backoffice.New(s.Pool, s.Store).CheckPayment(r.Context(), backoffice.PaymentContext{MerchantID: c.MerchantID, ClientIP: ip}); e != nil {
		childConsoleError(w, payment.ErrState)
		return
	}
	p := compat.Params{"type": c.PayType, "subchannel_id": strconv.FormatInt(id, 10), "out_trade_no": no, "name": name, "money": ledger.Format(amount), "clientip": ip, "param": param}
	parent, method, e := s.providerRoutedBrowserRequest(r.Context(), c.MerchantID, p)
	if e != nil {
		childConsoleError(w, e)
		return
	}
	resolved, e := s.paymentIntentServer(r.Context(), c.MerchantID, no, parent)
	if e != nil {
		childConsoleError(w, e)
		return
	}
	cost, e := resolved.ProviderFlow.PaymentCostQuote(parent, method)
	if e != nil {
		childConsoleError(w, e)
		return
	}
	order, e := s.Store.CreateOrder(r.Context(), payment.CreateOrderInput{Version: "v2", MerchantID: c.MerchantID, OutTradeNo: no, Name: name, Type: c.PayType, Amount: amount, Param: param, PaymentCost: cost})
	if e != nil {
		childConsoleError(w, e)
		return
	}
	if e = s.Store.SetOrderClientIP(r.Context(), order.ID, ip); e != nil {
		childConsoleError(w, e)
		return
	}
	attempt, e := resolved.ProviderFlow.Submit(r.Context(), order.TradeNo, parent, method, ip, "")
	if e != nil && attempt.ID == 0 {
		if saved, readErr := s.Store.ProviderAttempt(r.Context(), order.TradeNo); readErr == nil {
			attempt = saved
		} else {
			childConsoleError(w, e)
			return
		}
	}
	s.childTestResponse(w, order, attempt)
}
func (s *Server) childTestResponse(w http.ResponseWriter, o payment.Order, a payment.ProviderAttempt) {
	status := 200
	if a.State != "pending" {
		status = 202
	}
	response(w, status, 0, "", map[string]any{"trade_no": o.TradeNo, "state": a.State, "url": s.checkoutURL(o.TradeNo, "checkout")})
}
