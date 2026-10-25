package backoffice

import (
	"context"
	"errors"
	"testing"
)

func TestAdministratorDefaultsAndLastSuperProtection(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	first, err := s.Create(ctx, admin, "admin-users", input(`{"username":"superone","display_name":"Super One","password":"sufficient-super-password","admin_class":"super"}`))
	if err != nil {
		t.Fatal(err)
	}
	operator, err := s.Create(ctx, admin, "admin-users", input(`{"username":"staff","display_name":"Staff","password":"sufficient-staff-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	if operator.Fields["admin_class"] != "operator" {
		t.Fatal("new administrator did not default to restricted class")
	}
	if _, err = s.Update(ctx, admin, "admin-users", first.ID, input(`{"admin_class":"operator"}`)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("demoted finalsuper %v", err)
	}
	if _, err = s.Update(ctx, admin, "admin-users", operator.ID, input(`{"admin_class":"super"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, admin, "admin-users", first.ID, input(`{"admin_class":"operator"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, "DELETE FROM console_users WHERE username='staff' AND role='admin'"); err == nil {
		t.Fatal("database allowed final super deletion")
	}
}
