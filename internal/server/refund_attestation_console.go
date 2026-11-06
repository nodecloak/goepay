package server

import (
	"errors"
	"net/http"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

// refundAttestationConsole lets a super administrator close a refund that no
// automatic path can finish, after checking its outcome with the provider:
//
//	GET  /console/v1/admin/refunds/{id}/attestation
//	POST /console/v1/admin/refunds/{id}/attest-result {state, provider_reference, note}
//
// The POST is a mandatory funds action: it needs a fresh funds authorization.
func (s *Server) refundAttestationConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 4 || parts[1] != "refunds" || (parts[3] != "attestation" && parts[3] != "attest-result") {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.Issuer != nil || session.User.AdminClass != "super" {
		response(w, 403, -1, "own super administrator session required", nil)
		return true
	}
	id, ok := positiveFundsID(parts[2])
	if !ok {
		response(w, 404, -1, "refund not found", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if s.ProviderFlow == nil || s.Store == nil {
		response(w, 503, -1, "refund attestation unavailable", nil)
		return true
	}
	actor := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	var view payment.RefundAttestationView
	var e error
	switch {
	case parts[3] == "attestation" && r.Method == http.MethodGet:
		view, e = s.ProviderFlow.RefundAttestation(r.Context(), actor, id)
	case parts[3] == "attest-result" && r.Method == http.MethodPost:
		in, ok := identityJSON(w, r, "state", "provider_reference", "note")
		if !ok {
			return true
		}
		if len(in) != 3 {
			response(w, 400, -1, "state, provider_reference and note required", nil)
			return true
		}
		view, e = s.ProviderFlow.AttestRefund(r.Context(), actor, id, payment.RefundAttestationInput{State: in["state"], ProviderReference: in["provider_reference"], Note: in["note"]})
	default:
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	switch {
	case e == nil:
		response(w, 200, 0, "", view)
	case errors.Is(e, identity.ErrAdminSession):
		response(w, 403, -1, "own super administrator session required", nil)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "refund not found", nil)
	case errors.Is(e, payment.ErrState):
		response(w, 409, -1, "refund cannot be attested in its current state", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 422, -1, "invalid refund attestation", nil)
	default:
		s.fail(w, e)
	}
	return true
}
