package enrollment

import (
	"testing"

	"goepay/internal/identity"
)

func TestRecoverByPhoneRejectsPreviouslyBoundRegistrationPhone(t *testing.T) {
	s, _ := enrollmentFixture(t)
	ctx := t.Context()
	enableSMSGateway(t, s)
	phone := "13800138000"
	reg := issuePhoneCode(t, s, "reg", phone)
	v, err := s.RegisterByPhone(ctx, RegisterPhoneInput{Code: reg, Phone: phone, Username: "phone-old", Name: "Old", Password: "initial-password", Source: "192.0.2.50"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"bind_mobile":"13800138001"}'::jsonb WHERE id=$1`, v.MerchantID); err != nil {
		t.Fatal(err)
	}
	find := issuePhoneCode(t, s, "find", phone)
	if err = s.RecoverByPhone(ctx, RecoverPhoneInput{Code: find, Phone: phone, Password: "replacement-password", Source: "192.0.2.50"}); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err = s.pool.QueryRow(ctx, `SELECT password_hash FROM console_users WHERE username='phone-old' AND role='merchant'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !identity.CheckPassword(hash, "initial-password") {
		t.Fatal("previously registered number retained password-reset access after rebinding")
	}
}
