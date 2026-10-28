package server

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
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
	if len(parts) == 5 && parts[3] == "groups" {
		s.paymentDefendGroupConsole206(w, r, session, parts[4], token, store)
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

// GET/PATCH /settings/payment-defend/groups/{id}: original gedit.php
// config[pay_verify]: empty inherits, "0" off, "1" by rules, "3" forced.
func (s *Server) paymentDefendGroupConsole206(w http.ResponseWriter, r *http.Request, session identity.Session, rawID, token string, store *paymentdefend206.Store) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 0 || strconv.FormatInt(id, 10) != rawID {
		response(w, 400, -1, "invalid payment defend group", nil)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "payment defend query parameters are not accepted", nil)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPatch {
		w.Header().Set("Allow", "GET, PATCH")
		response(w, 405, -1, "payment defend method unavailable", nil)
		return
	}
	if store == nil {
		response(w, 503, -1, "payment defend policy unavailable", nil)
		return
	}
	actor := paymentdefend206.Actor{User: session.User, Token: token, CSRF: r.Header.Get("X-CSRF-Token"), Delegated: session.Issuer != nil}
	var view paymentdefend206.GroupView
	if r.Method == http.MethodGet {
		view, err = store.GroupRead(r.Context(), actor, id)
	} else {
		media, _, merr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		fields, ferr := coreRiskObject(json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)), []string{"pay_verify", "expected_fingerprint"})
		var in paymentdefend206.GroupUpdate
		if merr != nil || media != "application/json" || ferr != nil || json.Unmarshal(fields["pay_verify"], &in.PayVerify) != nil || json.Unmarshal(fields["expected_fingerprint"], &in.ExpectedFingerprint) != nil {
			response(w, 400, -1, "exact payment defend group JSON required", nil)
			return
		}
		view, err = store.GroupUpdate(r.Context(), actor, id, in)
	}
	switch {
	case err == nil:
		response(w, 200, 0, "", view)
	case errors.Is(err, identity.ErrAdminSession):
		response(w, 403, -1, "original persistent super administrator session required", nil)
	case errors.Is(err, paymentdefend206.ErrNotFound):
		response(w, 404, -1, "group unavailable", nil)
	case errors.Is(err, paymentdefend206.ErrConflict):
		response(w, 409, -1, "payment defend group or policy changed; reload before saving", nil)
	case errors.Is(err, paymentdefend206.ErrInvalid):
		response(w, 422, -1, "invalid payment defend group value", nil)
	default:
		response(w, 503, -1, "payment defend group could not be confirmed", nil)
	}
}
