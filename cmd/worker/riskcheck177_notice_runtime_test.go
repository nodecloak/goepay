package main

import (
	"context"
	"goepay/internal/merchantnotice"
	"goepay/internal/payment"
	"testing"
)

func TestRisk177NoticeRuntimeDefaultsOffAndRechecksLiveFlag(t *testing.T) {
	values := map[string]string{}
	get := func(k string) string { return values[k] }
	r, e := newCoreRiskNoticeRuntime(&payment.Store{}, get)
	if e != nil || r != nil {
		t.Fatal("notice auto enabled", r, e)
	}
	values["GOEPAY_ENABLE_CORE_RISK_NOTICES"] = "yes"
	if _, e = newCoreRiskNoticeRuntime(&payment.Store{}, get); e == nil {
		t.Fatal("invalid flag accepted")
	}
	values["GOEPAY_ENABLE_CORE_RISK_NOTICES"] = "true"
	values["GOEPAY_SMTP_HOST"] = "mail.example.invalid"
	values["GOEPAY_SMTP_PORT"] = "465"
	values["GOEPAY_SMTP_SECURITY"] = "implicit"
	values["GOEPAY_SMTP_USERNAME"] = "synthetic-risk177"
	values["GOEPAY_SMTP_PASSWORD"] = "synthetic-risk177-password"
	values["GOEPAY_SMTP_FROM"] = "risk177@example.invalid"
	r, e = newCoreRiskNoticeRuntime(&payment.Store{}, get)
	if e != nil {
		t.Fatal(e)
	}
	sender, _, e := r.ResolveCoreRiskNotice(context.Background(), "merchant")
	if e != nil || sender == nil || sender.NoticeTransport() != merchantnotice.SMTP {
		t.Fatal("real merchant SMTP factory unavailable", e)
	}
	values["GOEPAY_ENABLE_CORE_RISK_NOTICES"] = "false"
	if _, _, e = r.ResolveCoreRiskNotice(context.Background(), "merchant"); e == nil {
		t.Fatal("late off ignored")
	}
	if e = r.Handle(context.Background(), 1, "synthetic-token"); e == nil {
		t.Fatal("late off entered store")
	}
}
