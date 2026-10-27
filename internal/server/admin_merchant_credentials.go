package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"strconv"
)

func adminMerchantCredentialError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrAdminSession):
		response(w, 403, -1, "original super administrator session required", nil)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "merchant unavailable", nil)
	case errors.Is(e, payment.ErrConflict):
		response(w, 409, -1, "credentials changed; refresh before an explicit new operation", nil)
	default:
		response(w, 503, -1, "credential change could not be confirmed; refresh current state", nil)
	}
}

func (s *Server) adminMerchantCredentials(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 4 || parts[1] != "merchants" || parts[3] != "credentials" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	mid, ok := positiveFundsID(parts[2])
	if !ok || r.URL.RawQuery != "" || s.Store == nil {
		response(w, 400, -1, "invalid credential target", nil)
		return true
	}
	actor := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	if len(parts) == 4 && r.Method == "GET" {
		state, e := s.Store.AdminMerchantCredentialState(r.Context(), mid, actor)
		if e != nil {
			adminMerchantCredentialError(w, e)
		} else {
			response(w, 200, 0, "", state)
		}
		return true
	}
	if len(parts) != 5 || (parts[4] != "reset-md5" && parts[4] != "generate-rsa") {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	in, ok := identityJSON(w, r, "expected_fingerprint")
	if !ok {
		return true
	}
	if len(in) != 1 || !identityHex(in["expected_fingerprint"]) {
		response(w, 400, -1, "invalid credential confirmation", nil)
		return true
	}
	if !claimLoginVerification() {
		w.Header().Set("Retry-After", "1")
		response(w, 429, -1, "credential verification busy", nil)
		return true
	}
	defer releaseLoginVerification()
	if e := s.Store.CheckAdminMerchantCredential(r.Context(), mid, in["expected_fingerprint"], actor); e != nil {
		adminMerchantCredentialError(w, e)
		return true
	}
	if r.Context().Err() != nil {
		return true
	}
	value, private := "", ""
	var e error
	if parts[4] == "reset-md5" {
		raw := make([]byte, 16)
		_, e = rand.Read(raw)
		if e == nil {
			value = hex.EncodeToString(raw)
		}
	} else {
		value, private, e = generateMerchantRSA()
	}
	defer func() { value = ""; private = "" }()
	if e != nil {
		adminMerchantCredentialError(w, e)
		return true
	}
	if r.Context().Err() != nil {
		return true
	}
	state, e := s.Store.ReplaceAdminMerchantCredential(r.Context(), mid, in["expected_fingerprint"], parts[4], value, actor)
	if e != nil {
		adminMerchantCredentialError(w, e)
		return true
	}
	out := map[string]any{"merchant_id": strconv.FormatInt(state.MerchantID, 10), "revision": strconv.FormatInt(state.Revision, 10), "algorithm": state.Algorithm, "fingerprint": state.Fingerprint}
	if parts[4] == "reset-md5" {
		out["api_key"] = value
	} else {
		out["public_key"] = value
		out["private_key"] = private
	}
	response(w, 200, 0, "credential changed; save the new secret now", out)
	return true
}
