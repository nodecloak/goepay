package merchantnotice

import (
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type WebhookTarget struct{ Original, Canonical, Kind string }

var webhookPath = regexp.MustCompile(`^/open-apis/bot/v2/hook/[A-Za-z0-9_-]{1,128}$`)

// ParseWebhookTarget is shared by encrypted settings and the actual sender.
// It is pure and never resolves DNS or opens a connection.
func ParseWebhookTarget(endpoint string) (WebhookTarget, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" || u.RawPath != "" || strings.Contains(endpoint, "#") || u.Port() != "" && u.Port() != "443" || len(endpoint) > 4096 {
		return WebhookTarget{}, ErrInvalid
	}
	host := strings.ToLower(u.Hostname())
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return WebhookTarget{}, ErrInvalid
	}
	for _, values := range q {
		if len(values) != 1 || values[0] == "" || len(values[0]) > 1024 {
			return WebhookTarget{}, ErrInvalid
		}
		for _, c := range values[0] {
			if c < 33 || c > 126 {
				return WebhookTarget{}, ErrInvalid
			}
		}
	}
	kind := ""
	switch host {
	case "oapi.dingtalk.com":
		if u.Path != "/robot/send" || q.Get("access_token") == "" {
			return WebhookTarget{}, ErrInvalid
		}
		for key := range q {
			if key != "access_token" && key != "timestamp" && key != "sign" {
				return WebhookTarget{}, ErrInvalid
			}
		}
		kind = "ding"
	case "qyapi.weixin.qq.com":
		if u.Path != "/cgi-bin/webhook/send" || len(q) != 1 || q.Get("key") == "" {
			return WebhookTarget{}, ErrInvalid
		}
		kind = "work"
	case "open.feishu.cn", "open.larksuite.com":
		if len(q) != 0 || !webhookPath.MatchString(u.Path) {
			return WebhookTarget{}, ErrInvalid
		}
		kind = "feishu"
		if host == "open.larksuite.com" {
			kind = "lark"
		}
	default:
		return WebhookTarget{}, ErrInvalid
	}
	u.Host = host
	u.RawQuery = q.Encode()
	return WebhookTarget{Original: endpoint, Canonical: u.String(), Kind: kind}, nil
}

var opaqueTarget = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validTargetID(value string) bool { return value == "" || opaqueTarget.MatchString(value) }
func validTargetSecret(value string) bool {
	if len(value) > 1024 {
		return false
	}
	for _, c := range value {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func validNoticeEmail(value string) bool {
	if value == "" {
		return true
	}
	if !safeValue(value, 254, false) {
		return false
	}
	parsed, e := mail.ParseAddress(value)
	return e == nil && parsed.Address == value
}
func validNumericChat(value string) bool {
	if value == "" {
		return true
	}
	id, e := strconv.ParseInt(value, 10, 64)
	return e == nil && id != 0 && strconv.FormatInt(id, 10) == value
}
