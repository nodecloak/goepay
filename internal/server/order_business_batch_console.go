package server

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func (s *Server) orderBusinessBatchConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 3 || parts[1] != "orders" || parts[2] != "business-batches" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.Issuer != nil || (session.User.AdminClass != "super" && session.User.AdminClass != "operator") {
		response(w, 403, -1, "original administrator session required", nil)
		return true
	}
	lookup := len(parts) == 3 && r.Method == "GET" && r.URL.RawQuery != "" && !r.URL.ForceQuery
	if (r.URL.RawQuery != "" || r.URL.ForceQuery) && !lookup {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if s.Store == nil {
		response(w, 503, -1, "order batch service unavailable", nil)
		return true
	}
	a := payment.OrderBusinessBatchActor{User: session.User, Token: token, Delegated: session.Issuer != nil}
	fail := func(err error) {
		switch {
		case errors.Is(err, identity.ErrAdminSession):
			response(w, 403, -1, "original administrator session required", nil)
		case errors.Is(err, payment.ErrInvalid):
			response(w, 422, -1, "exact batch selection required", nil)
		case errors.Is(err, payment.ErrNotFound):
			response(w, 404, -1, "order batch not found", nil)
		case errors.Is(err, payment.ErrState), errors.Is(err, payment.ErrConflict):
			response(w, 409, -1, "batch source changed; inspect original state", nil)
		case isFundsAuthorizationError(err):
			fundsHTTPError(w, err)
		default:
			response(w, 503, -1, "order batch could not be confirmed", nil)
		}
	}
	if lookup {
		q, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil || len(q) != 1 || len(q["external_no"]) != 1 || q.Get("external_no") == "" {
			response(w, 400, -1, "one original external_no required", nil)
			return true
		}
		out, e := s.Store.LookupOrderBusinessBatch(r.Context(), a, q.Get("external_no"))
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "original batch request state", out)
		}
		return true
	}
	if len(parts) == 3 && r.Method == "POST" {
		var in payment.OrderBusinessBatchInput
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		out, err := s.Store.PrepareOrderBusinessBatch(r.Context(), a, in)
		if err != nil {
			fail(err)
		} else {
			status := http.StatusCreated
			if out.Replayed {
				status = http.StatusOK
			}
			response(w, status, 0, "original selection prepared; no business sent", out)
		}
		return true
	}
	if len(parts) == 4 && r.Method == "GET" {
		id, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != parts[3] {
			response(w, 404, -1, "order batch not found", nil)
			return true
		}
		out, err := s.Store.OrderBusinessBatch(r.Context(), a, id)
		if err != nil {
			fail(err)
		} else {
			response(w, 200, 0, "", out)
		}
		return true
	}
	if len(parts) == 5 && parts[4] == "cancel" && r.Method == "POST" {
		id, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != parts[3] {
			response(w, 404, -1, "order batch not found", nil)
			return true
		}
		var in struct {
			Expected string `json:"expected_fingerprint"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		out, err := s.Store.CancelOrderBusinessBatch(r.Context(), a, id, in.Expected)
		if err != nil {
			fail(err)
		} else {
			response(w, 200, 0, "only unsent batch items cancelled", out)
		}
		return true
	}
	if len(parts) == 5 && parts[4] == "execute" && r.Method == "POST" {
		id, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != parts[3] {
			response(w, 404, -1, "order batch not found", nil)
			return true
		}
		var in struct {
			Limit int `json:"limit"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		if in.Limit < 1 || in.Limit > 100 {
			response(w, 422, -1, "one to one hundred batch items required", nil)
			return true
		}
		batch, err := s.Store.OrderBusinessBatch(r.Context(), a, id)
		if err != nil {
			fail(err)
			return true
		}
		if batch.Action == "notify" {
			out, err := s.Store.ExecuteOrderBusinessBatchNotifications(r.Context(), a, id, in.Limit)
			if err != nil {
				fail(err)
			} else {
				response(w, 200, 0, "original notifications queued; delivery remains separate", out)
			}
			return true
		}
		if s.ProviderFlow == nil {
			response(w, 503, -1, "original payment query unavailable", nil)
			return true
		}
		out, err := s.ProviderFlow.ExecuteOrderBusinessBatchQuery(r.Context(), a, id, in.Limit)
		if err != nil {
			fail(err)
		} else {
			response(w, 200, 0, "original batch execution state", out)
		}
		return true
	}
	response(w, 405, -1, "batch method not available", nil)
	return true
}
