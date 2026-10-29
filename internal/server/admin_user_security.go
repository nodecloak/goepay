package server

import (
	"net/http"
	"strconv"

	"goepay/internal/backoffice"
	"goepay/internal/identity"
)

// adminUserSecurityConsole: POST /console/v1/admin/admin-users/{id}/security-reset
// A super administrator clears another admin's TOTP and/or disables their
// payment-password credential (lost-authenticator / lost-pay-password recovery).
// Setting another admin's payment password is NOT offered: the value stays
// self-service, so a super admin only forces a re-enrollment.
func (s *Server) adminUserSecurityConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) != 4 || parts[1] != "admin-users" || parts[3] != "security-reset" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.Issuer != nil || session.User.AdminClass != "super" {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	id, e := strconv.ParseInt(parts[2], 10, 64)
	if e != nil || id <= 0 {
		response(w, 400, -1, "invalid admin id", nil)
		return true
	}
	var in struct {
		DisableTOTP    bool `json:"disable_totp"`
		ResetFundsCred bool `json:"reset_funds_credential"`
	}
	if !decode(w, r, &in) {
		return true
	}
	p := backoffice.Principal{Role: role, SubjectID: session.User.ID, Username: session.User.Username, AdminClass: session.User.AdminClass}
	changes, err := backoffice.New(s.Pool, s.Store).ResetAdminSecurity(r.Context(), p, id, backoffice.AdminSecurityReset{DisableTOTP: in.DisableTOTP, ResetFundsCred: in.ResetFundsCred})
	if err != nil {
		status := 500
		switch err {
		case backoffice.ErrForbidden:
			status = 403
		case backoffice.ErrNotFound:
			status = 404
		case backoffice.ErrInvalid:
			status = 422
		}
		response(w, status, -1, "admin security reset could not be completed", nil)
		return true
	}
	response(w, 200, 0, "", changes)
	return true
}
