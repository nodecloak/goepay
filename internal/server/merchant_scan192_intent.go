package server

import (
	"errors"
	"net"
	"net/http"
	"strings"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
)

type merchantScanOutput192 struct {
	RequestID   string `json:"request_id"`
	TradeNo     string `json:"trade_no"`
	Money       string `json:"money"`
	ChargeMoney string `json:"charge_money"`
	NetMoney    string `json:"net_money"`
	FeeMoney    string `json:"fee_money"`
	Remark      string `json:"remark"`
	PayType     string `json:"type"`
	State       string `json:"state"`
	CanApply    bool   `json:"can_apply"`
	CanQuery    bool   `json:"can_query"`
}

func merchantScanError192(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrOriginalMerchantSession):
		response(w, 403, -1, "original merchant session required", nil)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "original request not found; do not create another charge while outcome is unknown", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 422, -1, "invalid cashier parameters", nil)
	case errors.Is(e, payment.ErrConflict), errors.Is(e, payment.ErrState), errors.Is(e, payment.ErrLiveDisabled):
		response(w, 409, -1, "original request cannot be submitted; recover the same request", nil)
	default:
		response(w, 503, -1, "original cashier request could not be confirmed", nil)
	}
}
func (s *Server) merchantScanResult192(w http.ResponseWriter, r *http.Request, a payment.MerchantRSAActor, key string) {
	v, e := s.Store.ReadMerchantScan106(r.Context(), a, key)
	if e != nil {
		merchantScanError192(w, e)
		return
	}
	enabled := false
	if view, e := s.withParentRuntime(r.Context(), a.User.ID, v.Intent.OutTradeNo); e == nil {
		enabled = view.merchantScanGrant192(view.ProviderFlow, a.User.ID, v.ChannelID)
	}
	out := merchantScanOutput192{RequestID: key, TradeNo: v.Order.TradeNo, Money: ledger.Format(v.Intent.Amount), ChargeMoney: ledger.Format(v.Charge), NetMoney: ledger.Format(v.Order.MerchantAmount), FeeMoney: ledger.Format(v.Order.PlatformFee), Remark: v.Intent.Name, PayType: v.Intent.Kind, State: v.State, CanApply: v.CanApply && enabled, CanQuery: v.CanQuery}
	response(w, 200, 0, "", out)
}

func (s *Server) merchantScanGrant192(flow *payment.ProviderFlow, mid, channel int64) bool {
	if flow == nil || !s.paymentExecutionWriteAllowed() {
		return false
	}
	origin := channel
	if source, ok := flow.ParentExecutionPermissionOrigins()[mid][channel]; ok {
		origin = source
	}
	return s.collectionOperatorAllowed236(mid, origin)
}
func (s *Server) merchantScanIntent192(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string, token string) {
	if s.Store == nil || s.ProviderFlow == nil {
		response(w, 503, -1, "merchant cashier unavailable", nil)
		return
	}
	a := payment.MerchantRSAActor{User: session.User, Token: token, CSRF: session.CSRF}
	if len(parts) == 3 && parts[2] == "prepare" {
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			response(w, 405, -1, "method not allowed", nil)
			return
		}
		var in struct {
			RequestID string `json:"request_id"`
			Money     string `json:"money"`
			Remark    string `json:"remark"`
			Code      string `json:"auth_code"`
		}
		if !decodeBuyerFeeInput104(w, r, &in, "request_id", "money", "remark", "auth_code") {
			return
		}
		amount, e := ledger.Parse(in.Money)
		if e != nil || amount <= 0 || amount > 99999999 || len(in.Money) > 9 || len(strings.SplitN(in.Money, ".", 2)[0]) > 6 {
			merchantScanError192(w, payment.ErrInvalid)
			return
		}
		kind, e := adapter.ScanPaymentType(in.Code)
		if e != nil {
			merchantScanError192(w, payment.ErrInvalid)
			return
		}
		if !s.paymentExecutionWriteAllowed() {
			merchantScanError192(w, payment.ErrLiveDisabled)
			return
		}
		ip, _, e := net.SplitHostPort(r.RemoteAddr)
		if e != nil {
			ip = r.RemoteAddr
		}
		parsed := net.ParseIP(ip)
		if parsed == nil {
			merchantScanError192(w, payment.ErrInvalid)
			return
		}
		ip = parsed.String()
		// Use the direct HTTP peer, never a browser-provided clientip or proxy header.
		ctx := payment.WithCoreRiskCustomerIP(r.Context(), ip, "direct_http")
		t, e := s.Store.DeclareMerchantScan106(ctx, a, payment.MerchantScanInput106{RequestID: in.RequestID, Amount: amount, Remark: in.Remark, Kind: kind, ClientIP: ip, AuthCode: in.Code})
		if e != nil {
			merchantScanError192(w, e)
			return
		}
		view, e := s.withParentRuntime(ctx, a.User.ID, t.OutTradeNo)
		if e != nil {
			merchantScanError192(w, e)
			return
		}
		p := compat.Params{"out_trade_no": t.OutTradeNo, "name": t.Name, "money": ledger.Format(t.Amount), "type": t.Kind, "method": "scan", "auth_code": in.Code, "clientip": t.ClientIP, "param": t.Marker()}
		channel, method, e := view.providerRoutedRequest(ctx, a.User.ID, p)
		if e != nil {
			merchantScanError192(w, e)
			return
		}
		if method != "scan" {
			merchantScanError192(w, payment.ErrConflict)
			return
		}
		view, e = view.paymentIntentServer(ctx, a.User.ID, t.OutTradeNo, channel)
		if e != nil {
			merchantScanError192(w, e)
			return
		}
		cost, e := view.ProviderFlow.PaymentCostQuote(channel, "scan")
		if e != nil {
			merchantScanError192(w, e)
			return
		}
		o, e := view.Store.CreateOrder(ctx, payment.CreateOrderInput{Version: "v2", PaymentCost: cost, MerchantID: a.User.ID, OutTradeNo: t.OutTradeNo, Name: t.Name, Type: t.Kind, Amount: t.Amount, Param: t.Marker()})
		if e != nil {
			merchantScanError192(w, e)
			return
		}
		if e = view.Store.SetOrderClientIP(ctx, o.ID, t.ClientIP); e != nil {
			merchantScanError192(w, e)
			return
		}
		if e = view.Store.BindMerchantScanOrder106(ctx, a, t.RequestID, channel); e != nil {
			merchantScanError192(w, e)
			return
		}
		s.merchantScanResult192(w, r, a, t.RequestID)
		return
	}
	if len(parts) < 4 || parts[2] != "intents" || len(parts) > 5 {
		response(w, 404, -1, "not found", nil)
		return
	}
	key := parts[3]
	if len(parts) == 4 {
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			response(w, 405, -1, "method not allowed", nil)
			return
		}
		s.merchantScanResult192(w, r, a, key)
		return
	}
	action := parts[4]
	if action != "apply" && action != "query" && action != "close" {
		response(w, 404, -1, "not found", nil)
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		response(w, 405, -1, "method not allowed", nil)
		return
	}
	var code string
	if action == "apply" {
		var body struct {
			Code string `json:"auth_code"`
		}
		if !decodeBuyerFeeInput104(w, r, &body, "auth_code") {
			return
		}
		code = body.Code
	} else {
		var body struct{}
		if !decodeBuyerFeeInput104(w, r, &body) {
			return
		}
	}
	if action == "close" {
		if e := s.Store.CloseMerchantScan106(r.Context(), a, key); e != nil {
			merchantScanError192(w, e)
			return
		}
		s.merchantScanResult192(w, r, a, key)
		return
	}
	v, e := s.Store.ReadMerchantScan106(r.Context(), a, key)
	if e != nil {
		merchantScanError192(w, e)
		return
	}
	if action == "apply" && v.State == "closed" {
		s.merchantScanResult192(w, r, a, key)
		return
	}
	if v.Order.TradeNo == "" {
		merchantScanError192(w, payment.ErrState)
		return
	}
	if action == "apply" {
		if e = s.Store.CheckMerchantScanCode106(v.Intent, code); e != nil {
			merchantScanError192(w, e)
			return
		}
		// Existing original attempts are read-only replays, including timeout/crash.
		// A replay can never call Scan again, even when the current grant is removed.
		if v.CanQuery || v.State != "prepared" {
			s.merchantScanResult192(w, r, a, key)
			return
		}
		if !v.CanApply || !s.paymentExecutionWriteAllowed() {
			merchantScanError192(w, payment.ErrState)
			return
		}
		view, e := s.withParentRuntime(r.Context(), a.User.ID, v.Intent.OutTradeNo)
		if e != nil {
			merchantScanError192(w, e)
			return
		}
		view, e = view.paymentIntentServer(r.Context(), a.User.ID, v.Intent.OutTradeNo, v.ChannelID)
		if e != nil {
			merchantScanError192(w, e)
			return
		}
		allowed := func(mid, channel int64) bool { return view.merchantScanGrant192(view.ProviderFlow, mid, channel) }
		ctx := s.Store.WithMerchantScan106(r.Context(), a, v.Intent, allowed)
		ctx = payment.WithCoreRiskCustomerIP(ctx, v.Intent.ClientIP, "direct_http")
		// SDK errors do not imply definite failure. Read the durable original state;
		// never serialize raw provider bodies or bearer code into a console response.
		_, _ = view.ProviderFlow.SubmitScan(ctx, v.Order.TradeNo, v.ChannelID, v.Intent.ClientIP, code)
	} else {
		if !v.CanQuery {
			merchantScanError192(w, payment.ErrState)
			return
		}
		if _, e = s.ProviderFlow.Query(r.Context(), a.User.ID, v.Order.TradeNo); e != nil {
			response(w, 409, -1, "original payment outcome remains unconfirmed; query the same request", nil)
			return
		}
	}
	s.merchantScanResult192(w, r, a, key)
}
