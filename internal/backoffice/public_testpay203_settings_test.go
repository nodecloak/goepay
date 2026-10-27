package backoffice

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPublicTestPay203OriginalPublicSettingsPersistWithoutPrivateKey(t *testing.T) {
	s := setup(t)
	p := Principal{Role: "admin", AdminClass: "super", SubjectID: 1, Username: "admin"}
	for _, item := range [][2]string{{"test_open", "0"}, {"captcha_open_test", "1"}, {"captcha_open_login", "0"}, {"captcha_version", "1"}, {"captcha_id", "0123456789abcdef0123456789abcdef"}} {
		raw, _ := json.Marshal(map[string]string{"key": item[0], "value": item[1]})
		record, err := s.Create(t.Context(), p, "settings", input(string(raw)))
		if err != nil || record.Fields["value"] != item[1] {
			t.Fatal("original public test payment or CAPTCHA setting is unavailable", item[0], err)
		}
	}
	for _, item := range [][2]string{{"test_open", "true"}, {"test_open", "01"}, {"captcha_open_test", "2"}, {"captcha_open_login", "yes"}, {"captcha_version", "2"}, {"captcha_id", "bad\nid"}, {"captcha_id", strings.Repeat("a", 129)}, {"captcha_key", "must-use-encrypted-vault"}} {
		raw, _ := json.Marshal(map[string]string{"key": item[0], "value": item[1]})
		if _, err := s.Create(t.Context(), p, "settings", input(string(raw))); !errors.Is(err, ErrInvalid) {
			t.Fatal("ambiguous public flag, invalid identifier or plaintext CAPTCHA secret accepted", item[0], err)
		}
	}
}
