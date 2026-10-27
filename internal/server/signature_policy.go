package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func signaturePolicyJSON(w http.ResponseWriter, r *http.Request) (int, string, bool) {
	fail := func() (int, string, bool) {
		response(w, 400, -1, "invalid signature policy confirmation", nil)
		return 0, "", false
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return fail()
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	v, e := d.Token()
	if e != nil || v != json.Delim('{') {
		return fail()
	}
	seen := map[string]bool{}
	var keytype int
	var expected string
	for d.More() {
		v, e = d.Token()
		if e != nil {
			return fail()
		}
		key, ok := v.(string)
		if !ok || seen[key] || (key != "keytype" && key != "expected_fingerprint") {
			return fail()
		}
		seen[key] = true
		var raw json.RawMessage
		if e = d.Decode(&raw); e != nil {
			return fail()
		}
		if key == "keytype" {
			if string(raw) != "0" && string(raw) != "1" {
				return fail()
			}
			keytype = int(raw[0] - '0')
		} else {
			if json.Unmarshal(raw, &expected) != nil || !identityHex(expected) {
				return fail()
			}
		}
	}
	if v, e = d.Token(); e != nil || v != json.Delim('}') || len(seen) != 2 {
		return fail()
	}
	if _, e = d.Token(); e != io.EOF {
		return fail()
	}
	return keytype, expected, true
}

func signaturePolicyError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrAdminSession):
		response(w, 403, -1, "original super administrator session required", nil)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "merchant unavailable", nil)
	case errors.Is(e, identity.ErrOriginalMerchantSession):
		response(w, 403, -1, "original merchant session required", nil)
	case errors.Is(e, payment.ErrConflict):
		response(w, 409, -1, "signature policy changed; refresh before confirming again", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 422, -1, "a valid merchant RSA public key is required for RSA-only mode", nil)
	default:
		response(w, 503, -1, "signature policy change could not be confirmed; refresh current state", nil)
	}
}

func (s *Server) accountSignaturePolicy(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string) bool {
	identityHeaders(w)
	if len(parts) != 3 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "invalid signature policy target", nil)
		return true
	}
	a, ok := merchantRSAActor(r, session)
	if !ok {
		response(w, 403, -1, "original merchant session required", nil)
		return true
	}
	if s.Store == nil {
		response(w, 503, -1, "signature policy unavailable", nil)
		return true
	}
	switch r.Method {
	case "GET":
		state, e := s.Store.MerchantSignaturePolicy(r.Context(), a)
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
		state, e := s.Store.UpdateMerchantSignaturePolicy(r.Context(), a, keytype, expected)
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
