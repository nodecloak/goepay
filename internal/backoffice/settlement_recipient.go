package backoffice

import (
	"goepay/internal/payment"
	"net/http"
	"strconv"
)

func (s *Service) serveSettlementRecipient(w http.ResponseWriter, r *http.Request, p Principal, parts []string) bool {
	if len(parts) != 1 || parts[0] != "settlement-recipient" {
		return false
	}
	mid := p.SubjectID
	if r.Method == "GET" {
		q := r.URL.Query()
		for k, vs := range q {
			if k != "merchant_id" || len(vs) != 1 {
				writeError(w, ErrInvalid)
				return true
			}
		}
		if p.Role == "admin" {
			var e error
			mid, e = strconv.ParseInt(q.Get("merchant_id"), 10, 64)
			if e != nil || mid <= 0 {
				writeError(w, ErrInvalid)
				return true
			}
		} else if raw := q.Get("merchant_id"); raw != "" && raw != strconv.FormatInt(mid, 10) {
			writeError(w, ErrForbidden)
			return true
		}
		v, e := s.store.SettlementRecipient(r.Context(), mid)
		if e != nil {
			writeError(w, e)
			return true
		}
		write(w, 200, v, "local recipient configuration; no external identity verification or payout")
		return true
	}
	if r.Method != "POST" {
		write(w, 405, nil, "method not allowed")
		return true
	}
	in, ok := body(w, r)
	if !ok {
		return true
	}
	for k := range in {
		if k != "merchant_id" && k != "type" && k != "account" && k != "name" && k != "bank_code" && k != "app_id" && k != "expected_config_id" {
			writeError(w, ErrInvalid)
			return true
		}
	}
	fields := decodeFields(mustJSON(in))
	text := func(key string) (string, bool) {
		v, exists := fields[key]
		if !exists {
			return "", true
		}
		s, ok := v.(string)
		return s, ok
	}
	raw, ok := text("merchant_id")
	if !ok {
		writeError(w, ErrInvalid)
		return true
	}
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
	var req payment.SettlementRecipientInput
	for _, f := range []struct {
		key string
		out *string
	}{{"type", &req.Type}, {"account", &req.Account}, {"name", &req.Name}, {"bank_code", &req.BankCode}, {"app_id", &req.AppID}} {
		*f.out, ok = text(f.key)
		if !ok {
			writeError(w, ErrInvalid)
			return true
		}
	}
	var expected *int64
	if _, exists := fields["expected_config_id"]; exists {
		raw, valid := text("expected_config_id")
		id, e := strconv.ParseInt(raw, 10, 64)
		if !valid || e != nil || id < 0 || strconv.FormatInt(id, 10) != raw {
			writeError(w, ErrInvalid)
			return true
		}
		expected = &id
	}
	actor := payment.SettlementRecipientActor{Role: p.Role, Class: p.AdminClass, ID: p.SubjectID, Name: p.Username}
	if p.Role == "admin" && p.AdminClass != "super" && p.AdminClass != "operator" {
		writeError(w, ErrForbidden)
		return true
	}
	v, e := s.store.SetSettlementRecipient(r.Context(), mid, req, expected, actor)
	if e != nil {
		writeError(w, e)
		return true
	}
	write(w, 200, v, "recipient version recorded; existing applications unchanged")
	return true
}
