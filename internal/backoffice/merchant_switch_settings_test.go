package backoffice

import (
	"strings"
	"testing"
)

func TestMerchantSwitchSettingValidation(t *testing.T) {
	for _, k := range []string{"user_totp_open", "subchannel_open", "user_profitsharing", "user_transfer_batch", "pay_domain_open", "verifytype"} {
		for _, v := range []string{"0", "1"} {
			if known, err := validateMerchantSwitchSetting(k, v); !known || err != nil {
				t.Error("rejected", k, v, err)
			}
		}
		for _, v := range []string{"", "2", "true", "x", "01"} {
			if known, err := validateMerchantSwitchSetting(k, v); !known || err == nil {
				t.Error("accepted", k, v)
			}
		}
	}
	for _, v := range []string{"https://pay.example.com", "https://pay.example.com\nhttps://pay2.example.com/", "\n https://pay.example.com \n\n"} {
		if known, err := validateMerchantSwitchSetting("onecode_domains", v); !known || err != nil {
			t.Error("onecode_domains rejected", v, err)
		}
	}
	for _, v := range []string{"pay.example.com", "http://pay.example.com", "https://pay.example.com/collect", "https://u:p@pay.example.com", "https://pay.example.com?x=1", strings.Repeat("https://a.example.com\n", 100)} {
		if known, err := validateMerchantSwitchSetting("onecode_domains", v); !known || err == nil {
			t.Error("onecode_domains accepted", v)
		}
	}
	for _, k := range []string{"pay_domain_forbid", "transfer_wxpay"} {
		if known, _ := validateMerchantSwitchSetting(k, "1"); known {
			t.Error("claimed a key it does not own:", k)
		}
	}
}

func TestOnecodeDomainOrigins(t *testing.T) {
	got, err := OnecodeDomainOrigins("https://Pay.Example.com/\n\nhttps://pay.example.com\nhttp://127.0.0.1:8187\nhttps://pay2.example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://pay.example.com", "http://127.0.0.1:8187", "https://pay2.example.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("origins %v want %v", got, want)
	}
	var lines []string
	for i := 0; i < onecodeDomainLimit+1; i++ {
		lines = append(lines, "https://p"+string(rune('a'+i))+".example.com")
	}
	if _, err = OnecodeDomainOrigins(strings.Join(lines, "\n")); err == nil {
		t.Fatal("more than the limit accepted")
	}
	if got, err = OnecodeDomainOrigins(""); err != nil || len(got) != 0 {
		t.Fatalf("empty: %v %v", got, err)
	}
}

// The wave-2 switches ride the default-ON MerchantFeatureVisible gate, so an
// unset deployment keeps each feature available.
func TestMerchantSwitchDefaultOnKeysAreVisibleKeys(t *testing.T) {
	visible := map[string]bool{}
	for _, k := range MerchantFeatureVisibleKeys {
		visible[k] = true
	}
	for _, k := range MerchantSwitchDefaultOnKeys {
		if !visible[k] {
			t.Error("default-on switch not registered as a visible feature key:", k)
		}
	}
}
