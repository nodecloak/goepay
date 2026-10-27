package backoffice

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParentChannel180OriginalTestMerchantSettingKeepsInt64ID(t *testing.T) {
	for _, value := range []string{"0", "9007199254740993", " 1"} {
		t.Run(value, func(t *testing.T) {
			s := setup(t)
			p := Principal{Role: "admin", AdminClass: "super", SubjectID: 1, Username: "admin"}
			r, e := s.Create(t.Context(), p, "settings", input(fmt.Sprintf(`{"key":"test_pay_uid","value":%q}`, value)))
			if e != nil || r.Fields["value"] != strings.TrimSpace(value) {
				t.Fatal("original fixed test merchant unavailable or rounded", value, e)
			}
		})
	}
}
func TestParentChannel180TestMerchantSettingRejectsNoncanonicalID(t *testing.T) {
	s := setup(t)
	p := Principal{Role: "admin", AdminClass: "super", SubjectID: 1, Username: "admin"}
	for _, value := range []string{"01", "-1", "1e2", "9223372036854775808"} {
		if _, e := s.Create(t.Context(), p, "settings", input(fmt.Sprintf(`{"key":"test_pay_uid","value":%q}`, value))); !errors.Is(e, ErrInvalid) {
			t.Fatal("noncanonical test merchant ID accepted", value, e)
		}
	}
}
