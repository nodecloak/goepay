package merchantnotice

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

type WechatTemplateConfig struct {
	ID, ConsoleURL string
	Fields         map[string]string
}
type WechatTemplateValue struct {
	Value string `json:"value"`
}
type WechatTemplate struct {
	TemplateID string                         `json:"template_id"`
	URL        string                         `json:"url"`
	Data       map[string]WechatTemplateValue `json:"data"`
}

var templateField80 = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

func clipWechatChars(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func clipWechatBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
func validateWechatInput(in Input) error {
	switch in.Scene {
	case "order", "settle", "login", "complain", "balance":
		_, err := Render(in)
		return err
	case "wxtransfer":
		if !safeValue(in.SiteName, 128, false) || in.At.IsZero() || in.At.Year() < 1970 || in.At.Year() > 9999 || !safeValue(in.Params["time"], 128, false) || in.Daily != nil {
			return ErrInvalid
		}
		_, err := normalizedMoney(in.Params["money"], maxMoney)
		return err
	default:
		return ErrInvalid
	}
}

// RenderWechat includes the source's template-only wxtransfer scene without
// making it a generic HTML template or a business-event sending authority.
func RenderWechat(in Input) (Message, error) {
	if err := validateWechatInput(in); err != nil {
		return Message{}, err
	}
	if in.Scene != "wxtransfer" {
		return Render(in)
	}
	amount, _ := normalizedMoney(in.Params["money"], maxMoney)
	m := Message{Title: "微信转账通知 - " + in.SiteName, Text: "微信转账到账金额：¥" + amount + "\n到账时间：" + in.Params["time"]}
	m.seal = m.fingerprint()
	return m, nil
}
func BuildWechatTemplate(in Input, cfg WechatTemplateConfig) (WechatTemplate, error) {
	var empty WechatTemplate
	if validateWechatInput(in) != nil || !validTargetID(cfg.ID) || cfg.ID == "" || len(cfg.Fields) == 0 || len(cfg.Fields) > 10 {
		return empty, ErrInvalid
	}
	base, err := url.Parse(cfg.ConsoleURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Opaque != "" || len(cfg.ConsoleURL) > 1024 {
		return empty, ErrInvalid
	}
	p := in.Params
	amount, _ := normalizedMoney(p["money"], maxMoney)
	values := map[string]string{}
	path := "/merchant/dashboard"
	switch in.Scene {
	case "order":
		values = map[string]string{"no": p["trade_no"], "name": clipWechatChars(p["name"], 20), "money": "¥" + amount, "time": p["time"], "outno": clipWechatBytes(p["out_trade_no"], 32)}
		path = "/merchant/trade/order"
	case "settle":
		net, _ := normalizedMoney(p["realmoney"], maxMoney)
		values = map[string]string{"type": "结算成功", "account": p["account"], "money": "¥" + amount, "realmoney": "¥" + net, "time": p["time"]}
		path = "/merchant/settle"
	case "login":
		values = map[string]string{"user": p["user"], "time": p["time"], "name": in.SiteName, "ip": p["clientip"], "iploc": p["ipinfo"]}
	case "complain":
		values = map[string]string{"order_no": p["trade_no"], "time": p["time"], "reason": p["content"], "type": p["type"], "name": clipWechatChars(p["name"], 20)}
	case "balance":
		amount, _ = normalizedMoney(p["money"], int64(^uint64(0)>>1))
		values = map[string]string{"user": p["user"], "time": p["time"], "money": amount, "msg": "为避免造成订单失败，请及时充值"}
	case "wxtransfer":
		values = map[string]string{"money": "¥" + amount, "time": p["time"]}
	}
	prefix := "wxnotice_tpl_" + in.Scene + "_"
	out := WechatTemplate{TemplateID: cfg.ID, URL: strings.TrimRight(cfg.ConsoleURL, "/") + path, Data: map[string]WechatTemplateValue{}}
	for key, field := range cfg.Fields {
		if !strings.HasPrefix(key, prefix) || !templateField80.MatchString(field) {
			return empty, ErrInvalid
		}
		value, exists := values[strings.TrimPrefix(key, prefix)]
		if !exists {
			return empty, ErrInvalid
		}
		if !safeValue(value, 4096, strings.TrimPrefix(key, prefix) == "reason") && !(in.Scene == "login" && key == prefix+"iploc" && value == "") {
			return empty, ErrInvalid
		}
		if _, duplicate := out.Data[field]; duplicate {
			return empty, ErrInvalid
		}
		out.Data[field] = WechatTemplateValue{Value: value}
	}
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > 16000 {
		return empty, ErrInvalid
	}
	return out, nil
}
