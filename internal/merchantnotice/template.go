// Package merchantnotice implements the original Pro message contract without
// network I/O. Delivery authority belongs to separately verified source events.
package merchantnotice

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid merchant notice input")

const maxMoney int64 = 999999999999

type Input struct {
	Scene, SiteName string
	At              time.Time
	Params          map[string]string
	Daily           *DailyReport
}

type Message struct {
	Title, HTML, Text string
	seal              [32]byte
}

func (m Message) fingerprint() [32]byte {
	raw, _ := json.Marshal([]string{m.Title, m.HTML, m.Text})
	return sha256.Sum256(raw)
}

// Valid proves only that the fixed renderer produced these exact fields.
// It is not a recipient binding or permission to send a message.
func (m Message) Valid() bool  { return m.seal == m.fingerprint() }
func (Message) String() string { return "merchantnotice.Message(rendered)" }

type AmountLine struct {
	Name   string
	Amount int64
}
type DailyReport struct {
	Date                     string
	Money, MerchantAmount    int64
	OrderCount, SuccessCount int64
	TypeMoney                []AmountLine
	HasTransfer              bool
	TransferTotal            int64
	TransferTypes            []AmountLine
}

type noticeSpec struct {
	title, body, required, money string
	suffix                       bool
}

// These are fixed source templates, never supplied by a profile or API client.
var noticeSpecs = map[string]noticeSpec{
	"complain_all":      {title: "全部交易投诉通知", required: "id type trade_no title", body: `尊敬的管理员，平台收到新的支付交易投诉，请前往管理后台核对。<br/><b>投诉ID：</b>{{.P.id}}<br/><b>支付方式：</b>{{.P.type}}<br/><b>平台订单号：</b>{{.P.trade_no}}<br/><b>投诉原因：</b>{{.P.title}}`},
	"payment_disabled":  {title: "支付权限关闭记录", suffix: true, required: "uid count", body: `商户 {{.P.uid}} 于 {{.Now}} 因连续 {{.P.count}} 个订单回调通知失败被关闭新支付权限。请检查回调站点及其防护设置，并前往后台核对当前状态。原已受理订单仍按原回执处理。`},
	"regaudit":          {title: "新注册商户待审核提醒", required: "uid account", body: `尊敬的{{.Site}}管理员，网站有新注册的商户待审核，请及时前往用户列表审核处理。<br/><b>商户ID：</b>{{.P.uid}}<br/><b>注册账号：</b>{{.P.account}}<br/><b>注册时间：</b>{{.Now}}`},
	"apply":             {title: "新的提现待处理提醒", required: "uid type realmoney", money: "realmoney", body: `尊敬的{{.Site}}管理员，商户{{.P.uid}}发起了手动提现申请，请及时处理。<br/><b>商户ID：</b>{{.P.uid}}<br/><b>提现方式：</b>{{.P.type}}<br/><b>提现金额：</b>{{.P.realmoney}}<br/><b>提交时间：</b>{{.Now}}`},
	"domain":            {title: "新的授权支付域名待审核提醒", required: "uid domain", body: `尊敬的{{.Site}}管理员，商户{{.P.uid}}提交了新的授权支付域名，请及时审核处理。<br/><b>商户ID：</b>{{.P.uid}}<br/><b>授权域名：</b>{{.P.domain}}<br/><b>提交时间：</b>{{.Now}}`},
	"internal_transfer": {title: "新的站内转账待审核提醒", required: "uid touid money", money: "money", body: `尊敬的{{.Site}}管理员，商户{{.P.uid}}发起了站内转账，请及时审核处理。<br/><b>发起方商户ID：</b>{{.P.uid}}<br/><b>收款方商户ID：</b>{{.P.touid}}<br/><b>转账金额：</b>¥{{.P.money}}<br/><b>提交时间：</b>{{.Now}}`},
	"order":             {title: "新订单通知", suffix: true, required: "name money type out_trade_no trade_no time", money: "money", body: `尊敬的商户，您有一条新订单通知。<br/><b>商品名称：</b>{{.P.name}}<br/><b>订单金额：</b>¥{{.P.money}}<br/><b>支付方式：</b>{{.P.type}}<br/><b>商户订单号：</b>{{.P.out_trade_no}}<br/><b>系统订单号：</b>{{.P.trade_no}}<br/><b>支付完成时间：</b>{{.P.time}}`},
	"settle":            {title: "结算完成通知", suffix: true, required: "money realmoney account time", money: "money realmoney", body: `尊敬的商户，今日结算已完成，请查收。<br/><b>结算金额：</b>¥{{.P.money}}<br/><b>实际到账：</b>¥{{.P.realmoney}}<br/><b>结算账号：</b>{{.P.account}}<br/><b>结算完成时间：</b>{{.P.time}}`},
	"login":             {title: "账号登录通知", suffix: true, required: "user time clientip", body: `尊敬的商户，您的账号<b>{{.P.user}}</b>已于{{.P.time}}成功登录到商户平台。<br/><b>登录IP：</b>{{.P.clientip}}<br/><b>登录时间：</b>{{.P.time}}`},
	"complain":          {title: "支付交易投诉通知", suffix: true, required: "type trade_no title content name money time", money: "money", body: `尊敬的商户，{{.P.type}}！<br/><b>系统订单号：</b>{{.P.trade_no}}<br/><b>投诉原因：</b>{{.P.title}}<br/><b>投诉详情：</b>{{.P.content}}<br/><b>商品名称：</b>{{.P.name}}<br/><b>订单金额：</b>¥{{.P.money}}<br/><b>投诉时间：</b>{{.P.time}}`},
	"mchrisk":           {title: "渠道商户违规处置通知", suffix: true, required: "mchid mchname risk_desc punish_type punish_time recover_way", body: `尊敬的商户，您有新的渠道商户违规处置记录！<br/><b>渠道子商户号：</b>{{.P.mchid}}<br/><b>商户名称：</b>{{.P.mchname}}<br/><b>风险类型：</b>{{.P.risk_desc}}<br/><b>管控能力：</b>{{.P.punish_type}}<br/><b>管控开始时间：</b>{{.P.punish_time}}<br/><b>解除方式：</b>{{.P.recover_way}}`},
	"balance":           {title: "商户余额不足提醒", suffix: true, required: "msgmoney money", money: "msgmoney money", body: `尊敬的商户，您的手续费余额不足{{.P.msgmoney}}元，为避免造成订单失败请及时充值。<br/>当前余额：{{.P.money}}元`},
	"group":             {title: "会员用户组到期提醒", suffix: true, required: "group endtime", body: `尊敬的商户，您的购买的会员用户组 <b>{{.P.group}}</b> 已于 {{.P.endtime}} 到期，请及时前往商户平台续费。`},
	"domain_audit":      {title: "授权域名审核结果通知", suffix: true, required: "domain result", body: `尊敬的商户，您添加的授权域名 <b>{{.P.domain}}</b> {{.P.result_label}}，请及时前往商户平台查看。`},
	"channellimit":      {title: "支付通道达到限额提醒", suffix: true, required: "name id reason", body: `尊敬的管理员：支付通道"<b>{{.P.name}}</b>"今日已达到限额，该通道今日将不再接收新订单。<br/><b>通道ID：</b>{{.P.id}}<br/><b>限额原因：</b>{{.P.reason}}<br/><b>触发时间：</b>{{.Now}}`},
	"daily_report":      {title: "收款日报通知", suffix: true, body: `尊敬的商户，以下是您 <b>{{.Daily.Date}}</b> 的收款日报。<br/><b>订单金额总计：</b>¥{{.Daily.Money}}<br/>{{range .Daily.Types}}<b>{{.Name}}：</b>¥{{.Money}}<br/>{{end}}<b>分成金额总计：</b>¥{{.Daily.Merchant}}<br/><b>订单总数：</b>{{.Daily.Orders}}<br/><b>成功订单数量：</b>{{.Daily.Success}}<br/><b>订单成功率：</b>{{.Daily.Rate}}%{{if .Daily.HasTransfer}}<br/><b>代付总额：</b>¥{{.Daily.Transfer}}{{range .Daily.TransferTypes}}<br/><b>{{.Name}}代付：</b>¥{{.Money}}{{end}}{{end}}`},
	"core_risk":         {title: "核心支付风控提醒", required: "kind id reason", body: `{{.P.kind}} <b>{{.P.id}}</b> 已触发支付风控规则。<br/><b>处理原因：</b>{{.P.reason}}<br/><b>处理时间：</b>{{.Now}}<br/>原订单查询与回调继续处理；新付款受限。`},
}

var noticeTemplates = func() map[string]*template.Template {
	out := make(map[string]*template.Template, len(noticeSpecs))
	for scene, spec := range noticeSpecs {
		out[scene] = template.Must(template.New(scene).Option("missingkey=error").Parse(spec.body))
	}
	return out
}()

func safeValue(value string, limit int, multiline bool) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, c := range value {
		if c < 32 || c == 127 {
			if multiline && (c == '\n' || c == '\t') {
				continue
			}
			return false
		}
	}
	return true
}

var decimalMoney = regexp.MustCompile(`^(0|[1-9][0-9]{0,16})(?:\.([0-9]{1,2}))?$`)

func normalizedMoney(value string, limit int64) (string, error) {
	parts := decimalMoney.FindStringSubmatch(value)
	if parts == nil {
		return "", ErrInvalid
	}
	whole, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil {
		return "", ErrInvalid
	}
	fraction := int64(0)
	if parts[2] != "" {
		fraction, _ = strconv.ParseInt(parts[2], 10, 64)
		if len(parts[2]) == 1 {
			fraction *= 10
		}
	}
	if whole > limit/100 || whole == limit/100 && fraction > limit%100 {
		return "", ErrInvalid
	}
	amount := whole*100 + fraction
	return money(amount), nil
}
func money(amount int64) string { return fmt.Sprintf("%d.%02d", amount/100, amount%100) }

type lineView struct{ Name, Money string }
type dailyView struct {
	Date, Money, Merchant, Rate, Transfer string
	Orders, Success                       int64
	Types, TransferTypes                  []lineView
	HasTransfer                           bool
}

func amountLines(lines []AmountLine, total int64) ([]lineView, error) {
	if len(lines) > 64 {
		return nil, ErrInvalid
	}
	out := make([]lineView, 0, len(lines))
	sum := int64(0)
	for _, line := range lines {
		if !safeValue(line.Name, 128, false) || line.Amount < 0 || line.Amount > total-sum {
			return nil, ErrInvalid
		}
		sum += line.Amount
		out = append(out, lineView{line.Name, money(line.Amount)})
	}
	if sum != total {
		return nil, ErrInvalid
	}
	return out, nil
}

func dailyPresentation(d *DailyReport) (*dailyView, error) {
	if d == nil || d.Money < 0 || d.MerchantAmount < 0 || d.MerchantAmount > d.Money || d.OrderCount < 0 || d.SuccessCount < 0 || d.SuccessCount > d.OrderCount || d.TransferTotal < 0 || !d.HasTransfer && (d.TransferTotal != 0 || len(d.TransferTypes) != 0) {
		return nil, ErrInvalid
	}
	date, e := time.Parse("2006-01-02", d.Date)
	if e != nil || date.Format("2006-01-02") != d.Date {
		return nil, ErrInvalid
	}
	types, e := amountLines(d.TypeMoney, d.Money)
	if e != nil {
		return nil, e
	}
	transfers, e := amountLines(d.TransferTypes, d.TransferTotal)
	if e != nil {
		return nil, e
	}
	rate := int64(0)
	if d.OrderCount > 0 {
		// Round the exact rational percentage half-up, including counts close
		// to MaxInt64. Neither money nor count ratios pass through float64.
		numerator := new(big.Int).Mul(big.NewInt(d.SuccessCount), big.NewInt(10000))
		denominator := big.NewInt(d.OrderCount)
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(numerator, denominator, remainder)
		if remainder.Lsh(remainder, 1).Cmp(denominator) >= 0 {
			quotient.Add(quotient, big.NewInt(1))
		}
		rate = quotient.Int64()
	}
	return &dailyView{Date: d.Date, Money: money(d.Money), Merchant: money(d.MerchantAmount), Rate: money(rate), Transfer: money(d.TransferTotal), Orders: d.OrderCount, Success: d.SuccessCount, Types: types, TransferTypes: transfers, HasTransfer: d.HasTransfer}, nil
}

func Render(in Input) (Message, error) {
	spec, ok := noticeSpecs[in.Scene]
	if !ok || !safeValue(in.SiteName, 128, false) || in.At.IsZero() || in.At.Year() < 1970 || in.At.Year() > 9999 {
		return Message{}, ErrInvalid
	}
	params := make(map[string]string)
	for _, key := range strings.Fields(spec.required) {
		value := in.Params[key]
		if !safeValue(value, 4096, key == "content") {
			return Message{}, ErrInvalid
		}
		params[key] = value
	}
	for _, key := range strings.Fields(spec.money) {
		limit := maxMoney
		if in.Scene == "balance" {
			limit = math.MaxInt64
		}
		value, e := normalizedMoney(params[key], limit)
		if e != nil {
			return Message{}, e
		}
		params[key] = value
	}
	if in.Scene == "domain_audit" {
		switch params["result"] {
		case "1":
			params["result_label"] = "审核通过"
		case "0":
			params["result_label"] = "审核未通过"
		default:
			return Message{}, ErrInvalid
		}
	}
	var daily *dailyView
	if in.Scene == "daily_report" {
		var e error
		daily, e = dailyPresentation(in.Daily)
		if e != nil {
			return Message{}, e
		}
	} else if in.Daily != nil {
		return Message{}, ErrInvalid
	}
	title := spec.title
	if spec.suffix {
		title += " - " + in.SiteName
	}
	view := struct {
		Site, Now string
		P         map[string]string
		Daily     *dailyView
	}{in.SiteName, in.At.In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02 15:04:05"), params, daily}
	var body bytes.Buffer
	if e := noticeTemplates[in.Scene].Execute(&body, view); e != nil || body.Len() > 32768 {
		return Message{}, ErrInvalid
	}
	raw := body.String()
	plain := strings.NewReplacer("<br/>", "\n", "<b>", "", "</b>", "").Replace(raw)
	message := Message{Title: title, HTML: raw, Text: html.UnescapeString(plain)}
	message.seal = message.fingerprint()
	return message, nil
}
