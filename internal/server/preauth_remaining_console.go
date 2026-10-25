package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func remainingReleaseInput(id, amount, revision string) (payment.PreauthRemainingReleaseInput, error) {
	var in payment.PreauthRemainingReleaseInput
	n, e := strconv.ParseInt(amount, 10, 64)
	if e != nil || n <= 0 || fmt.Sprint(n) != amount || !identityHex(id) {
		return in, payment.ErrInvalid
	}
	r, e := strconv.ParseInt(revision, 10, 64)
	if e != nil || r < 0 || r >= 100 || fmt.Sprint(r) != revision {
		return in, payment.ErrInvalid
	}
	return payment.PreauthRemainingReleaseInput{ExpectedObservationID: id, ExpectedRemainingCents: n, ExpectedSourceRevision: r}, nil
}
func (s *Server) preauthorizationRemainingConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 4 || parts[1] != "orders" || parts[3] != "release-remaining-authorization" {
		return false
	}
	identityHeaders(w)
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	cookie, originalCookie := identityCookie(r, "goepay_"+role)
	if !originalCookie || subtle.ConstantTimeCompare([]byte(cookie), []byte(token)) != 1 {
		response(w, 403, -1, "original console session required", nil)
		return true
	}
	if session.Issuer != nil || role == "admin" && session.User.AdminClass != "super" {
		response(w, 403, -1, "original owner or super administrator required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters are not accepted", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil || s.ProviderFlow == nil {
		response(w, 503, -1, "authorization release unavailable", nil)
		return true
	}
	o, e := s.Store.OrderByTradeNo(r.Context(), parts[2])
	if e != nil || role != "admin" && o.MerchantID != session.User.ID {
		response(w, 404, -1, "order not found", nil)
		return true
	}
	fields, ok := identityJSON(w, r, "expected_observation_id", "expected_remaining_cents", "expected_source_revision")
	if !ok {
		return true
	}
	in, e := remainingReleaseInput(fields["expected_observation_id"], fields["expected_remaining_cents"], fields["expected_source_revision"])
	if e != nil || len(fields) != 3 {
		response(w, 400, -1, "exact observation, remaining cents and source revision are required", nil)
		return true
	}
	check := func(ctx context.Context) error { return s.preauthorizationObservationPermission(ctx, o) }
	if e = check(r.Context()); e != nil {
		observationHTTPError(w, e)
		return true
	}
	ctx := r.Context()
	if role == "merchant" {
		// Merchant confirmation is explicit and bound to the same non-delegated
		// console session as the observation. Administrative grants are separate.
		ctx = payment.WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
			if kind != "authorization-release-remaining" || key != fmt.Sprintf("%s:%d:%s", o.TradeNo, in.ExpectedSourceRevision, in.ExpectedObservationID) {
				return payment.ErrInvalid
			}
			return check(c)
		})
	}
	release, e := s.ProviderFlow.ReleaseRemainingAuthorization(ctx, o.MerchantID, o.TradeNo, in, token, role, check)
	if isFundsAuthorizationError(e) {
		fundsHTTPError(w, e)
		return true
	}
	if e == nil {
		status := 200
		if release.State != "succeeded" && release.State != "failed" {
			status = 202
		}
		response(w, status, 0, "remaining authorization release result", release)
		return true
	}
	var unknown *adapter.UnknownOutcome
	if errors.As(e, &unknown) && release.OrderID > 0 {
		response(w, 202, 0, "remaining release pending; preserve and query the original request", release)
		return true
	}
	if errors.Is(e, payment.ErrLiveDisabled) || errors.Is(e, adapter.ErrUnsupported) {
		response(w, 503, -1, "remaining release not enabled", nil)
		return true
	}
	if errors.Is(e, payment.ErrState) || errors.Is(e, payment.ErrConflict) || errors.Is(e, payment.ErrPreauthObservation) {
		response(w, 409, -1, "confirmation changed or expired; inspect the original request", release)
		return true
	}
	s.fail(w, e)
	return true
}
