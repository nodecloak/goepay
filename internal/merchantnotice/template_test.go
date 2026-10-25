package merchantnotice

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestMerchantNotice80RenderedMessageCannotBeMutatedOrInventedAfterValidation(t *testing.T) {
	message, e := Render(input80("order"))
	if e != nil {
		t.Fatal(e)
	}
	if !message.Valid() {
		t.Fatal("original rendering has no private validation seal")
	}
	for _, mutate := range []func(*Message){func(m *Message) { m.Title += "changed" }, func(m *Message) { m.HTML = "<script>changed</script>" }, func(m *Message) { m.Text += "changed" }} {
		copy := message
		mutate(&copy)
		if copy.Valid() {
			t.Fatal("changed message retained renderer provenance")
		}
	}
	raw, e := json.Marshal(message)
	if e != nil {
		t.Fatal(e)
	}
	var invented Message
	if e = json.Unmarshal(raw, &invented); e != nil {
		t.Fatal(e)
	}
	if invented.Valid() {
		t.Fatal("serialized fields manufactured private renderer provenance")
	}
}

func input80(scene string) Input {
	return Input{Scene: scene, SiteName: "本地易支付", At: time.Date(2026, 10, 24, 9, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)), Params: map[string]string{
		"uid": "1000", "touid": "1001", "account": "local@example.invalid", "type": "微信", "money": "12.3", "realmoney": "12.00", "domain": "local.example.invalid", "name": "本地商品", "out_trade_no": "OUT80", "trade_no": "TRADE80", "time": "2026-10-24 09:00:00", "user": "local80", "clientip": "127.0.0.1", "ipinfo": "本地", "title": "测试原因", "content": "测试详情", "mchid": "mch80", "mchname": "本地商户", "risk_desc": "测试风险", "punish_type": "测试限制", "punish_time": "2026-10-24 09:00:00", "recover_way": "本地核实", "msgmoney": "20", "group": "本地会员", "endtime": "2026-10-24", "result": "1", "id": "7", "reason": "本地限额",
	}}
}

func TestMerchantNotice80OriginalScenesKeepTitlesAndCompleteFields(t *testing.T) {
	wants := map[string]string{"regaudit": "新注册商户待审核提醒", "apply": "新的提现待处理提醒", "domain": "新的授权支付域名待审核提醒", "internal_transfer": "新的站内转账待审核提醒", "order": "新订单通知 - 本地易支付", "settle": "结算完成通知 - 本地易支付", "login": "账号登录通知 - 本地易支付", "complain": "支付交易投诉通知 - 本地易支付", "mchrisk": "渠道商户违规处置通知 - 本地易支付", "balance": "商户余额不足提醒 - 本地易支付", "group": "会员用户组到期提醒 - 本地易支付", "domain_audit": "授权域名审核结果通知 - 本地易支付", "channellimit": "支付通道达到限额提醒 - 本地易支付"}
	for scene, title := range wants {
		t.Run(scene, func(t *testing.T) {
			in := input80(scene)
			got, e := Render(in)
			if e != nil || got.Title != title || got.HTML == "" || got.Text == "" || strings.Contains(got.Text, "<br/>") || strings.Contains(got.Text, "<b>") {
				t.Fatal(got, e)
			}
			in.Params = map[string]string{}
			if _, e = Render(in); e == nil {
				t.Fatal("incomplete original event rendered as a complete notification")
			}
		})
	}
}

func TestMerchantNotice80EscapesOriginalHTMLAndKeepsPlainTextLiteral(t *testing.T) {
	in := input80("order")
	in.SiteName = "站点 & 平台"
	in.Params["name"] = `<img src=x onerror="alert(1)"> & A`
	got, e := Render(in)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(got.HTML, "<img") || !strings.Contains(got.HTML, "&lt;img") || !strings.Contains(got.HTML, "&amp; A") || !strings.Contains(got.Text, in.Params["name"]) || !strings.Contains(got.HTML, "¥12.30") {
		t.Fatal("unsafe HTML or altered money", got)
	}
	for _, mutate := range []func(*Input){func(i *Input) { i.Scene = "unknown80" }, func(i *Input) { i.Scene = "wxtransfer" }, func(i *Input) { i.SiteName = "title\r\nBcc: injected" }, func(i *Input) { i.SiteName = string([]byte{0xff}) }, func(i *Input) { i.At = time.Time{} }, func(i *Input) { i.Params["money"] = "1e3" }, func(i *Input) { i.Params["money"] = "-1" }, func(i *Input) { i.Params["money"] = "0.001" }, func(i *Input) { i.Params["money"] = "10000000000" }, func(i *Input) { i.Params["name"] = strings.Repeat("x", 4097) }} {
		bad := input80("order")
		mutate(&bad)
		if _, e = Render(bad); e == nil {
			t.Fatal("untrusted or unimplemented notice rendered")
		}
	}
}

func daily80() *DailyReport {
	return &DailyReport{Date: "2026-10-23", Money: 1230, MerchantAmount: 1200, OrderCount: 3, SuccessCount: 2, TypeMoney: []AmountLine{{Name: "微信", Amount: 1000}, {Name: "支付宝", Amount: 230}}, HasTransfer: true, TransferTotal: 500, TransferTypes: []AmountLine{{Name: "银行卡", Amount: 500}}}
}

func TestMerchantNotice80DailyUsesExactFenAndOriginalSuccessfulHistoryCounts(t *testing.T) {
	in := input80("daily_report")
	in.Daily = daily80()
	got, e := Render(in)
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"收款日报通知 - 本地易支付", "2026-10-23", "订单金额总计：¥12.30", "微信：¥10.00", "分成金额总计：¥12.00", "订单总数：3", "成功订单数量：2", "订单成功率：66.67%", "代付总额：¥5.00", "银行卡代付：¥5.00"} {
		if !strings.Contains(got.Title+"\n"+got.Text, want) {
			t.Fatal("original daily field missing", want, got)
		}
	}
	for _, pair := range [][2]int64{{0, 0}, {math.MaxInt64, math.MaxInt64 - 1}, {6, 1}, {32, 1}} {
		in.Daily = &DailyReport{Date: "2026-10-23", OrderCount: pair[0], SuccessCount: pair[1]}
		out, e := Render(in)
		if e != nil {
			t.Fatal("count overflow or zero denominator", pair, e)
		}
		want := map[int64]string{0: "0.00%", math.MaxInt64: "100.00%", 6: "16.67%", 32: "3.13%"}[pair[0]]
		if !strings.Contains(out.Text, want) {
			t.Fatal("percentage is not exact half-up", out.Text)
		}
	}
	for _, mutate := range []func(*DailyReport){func(d *DailyReport) { d.Date = "2026-02-30" }, func(d *DailyReport) { d.SuccessCount = 4 }, func(d *DailyReport) { d.Money = -1 }, func(d *DailyReport) { d.MerchantAmount = 1231 }, func(d *DailyReport) { d.TypeMoney[0].Amount++ }, func(d *DailyReport) { d.TransferTotal++ }, func(d *DailyReport) { d.HasTransfer = false }, func(d *DailyReport) { d.OrderCount = -1 }} {
		in.Daily = daily80()
		mutate(in.Daily)
		if _, e = Render(in); e == nil {
			t.Fatal("inconsistent daily snapshot rendered")
		}
	}
}

func TestMerchantNotice80DailyAndBalanceAggregatesDoNotUseSingleTransactionCeiling(t *testing.T) {
	in := input80("daily_report")
	in.Daily = &DailyReport{Date: "2026-10-23", Money: math.MaxInt64, MerchantAmount: math.MaxInt64 - 1, OrderCount: 2, SuccessCount: 2, TypeMoney: []AmountLine{{Name: "微信", Amount: math.MaxInt64}}}
	message, e := Render(in)
	if e != nil || !strings.Contains(message.Text, "92233720368547758.07") {
		t.Fatal("valid aggregate was restricted to a single payment", e)
	}
	in = input80("balance")
	in.Params["money"], in.Params["msgmoney"] = "92233720368547758.07", "92233720368547758.07"
	message, e = Render(in)
	if e != nil || !strings.Contains(message.Text, "92233720368547758.07") {
		t.Fatal("balance renderer overflowed or capped valid int64", e)
	}
	in.Params["money"] = "92233720368547758.08"
	if _, e = Render(in); e == nil {
		t.Fatal("money above int64 accepted")
	}
	selected := selection80()
	selected.Scene = "balance"
	selected.Amount = math.MaxInt64
	if _, e = SelectMerchant(selected); e != nil {
		t.Fatal("aggregate balance used order ceiling", e)
	}
}
