package identity

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestTOTPRFCVectorAndStrictCodes(t *testing.T) {
	secret := []byte("12345678901234567890")
	if got := totpCode(secret, 1); got != "287082" {
		t.Fatalf("RFC vector %s", got)
	}
	for _, bad := range []string{"287082 ", "28708", "+287082", "２８７０８２"} {
		if _, ok := matchTOTP(secret, bad, time.Unix(59, 0)); ok {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestTOTPBindConsumesCodeAndRejectsReplay(t *testing.T) {
	// Reuse the isolated schema fixture of the persistent session test helper.
	pool := identityTestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('mfa','admin',9,'MFA','test')"); err != nil {
		t.Fatal(err)
	}
	m, err := NewMFA(pool, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0)
	m.now = func() time.Time { return now }
	enrollment, err := m.Prepare(ctx, "mfa", "admin")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := decodeTOTPSecret(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	code := totpCode(raw, now.Unix()/30)
	if err = m.Confirm(ctx, "mfa", "admin", code); err != nil {
		t.Fatal(err)
	}
	var epoch int64
	if err = pool.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='mfa' AND role='admin'").Scan(&epoch); err != nil || epoch != 2 {
		t.Fatalf("MFA enrollment did not advance auth epoch: %d %v", epoch, err)
	}
	if _, _, err = NewPersistentSessions(pool, false).Create(User{Username: "mfa", Role: "admin", ID: 9, AuthVersion: 1}); err == nil {
		t.Fatal("password-only in-flight login survived enrollment")
	}
	if err = m.Verify(ctx, "mfa", "admin", code); err == nil {
		t.Fatal("bind code replay accepted")
	}
	now = now.Add(30 * time.Second)
	code = totpCode(raw, now.Unix()/30)
	if err = m.Verify(ctx, "mfa", "admin", code); err != nil {
		t.Fatal(err)
	}
	if err = m.Verify(ctx, "mfa", "admin", code); err == nil {
		t.Fatal("login code replay accepted")
	}
	if err = m.Disable(ctx, "mfa", "admin", code); err == nil {
		t.Fatal("disable accepted reused code")
	}
	now = now.Add(30 * time.Second)
	if err = m.Disable(ctx, "mfa", "admin", totpCode(raw, now.Unix()/30)); err != nil {
		t.Fatal(err)
	}
	enabled, err := m.Enabled(ctx, "mfa", "admin")
	if err != nil || enabled {
		t.Fatalf("enabled %v %v", enabled, err)
	}
}
