package kyc

import "testing"

func TestKYC233IdentityValidation(t *testing.T) {
	good := Submission{Consent: true, Name: "测试人员", Number: "11010519491231002X"}
	if !validSubmission(good, 4, false) {
		t.Fatal("synthetic checksum fixture rejected")
	}
	for _, change := range []func(*Submission){func(s *Submission) { s.Consent = false }, func(s *Submission) { s.Number = "110105194912310021" }, func(s *Submission) { s.Name = "" }, func(s *Submission) { s.CardType = 6 }, func(s *Submission) { s.CertType = 1 }} {
		bad := good
		change(&bad)
		if validSubmission(bad, 4, false) {
			t.Fatal("invalid identity accepted")
		}
	}
}

func TestOCR233UploadValidation(t *testing.T) {
	if _, e := validateImage([]byte("not image")); e == nil {
		t.Fatal("arbitrary bytes accepted")
	}
	if !validDocument("idcard") || validDocument("../../etc/passwd") || validDocument("https://localhost") {
		t.Fatal("document enum")
	}
}
