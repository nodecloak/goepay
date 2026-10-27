package server

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"

	"goepay/internal/identity"
	"goepay/internal/paymentdefend206"
)

// The caller resolves/injects the protected store. This handler does not add a
// route, discover secrets, or grant a payment execution capability.
func (s *Server) paymentDefendSettingsConsole206(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string, store *paymentdefend206.Store) bool {
	if len(parts) < 3 || parts[1] != "settings" || parts[2] != "payment-defend" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if len(parts) != 3 {
		response(w, 404, -1, "payment defend target unavailable", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "payment defend query parameters are not accepted", nil)
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPatch {
		w.Header().Set("Allow", "GET, PATCH")
		response(w, 405, -1, "payment defend method unavailable", nil)
		return true
	}
	if store == nil {
		response(w, 503, -1, "payment defend policy unavailable", nil)
		return true
	}
	actor := paymentdefend206.Actor{User: session.User, Token: token, CSRF: r.Header.Get("X-CSRF-Token"), Delegated: session.Issuer != nil}
	var view paymentdefend206.View
	var err error
	if r.Method == http.MethodGet {
		view, err = store.Read(r.Context(), actor)
	} else {
		input, ok := paymentDefendInput206(w, r)
		if !ok {
			return true
		}
		view, err = store.Update(r.Context(), actor, input)
	}
	if err == nil {
		response(w, 200, 0, "", view)
		return true
	}
	switch {
	case errors.Is(err, identity.ErrAdminSession):
		response(w, 403, -1, "original persistent super administrator session required", nil)
	case errors.Is(err, paymentdefend206.ErrConflict):
		response(w, 409, -1, "payment defend source or revision changed; reload before saving", nil)
	case errors.Is(err, paymentdefend206.ErrInvalid):
		response(w, 422, -1, "invalid payment defend policy values", nil)
	default:
		response(w, 503, -1, "payment defend policy could not be confirmed", nil)
	}
	return true
}

func paymentDefendInput206(w http.ResponseWriter, r *http.Request) (paymentdefend206.Update, bool) {
	fail := func() (paymentdefend206.Update, bool) {
		response(w, 400, -1, "exact payment defend policy JSON required", nil)
		return paymentdefend206.Update{}, false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return fail()
	}
	outer, err := coreRiskObject(json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)), []string{"settings", "expected_fingerprint"})
	if err != nil {
		return fail()
	}
	var input paymentdefend206.Update
	if json.Unmarshal(outer["expected_fingerprint"], &input.ExpectedFingerprint) != nil || len(input.ExpectedFingerprint) != 64 || strings.Trim(input.ExpectedFingerprint, "0123456789abcdef") != "" {
		return fail()
	}
	names := make([]string, 0, 7)
	for name := range paymentdefend206.Defaults() {
		names = append(names, name)
	}
	settings, err := coreRiskObject(json.NewDecoder(strings.NewReader(string(outer["settings"]))), names)
	if err != nil {
		return fail()
	}
	input.Settings = paymentdefend206.Settings{}
	for name, raw := range settings {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return fail()
		}
		input.Settings[name] = value
	}
	return input, true
}
