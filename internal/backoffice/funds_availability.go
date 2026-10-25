package backoffice

import (
	"goepay/internal/ledger"
	"net/http"
	"strconv"
)

func (s *Service) serveFundsAvailability(w http.ResponseWriter, r *http.Request, p Principal, parts []string) bool {
	if len(parts) != 1 || parts[0] != "funds-availability" {
		return false
	}
	if r.Method != http.MethodGet {
		writeError(w, ErrInvalid)
		return true
	}
	mid := p.SubjectID
	raw := r.URL.Query().Get("merchant_id")
	if p.Role == "admin" {
		var e error
		mid, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || mid <= 0 {
			writeError(w, ErrInvalid)
			return true
		}
	} else if raw != "" && raw != strconv.FormatInt(mid, 10) {
		writeError(w, ErrForbidden)
		return true
	}
	v, e := s.store.FundsAvailability(r.Context(), mid)
	if e != nil {
		writeError(w, e)
		return true
	}
	write(w, http.StatusOK, map[string]any{"merchant_id": strconv.FormatInt(mid, 10), "available_money": ledger.Format(v.Available), "ledger_available_money": ledger.Format(v.LedgerAvailable), "today_blocked_money": ledger.Format(v.Blocked), "settle_type": v.SettleType}, "")
	return true
}
