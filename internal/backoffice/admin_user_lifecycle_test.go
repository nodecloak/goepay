package backoffice

import (
	"context"
	"errors"
	"testing"
)

func TestAdminUserDeleteGuardsAndSuperOnly(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	super := Principal{Role: "admin", SubjectID: 1, Username: "admin", AdminClass: "super"}
	operator := Principal{Role: "admin", SubjectID: 1, Username: "admin", AdminClass: "operator"}

	// Seed a self super (so the acting admin "admin" exists in console_users as
	// a super) plus two more admins to delete.
	self, err := s.Create(ctx, super, "admin-users", input(`{"username":"admin","display_name":"Root","password":"sufficient-root-password","admin_class":"super"}`))
	if err != nil {
		t.Fatal("seed self", err)
	}
	backup, err := s.Create(ctx, super, "admin-users", input(`{"username":"backupsuper","display_name":"Backup","password":"sufficient-backup-password","admin_class":"super"}`))
	if err != nil {
		t.Fatal("seed backup super", err)
	}
	op, err := s.Create(ctx, super, "admin-users", input(`{"username":"staff","display_name":"Staff","password":"sufficient-staff-password"}`))
	if err != nil {
		t.Fatal("seed operator", err)
	}

	// Operator cannot delete anyone.
	if e := s.Delete(ctx, operator, "admin-users", op.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("operator delete must be forbidden", e)
	}
	// Super cannot delete themselves.
	if e := s.Delete(ctx, super, "admin-users", self.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("self-delete must be forbidden", e)
	}
	// Super may delete an operator.
	if e := s.Delete(ctx, super, "admin-users", op.ID); e != nil {
		t.Fatal("super delete operator", e)
	}
	// Deleting again is NotFound.
	if e := s.Delete(ctx, super, "admin-users", op.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("second delete must be not found", e)
	}
	// Super may delete the backup super (another super still remains: self).
	if e := s.Delete(ctx, super, "admin-users", backup.ID); e != nil {
		t.Fatal("super delete backup super", e)
	}
	// Schema exposes Delete only to super admins.
	if sc, e := s.Schema(super, "admin-users"); e != nil || !sc.Permissions.Delete {
		t.Fatal("super schema must allow delete", e, sc.Permissions)
	}
	if sc, e := s.Schema(operator, "admin-users"); e != nil || sc.Permissions.Delete {
		t.Fatal("operator schema must not allow delete", e, sc.Permissions)
	}
}

func TestAdminSecurityResetSuperOnlyAndSelfProtected(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	super := Principal{Role: "admin", SubjectID: 1, Username: "admin", AdminClass: "super"}
	operator := Principal{Role: "admin", SubjectID: 1, Username: "admin", AdminClass: "operator"}
	selfRow, err := s.Create(ctx, super, "admin-users", input(`{"username":"admin","display_name":"Root","password":"sufficient-root-password","admin_class":"super"}`))
	if err != nil {
		t.Fatal("seed self", err)
	}
	target, err := s.Create(ctx, super, "admin-users", input(`{"username":"locked","display_name":"Locked","password":"sufficient-locked-password","admin_class":"super"}`))
	if err != nil {
		t.Fatal("seed target", err)
	}
	// Seed the target's TOTP + fund credential so the reset has something to do.
	if _, e := s.pool.Exec(ctx, `INSERT INTO console_mfa(username,role,secret,last_counter) VALUES('locked','admin','\x1234'::bytea,7)`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO admin_funds_credentials(username,password_hash) VALUES('locked','$argon2id$v=19$m=1,t=1,p=1$c2FsdHNhbHQ$aaaa')`); e != nil {
		t.Fatal(e)
	}

	// Operator cannot reset.
	if _, e := s.ResetAdminSecurity(ctx, operator, target.ID, AdminSecurityReset{DisableTOTP: true}); !errors.Is(e, ErrForbidden) {
		t.Fatal("operator reset must be forbidden", e)
	}
	// Empty reset is invalid.
	if _, e := s.ResetAdminSecurity(ctx, super, target.ID, AdminSecurityReset{}); !errors.Is(e, ErrInvalid) {
		t.Fatal("empty reset must be invalid", e)
	}
	// Super cannot reset their own account here (use self-service).
	if _, e := s.ResetAdminSecurity(ctx, super, selfRow.ID, AdminSecurityReset{DisableTOTP: true}); !errors.Is(e, ErrForbidden) {
		t.Fatal("self reset must be refused", e)
	}
	// Super clears the target's TOTP and disables the fund credential.
	changes, e := s.ResetAdminSecurity(ctx, super, target.ID, AdminSecurityReset{DisableTOTP: true, ResetFundsCred: true})
	if e != nil {
		t.Fatal("reset", e)
	}
	if changes["totp_cleared"] != true || changes["funds_credential_disabled"] != true {
		t.Fatalf("reset did not apply: %+v", changes)
	}
	var mfaRows, enabledCred int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM console_mfa WHERE username='locked'").Scan(&mfaRows); e != nil || mfaRows != 0 {
		t.Fatal("totp not cleared", mfaRows, e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM admin_funds_credentials WHERE username='locked' AND enabled").Scan(&enabledCred); e != nil || enabledCred != 0 {
		t.Fatal("fund credential not disabled", enabledCred, e)
	}
}
