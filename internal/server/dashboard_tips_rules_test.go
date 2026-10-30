package server

import (
	"strings"
	"testing"
)

func TestMerchantTipsForRules(t *testing.T) {
	healthy := merchantTipState{Pay: 1, PhoneBound: true, SMSReady: true, EmailBound: true}
	titles := func(st merchantTipState) string {
		var out []string
		for _, tip := range merchantTipsFor(st) {
			out = append(out, tip.Type+"|"+tip.Link+"|"+tip.Title)
		}
		return strings.Join(out, "\n")
	}
	if got := titles(healthy); got != "" {
		t.Fatalf("healthy merchant has tips: %s", got)
	}
	cases := []struct {
		name string
		edit func(*merchantTipState)
		want string // substring of the joined output; "" means no tips at all
	}{
		{"pending audit", func(s *merchantTipState) { s.Pay = 2 }, "warning|/account/settings|您的支付功能正在审核中"},
		{"restricted", func(s *merchantTipState) { s.Pay = 0 }, "error|/account/settings|您的支付功能已被限制"},
		{"no settlement account", func(s *merchantTipState) { s.SettlementMissing = true }, "warning|/account/settings|您还未设置结算账户"},
		{"no phone, sms ready", func(s *merchantTipState) { s.PhoneBound = false }, "warning|/account/settings|您还未绑定手机号码"},
		{"no phone, no sms sender", func(s *merchantTipState) { s.PhoneBound, s.SMSReady = false, false }, ""},
		{"no email", func(s *merchantTipState) { s.EmailBound = false }, "warning|/account/settings|您还未绑定邮箱"},
		{"deposit below minimum", func(s *merchantTipState) {
			s.DepositEnabled, s.DepositMinCents, s.DepositHeldCents = true, 1000, 999
		}, "error|/account/deposit|您的保证金不足，当前平台要求最低缴纳保证金 10.00 元"},
		{"deposit at minimum", func(s *merchantTipState) {
			s.DepositEnabled, s.DepositMinCents, s.DepositHeldCents = true, 1000, 1000
		}, ""},
		{"deposit feature off", func(s *merchantTipState) {
			s.DepositEnabled, s.DepositMinCents, s.DepositHeldCents = false, 1000, 0
		}, ""},
		{"no minimum", func(s *merchantTipState) { s.DepositEnabled = true }, ""},
	}
	for _, c := range cases {
		st := healthy
		c.edit(&st)
		got := titles(st)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	// Fixed order: payment status, deposit, settlement, phone, email.
	all := merchantTipState{Pay: 0, SettlementMissing: true, SMSReady: true, DepositEnabled: true, DepositMinCents: 100}
	tips := merchantTipsFor(all)
	order := []string{"已被限制", "保证金不足", "结算账户", "手机号码", "邮箱"}
	if len(tips) != len(order) {
		t.Fatalf("tips %+v", tips)
	}
	for i, want := range order {
		if !strings.Contains(tips[i].Title, want) {
			t.Fatalf("tip %d is %q, want %q", i, tips[i].Title, want)
		}
	}
}
