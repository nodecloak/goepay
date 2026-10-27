package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"goepay/internal/identity"
	"goepay/internal/migration"
	"goepay/internal/payment"
)

func (s *Server) combinedOrdersConsole204(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 4 || parts[1] != "orders" || parts[3] != "suborders" {
		return false
	}
	identityHeaders(w)
	if len(parts) != 4 {
		response(w, 404, -1, "historical suborders are read only", nil)
		return true
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		response(w, 405, -1, "historical suborders are read only", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query authority is not accepted", nil)
		return true
	}
	if role != "admin" && role != "merchant" || session.User.Role != role || session.User.ID <= 0 {
		response(w, 403, -1, "authorized parent order read required", nil)
		return true
	}
	if s.CombinedOrderHistory204 == nil {
		response(w, 503, -1, "historical suborders reader unavailable", nil)
		return true
	}
	mid := session.User.ID
	if role == "admin" {
		mid = 0
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	out, err := s.CombinedOrderHistory204.Read(ctx, mid, parts[2])
	if err != nil {
		status := 503
		if errors.Is(err, migration.ErrCombinedOrderNotFound204) {
			status = 404
		} else if errors.Is(err, migration.ErrCombinedHistorySource204) || errors.Is(err, migration.ErrConflict) {
			status = 409
		} else if errors.Is(err, migration.ErrValidation) {
			status = 400
		}
		response(w, status, -1, "historical suborder source could not be confirmed", nil)
	} else {
		response(w, 200, 0, "historical assertions only; no verified payment source", out)
	}
	return true
}

func (s *Server) combinedOrderDetailCapability204(ctx context.Context, role string, session identity.Session, order payment.Order) (bool, error) {
	if s.CombinedOrderHistory204 == nil {
		return false, nil
	}
	mid := session.User.ID
	if role == "admin" {
		mid = 0
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	capabilities, err := s.CombinedOrderHistory204.Capabilities(ctx, mid, []int64{order.ID})
	return capabilities[order.ID], err
}
