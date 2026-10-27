package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
)

type parentTestOutput struct {
	RequestID    string `json:"request_id"`
	ChannelID    int64  `json:"channel_id,string"`
	MerchantID   int64  `json:"merchant_id,string"`
	OutTradeNo   string `json:"out_trade_no"`
	TradeNo      string `json:"trade_no"`
	Name         string `json:"name"`
	Money        string `json:"money"`
	State        string `json:"state"`
	CheckoutURL  string `json:"checkout_url"`
	MaterialType string `json:"material_type"`
	CanQuery     bool   `json:"can_query"`
}

// The ordinary orchestration emits its merchant wire response. The console
// returns only the protected test DTO, read from its original durable order.
type parentTestWireSink struct{ header http.Header }

func (w *parentTestWireSink) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}
func (*parentTestWireSink) WriteHeader(int)             {}
func (*parentTestWireSink) Write(b []byte) (int, error) { return len(b), nil }

func (s *Server) parentTestOutput(r *http.Request, t payment.ParentPaymentTest) (parentTestOutput, error) {
	out := parentTestOutput{RequestID: t.RequestID, ChannelID: t.ChannelID, MerchantID: t.MerchantID, OutTradeNo: t.OutTradeNo, Name: t.Name, Money: ledger.Format(t.Amount), State: "blocked"}
	o, e := s.Store.Order(r.Context(), t.MerchantID, t.OutTradeNo)
	if errors.Is(e, payment.ErrNotFound) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	if o.Amount != t.Amount || o.Name != t.Name || o.Param != t.Marker() {
		return out, payment.ErrConflict
	}
	out.TradeNo = o.TradeNo
	out.State = "prepared"
	a, e := s.Store.ProviderAttempt(r.Context(), o.TradeNo)
	if errors.Is(e, payment.ErrNotFound) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	if a.ChannelID != t.ChannelID {
		return out, payment.ErrConflict
	}
	out.State = a.State
	out.MaterialType = a.MaterialType
	out.CanQuery = true
	if o.Status == "paid" || o.Status == "closed" || o.Status == "failed" {
		out.State = o.Status
	}
	if a.Material != "" {
		u, e := url.Parse(s.checkoutURL(o.TradeNo, "checkout"))
		if e != nil {
			return out, e
		}
		out.CheckoutURL = u.EscapedPath() + "?" + u.RawQuery
	}
	return out, nil
}
func (s *Server) parentPaymentTestRoute(w http.ResponseWriter, r *http.Request, session identity.Session, token string, parts []string) bool {
	if len(parts) < 4 || parts[3] != "tests" {
		return false
	}
	channel, ok := positiveFundsID(parts[2])
	if !ok || r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "invalid payment test target", nil)
		return true
	}
	actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	operator := func(mid, source int64) bool {
		for _, id := range s.MerchantChannels[mid] {
			if id == source {
				return true
			}
		}
		return false
	}
	var original payment.ParentPaymentTest
	var e error
	if len(parts) == 4 && r.Method == "POST" {
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
			response(w, 400, -1, "invalid payment test body", nil)
			return true
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
		fields, parseErr := parentExactObject(raw)
		if err != nil || parseErr != nil || len(fields) != 4 {
			response(w, 400, -1, "invalid payment test fields", nil)
			return true
		}
		var in payment.ParentPaymentTestInput
		var money string
		for k, v := range fields {
			var target *string
			switch k {
			case "request_id":
				target = &in.RequestID
			case "name":
				target = &in.Name
			case "money":
				target = &money
			case "expected_fingerprint":
				target = &in.Expected
			default:
				response(w, 400, -1, "invalid payment test fields", nil)
				return true
			}
			if json.Unmarshal(v, target) != nil {
				response(w, 400, -1, "invalid payment test field", nil)
				return true
			}
		}
		in.Amount, e = ledger.Parse(money)
		if e != nil || in.Amount <= 0 {
			response(w, 422, -1, "invalid payment test amount", nil)
			return true
		}
		var owner bool
		original, owner, e = s.ProviderFlow.CreateParentPaymentTest(r.Context(), actor, channel, in, s.ProviderFlow.CallbackBaseURL, operator)
		if e != nil {
			parentChannelError(w, e)
			return true
		}
		if owner {
			// No caller-controlled merchant, callback, API key, method, subject
			// override or upstream endpoint is accepted by this console action.
			ctx := s.ProviderFlow.WithParentPaymentTest(r.Context(), actor, original, operator)
			p := compat.Params{"out_trade_no": original.OutTradeNo, "type": original.Kind, "name": original.Name, "money": ledger.Format(original.Amount), "channel_id": fmt.Sprint(channel), "method": "web", "return_url": original.Origin + "/admin/payapi/pay-channel?test_request=" + original.RequestID, "param": original.Marker()}
			s.providerCreate(&parentTestWireSink{}, r.WithContext(ctx), false, original.MerchantID, p)
		}
	} else if len(parts) == 5 && r.Method == "GET" || len(parts) == 6 && parts[5] == "query" && r.Method == "POST" {
		if len(parts) == 6 {
			if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
				response(w, 400, -1, "invalid original query", nil)
				return true
			}
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128))
			fields, parseErr := parentExactObject(raw)
			if err != nil || parseErr != nil || len(fields) != 0 {
				response(w, 400, -1, "invalid original query", nil)
				return true
			}
		}
		original, e = s.Store.ReadParentPaymentTest(r.Context(), actor, channel, parts[4])
		if e != nil {
			parentChannelError(w, e)
			return true
		}
		if len(parts) == 6 {
			o, err := s.Store.Order(r.Context(), original.MerchantID, original.OutTradeNo)
			if err != nil {
				parentChannelError(w, err)
				return true
			}
			if o.Name != original.Name || o.Amount != original.Amount || o.Param != original.Marker() || o.Type != original.Kind {
				parentChannelError(w, payment.ErrConflict)
				return true
			}
			if _, e = s.ProviderFlow.Query(r.Context(), original.MerchantID, o.TradeNo); e != nil {
				response(w, 409, -1, "original payment outcome remains unconfirmed; read the same request", nil)
				return true
			}
		}
	} else {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	// An expired/replaced session after upstream I/O may read the same durable
	// request later; it never receives customer materials through this response.
	if _, e = s.Store.ReadParentPaymentTest(r.Context(), actor, channel, original.RequestID); e != nil {
		parentChannelError(w, e)
		return true
	}
	out, e := s.parentTestOutput(r, original)
	if e != nil {
		parentChannelError(w, e)
		return true
	}
	response(w, 200, 0, "", out)
	return true
}
