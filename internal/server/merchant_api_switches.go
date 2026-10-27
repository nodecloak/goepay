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

func merchantAPISwitchError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrOriginalMerchantSession):
		response(w, 403, -1, "original merchant session required", nil)
	case errors.Is(e, payment.ErrConflict):
		response(w, 409, -1, "API switches changed; refresh before saving", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 422, -1, "invalid API switches", nil)
	default:
		response(w, 503, -1, "API switches could not be confirmed", nil)
	}
}
func apiSwitchInput(w http.ResponseWriter, r *http.Request) ([3]int, string, bool) {
	var flags [3]int
	fail := func() ([3]int, string, bool) {
		response(w, 400, -1, "exact API switches JSON required", nil)
		return flags, "", false
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return fail()
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return fail()
	}
	seen := map[string]bool{}
	keys := map[string]int{"keylogin": 0, "refund": 1, "transfer": 2}
	expected := ""
	for d.More() {
		t, e = d.Token()
		key, ok := t.(string)
		if e != nil || !ok || seen[key] {
			return fail()
		}
		seen[key] = true
		var raw json.RawMessage
		if d.Decode(&raw) != nil {
			return fail()
		}
		if key == "expected_fingerprint" {
			if json.Unmarshal(raw, &expected) != nil || !identityHex(expected) {
				return fail()
			}
			continue
		}
		idx, ok := keys[key]
		if !ok || (string(raw) != "0" && string(raw) != "1") {
			return fail()
		}
		if string(raw) == "1" {
			flags[idx] = 1
		}
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || len(seen) != 4 {
		return fail()
	}
	if _, e = d.Token(); e != io.EOF {
		return fail()
	}
	return flags, expected, true
}
func (s *Server) accountAPISwitches(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string) bool {
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
		response(w, 503, -1, "API settings unavailable", nil)
		return true
	}
	switch r.Method {
	case "GET":
		out, e := s.Store.MerchantAPISwitches(r.Context(), a)
		if e != nil {
			merchantAPISwitchError(w, e)
		} else {
			response(w, 200, 0, "", out)
		}
	case "PATCH":
		flags, expected, ok := apiSwitchInput(w, r)
		if !ok {
			return true
		}
		out, e := s.Store.UpdateMerchantAPISwitches(r.Context(), a, flags[0], flags[1], flags[2], expected)
		if e != nil {
			merchantAPISwitchError(w, e)
		} else {
			response(w, 200, 0, "API switches saved", out)
		}
	default:
		response(w, 405, -1, "method not allowed", nil)
	}
	return true
}
