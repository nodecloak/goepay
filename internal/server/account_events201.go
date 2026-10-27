package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"goepay/internal/identity"
	"goepay/internal/refundevent201"
)

func refundEventAccountError201(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrOriginalMerchantSession):
		response(w, 403, -1, "original merchant session required", nil)
	case errors.Is(err, refundevent201.ErrConflict):
		response(w, 409, -1, "event subscription changed; refresh before saving", nil)
	case errors.Is(err, refundevent201.ErrInvalid):
		response(w, 422, -1, "invalid event URL or event types", nil)
	default:
		response(w, 503, -1, "event subscription could not be confirmed", nil)
	}
}

func refundEventJSON201(w http.ResponseWriter, r *http.Request) (string, []string, string, bool) {
	fail := func() (string, []string, string, bool) {
		response(w, 400, -1, "exact event subscription JSON required", nil)
		return "", nil, "", false
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return fail()
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return fail()
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return fail()
		}
		key, ok := token.(string)
		if !ok {
			return fail()
		}
		if key != "event_url" && key != "event_types" && key != "expected_fingerprint" {
			return fail()
		}
		if _, exists := fields[key]; exists {
			return fail()
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil || string(raw) == "null" {
			return fail()
		}
		fields[key] = raw
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return fail()
	}
	if _, err = d.Token(); err != io.EOF || len(fields) != 3 {
		return fail()
	}
	var endpoint, expected string
	var types []string
	if json.Unmarshal(fields["event_url"], &endpoint) != nil || json.Unmarshal(fields["event_types"], &types) != nil || types == nil || json.Unmarshal(fields["expected_fingerprint"], &expected) != nil || len(expected) != 64 || strings.Trim(expected, "0123456789abcdef") != "" {
		return fail()
	}
	return endpoint, types, expected, true
}

func (s *Server) accountEvents201(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string) bool {
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
		response(w, 503, -1, "event subscription unavailable", nil)
		return true
	}
	switch r.Method {
	case "GET":
		out, err := s.Store.MerchantRefundEventSubscription201(r.Context(), a)
		if err != nil {
			refundEventAccountError201(w, err)
		} else {
			response(w, 200, 0, "", out)
		}
	case "PATCH":
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
			response(w, 403, -1, "CSRF token required", nil)
			return true
		}
		endpoint, types, expected, ok := refundEventJSON201(w, r)
		if !ok {
			return true
		}
		out, err := s.Store.UpdateMerchantRefundEventSubscription201(r.Context(), a, endpoint, types, expected)
		if err != nil {
			refundEventAccountError201(w, err)
		} else {
			response(w, 200, 0, "event subscription saved", out)
		}
	default:
		response(w, 405, -1, "method not allowed", nil)
	}
	return true
}
