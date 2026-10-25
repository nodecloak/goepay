package backoffice

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var merchantPhonePattern = regexp.MustCompile(`^\+?[0-9]{5,20}$`)
var merchantQQPattern = regexp.MustCompile(`^[1-9][0-9]{4,19}$`)

func init() {
	d := definitions["merchants"]
	fields := make([]Field, 0, len(d.fields)+3)
	for _, field := range d.fields {
		fields = append(fields, field)
		if field.Name == "email" {
			phone, qq := text("phone", "手机号码", false, 21), text("qq", "QQ", false, 20)
			phone.Pattern, qq.Pattern = merchantPhonePattern.String(), merchantQQPattern.String()
			fields = append(fields,
				phone,
				qq,
				text("url", "网站域名", false, 2048))
		}
	}
	d.fields = fields
	definitions["merchants"] = d
}

// Only explicit edits are validated. A malformed legacy contact must not make
// an unrelated name/status change overwrite or adopt its stored private shape.
func validateMerchantContacts(values map[string]any, changed map[string]json.RawMessage) error {
	for _, key := range []string{"phone", "qq", "url"} {
		if _, ok := changed[key]; !ok {
			continue
		}
		value, ok := values[key].(string)
		if !ok || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return ErrInvalid
		}
		if value == "" {
			continue
		}
		switch key {
		case "phone":
			if !merchantPhonePattern.MatchString(value) {
				return ErrInvalid
			}
		case "qq":
			if !merchantQQPattern.MatchString(value) {
				return ErrInvalid
			}
		case "url":
			if strings.ContainsAny(value, "\\ \t\r\n") {
				return ErrInvalid
			}
			host := value
			if strings.Contains(value, "://") {
				u, err := url.Parse(value)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" || u.Opaque != "" {
					return ErrInvalid
				}
				host = u.Hostname()
				if port := u.Port(); port != "" {
					n, e := strconv.Atoi(port)
					if e != nil || n < 1 || n > 65535 {
						return ErrInvalid
					}
				}
			}
			if len(host) > 253 || !domainPattern.MatchString(strings.ToLower(host)) {
				return ErrInvalid
			}
		}
	}
	return nil
}
