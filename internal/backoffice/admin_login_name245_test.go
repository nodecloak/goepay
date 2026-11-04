package backoffice

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"goepay/internal/identity"
)

func TestAdministratorLoginName245CRUDPreservesCanonicalIdentityAndCAS(t *testing.T) {
	s := setup(t)
	ctx := t.Context()
	root := Principal{Role: "admin", SubjectID: 1, Username: "root245", AdminClass: "super"}
	row, e := s.Create(ctx, root, "admin-users", input(`{"username":"canonical245","display_name":"Login245","password":"synthetic-login245-password","admin_class":"super"}`))
	if e != nil {
		t.Fatal(e)
	}
	var subject, version int64
	if e = s.pool.QueryRow(ctx, `SELECT subject_id,auth_version FROM console_users WHERE id=$1`, row.ID).Scan(&subject, &version); e != nil {
		t.Fatal(e)
	}
	u := identity.User{Role: "admin", Username: "canonical245", ID: subject, AuthVersion: version, AdminClass: "super"}
	sessions := identity.NewPersistentSessions(s.pool, false)
	token, _, e := sessions.Create(u)
	if e != nil {
		t.Fatal(e)
	}
	for _, body := range []string{
		`{"username":"alias245"}`,
		`{"username":"bad name","expected_username":"canonical245"}`,
		`{"expected_username":"canonical245"}`,
	} {
		if _, e = s.Update(ctx, root, "admin-users", row.ID, input(body)); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid login edit accepted", body, e)
		}
	}
	if _, e = s.Update(ctx, root, "admin-users", row.ID, input(`{"username":"alias245","expected_username":"canonical245"}`)); e != nil {
		t.Fatal(e)
	}
	var canonical, alias string
	var renamedVersion int64
	if e = s.pool.QueryRow(ctx, `SELECT username,COALESCE(login_name,username),auth_version FROM console_users WHERE id=$1`, row.ID).Scan(&canonical, &alias, &renamedVersion); e != nil || canonical != "canonical245" || alias != "alias245" || renamedVersion != version+1 {
		t.Fatal("CRUD rekeyed account or failed epoch", canonical, alias, renamedVersion, e)
	}
	if _, ok := sessions.Get(token, "admin"); ok {
		t.Fatal("CRUD rename retained old session")
	}
	if _, e = s.Update(ctx, root, "admin-users", row.ID, input(`{"username":"old-editor245","expected_username":"canonical245"}`)); !errors.Is(e, ErrConflict) {
		t.Fatal("stale editor overwrote changed login", e)
	}
	item, e := s.Get(ctx, root, "admin-users", row.ID)
	if e != nil || item.Fields["username"] != "alias245" {
		t.Fatal("detail lost effective username", item, e)
	}
	page, e := s.List(ctx, root, "admin-users", Query{Search: "alias245"})
	if e != nil || page.Total != 1 || page.Items[0].Fields["username"] != "alias245" {
		t.Fatal("effective username not searchable", page, e)
	}
	// An unchanged full editor payload leaves the authority generation alone.
	if _, e = s.Update(ctx, root, "admin-users", row.ID, input(`{"username":"alias245","expected_username":"alias245"}`)); e != nil {
		t.Fatal(e)
	}
	if e = s.pool.QueryRow(ctx, `SELECT auth_version FROM console_users WHERE id=$1`, row.ID).Scan(&version); e != nil || version != renamedVersion {
		t.Fatal("no-op username edit changed authority", version, e)
	}
	other, e := s.Create(ctx, root, "admin-users", input(`{"username":"other245","display_name":"Other","password":"synthetic-other245-password","admin_class":"super"}`))
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"canonical245", "alias245"} {
		payload := fmt.Sprintf(`{"username":%q,"expected_username":"other245"}`, name)
		if _, e = s.Update(ctx, root, "admin-users", other.ID, input(payload)); !errors.Is(e, ErrConflict) {
			t.Fatal("canonical or effective duplicate accepted", name, e)
		}
		payload = fmt.Sprintf(`{"username":%q,"display_name":"Duplicate","password":"synthetic-duplicate245-password"}`, name)
		if _, e = s.Create(ctx, root, "admin-users", input(payload)); !errors.Is(e, ErrConflict) {
			t.Fatal("create collided with alias without business conflict", name, e)
		}
	}
	// Self-protection continues to compare the canonical identity after rename.
	self := Principal{Role: "admin", SubjectID: subject, Username: "canonical245", AdminClass: "super"}
	if _, e = s.Update(ctx, self, "admin-users", row.ID, input(`{"enabled":false}`)); !errors.Is(e, ErrForbidden) {
		t.Fatal("effective projection bypassed self-disable protection", e)
	}
	if e = s.DeleteAdministrator(ctx, self, row.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("effective projection bypassed self-delete protection", e)
	}
	if _, e = s.ResetAdminSecurity(ctx, self, row.ID, AdminSecurityReset{DisableTOTP: true}); !errors.Is(e, ErrForbidden) {
		t.Fatal("effective projection bypassed own recovery protection", e)
	}
	if _, e = s.Update(ctx, root, "admin-users", row.ID, input(`{"username":"canonical245","expected_username":"alias245"}`)); e != nil {
		t.Fatal("cannot return to own canonical login", e)
	}
	var isNull bool
	if e = s.pool.QueryRow(ctx, `SELECT login_name IS NULL FROM console_users WHERE id=$1`, row.ID).Scan(&isNull); e != nil || !isNull {
		t.Fatal("canonical login did not clear alias", e)
	}
	// A merchant cannot acquire the administrator-only alias column.
	if _, e = s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,login_name) VALUES('merchant245','merchant',101,'Merchant','synthetic','merchant-alias245')`); e == nil {
		t.Fatal("database accepted merchant login alias")
	}
	var encoded []byte
	if e = s.pool.QueryRow(ctx, `SELECT jsonb_agg(changes)::text FROM backoffice_audit WHERE resource='admin-users'`).Scan(&encoded); e != nil {
		t.Fatal(e)
	}
	var changes []map[string]any
	if e = json.Unmarshal(encoded, &changes); e != nil {
		t.Fatal(e)
	}
	for _, change := range changes {
		if change["password"] != nil && change["password"] != "[changed]" {
			t.Fatal("admin login audit exposed password")
		}
	}
}
