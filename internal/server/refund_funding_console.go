package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"goepay/internal/identity"
	"goepay/internal/ledger"
)

// Funding preview is an owner-scoped local read. It creates no refund intent,
// authorization or provider call; reservation recomputes inside its transaction.
func (s *Server) refundFundingConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) != 4 || parts[1] != "orders" || parts[3] != "preview-refund" {
		return false
	}
	if r.Method != http.MethodPost {
		response(w, 405, -1, "POST required", nil)
		return true
	}
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "query parameters are not accepted", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "refund funding unavailable", nil)
		return true
	}
	o, err := s.Store.OrderByTradeNo(r.Context(), parts[2])
	if err != nil || role != "admin" && o.MerchantID != session.User.ID {
		response(w, 404, -1, "order not found", nil)
		return true
	}
	var raw json.RawMessage
	if !decodeFundsJSON(w, r, &raw) {
		return true
	}
	definition := fundsTargetDefinition{guard: "refund", category: "order", keys: []string{"amount", "external_no"}}
	fields, _, err := normalizeFundsBody(raw, definition)
	no := fundsText(fields, "external_no")
	amount, amountErr := ledger.Parse(fundsText(fields, "amount"))
	if err != nil || amountErr != nil || len(fields) != 2 || amount <= 0 || no == "" || strings.TrimSpace(no) != no || strings.ContainsAny(no, "\r\n\x00") {
		response(w, 422, -1, "exact positive amount and external_no required", nil)
		return true
	}
	preview, err := s.Store.RefundFundingPreview(r.Context(), s.Pool, o.ID, no, amount)
	if err != nil {
		s.fail(w, err)
		return true
	}
	response(w, 200, 0, "local funding preview; reservation revalidates original evidence", preview)
	return true
}
