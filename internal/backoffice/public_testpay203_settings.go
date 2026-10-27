package backoffice

import "regexp"

var publicCaptchaID203 = regexp.MustCompile(`^[A-Za-z0-9_-]{0,128}$`)

func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "测试支付", Value: "test_open"},
				Option{Label: "测试支付开启验证码", Value: "captcha_open_test"},
				Option{Label: "极验滑动验证码ID", Value: "captcha_id"},
				Option{Label: "极验版本", Value: "captcha_version"},
				Option{Label: "登录开启验证码", Value: "captcha_open_login"},
			)
		}
	}
	definitions["settings"] = d
}

func validatePublicTestPaySetting203(key, value string) error {
	switch key {
	case "test_open", "captcha_open_test", "captcha_open_login", "captcha_version":
		if value != "0" && value != "1" {
			return ErrInvalid
		}
	case "captcha_id":
		if !publicCaptchaID203.MatchString(value) {
			return ErrInvalid
		}
	}
	return nil
}
