package backoffice

import (
	"errors"
	"goepay/internal/payment"
	"net/http"
	"net/url"
	"strconv"
)

func (s *Service) settlementRecipientCanWrite(p Principal) bool {
	if p.SubjectID <= 0 || p.Role != "merchant" && p.Role != "admin" || p.Role == "admin" && p.AdminClass != "super" && p.AdminClass != "operator" {
		return false
	}
	return s.SettlementRecipientWriteAllowed == nil || s.SettlementRecipientWriteAllowed(p)
}

func (s *Service) serveSettlementRecipient(w http.ResponseWriter, r *http.Request, p Principal, parts []string) bool {
	// New read: the settlement/transfer account types this platform allows, so
	// the 结算设置 and transfer forms can hide the types an administrator turned
	// off (settle_*/transfer_*). Additive endpoint — the existing GET/POST
	// shapes the contract tests pin are unchanged.
	if len(parts) == 2 && parts[0] == "settlement-recipient" && parts[1] == "allowed-types" {
		if r.Method != "GET" {
			write(w, 405, nil, "method not allowed")
			return true
		}
		if p.SubjectID <= 0 || (p.Role != "admin" && p.Role != "merchant") {
			writeError(w, ErrForbidden)
			return true
		}
		query, queryErr := url.ParseQuery(r.URL.RawQuery)
		if queryErr != nil || p.Role == "merchant" && len(query) > 0 {
			writeError(w, ErrInvalid)
			return true
		}
		var groupID *int64
		mid := int64(0)
		if len(query) > 1 {
			writeError(w, ErrInvalid)
			return true
		}
		for key, values := range query {
			if key != "group_id" && key != "merchant_id" || len(values) != 1 {
				writeError(w, ErrInvalid)
				return true
			}
			id, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil || id < 0 || key == "merchant_id" && id == 0 || strconv.FormatInt(id, 10) != values[0] {
				writeError(w, ErrInvalid)
				return true
			}
			if key == "group_id" {
				groupID = &id
			} else {
				mid = id
			}
		}
		if p.Role == "merchant" {
			mid = p.SubjectID
		}
		types, e := s.store.AllowedSettlementRecipientTypes(r.Context(), mid, groupID)
		if e != nil {
			writeError(w, e)
			return true
		}
		values, e := s.SettingValues(r.Context(), "settle_alipay", "settle_wxpay", "settle_qqpay", "settle_bank", "transfer_wxpay", "transfer_qqpay", "transfer_bank")
		if e != nil {
			writeError(w, e)
			return true
		}
		write(w, 200, map[string]any{
			"settlement": types,
			"transfer":   AllowedTransferTypes(values),
			"can_write":  s.settlementRecipientCanWrite(p),
		}, "")
		return true
	}
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
		write(w, 200, struct {
			payment.SettlementRecipientView
			CanWrite bool `json:"can_write"`
		}{v, s.settlementRecipientCanWrite(p)}, "local recipient configuration; no external identity verification or payout")
		return true
	}
	if r.Method != "POST" {
		write(w, 405, nil, "method not allowed")
		return true
	}
	if s.SettlementRecipientWriteAllowed != nil && !s.SettlementRecipientWriteAllowed(p) {
		writeError(w, ErrForbidden)
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
	// settle_* per-channel switches decide which settlement account types a
	// merchant may choose (结算设置) and a payout may use. A type whose rail is
	// turned off is refused on save; a blank/missing switch keeps the rail
	// enabled, so a default install is unchanged.
	if req.Type != "" {
		values, e := s.SettingValues(r.Context(), "settle_alipay", "settle_wxpay", "settle_qqpay", "settle_bank")
		if e != nil {
			writeError(w, e)
			return true
		}
		if !SettlementAccountTypeAllowed(values, req.Type) {
			writeError(w, PaymentRefusedError{Message: "该收款方式的结算已关闭"})
			return true
		}
	}
	v, e := s.store.SetSettlementRecipient(r.Context(), mid, req, expected, actor)
	if e != nil {
		if errors.Is(e, payment.ErrSettlementType) {
			e = PaymentRefusedError{Message: "该收款方式的结算已关闭"}
		}
		writeError(w, e)
		return true
	}
	write(w, 200, v, "recipient version recorded; existing applications unchanged")
	return true
}
