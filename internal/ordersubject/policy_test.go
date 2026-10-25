package ordersubject

import (
	"strings"
	"testing"
	"time"
)

func TestSubjectRulesStrictMoneyAndClosedIntervals(t *testing.T) {
	for _, raw := range []string{`[{"name":"ok","min":"0.00","max":"1.20"}]`, `[]`} {
		if _, e := ParseRules(raw); e != nil {
			t.Fatalf("valid %s: %v", raw, e)
		}
	}
	for _, raw := range []string{`null`, `{}`, `[{"name":"x","min":1,"max":"2"}]`, `[{"name":"x","min":"01","max":"2"}]`, `[{"name":"x","min":"1.001","max":"2"}]`, `[{"name":"x","min":"1e2","max":"200"}]`, `[{"name":"x","min":"2","max":"1"}]`, `[{"name":"x","name":"y","min":"1","max":"2"}]`, `[{"name":"x","min":"1","max":"2","extra":1}]`, `[{"name":"x","min":"1","max":"2"},{"name":"y","min":"2","max":"3"}]`, `[] []`} {
		if _, e := ParseRules(raw); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestSubjectLiteralOrderRandomAndBounds(t *testing.T) {
	v := Variables{Name: "raw-[order]", TradeNo: "T1", OutTradeNo: "M1", QQ: "123", Phone: "456", Now: time.Unix(1700000000, 0)}
	got, e := Render(`[name]/[order]/[outorder]/[qq]/[phone]/[time]/[unknown]`, v)
	if e != nil || got != "raw-T1/T1/M1/123/456/1700000000/[unknown]" {
		t.Fatalf("%q %v", got, e)
	}
	for range 30 {
		got, e = Render("first|second", v)
		if e != nil || (got != "first" && got != "second") {
			t.Fatal(got, e)
		}
	}
	for _, s := range []string{"", "x||y", strings.Repeat("界", 43), "x\n", strings.Repeat("[name]", 100)} {
		v.Name = strings.Repeat("a", 256)
		if _, e = Render(s, v); e == nil {
			t.Fatalf("accepted oversized/invalid %q", s)
		}
	}
}

func TestSubjectScopeAndTemplateValidation(t *testing.T) {
	for _, p := range []Policy{{Scope: "global", ScopeID: 0}, {Scope: "merchant", ScopeID: 1, Template: "[name]"}, {Scope: "channel", ScopeID: 7, RulesText: "[]"}} {
		if e := p.Validate(); e != nil {
			t.Fatal(e)
		}
	}
	for _, p := range []Policy{{Scope: "global", ScopeID: 1}, {Scope: "merchant"}, {Scope: "channel", ScopeID: -1}, {Scope: "group", ScopeID: 1}, {Scope: "global", Template: "a||b"}} {
		if e := p.Validate(); e == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
}
