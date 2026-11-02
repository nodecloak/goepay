package merchantnotice

import "testing"

func TestNotice230ChannelSwitchBlocksEverySelectedRoute(t *testing.T) {
	for mode, kind := range map[Mode]Transport{1: Wechat, 2: SMTP, 3: SMS, 4: Robot} {
		input := SelectionInput{MerchantID: 1, Scene: "balance", Mode: mode, Global: GlobalSwitches{MailRobot: true, Channels: map[Transport]bool{kind: false}}, Recipients: map[Transport]bool{kind: true}, Configured: map[Transport]bool{kind: true}}
		routes, e := SelectMerchant(input)
		if e != nil || len(routes) != 1 || routes[0].Ready || routes[0].Blocker != "platform_disabled" {
			t.Fatalf("channel %s ignored off switch: %+v %v", kind, routes, e)
		}
		input.Global.Channels = nil
		routes, e = SelectMerchant(input)
		if e != nil || !routes[0].Ready {
			t.Fatal("legacy unset switch changed")
		}
	}
}
