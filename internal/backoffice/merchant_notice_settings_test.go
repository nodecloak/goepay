package backoffice

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestMerchantNotice80OriginalGlobalSwitchesAndTemplateMappingsAreEditableWithoutCredentials(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := Principal{Role: "admin", AdminClass: "super", SubjectID: 1, Username: "admin"}
	for key, value := range map[string]string{"msgconfig_order": "1", "msgconfig_balance": "0", "telegramnotice": "1", "qqbotnotice": "0", "voicenotice": "0", "wxnotice_tpl_balance": "local-template80", "wxnotice_tpl_balance_user": "character_string1", "user_transfer": "1", "sitename": "本地易支付"} {
		if _, err := s.Create(ctx, p, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, key, value))); err != nil {
			t.Fatal("original nonsecret platform field unavailable", key, err)
		}
	}
	for _, pair := range [][2]string{{"msgconfig_login", "true"}, {"msgconfig_group", "01"}, {"qqbotnotice", "2"}, {"wxnotice_tpl_order_name", "thing1\n"}, {"wxnotice_tpl_balance_realmoney", "amount1"}, {"wxnotice_appsecret", "secret80"}, {"merchant_voice_secret", "secret80"}} {
		if _, err := s.Create(ctx, p, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); !errors.Is(err, ErrInvalid) {
			t.Fatal("ambiguous or secret platform field accepted", pair[0], err)
		}
	}
	group, err := s.Create(ctx, p, "groups", input(`{"name":"original-transfer-group80","user_transfer":"0"}`))
	if err != nil || group.Fields["user_transfer"] != "0" {
		t.Fatal("original group transfer condition absent", group, err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM merchant_notice_events`).Scan(&count); err != nil || count != 0 {
		t.Fatal("settings emitted notification", count, err)
	}
}
