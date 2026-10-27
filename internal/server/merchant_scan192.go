package server

import (
	"errors"
	"net/http"
	"sort"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func (s *Server) merchantScanPlatform192(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "settings" || parts[2] != "scanpay" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if s.Store == nil {
		response(w, 503, -1, "scan payment settings unavailable", nil)
		return true
	}
	a := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	fail := func(err error) {
		if errors.Is(err, identity.ErrAdminSession) {
			response(w, 403, -1, "original super administrator session required", nil)
		} else if r.Method == "PATCH" && errors.Is(err, payment.ErrConflict) {
			response(w, 409, -1, "scan payment setting changed or cannot be confirmed; refresh before confirming", nil)
		} else if errors.Is(err, payment.ErrInvalid) {
			response(w, 422, -1, "invalid scan payment setting", nil)
		} else {
			response(w, 503, -1, "scan payment settings could not be confirmed", nil)
		}
	}
	switch r.Method {
	case "GET":
		out, err := s.Store.MerchantScanPlatformSettings106(r.Context(), a)
		if err != nil {
			fail(err)
		} else {
			response(w, 200, 0, "", out)
		}
	case "PATCH":
		var in struct {
			Open     string `json:"scanpay_open"`
			Expected string `json:"expected_fingerprint"`
		}
		if !decodeBuyerFeeInput104(w, r, &in, "scanpay_open", "expected_fingerprint") {
			return true
		}
		if !identityHex(in.Expected) {
			response(w, 400, -1, "exact scan payment setting confirmation required", nil)
			return true
		}
		out, err := s.Store.UpdateMerchantScanPlatformSettings106(r.Context(), a, in.Open, in.Expected)
		if err != nil {
			fail(err)
		} else {
			response(w, 200, 0, "", out)
		}
	default:
		w.Header().Set("Allow", "GET, PATCH")
		response(w, 405, -1, "method not allowed", nil)
	}
	return true
}

func (s *Server) merchantScanConsole192(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "scanpay" {
		return false
	}
	identityHeaders(w)
	if role != "merchant" || session.Issuer != nil {
		response(w, 403, -1, "original merchant session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if len(parts) != 3 || parts[2] != "capabilities" {
		s.merchantScanIntent192(w, r, session, parts, token)
		return true
	}
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if s.Store == nil {
		response(w, 503, -1, "merchant cashier unavailable", nil)
		return true
	}
	a := payment.MerchantRSAActor{User: session.User, Token: token, CSRF: session.CSRF}
	open, enabled, err := s.Store.MerchantScanAvailability106(r.Context(), a)
	if err != nil {
		if errors.Is(err, identity.ErrOriginalMerchantSession) {
			response(w, 403, -1, "original merchant session required", nil)
		} else {
			response(w, 503, -1, "merchant cashier authority could not be confirmed", nil)
		}
		return true
	}
	types := []string{}
	if open && enabled && s.paymentExecutionWriteAllowed() {
		view, err := s.withParentRuntime(r.Context(), session.User.ID, "")
		if err != nil {
			response(w, 503, -1, "payment channel source could not be confirmed", nil)
			return true
		}
		seen := map[string]bool{}
		for _, id := range view.MerchantChannels[session.User.ID] {
			p := view.ProviderFlow.Providers[id]
			if p == nil || !p.Capabilities().Query || !hasProviderMethod(p.Capabilities().Methods, "scan") {
				continue
			}
			if _, ok := p.(adapter.ScanProvider); !ok {
				continue
			}
			if _, ok := p.(adapter.ScanValidator); !ok {
				continue
			}
			kind := p.ID()
			if kind != "alipay" && kind != "wxpay" {
				continue
			}
			if !seen[kind] {
				types = append(types, kind)
				seen[kind] = true
			}
		}
		sort.Strings(types)
	}
	// Recheck persistent authority after catalog reads. A later operator change
	// will be checked again by prepare/apply; this response grants no write.
	open, enabled, err = s.Store.MerchantScanAvailability106(r.Context(), a)
	if err != nil {
		if errors.Is(err, identity.ErrOriginalMerchantSession) {
			response(w, 403, -1, "original merchant session required", nil)
		} else {
			response(w, 503, -1, "merchant cashier authority could not be confirmed", nil)
		}
		return true
	}
	if !open || !enabled || !s.paymentExecutionWriteAllowed() {
		types = []string{}
	}
	response(w, 200, 0, "", struct {
		PlatformOpen  bool     `json:"platform_open"`
		CanPrepare    bool     `json:"can_prepare"`
		PayTypes      []string `json:"pay_types"`
		MaxInputMoney string   `json:"max_input_money"`
	}{open, len(types) > 0, types, "999999.99"})
	return true
}
