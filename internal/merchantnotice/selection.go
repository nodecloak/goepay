package merchantnotice

type Mode uint8
type Transport string

const (
	Wechat   Transport = "wechat_template"
	SMTP     Transport = "smtp"
	SMS      Transport = "sms"
	Robot    Transport = "robot"
	Telegram Transport = "telegram"
	QQBot    Transport = "qqbot"
	Voice    Transport = "voice"
)

type GlobalSwitches struct {
	MailRobot, Telegram, QQBot, Voice, GroupSMS bool
}

// Presence and configuration are preflight facts, not recipient identifiers or
// credentials. This pure selector grants no authority to create a delivery.
type SelectionInput struct {
	MerchantID             int64
	Scene, Subject         string
	Mode                   Mode
	Amount, OrderMinimum   int64
	VoiceOrder             bool
	Global                 GlobalSwitches
	Recipients, Configured map[Transport]bool
}

type Route struct {
	Transport Transport
	Triggers  []string
	Ready     bool
	Blocker   string
}

func SelectMerchant(in SelectionInput) ([]Route, error) {
	if in.MerchantID <= 0 || in.Mode > 6 || in.Amount < 0 || in.Scene == "order" && in.Amount > maxMoney || in.OrderMinimum < 0 || in.OrderMinimum > maxMoney {
		return nil, ErrInvalid
	}
	switch in.Scene {
	case "order", "settle", "login", "complain", "mchrisk", "balance", "group", "domain_audit", "daily_report":
	default:
		return nil, ErrInvalid
	}
	routes := make([]Route, 0, 4)
	add := func(kind Transport, trigger, blocker string) {
		if blocker == "" && !in.Recipients[kind] {
			blocker = "recipient_missing"
		}
		if blocker == "" && !in.Configured[kind] {
			blocker = "transport_unconfigured"
		}
		candidate := Route{Transport: kind, Triggers: []string{trigger}, Ready: blocker == "", Blocker: blocker}
		for i := range routes {
			if routes[i].Transport == kind {
				routes[i].Triggers = append(routes[i].Triggers, trigger)
				if candidate.Ready || !routes[i].Ready && routes[i].Blocker == "template_unsupported_scene" {
					routes[i].Ready, routes[i].Blocker = candidate.Ready, candidate.Blocker
				}
				return
			}
		}
		routes = append(routes, candidate)
	}
	if in.Scene == "order" && in.Global.Voice && in.VoiceOrder {
		blocker := ""
		if in.Subject == "付款码收款" {
			blocker = "barcode_voice_excluded"
		}
		add(Voice, "order-voice", blocker)
	}
	if in.Mode != 0 {
		kind := map[Mode]Transport{1: Wechat, 2: SMTP, 3: SMS, 4: Robot, 5: Telegram, 6: QQBot}[in.Mode]
		blocker := ""
		switch kind {
		case Wechat:
			switch in.Scene {
			case "order", "settle", "login", "complain", "balance":
			default:
				blocker = "template_unsupported_scene"
			}
		case SMTP, Robot:
			if in.Scene != "daily_report" && !in.Global.MailRobot {
				blocker = "platform_disabled"
			}
		case SMS:
			if in.Scene != "balance" && in.Scene != "complain" {
				blocker = "template_unsupported_scene"
			}
		case Telegram:
			if !in.Global.Telegram {
				blocker = "platform_disabled"
			}
		case QQBot:
			if !in.Global.QQBot {
				blocker = "platform_disabled"
			}
		}
		if in.Scene == "order" && in.OrderMinimum > 0 && in.Amount < in.OrderMinimum {
			blocker = "amount_below_minimum"
		}
		add(kind, "selected", blocker)
	}
	if (in.Scene == "group" || in.Scene == "domain_audit") && in.Global.MailRobot {
		add(SMTP, in.Scene+"-mail", "")
	}
	if in.Scene == "group" && in.Global.GroupSMS {
		add(SMS, "group-sms", "")
	}
	return routes, nil
}
