package backoffice

import (
	"errors"
	"fmt"
	"testing"
)

func TestSite191OriginalSettingFieldsAndSafeClearing(t *testing.T) {
	s := setup(t)
	a := Principal{Role: "admin", AdminClass: "super", SubjectID: 1}
	for key, value := range map[string]string{"title": "网站标题191", "keywords": "支付,191", "description": "网站说明", "orgname": "公司191", "email": "site191@example.test", "kfqq": "123456", "kfqq_url": "https://qq.example.invalid/contact", "wework_url": "https://wework.example.invalid/contact", "qqqun": "https://group.example.invalid/join", "login_subtitle": "欢迎登录", "homepage": "2", "homepage_url": "https://home.example.invalid/"} {
		r, err := s.Create(t.Context(), a, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, key, value)))
		if err != nil {
			t.Fatal("original field unavailable", key, err)
		}
		if key != "homepage" {
			if _, err = s.Update(t.Context(), a, "settings", r.ID, input(`{"value":""}`)); err != nil {
				t.Fatal("optional original value cannot clear", key, err)
			}
		}
	}
	for _, pair := range [][2]string{{"homepage", "4"}, {"homepage", "02"}, {"homepage_url", "javascript:alert(191)"}, {"kfqq_url", "https://user:password@example.invalid/"}, {"email", "Name <site@example.test>"}, {"title", "a\nb"}} {
		if _, err := s.Create(t.Context(), a, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); !errors.Is(err, ErrInvalid) {
			t.Fatal("unsafe original website setting accepted", pair[0], err)
		}
	}
}
