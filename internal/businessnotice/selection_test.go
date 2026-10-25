package businessnotice

import (
	"goepay/internal/merchantnotice"
	"testing"
)

func TestAdminBusiness83OriginalUID0SwitchNamespace(t *testing.T) {
	for _, scene := range []string{"regaudit", "apply", "domain", "internal_transfer"} {
		for value, want := range map[string]merchantnotice.Transport{"0": "", "1": merchantnotice.SMTP, "2": merchantnotice.Robot, "4": merchantnotice.Telegram, "5": merchantnotice.QQBot} {
			got, err := ModeFor(scene, value)
			if err != nil || got != want {
				t.Errorf("%s/%s: got %q want %q err %v", scene, value, got, want, err)
			}
		}
		for _, bad := range []string{"", "00", "01", "3", "6", "true", " 1", "1\n"} {
			if _, err := ModeFor(scene, bad); err == nil {
				t.Errorf("invalid mode %q accepted", bad)
			}
		}
	}
	for _, scene := range []string{"order", "risk", "complain", "wxtransfer", ""} {
		if _, err := ModeFor(scene, "1"); err == nil {
			t.Errorf("unrelated scene %q adopted", scene)
		}
	}
	key, err := SettingKey("internal_transfer")
	if err != nil || key != "msgconfig_domain" {
		t.Fatal("original shared domain switch lost", key, err)
	}
}
