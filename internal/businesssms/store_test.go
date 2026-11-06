package businesssms

import "testing"

func TestBusinessTemplates231ValidateIndependentScenes(t *testing.T) {
	for _, provider := range []string{"aliyun", "qcloud", "smsbao"} {
		for _, scene := range []string{"balance", "complain", "group"} {
			value := "SMS_NOTICE"
			if provider == "qcloud" {
				value = "123456"
			}
			if provider == "smsbao" {
				value = "业务提醒：{code}"
			}
			if !ValidTemplate(provider, scene, value) {
				t.Fatal(provider, scene)
			}
		}
	}
	for _, item := range [][3]string{{"unknown", "balance", "x"}, {"aliyun", "login", "x"}, {"qcloud", "group", "not-numeric"}, {"smsbao", "balance", "missing parameter"}} {
		if ValidTemplate(item[0], item[1], item[2]) {
			t.Fatal("invalid template accepted")
		}
	}
}

func TestBusinessSnapshot231NeverFormatsSecrets(t *testing.T) {
	var s Snapshot
	if s.String() != "businesssms231.Snapshot(redacted)" {
		t.Fatal("unsafe formatter")
	}
}

func TestBusinessSMS231NormalizesVerifiedMainlandPhonesOnly(t *testing.T) {
	for _, phone := range []string{"13800138000", "+8613800138000", "8613800138000"} {
		if normalizedPhone(phone) != "13800138000" {
			t.Fatal("verified mainland phone was rejected")
		}
	}
	for _, phone := range []string{"+14155552671", "not-a-phone", "13800", ""} {
		if normalizedPhone(phone) != "" {
			t.Fatal("unsupported recipient accepted")
		}
	}
}
