package backoffice

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestAdminNotice77OriginalSettingsAreEditableButCredentialsNeverEnterGenericRecords(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	admin := Principal{Role: "admin", AdminClass: "super", SubjectID: 1}
	for key, value := range map[string]string{"msgconfig_risk": "2", "msgtelegram_chatid": "-12345", "qqbot_openid": "local77", "mail_recv": "admin@example.test", "mail_name": "fallback@example.test"} {
		if _, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, key, value))); e != nil {
			t.Fatal("original settings field unavailable", key, e)
		}
	}
	for _, pair := range [][2]string{{"msgconfig_risk", "01"}, {"msgconfig_risk", "4"}, {"msgtelegram_chatid", "0123"}, {"qqbot_openid", "a/b"}, {"mail_recv", "Name <a@example.test>"}, {"login_telegram_token", "secret77"}, {"qqbot_appsecret", "secret77"}, {"msgrobot_url", "https://oapi.dingtalk.com/robot/send?access_token=secret77"}} {
		if _, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); !errors.Is(e, ErrInvalid) {
			t.Fatal("unsafe or secret setting persisted", pair[0], e)
		}
	}
}
