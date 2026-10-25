package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func generateMerchantRSA() (public, private string, e error) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return "", "", e
	}
	pub, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		return "", "", e
	}
	priv, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return "", "", e
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv})), nil
}
func merchantRSAActor(r *http.Request, session identity.Session) (payment.MerchantRSAActor, bool) {
	token, ok := identityCookie(r, "goepay_merchant")
	a := payment.MerchantRSAActor{User: session.User, Token: token, CSRF: session.CSRF}
	return a, ok && session.Issuer == nil && session.User.Role == "merchant" && session.User.AuthVersion > 0
}
func accountRSAError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, payment.ErrMerchantRSAPassword):
		response(w, 401, -1, "invalid current password", nil)
	case errors.Is(e, payment.ErrMerchantRSAActor):
		response(w, 403, -1, "original merchant session required", nil)
	case errors.Is(e, payment.ErrMerchantRSAConflict):
		response(w, 409, -1, "RSA key changed; refresh account info before retrying", nil)
	default:
		response(w, 503, -1, "RSA key rotation could not be confirmed; refresh account info", nil)
	}
}
func (s *Server) accountRSA(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string) bool {
	identityHeaders(w)
	if len(parts) != 4 || parts[3] != "rotate" {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	a, ok := merchantRSAActor(r, session)
	if !ok || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
		response(w, 403, -1, "original merchant session required", nil)
		return true
	}
	in, ok := identityJSON(w, r, "current_password", "expected_fingerprint")
	if !ok {
		return true
	}
	if len(in) != 2 || in["current_password"] == "" || len(in["current_password"]) > 1024 || !identityHex(in["expected_fingerprint"]) {
		response(w, 400, -1, "invalid RSA rotation request", nil)
		return true
	}
	if !claimLoginVerification() {
		w.Header().Set("Retry-After", "1")
		response(w, 429, -1, "credential verification busy", nil)
		return true
	}
	defer releaseLoginVerification()
	if e := s.Store.CheckMerchantRSARotation(r.Context(), a, in["current_password"], in["expected_fingerprint"]); e != nil {
		accountRSAError(w, e)
		return true
	}
	if r.Context().Err() != nil {
		return true
	}
	public, private, e := generateMerchantRSA()
	if e != nil {
		accountRSAError(w, e)
		return true
	}
	defer func() { private = "" }()
	if r.Context().Err() != nil {
		return true
	}
	state, e := s.Store.RotateMerchantRSA(r.Context(), a, in["current_password"], in["expected_fingerprint"], public)
	if e != nil {
		accountRSAError(w, e)
		return true
	}
	response(w, 200, 0, "RSA keypair created; save the private key now", map[string]any{"algorithm": state.Algorithm, "public_key": state.PublicKey, "private_key": private, "fingerprint": state.Fingerprint})
	return true
}
