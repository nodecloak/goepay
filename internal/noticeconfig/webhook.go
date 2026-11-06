package noticeconfig

import (
	"net/url"
	"strings"
)

// Match the existing notification adapter's fixed official providers. A URL
// cannot turn this credential editor into a general outbound HTTP client.
func validWebhook(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || strings.Contains(raw, "#") || (u.Port() != "" && u.Port() != "443") || len(raw) > 4096 {
		return false
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return false
	}
	for _, values := range q {
		if len(values) != 1 || !printable(values[0], 1024) {
			return false
		}
	}
	switch strings.ToLower(u.Hostname()) {
	case "qyapi.weixin.qq.com":
		return u.Path == "/cgi-bin/webhook/send" && len(u.Query()) == 1 && printable(u.Query().Get("key"), 512)
	case "oapi.dingtalk.com":
		if u.Path != "/robot/send" || !printable(q.Get("access_token"), 1024) {
			return false
		}
		for key := range q {
			if key != "access_token" && key != "timestamp" && key != "sign" {
				return false
			}
		}
		return true
	case "open.feishu.cn", "open.larksuite.com":
		return len(u.Query()) == 0 && strings.HasPrefix(u.Path, "/open-apis/bot/v2/hook/") && botID.MatchString(strings.TrimPrefix(u.Path, "/open-apis/bot/v2/hook/"))
	}
	return false
}
