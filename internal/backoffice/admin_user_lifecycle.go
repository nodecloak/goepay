package backoffice

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Admin-user lifecycle operations beyond create/update (B14 #1-3):
//   - deleteAdministrator: a super administrator removes another admin, with
//     self-protection (never yourself, never the last enabled admin). The
//     last-super-admin invariant is additionally enforced by the DB trigger
//     guard_last_super_admin.
//   - disableAdminTotp: a super administrator clears another admin's TOTP
//     (lost-authenticator recovery). Enabling TOTP stays self-service because
//     the secret is generated and confirmed by its owner; only clearing is a
//     safe cross-admin action.
//   - resetAdminFundsCredential: a super administrator disables another admin's
//     payment-password credential, forcing that admin to set a fresh one. The
//     super admin never chooses the value — setting a specific payment password
//     for another admin would break the self-service fund-authorization model.
//
// All three require an original (non-delegated) super-admin principal, which
// the console handler establishes before calling the service.

func (s *Service) requireSuperDeleteTarget(ctx context.Context, tx pgx.Tx, p Principal, id int64) (string, error) {
	if p.Role != "admin" || p.AdminClass != "super" {
		return "", ErrForbidden
	}
	var username string
	var enabled bool
	if e := tx.QueryRow(ctx, "SELECT username,enabled FROM console_users WHERE id=$1 AND role='admin' FOR UPDATE", id).Scan(&username, &enabled); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", e
	}
	if username == p.Username {
		// Never delete yourself.
		return "", ErrForbidden
	}
	return username, nil
}

func (s *Service) deleteAdministrator(ctx context.Context, p Principal, id int64) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(74973232)"); e != nil {
		return e
	}
	username, e := s.requireSuperDeleteTarget(ctx, tx, p, id)
	if e != nil {
		return e
	}
	// Never remove the last enabled administrator (the DB trigger separately
	// guards the last ENABLED SUPER admin; this guards the last enabled admin of
	// any class so the console can never be left with no one who can sign in).
	var others int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM console_users WHERE role='admin' AND enabled AND id<>$1", id).Scan(&others); e != nil {
		return e
	}
	if others < 1 {
		return ErrForbidden
	}
	tag, e := tx.Exec(ctx, "DELETE FROM console_users WHERE id=$1 AND role='admin'", id)
	if e != nil {
		// The guard_last_super_admin trigger raises for the last super admin.
		return businessError(e)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if e = audit(ctx, tx, p, "admin-users", id, "delete", map[string]any{"username": username}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// AdminSecurityReset is the super-admin recovery action on another admin.
type AdminSecurityReset struct {
	DisableTOTP    bool
	ResetFundsCred bool
}

// adminSecurityReset clears another admin's TOTP and/or disables their payment
// password credential. Super-admin only; never the acting admin themselves
// (self-service flows exist for that). Returns what was actually changed.
func (s *Service) adminSecurityReset(ctx context.Context, p Principal, id int64, in AdminSecurityReset) (map[string]any, error) {
	if p.Role != "admin" || p.AdminClass != "super" {
		return nil, ErrForbidden
	}
	if !in.DisableTOTP && !in.ResetFundsCred {
		return nil, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(74973232)"); e != nil {
		return nil, e
	}
	var username string
	if e = tx.QueryRow(ctx, "SELECT username FROM console_users WHERE id=$1 AND role='admin' FOR UPDATE", id).Scan(&username); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, e
	}
	if username == p.Username {
		// Use the self-service security flows for your own account.
		return nil, ErrForbidden
	}
	changes := map[string]any{}
	if in.DisableTOTP {
		tag, e := tx.Exec(ctx, "DELETE FROM console_mfa WHERE username=$1 AND role='admin'", username)
		if e != nil {
			return nil, e
		}
		changes["totp_cleared"] = tag.RowsAffected() == 1
	}
	if in.ResetFundsCred {
		// Disable (not delete) the fund credential: the target must set a fresh
		// one through the self-service flow. Disabling bumps credential_epoch via
		// the DB trigger, invalidating outstanding challenges/grants.
		tag, e := tx.Exec(ctx, "UPDATE admin_funds_credentials SET enabled=false WHERE username=$1 AND enabled", username)
		if e != nil {
			return nil, e
		}
		changes["funds_credential_disabled"] = tag.RowsAffected() == 1
	}
	if e = audit(ctx, tx, p, "admin-users", id, "security-reset", changes); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	changes["username"] = username
	return changes, nil
}

// DeleteAdministrator / AdminSecurityReset are the exported entry points the
// console handler calls after establishing an original super-admin principal.
func (s *Service) DeleteAdministrator(ctx context.Context, p Principal, id int64) error {
	return s.deleteAdministrator(ctx, p, id)
}
func (s *Service) ResetAdminSecurity(ctx context.Context, p Principal, id int64, in AdminSecurityReset) (map[string]any, error) {
	return s.adminSecurityReset(ctx, p, id, in)
}
