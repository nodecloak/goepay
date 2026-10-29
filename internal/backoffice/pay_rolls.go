package backoffice

import (
	"encoding/json"
	"goepay/internal/ledger"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type PayRollChannel struct{ ID, Weight int64 }
type PayRollLimit struct {
	MinAmount, MaxAmount, DailyAmount  int64
	DailyCount, RateSeconds, RateCount int64
	StartHour, EndHour                 *int64
}
type PayRoll struct {
	MerchantID     int64
	Type, Strategy string
	Enabled        bool
	DefaultRoute   bool
	Channels       []PayRollChannel
	Limits         PayRollLimit
	Rules          map[int64]PayRollLimit
}

func payRollDefinition() definition {
	limits := text("limits", "组限制 JSON（金额字符串；时段为UTC+8小时）", false, 20000)
	limits.Type = "text"
	limits.Default = "{}"
	rules := text("rules", "各通道限制 JSON（通道ID到限制对象）", false, 20000)
	rules.Type = "text"
	rules.Default = "{}"
	// 轮询方式 with the original Chinese names (value enum unchanged — validation
	// matches on Value): round_robin 顺序轮询, weighted 权重轮询, first 首个可用,
	// priority 优先级.
	strategy := selectField("strategy", "轮询方式", "round_robin", "weighted", "first", "priority")
	strategyNames := map[string]string{"round_robin": "顺序轮询", "weighted": "权重轮询", "first": "首个可用", "priority": "优先级"}
	for i := range strategy.Options {
		strategy.Options[i].Label = strategyNames[strategy.Options[i].Value]
	}
	return definition{title: "通道轮询", description: "同订单持久绑定当前有权使用的同产品通道，未知不切换。同一商户与支付方式只允许一个默认轮询组；关闭默认用途可保存多个命名组供用户组指定或随机选择。旧缺商户/类型资料不执行。限制键：min_amount/max_amount/daily_amount 为元字符串，daily_count、rate_limit_seconds/count 为整数，start_hour/end_hour 为UTC+8小时；rules按通道ID填写。使用后禁用保留记录。", noDelete: true, fields: []Field{text("name", "名称", true, 100), text("merchant_id", "商户ID（0为全局默认）", true, 19), selectField("type", "支付方式", "alipay", "wxpay", "qqpay", "bank", "jdpay", "paypal", "douyinpay", "ecny"), strategy, text("channel_ids", "通道ID:权重，逗号分隔", true, 1000), limits, rules, flag("default_route", "用于继承系统路由的默认组", true), flag("enabled", "执行路由", false)}}
}

var routeMoneyPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,2})?$`)

func routeInteger(v any, max int64) (int64, error) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, ErrInvalid
	}
	s := n.String()
	x, e := strconv.ParseInt(s, 10, 64)
	if e != nil || x < 0 || x > max || strconv.FormatInt(x, 10) != s {
		return 0, ErrInvalid
	}
	return x, nil
}
func routeMoney(v any) (int64, error) {
	s, ok := v.(string)
	if !ok || len(s) > 21 || !routeMoneyPattern.MatchString(s) {
		return 0, ErrInvalid
	}
	if strings.Trim(s, "0.") == "" {
		return 0, nil
	}
	x, e := ledger.Parse(s)
	if e != nil {
		return 0, ErrInvalid
	}
	return x, nil
}

// Decode without losing duplicate keys or converting integers to float64.
func routeJSONValue(d *json.Decoder) (any, error) {
	t, e := d.Token()
	if e != nil {
		return nil, ErrInvalid
	}
	if delim, ok := t.(json.Delim); ok {
		if delim != '{' {
			return nil, ErrInvalid
		}
		m := map[string]any{}
		for d.More() {
			k, e := d.Token()
			key, ok := k.(string)
			if e != nil || !ok {
				return nil, ErrInvalid
			}
			if _, exists := m[key]; exists {
				return nil, ErrInvalid
			}
			v, e := routeJSONValue(d)
			if e != nil {
				return nil, e
			}
			m[key] = v
		}
		end, e := d.Token()
		if e != nil || end != json.Delim('}') {
			return nil, ErrInvalid
		}
		return m, nil
	}
	return t, nil
}
func routeJSONObject(v any) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	s, ok := v.(string)
	if !ok || len(s) > 20000 {
		return nil, ErrInvalid
	}
	if s == "" {
		s = "{}"
	}
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	x, e := routeJSONValue(d)
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	m, ok := x.(map[string]any)
	if !ok {
		return nil, ErrInvalid
	}
	return m, nil
}
func parsePayRollLimit(m map[string]any) (PayRollLimit, error) {
	var l PayRollLimit
	for key, value := range m {
		var e error
		switch key {
		case "min_amount":
			l.MinAmount, e = routeMoney(value)
		case "max_amount":
			l.MaxAmount, e = routeMoney(value)
		case "daily_amount":
			l.DailyAmount, e = routeMoney(value)
		case "daily_count":
			l.DailyCount, e = routeInteger(value, 1000000)
		case "rate_limit_seconds":
			l.RateSeconds, e = routeInteger(value, 86400)
		case "rate_limit_count":
			l.RateCount, e = routeInteger(value, 1000000)
		case "start_hour":
			var n int64
			n, e = routeInteger(value, 23)
			l.StartHour = &n
		case "end_hour":
			var n int64
			n, e = routeInteger(value, 23)
			l.EndHour = &n
		default:
			return l, ErrInvalid
		}
		if e != nil {
			return l, ErrInvalid
		}
	}
	if l.MaxAmount > 0 && l.MinAmount > l.MaxAmount || (l.StartHour == nil) != (l.EndHour == nil) || (l.RateSeconds == 0) != (l.RateCount == 0) {
		return l, ErrInvalid
	}
	return l, nil
}
func (l PayRollLimit) Allows(amount int64, at time.Time) bool {
	if amount < l.MinAmount || l.MaxAmount > 0 && amount > l.MaxAmount {
		return false
	}
	if l.StartHour == nil || *l.StartHour == *l.EndHour {
		return true
	}
	h := int64(at.In(time.FixedZone("UTC+8", 8*3600)).Hour())
	start, end := *l.StartHour, *l.EndHour
	if start < end {
		return h >= start && h <= end
	}
	return h >= start || h <= end
}
func ParsePayRoll(v map[string]any) (PayRoll, error) {
	var r PayRoll
	for key := range v {
		switch key {
		case "name", "merchant_id", "type", "strategy", "channel_ids", "limits", "rules", "enabled", "default_route":
		default:
			return r, ErrInvalid
		}
	}
	m, ok := v["merchant_id"].(string)
	if !ok {
		return r, ErrInvalid
	}
	var e error
	r.MerchantID, e = strconv.ParseInt(m, 10, 64)
	if e != nil || r.MerchantID < 0 || strconv.FormatInt(r.MerchantID, 10) != m {
		return r, ErrInvalid
	}
	r.Type, _ = v["type"].(string)
	if !catalogPayType(r.Type) {
		return r, ErrInvalid
	}
	r.Enabled, ok = v["enabled"].(bool)
	if !ok {
		return r, ErrInvalid
	}
	r.DefaultRoute = true
	if raw, exists := v["default_route"]; exists {
		r.DefaultRoute, ok = raw.(bool)
		if !ok {
			return r, ErrInvalid
		}
	}
	r.Strategy, _ = v["strategy"].(string)
	switch r.Strategy {
	case "round_robin", "weighted", "first", "priority":
	default:
		return r, ErrInvalid
	}
	ids, ok := v["channel_ids"].(string)
	if !ok || len(ids) > 1000 {
		return r, ErrInvalid
	}
	seen := map[int64]bool{}
	var total int64
	for _, entry := range strings.Split(ids, ",") {
		parts := strings.Split(strings.TrimSpace(entry), ":")
		if len(parts) > 2 {
			return r, ErrInvalid
		}
		id, e := strconv.ParseInt(parts[0], 10, 64)
		if e != nil || id <= 0 || strconv.FormatInt(id, 10) != parts[0] || seen[id] {
			return r, ErrInvalid
		}
		seen[id] = true
		// Match Pro rollinfo_decode: an omitted weight is zero. It is valid
		// for non-weighted strategies; weighted groups need a positive total.
		weight := int64(0)
		if len(parts) == 2 {
			weight, e = strconv.ParseInt(parts[1], 10, 64)
			if e != nil || weight < 0 || weight > 1000000 || strconv.FormatInt(weight, 10) != parts[1] {
				return r, ErrInvalid
			}
		}
		r.Channels = append(r.Channels, PayRollChannel{ID: id, Weight: weight})
		total += weight
	}
	if len(r.Channels) > 100 || r.Strategy == "weighted" && total == 0 {
		return r, ErrInvalid
	}
	limits, e := routeJSONObject(v["limits"])
	if e != nil {
		return r, e
	}
	r.Limits, e = parsePayRollLimit(limits)
	if e != nil {
		return r, e
	}
	rules, e := routeJSONObject(v["rules"])
	if e != nil {
		return r, e
	}
	r.Rules = map[int64]PayRollLimit{}
	for key, value := range rules {
		id, e := strconv.ParseInt(key, 10, 64)
		if e != nil || id <= 0 || strconv.FormatInt(id, 10) != key || !seen[id] {
			return r, ErrInvalid
		}
		m, ok := value.(map[string]any)
		if !ok {
			return r, ErrInvalid
		}
		l, e := parsePayRollLimit(m)
		if e != nil {
			return r, e
		}
		r.Rules[id] = l
	}
	return r, nil
}
