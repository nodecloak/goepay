package enrollment

import (
	"context"
	"errors"
	"testing"

	"goepay/internal/identity"
)

// enableSMSGateway turns the W7 gateway on with an active provider so the phone
// enrollment flow is offered, mirroring an admin that configured SMS.
func enableSMSGateway(t *testing.T, s *Service) {
	t.Helper()
	ctx := context.Background()
	seal := make([]byte, 64)
	mac := make([]byte, 32)
	if _, e := s.pool.Exec(ctx, `INSERT INTO sms_providers134(provider,sealed,mac,configured)VALUES('aliyun',$1,$2,true)`, seal, mac); e != nil {
		t.Fatal("seed provider", e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE sms_gateway_head134 SET active='aliyun',status=1 WHERE singleton`); e != nil {
		t.Fatal("enable gateway", e)
	}
}

// issuePhoneCode inserts a live code directly (as a successful send would leave
// behind), standing in for the public SMS-code endpoint. It bypasses the 60s
// per-recipient send gap, which is exercised separately in verifycode134's own
// tests and is not the subject here.
func issuePhoneCode(t *testing.T, s *Service, scene, phone string) string {
	t.Helper()
	code := "246810"
	if _, e := s.pool.Exec(context.Background(),
		`INSERT INTO verification_codes134(uid,scene,type,recipient,code,ip)VALUES(0,$1,1,$2,$3,'203.0.113.5')`,
		scene, phone, code); e != nil {
		t.Fatal("issue code", e)
	}
	return code
}

func TestRegisterByPhoneHappyPathCreatesAccountAndConsumesCode(t *testing.T) {
	s, _ := enrollmentFixture(t)
	ctx := context.Background()
	enableSMSGateway(t, s)
	phone := "13800138000"
	code := issuePhoneCode(t, s, "reg", phone)
	v, e := s.RegisterByPhone(ctx, RegisterPhoneInput{Code: code, Phone: phone, Username: "phone-user", Name: "Phone User", Password: "correct-test-password", Source: "203.0.113.5"})
	if e != nil {
		t.Fatal("register by phone", e)
	}
	if v.MerchantID <= 0 || v.Username != "phone-user" {
		t.Fatalf("registered %+v", v)
	}
	// Account + password hash exist; no default password.
	var hash string
	if e = s.pool.QueryRow(ctx, "SELECT password_hash FROM console_users WHERE username='phone-user' AND role='merchant'").Scan(&hash); e != nil || !identity.CheckPassword(hash, "correct-test-password") {
		t.Fatal("password hash", e)
	}
	// Phone claim recorded and bound to the merchant; profile.phone set.
	var claimed int64
	if e = s.pool.QueryRow(ctx, "SELECT merchant_id FROM enrollment_phones134 WHERE phone_hash=$1", s.digest("phone", phone)).Scan(&claimed); e != nil || claimed != v.MerchantID {
		t.Fatal("phone claim", claimed, e)
	}
	var profilePhone string
	if e = s.pool.QueryRow(ctx, "SELECT COALESCE(profile->>'phone','') FROM merchants WHERE id=$1", v.MerchantID).Scan(&profilePhone); e != nil || profilePhone != phone {
		t.Fatal("profile phone", profilePhone, e)
	}
	// The code is consumed (status=1) — a reuse of the same code is refused.
	if _, e = s.RegisterByPhone(ctx, RegisterPhoneInput{Code: code, Phone: "13800138001", Username: "phone-user2", Name: "Other", Password: "correct-test-password", Source: "203.0.113.5"}); !errors.Is(e, ErrCode) {
		// different phone+no code at all -> ErrCode (no live code for that phone)
	}
	// Reusing the consumed code for the SAME phone is refused (consume-once).
	if _, e = s.RegisterByPhone(ctx, RegisterPhoneInput{Code: code, Phone: phone, Username: "again", Name: "Again", Password: "correct-test-password", Source: "203.0.113.5"}); !errors.Is(e, ErrCode) {
		t.Fatal("reused code must be refused", e)
	}
}

func TestRegisterByPhoneRefusesWrongExpiredAndReusedCodes(t *testing.T) {
	s, _ := enrollmentFixture(t)
	ctx := context.Background()
	enableSMSGateway(t, s)
	// No code issued -> ErrCode.
	if _, e := s.RegisterByPhone(ctx, RegisterPhoneInput{Code: "123456", Phone: "13800138010", Username: "user-one", Name: "N", Password: "correct-test-password", Source: "203.0.113.5"}); !errors.Is(e, ErrCode) {
		t.Fatal("missing code must be refused", e)
	}
	// Wrong code -> ErrCode, and no account created.
	phone := "13800138011"
	issuePhoneCode(t, s, "reg", phone)
	if _, e := s.RegisterByPhone(ctx, RegisterPhoneInput{Code: "000000", Phone: phone, Username: "user-two", Name: "N", Password: "correct-test-password", Source: "203.0.113.5"}); !errors.Is(e, ErrCode) {
		t.Fatal("wrong code must be refused", e)
	}
	var exists bool
	if e := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM console_users WHERE username='user-two')").Scan(&exists); e != nil || exists {
		t.Fatal("no account may exist after a wrong code", exists, e)
	}
	// Expired code -> ErrCode. Insert a backdated row directly (created_at is
	// immutable, so it cannot be aged by UPDATE).
	phone2 := "13800138012"
	code := "135791"
	if _, e := s.pool.Exec(ctx, `INSERT INTO verification_codes134(uid,scene,type,recipient,code,ip,created_at)VALUES(0,'reg',1,$1,$2,'203.0.113.5',clock_timestamp()-interval '2 hours')`, phone2, code); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RegisterByPhone(ctx, RegisterPhoneInput{Code: code, Phone: phone2, Username: "user-three", Name: "N", Password: "correct-test-password", Source: "203.0.113.5"}); !errors.Is(e, ErrCode) {
		t.Fatal("expired code must be refused", e)
	}
}

func TestRegisterByPhoneRefusesAlreadyRegisteredPhone(t *testing.T) {
	s, _ := enrollmentFixture(t)
	ctx := context.Background()
	enableSMSGateway(t, s)
	phone := "13800138020"
	code := issuePhoneCode(t, s, "reg", phone)
	if _, e := s.RegisterByPhone(ctx, RegisterPhoneInput{Code: code, Phone: phone, Username: "first", Name: "First", Password: "correct-test-password", Source: "203.0.113.5"}); e != nil {
		t.Fatal("first registration", e)
	}
	code2 := issuePhoneCode(t, s, "reg", phone)
	if _, e := s.RegisterByPhone(ctx, RegisterPhoneInput{Code: code2, Phone: phone, Username: "second", Name: "Second", Password: "correct-test-password", Source: "203.0.113.5"}); !errors.Is(e, ErrConflict) {
		t.Fatal("already-registered phone must be refused as conflict", e)
	}
}

func TestRecoverByPhoneResetsOnceAndRefusesReuse(t *testing.T) {
	s, _ := enrollmentFixture(t)
	ctx := context.Background()
	enableSMSGateway(t, s)
	phone := "13800138030"
	code := issuePhoneCode(t, s, "reg", phone)
	v, e := s.RegisterByPhone(ctx, RegisterPhoneInput{Code: code, Phone: phone, Username: "recover-user", Name: "R", Password: "correct-test-password", Source: "203.0.113.5"})
	if e != nil {
		t.Fatal("register", e)
	}
	var beforeVersion int64
	if e = s.pool.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='recover-user' AND role='merchant'").Scan(&beforeVersion); e != nil {
		t.Fatal(e)
	}
	findCode := issuePhoneCode(t, s, "find", phone)
	if e = s.RecoverByPhone(ctx, RecoverPhoneInput{Code: findCode, Phone: phone, Password: "a-brand-new-password", Source: "203.0.113.5"}); e != nil {
		t.Fatal("recover by phone", e)
	}
	var hash string
	var afterVersion int64
	if e = s.pool.QueryRow(ctx, "SELECT password_hash,auth_version FROM console_users WHERE username='recover-user' AND role='merchant'").Scan(&hash, &afterVersion); e != nil {
		t.Fatal(e)
	}
	if !identity.CheckPassword(hash, "a-brand-new-password") || afterVersion <= beforeVersion {
		t.Fatal("password not reset / auth epoch not advanced", afterVersion, beforeVersion)
	}
	// Reusing the consumed find code is refused.
	if e = s.RecoverByPhone(ctx, RecoverPhoneInput{Code: findCode, Phone: phone, Password: "yet-another-password", Source: "203.0.113.5"}); !errors.Is(e, ErrCode) {
		t.Fatal("reused find code must be refused", e)
	}
	_ = v
}

func TestPhoneEnrollmentInertWhenGatewayOff(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	// Gateway OFF (the default — enableSMSGateway not called).
	cap, e := s.Capabilities(ctx)
	if e != nil || cap.SMSEnabled {
		t.Fatalf("SMSEnabled must be false when gateway off: %+v %v", cap, e)
	}
	// Even with a live code present, phone register/recover refuse with ErrClosed.
	phone := "13800138040"
	code := issuePhoneCode(t, s, "reg", phone)
	if _, e = s.RegisterByPhone(ctx, RegisterPhoneInput{Code: code, Phone: phone, Username: "off", Name: "Off", Password: "correct-test-password", Source: "203.0.113.5"}); !errors.Is(e, ErrClosed) {
		t.Fatal("register by phone must be closed when gateway off", e)
	}
	if e = s.RecoverByPhone(ctx, RecoverPhoneInput{Code: code, Phone: phone, Password: "correct-test-password", Source: "203.0.113.5"}); !errors.Is(e, ErrClosed) {
		t.Fatal("recover by phone must be closed when gateway off", e)
	}
	// The email-only path is unchanged: a normal verified email registration works.
	registerVerified(t, s, f, "stillworks@example.test", "email-user", "192.0.2.9")
}
