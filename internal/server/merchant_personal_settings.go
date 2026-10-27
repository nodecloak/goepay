package server

import (
	"errors"
	"net/http"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func personalSettingsError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrOriginalMerchantSession):
		response(w, 403, -1, "original merchant session required", nil)
	case errors.Is(e, payment.ErrConflict):
		response(w, 409, -1, "personal settings changed; refresh before saving", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 422, -1, "invalid QQ or personal amount bounds", nil)
	default:
		response(w, 503, -1, "personal settings could not be confirmed", nil)
	}
}
func (s *Server) accountPersonalSettings(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string) bool {
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
		response(w, 503, -1, "personal settings unavailable", nil)
		return true
	}
	switch r.Method {
	case "GET":
		state, e := s.Store.MerchantPersonalSettings(r.Context(), a)
		if e != nil {
			personalSettingsError(w, e)
		} else {
			response(w, 200, 0, "", state)
		}
	case "PATCH":
		in, ok := identityJSON(w, r, "qq", "pay_minmoney", "pay_maxmoney", "expected_fingerprint")
		if !ok {
			return true
		}
		if len(in) != 4 || !identityHex(in["expected_fingerprint"]) {
			response(w, 400, -1, "exact personal settings confirmation required", nil)
			return true
		}
		state, e := s.Store.UpdateMerchantPersonalSettings(r.Context(), a, in["qq"], in["pay_minmoney"], in["pay_maxmoney"], in["expected_fingerprint"])
		if e != nil {
			personalSettingsError(w, e)
		} else {
			response(w, 200, 0, "personal settings saved", state)
		}
	default:
		response(w, 405, -1, "method not allowed", nil)
	}
	return true
}
