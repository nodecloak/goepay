package server

import (
	"errors"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
)

func (s *Server) buyerMinimumConsole104(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "buyer-fees" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if s.Store == nil {
		response(w, 503, -1, "minimum fees unavailable", nil)
		return true
	}
	var b *payment.RuntimeChannel
	if len(parts) == 3 && parts[2] == "global" {
	} else if len(parts) == 4 && parts[2] == "parents" {
		id, ok := positiveFundsID(parts[3])
		if !ok {
			response(w, 400, -1, "invalid original parent channel", nil)
			return true
		}
		if s.ProviderFlow == nil {
			response(w, 503, -1, "parent registry unavailable", nil)
			return true
		}
		value, e := s.ProviderFlow.AdminParentRuntimeChannel(r.Context(), id)
		if e != nil {
			response(w, 404, -1, "registered parent unavailable", nil)
			return true
		}
		b = &value
	} else {
		response(w, 404, -1, "not found", nil)
		return true
	}
	actor := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	fail := func(e error) {
		switch {
		case errors.Is(e, identity.ErrAdminSession):
			response(w, 403, -1, "original super administrator session required", nil)
		case errors.Is(e, payment.ErrConflict):
			response(w, 409, -1, "minimum fees changed; inspect before confirming", nil)
		case errors.Is(e, payment.ErrInvalid):
			response(w, 422, -1, "invalid minimum fee amount", nil)
		default:
			response(w, 503, -1, "minimum fees could not be confirmed", nil)
		}
	}
	switch r.Method {
	case "GET":
		v, e := s.Store.BuyerMinimumSettings104(r.Context(), b, actor)
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "", v)
		}
	case "PATCH":
		var in struct {
			Threshold string `json:"payfee_lessthan"`
			Cost      string `json:"payfee_mincost"`
			Expected  string `json:"expected_fingerprint"`
		}
		if !decodeBuyerFeeInput104(w, r, &in, "payfee_lessthan", "payfee_mincost", "expected_fingerprint") {
			return true
		}
		if !identityHex(in.Expected) {
			response(w, 400, -1, "exact minimum confirmation required", nil)
			return true
		}
		v, e := s.Store.UpdateBuyerMinimumSettings104(r.Context(), b, actor, in.Threshold, in.Cost, in.Expected)
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "minimum fees saved for future orders", v)
		}
	default:
		response(w, 405, -1, "method not allowed", nil)
	}
	return true
}
