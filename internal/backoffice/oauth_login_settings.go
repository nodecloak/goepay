package backoffice

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// 快捷登录 (original admin/set.php mod=oauth) non-secret settings. The provider
// enable switches, public appids and the relay API url are ordinary plaintext
// settings; the appkey / bot token are write-only secrets kept in
// connect_login_credentials (see internal/server/oauth_credentials.go), never
// in these settings records.
func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "QQ快捷登录开关 login_qq（0关闭/1开启）", Value: "login_qq"},
				Option{Label: "QQ互联 APPID login_qq_appid", Value: "login_qq_appid"},
				Option{Label: "微信快捷登录开关 login_wx（0关闭/1开启）", Value: "login_wx"},
				Option{Label: "支付宝快捷登录开关 login_alipay（0关闭/1开启）", Value: "login_alipay"},
				Option{Label: "第三方登录中转 APPID login_appid", Value: "login_appid"},
				Option{Label: "第三方登录中转API地址 login_apiurl", Value: "login_apiurl"},
				Option{Label: "Telegram快捷登录开关 login_tg（0关闭/1开启）", Value: "login_tg"},
			)
		}
	}
	d.description += " 快捷登录的开关与APPID/中转地址在此保存；各平台的APPKEY与Telegram Bot Token为写入即加密的密钥，仅在下方单独维护，保存后不回显。"
	definitions["settings"] = d
}

// validateOAuthLoginSetting validates the non-secret 快捷登录 settings. The
// appkey / bot token are never accepted here (they go through the write-only
// secret store), so those keys are not recognised by this validator.
func validateOAuthLoginSetting(key, value string) (bool, error) {
	switch key {
	case "login_qq", "login_wx", "login_alipay", "login_tg":
		if value != "0" && value != "1" {
			return true, ErrInvalid
		}
	case "login_qq_appid", "login_appid":
		if value == "" || len(value) > 128 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n\t ") {
			return true, ErrInvalid
		}
	case "login_apiurl":
		u, e := url.Parse(value)
		if e != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.Fragment != "" || len(value) > 2048 {
			return true, ErrInvalid
		}
	default:
		return false, nil
	}
	return true, nil
}
