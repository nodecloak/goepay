package server

import (
	"net/http"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func (s *Server) adminMerchantSignaturePolicy(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 4 || parts[1] != "merchants" || parts[3] != "signature-policy" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if len(parts) != 4 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	mid, ok := positiveFundsID(parts[2])
	if !ok || r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "invalid signature policy target", nil)
		return true
	}
	if s.Store == nil {
		response(w, 503, -1, "signature policy unavailable", nil)
		return true
	}
	a := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	switch r.Method {
	case "GET":
		state, e := s.Store.AdminMerchantSignaturePolicy(r.Context(), mid, a)
		if e != nil {
			signaturePolicyError(w, e)
		} else {
			response(w, 200, 0, "", state)
		}
	case "PATCH":
		keytype, expected, valid := signaturePolicyJSON(w, r)
		if !valid {
			return true
		}
		state, e := s.Store.UpdateAdminMerchantSignaturePolicy(r.Context(), mid, a, keytype, expected)
		if e != nil {
			signaturePolicyError(w, e)
		} else {
			response(w, 200, 0, "signature policy updated", state)
		}
	default:
		response(w, 405, -1, "method not allowed", nil)
	}
	return true
}
