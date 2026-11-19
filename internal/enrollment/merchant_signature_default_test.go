package enrollment

import "testing"

func TestEmailAndPhoneEnrollmentUseDualSignatureDefaultWithoutRSACredentials(t *testing.T) {
	s, sender := enrollmentFixture(t)
	email := registerVerified(t, s, sender, "dual-signature@example.test", "dual-email", "192.0.2.101")
	enableSMSGateway(t, s)
	phone := "13800138991"
	code := issuePhoneCode(t, s, "reg", phone)
	registered, err := s.RegisterByPhone(t.Context(), RegisterPhoneInput{Code: code, Phone: phone, Username: "dual-phone", Name: "Phone signature", Password: "synthetic-signature-password", Source: "192.0.2.102"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{email.MerchantID, registered.MerchantID} {
		m, err := s.store.Merchant(t.Context(), id)
		if err != nil || m.Keytype == nil || *m.Keytype != 0 || m.SignatureAlgorithm != "MD5" || m.RSAPublicKey != "" {
			t.Fatal("registration did not preserve independent signing mode and credentials", id, err)
		}
		balance, err := s.store.Balance(t.Context(), id)
		if err != nil || balance.Available != 0 || balance.Reserved != 0 {
			t.Fatal("registration changed financial balance", balance, err)
		}
	}
}
