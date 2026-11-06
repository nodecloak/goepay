package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goepay/internal/identity"
	"goepay/internal/orderalias"
	"goepay/internal/payment"
)

// Main injects the independent bootstrap master. This constructor does not
// enable a provider mode or modify the payment Store's keys or financial MACs.
func NewOrderAPITradeAliasStore120ForMaster(s *Server, master []byte) (*orderalias.Store, error) {
	if s == nil {
		return nil, orderalias.ErrUnavailable
	}
	return orderalias.NewStore(s.Pool, s.Store, master)
}

// Compatibility fallback is only for existing fixtures with no explicit store.
// Main supplies its bootstrap-backed store even when provider mode is disabled.
func (s *Server) orderAPITradeAliasStore120(store *orderalias.Store) (*orderalias.Store, error) {
	if store != nil {
		return store, nil
	}
	return NewOrderAPITradeAliasStore120ForMaster(s, s.CheckoutKey)
}

// The root dispatcher wires this helper after common persistent authentication
// and CSRF checks. The domain still checks the persisted actor on its own tx.
func (s *Server) orderAPITradeAliasConsole120(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string, store *orderalias.Store) bool {
	if len(parts) < 4 || parts[1] != "orders" || parts[3] != "api-trade-alias" {
		return false
	}
	identityHeaders(w)
	if role != "admin" {
		response(w, 404, -1, "order alias target unavailable", nil)
		return true
	}
	if len(parts) != 4 {
		response(w, 404, -1, "order alias target unavailable", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "order alias query parameters are not accepted", nil)
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPatch {
		w.Header().Set("Allow", "GET, PATCH")
		response(w, 405, -1, "order alias method unavailable", nil)
		return true
	}
	if s.Sessions == nil || !s.Sessions.SupportsDelegation() {
		response(w, 503, -1, "persistent original administrator session required", nil)
		return true
	}
	if session.Issuer != nil || session.User.Role != "admin" {
		orderAPITradeAliasError120(w, identity.ErrAdminSession)
		return true
	}
	resolved, err := s.orderAPITradeAliasStore120(store)
	if err != nil {
		orderAPITradeAliasError120(w, err)
		return true
	}
	actor := orderalias.Actor{User: session.User, Token: token, CSRF: r.Header.Get("X-CSRF-Token"), Delegated: session.Issuer != nil}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	var view orderalias.View
	if r.Method == http.MethodGet {
		raw, readErr := io.ReadAll(io.LimitReader(r.Body, 1))
		if readErr != nil || len(raw) != 0 {
			response(w, 400, -1, "order alias GET has no request body", nil)
			return true
		}
		view, err = resolved.Read(ctx, actor, parts[2])
	} else {
		input, ok := orderAPITradeAliasInput120(w, r)
		if !ok {
			return true
		}
		view, err = resolved.Update(ctx, actor, parts[2], input)
	}
	if err != nil {
		orderAPITradeAliasError120(w, err)
		return true
	}
	response(w, 200, 0, "", view)
	return true
}

func (s *Server) canEditOrderAPITradeAlias120(r *http.Request, role string, session identity.Session, order payment.Order, store *orderalias.Store) bool {
	if s == nil || role != "admin" || session.User.Role != "admin" || session.Issuer != nil || s.Sessions == nil || !s.Sessions.SupportsDelegation() {
		return false
	}
	cookie, err := r.Cookie("goepay_admin")
	if err != nil {
		return false
	}
	resolved, err := s.orderAPITradeAliasStore120(store)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	view, err := resolved.Read(ctx, orderalias.Actor{User: session.User, Token: cookie.Value, Delegated: false}, order.TradeNo)
	return err == nil && view.CanUpdate && view.TradeNo == order.TradeNo && view.MerchantID == strconv.FormatInt(order.MerchantID, 10)
}

func orderAPITradeAliasInput120(w http.ResponseWriter, r *http.Request) (orderalias.Update, bool) {
	fail := func() (orderalias.Update, bool) {
		response(w, 400, -1, "exact order alias JSON required", nil)
		return orderalias.Update{}, false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return fail()
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32769))
	if err != nil || len(raw) > 32768 || !utf8.Valid(raw) {
		return fail()
	}
	fields, err := coreRiskObject(json.NewDecoder(bytes.NewReader(raw)), []string{"api_trade_no_alias", "expected_fingerprint"})
	if err != nil {
		return fail()
	}
	var input orderalias.Update
	if json.Unmarshal(fields["api_trade_no_alias"], &input.Alias) != nil || json.Unmarshal(fields["expected_fingerprint"], &input.ExpectedFingerprint) != nil || len(input.ExpectedFingerprint) != 64 || strings.Trim(input.ExpectedFingerprint, "0123456789abcdef") != "" {
		return fail()
	}
	return input, true
}
func orderAPITradeAliasError120(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrAdminSession):
		response(w, 403, -1, "original persistent administrator session required", nil)
	case errors.Is(err, payment.ErrNotFound):
		response(w, 404, -1, "order alias target unavailable", nil)
	case errors.Is(err, orderalias.ErrConflict), errors.Is(err, payment.ErrConflict), errors.Is(err, payment.ErrState):
		response(w, 409, -1, "order or alias source changed; reload before saving", nil)
	case errors.Is(err, orderalias.ErrInvalid), errors.Is(err, payment.ErrInvalid):
		response(w, 422, -1, "invalid independent display alias", nil)
	default:
		response(w, 503, -1, "independent display alias could not be confirmed", nil)
	}
}
