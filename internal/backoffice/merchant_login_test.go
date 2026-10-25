package backoffice

import (
	"context"
	"goepay/internal/identity"
	"testing"
)

func TestMerchantConsoleCredentialLifecycleIsAtomicAndRedacted(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	created, err := s.Create(ctx, admin, "merchants", input(`{"id":"801","name":"New merchant","api_key":"new-merchant-api-key-test","signature_algorithm":"MD5","login_username":"merchant801","login_password":"merchant-login-password-801"}`))
	if err != nil {
		t.Fatal(err)
	}
	if created.Fields["login_password"] != nil {
		t.Fatal("returned plaintext password")
	}
	var hash string
	var version int64
	if err = s.pool.QueryRow(ctx, "SELECT password_hash,auth_version FROM console_users WHERE username='merchant801' AND role='merchant' AND subject_id=801").Scan(&hash, &version); err != nil || !identity.CheckPassword(hash, "merchant-login-password-801") {
		t.Fatalf("new merchant cannot log in %v", err)
	}
	if _, err = s.Update(ctx, admin, "merchants", 801, input(`{"login_password":"replacement-merchant-password"}`)); err != nil {
		t.Fatal(err)
	}
	var next int64
	if err = s.pool.QueryRow(ctx, "SELECT password_hash,auth_version FROM console_users WHERE username='merchant801' AND role='merchant'").Scan(&hash, &next); err != nil || next <= version || !identity.CheckPassword(hash, "replacement-merchant-password") {
		t.Fatal("credential reset did not revoke old sessions")
	}
	version = next
	if _, err = s.Update(ctx, admin, "merchants", 801, input(`{"enabled":false}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.pool.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='merchant801' AND role='merchant'").Scan(&next); err != nil || next <= version {
		t.Fatal("merchant disable did not revoke sessions")
	}
	var leaked bool
	if err = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE changes::text LIKE '%merchant-login-password-801%' OR changes::text LIKE '%replacement-merchant-password%')").Scan(&leaked); err != nil || leaked {
		t.Fatal("password leaked into audit")
	}
	if _, err = s.Create(ctx, admin, "merchants", input(`{"id":"802","name":"Duplicate login","api_key":"duplicate-merchant-api-key","signature_algorithm":"MD5","login_username":"merchant801","login_password":"sufficient-duplicate-password"}`)); err == nil {
		t.Fatal("duplicate login accepted")
	}
	var exists bool
	s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM merchants WHERE id=802)").Scan(&exists)
	if exists {
		t.Fatal("failed credentials left orphan merchant")
	}
}
