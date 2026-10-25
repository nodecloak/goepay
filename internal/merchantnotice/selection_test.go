package merchantnotice

import (
	"reflect"
	"testing"
)

func selection80() SelectionInput {
	all := map[Transport]bool{Wechat: true, SMTP: true, SMS: true, Robot: true, Telegram: true, QQBot: true, Voice: true}
	return SelectionInput{MerchantID: 1000, Scene: "order", Mode: 2, Amount: 1230, Subject: "本地商品", Global: GlobalSwitches{MailRobot: true, Telegram: true, QQBot: true, Voice: true, GroupSMS: true}, Recipients: all, Configured: all}
}
func ready80(routes []Route) []Transport {
	out := []Transport{}
	for _, r := range routes {
		if r.Ready {
			out = append(out, r.Transport)
		}
	}
	return out
}

func TestMerchantNotice80SelectedTransportNeverFallsBack(t *testing.T) {
	for mode, kind := range map[Mode]Transport{1: Wechat, 2: SMTP, 3: SMS, 4: Robot, 5: Telegram, 6: QQBot} {
		t.Run(string(kind), func(t *testing.T) {
			in := selection80()
			in.Mode = mode
			if kind == SMS {
				in.Scene = "balance"
			}
			got, e := SelectMerchant(in)
			if e != nil || !reflect.DeepEqual(ready80(got), []Transport{kind}) {
				t.Fatal(got, e)
			}
			in.Recipients = map[Transport]bool{}
			got, e = SelectMerchant(in)
			if e != nil || len(got) != 1 || got[0].Ready || got[0].Blocker != "recipient_missing" {
				t.Fatal("missing chosen destination silently fell back", got, e)
			}
			in = selection80()
			in.Mode = mode
			if kind == SMS {
				in.Scene = "balance"
			}
			in.Configured = map[Transport]bool{}
			got, e = SelectMerchant(in)
			if e != nil || len(got) != 1 || got[0].Ready || got[0].Blocker != "transport_unconfigured" {
				t.Fatal(got, e)
			}
		})
	}
}

func TestMerchantNotice80OriginalDailyBypassAndIndependentPlatformSwitches(t *testing.T) {
	for _, mode := range []Mode{2, 4} {
		in := selection80()
		in.Mode = mode
		in.Global.MailRobot = false
		got, e := SelectMerchant(in)
		if e != nil || len(ready80(got)) != 0 {
			t.Fatal("ordinary mail/robot bypassed original scene switch", got, e)
		}
		in.Scene = "daily_report"
		got, e = SelectMerchant(in)
		if e != nil || len(ready80(got)) != 1 {
			t.Fatal("daily mail/robot incorrectly required ordinary scene switch", got, e)
		}
	}
	for _, mode := range []Mode{5, 6} {
		in := selection80()
		in.Mode = mode
		in.Scene = "daily_report"
		in.Global.Telegram = false
		in.Global.QQBot = false
		got, e := SelectMerchant(in)
		if e != nil || len(ready80(got)) != 0 {
			t.Fatal("daily bypassed independent Bot switch", got, e)
		}
	}
	in := selection80()
	in.Scene = "daily_report"
	in.Mode = 1
	got, e := SelectMerchant(in)
	if e != nil || len(got) != 1 || got[0].Blocker != "template_unsupported_scene" {
		t.Fatal("nonexistent original Wechat daily template advertised", got, e)
	}
}

func TestMerchantNotice80VoicePrecedesAmountThresholdAndExcludesOriginalBarcodeName(t *testing.T) {
	in := selection80()
	in.VoiceOrder = true
	in.OrderMinimum = 2000
	got, e := SelectMerchant(in)
	if e != nil || !reflect.DeepEqual(ready80(got), []Transport{Voice}) {
		t.Fatal("order threshold suppressed independent original voice", got, e)
	}
	in.Subject = "付款码收款"
	got, e = SelectMerchant(in)
	if e != nil || len(ready80(got)) != 0 {
		t.Fatal("original barcode voice exclusion lost", got, e)
	}
	in.Amount = 2000
	got, e = SelectMerchant(in)
	if e != nil || !reflect.DeepEqual(ready80(got), []Transport{SMTP}) {
		t.Fatal("exact original order threshold was excluded", got, e)
	}
	in.Scene = "settle"
	got, e = SelectMerchant(in)
	if e != nil || !reflect.DeepEqual(ready80(got), []Transport{SMTP}) {
		t.Fatal("order-only voice or threshold leaked to settlement", got, e)
	}
}

func TestMerchantNotice80MandatoryGroupRoutesRemainIndependentAndDeduplicateSameRecipient(t *testing.T) {
	in := selection80()
	in.Scene = "group"
	in.Mode = 0
	got, e := SelectMerchant(in)
	if e != nil || !reflect.DeepEqual(ready80(got), []Transport{SMTP, SMS}) {
		t.Fatal("original group extra routes missing", got, e)
	}
	in.Mode = 2
	got, e = SelectMerchant(in)
	if e != nil || !reflect.DeepEqual(ready80(got), []Transport{SMTP, SMS}) || len(got[0].Triggers) != 2 {
		t.Fatal("same original email duplicated or extra trigger lost", got, e)
	}
	in.Mode = 3
	got, e = SelectMerchant(in)
	if e != nil || !reflect.DeepEqual(ready80(got), []Transport{SMS, SMTP}) {
		t.Fatal("group forced SMS lost to unsupported selected SMS branch", got, e)
	}
	in.Scene = "domain_audit"
	in.Mode = 0
	got, e = SelectMerchant(in)
	if e != nil || !reflect.DeepEqual(ready80(got), []Transport{SMTP}) {
		t.Fatal(got, e)
	}
	for _, mutate := range []func(*SelectionInput){func(i *SelectionInput) { i.MerchantID = 0 }, func(i *SelectionInput) { i.Scene = "unknown80" }, func(i *SelectionInput) { i.Mode = 7 }, func(i *SelectionInput) { i.Amount = -1 }, func(i *SelectionInput) { i.OrderMinimum = -1 }} {
		bad := selection80()
		mutate(&bad)
		if _, e = SelectMerchant(bad); e == nil {
			t.Fatal("untrusted merchant routing inputs accepted")
		}
	}
}
