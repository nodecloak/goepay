package backoffice

import (
	"fmt"
	"net/http"
	"strconv"
)

func (s *Service) serveProfitRule(w http.ResponseWriter, r *http.Request, p Principal, parts []string) bool {
	if len(parts) == 0 || parts[0] != "profitsharing-rules" {
		return false
	}
	if len(parts) != 3 || parts[2] != "execute" || r.Method != "POST" {
		write(w, 405, nil, "method not allowed")
		return true
	}
	id, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil {
		writeError(w, ErrInvalid)
		return true
	}
	if _, e = s.Get(r.Context(), p, "profitsharing-rules", id); e != nil {
		writeError(w, e)
		return true
	}
	in, ok := body(w, r)
	if !ok {
		return true
	}
	if len(in) != 1 {
		writeError(w, ErrInvalid)
		return true
	}
	trade, e := stringInput(in, "trade_no")
	if e != nil || trade == "" {
		writeError(w, ErrInvalid)
		return true
	}
	o, e := s.store.OrderByTradeNo(r.Context(), trade)
	if e != nil {
		writeError(w, e)
		return true
	}
	if p.Role != "admin" && o.MerchantID != p.SubjectID {
		writeError(w, ErrForbidden)
		return true
	}
	v, e := s.store.ExecuteProfitRule(r.Context(), id, trade, fmt.Sprintf("%s:%d:%s", p.Role, p.SubjectID, p.Username))
	if e != nil {
		writeError(w, e)
		return true
	}
	write(w, 201, requestJSON(v), "calculated order share reserved; administrator review required")
	return true
}
