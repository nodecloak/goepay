package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func decodeBuyerFeeInput104(w http.ResponseWriter, r *http.Request, out any, keys ...string) bool {
	var raw map[string]json.RawMessage
	if !decodeFundsJSON(w, r, &raw) {
		return false
	}
	valid := len(raw) == len(keys)
	for _, k := range keys {
		v, ok := raw[k]
		if !ok || string(v) == "null" {
			valid = false
		}
	}
	if valid {
		b, e := json.Marshal(raw)
		valid = e == nil && json.Unmarshal(b, out) == nil
	}
	if !valid {
		response(w, 400, -1, "exact original fee fields required", nil)
	}
	return valid
}

func (s *Server) accountBuyerFees104(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string) bool {
	identityHeaders(w)
	if len(parts) != 3 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	a, ok := merchantRSAActor(r, session)
	if !ok {
		response(w, 403, -1, "original merchant session required", nil)
		return true
	}
	if s.Store == nil {
		response(w, 503, -1, "buyer charge settings unavailable", nil)
		return true
	}
	fail := func(e error) {
		switch {
		case errors.Is(e, identity.ErrOriginalMerchantSession):
			response(w, 403, -1, "original merchant session required", nil)
		case errors.Is(e, payment.ErrInvalid):
			response(w, 422, -1, "invalid buyer charge mode or rate", nil)
		case errors.Is(e, payment.ErrConflict):
			response(w, 409, -1, "buyer charge settings changed; inspect before confirming", nil)
		default:
			response(w, 503, -1, "buyer charge settings could not be confirmed", nil)
		}
	}
	switch r.Method {
	case "GET":
		v, e := s.Store.MerchantBuyerFeeSettings104(r.Context(), a)
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "", v)
		}
	case "PATCH":
		var in struct {
			Mode     *int    `json:"mode"`
			Rate     *string `json:"mode_feerate"`
			Expected string  `json:"expected_fingerprint"`
		}
		if !decodeBuyerFeeInput104(w, r, &in, "mode", "mode_feerate", "expected_fingerprint") {
			return true
		}
		if in.Mode == nil || in.Rate == nil || !identityHex(in.Expected) {
			response(w, 400, -1, "exact original buyer charge confirmation required", nil)
			return true
		}
		v, e := s.Store.UpdateMerchantBuyerFeeSettings104(r.Context(), a, *in.Mode, *in.Rate, in.Expected)
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "buyer charge settings saved for future orders", v)
		}
	default:
		response(w, 405, -1, "method not allowed", nil)
	}
	return true
}
