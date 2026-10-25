package merchantnotice

import (
	"errors"
	"strings"
)

var (
	ErrForbidden = errors.New("notice configuration access denied")
	ErrConflict  = errors.New("protected notice source conflict")
	ErrNotFound  = errors.New("notice owner not found")
	ErrState     = errors.New("notice configuration unavailable")
)

type Actor struct {
	Role             string
	ID               int64
	Name, AdminClass string
}

func (a Actor) valid(mid int64, write bool) bool {
	if mid <= 0 || a.ID <= 0 || !safeValue(a.Name, 128, false) {
		return false
	}
	if a.Role == "merchant" {
		return a.ID == mid && a.AdminClass == ""
	}
	return a.Role == "admin" && (a.AdminClass == "super" || a.AdminClass == "operator" || !write && a.AdminClass == "auditor")
}

// Thresholds are integer fen internally. Public views preserve the original
// order_money/balance_money names with decimal yuan strings.
type Preferences struct {
	Order        Mode  `json:"order"`
	Settle       Mode  `json:"settle"`
	Login        Mode  `json:"login"`
	Complain     Mode  `json:"complain"`
	MchRisk      Mode  `json:"mchrisk"`
	Balance      Mode  `json:"balance"`
	Group        Mode  `json:"group"`
	DomainAudit  Mode  `json:"domain_audit"`
	DailyReport  Mode  `json:"daily_report"`
	OrderMoney   int64 `json:"order_money_fen"`
	BalanceMoney int64 `json:"balance_money_fen"`
}

func (p Preferences) valid() bool {
	for _, mode := range []Mode{p.Order, p.Settle, p.Login, p.Complain, p.MchRisk, p.Balance, p.Group, p.DomainAudit, p.DailyReport} {
		if mode > 6 {
			return false
		}
	}
	return p.OrderMoney >= 0 && p.OrderMoney <= maxMoney && p.BalanceMoney >= 0
}

type PreferencesView struct {
	Order        Mode   `json:"order"`
	Settle       Mode   `json:"settle"`
	Login        Mode   `json:"login"`
	Complain     Mode   `json:"complain"`
	MchRisk      Mode   `json:"mchrisk"`
	Balance      Mode   `json:"balance"`
	Group        Mode   `json:"group"`
	DomainAudit  Mode   `json:"domain_audit"`
	DailyReport  Mode   `json:"daily_report"`
	OrderMoney   string `json:"order_money"`
	BalanceMoney string `json:"balance_money"`
}

func (p Preferences) view() PreferencesView {
	return PreferencesView{p.Order, p.Settle, p.Login, p.Complain, p.MchRisk, p.Balance, p.Group, p.DomainAudit, p.DailyReport, money(p.OrderMoney), money(p.BalanceMoney)}
}

// All target fields are explicit notification destinations. They are not
// verified identities and grant no login, payment or merchant permission.
type TargetPatch struct {
	WXUID          *string
	WXAppID        *string
	TelegramUID    *string
	MsgRobotURL    *string
	QQBotAppID     *string
	QQBotAppSecret *string
	QQBotOpenID    *string
	VoiceDeviceID  *string
}
type SettingsWrite struct {
	Preferences Preferences
	VoiceOrder  bool
	Targets     TargetPatch
	ClearFields []string
}
type SettingsView struct {
	ID          int64             `json:"id,string"`
	MerchantID  int64             `json:"merchant_id,string"`
	Revision    int64             `json:"revision,string"`
	MsgConfig   PreferencesView   `json:"msgconfig"`
	VoiceOrder  bool              `json:"voice_order"`
	EmailMasked string            `json:"email_masked"`
	Configured  map[string]bool   `json:"configured"`
	TargetMasks map[string]string `json:"target_masks"`
}
type storedTargets struct {
	Email          string `json:"email"`
	WXUID          string `json:"wx_uid"`
	WXAppID        string `json:"wx_appid"`
	TelegramUID    string `json:"telegram_uid"`
	MsgRobotURL    string `json:"msgrobot_url"`
	QQBotAppID     string `json:"qqbot_appid"`
	QQBotAppSecret string `json:"qqbot_appsecret"`
	QQBotOpenID    string `json:"qqbot_openid"`
	VoiceDeviceID  string `json:"voice_devid"`
}

func (t storedTargets) valid() bool {
	if !validNoticeEmail(t.Email) || !validTargetID(t.WXUID) || !validTargetID(t.WXAppID) || !validNumericChat(t.TelegramUID) || !validTargetID(t.QQBotAppID) || !validTargetSecret(t.QQBotAppSecret) || !validTargetID(t.QQBotOpenID) || !validTargetID(t.VoiceDeviceID) {
		return false
	}
	if t.MsgRobotURL != "" {
		if _, e := ParseWebhookTarget(t.MsgRobotURL); e != nil {
			return false
		}
	}
	return true
}

type profileData struct {
	Version     int           `json:"version"`
	Preferences Preferences   `json:"preferences"`
	VoiceOrder  bool          `json:"voice_order"`
	Targets     storedTargets `json:"targets"`
}

func (d profileData) valid() bool {
	return d.Version == 1 && d.Preferences.valid() && d.Targets.valid()
}

func applySettings(old profileData, in SettingsWrite, email string) (profileData, error) {
	if !in.Preferences.valid() || len(in.ClearFields) > 8 {
		return profileData{}, ErrInvalid
	}
	d := old
	d.Version = 1
	d.Preferences = in.Preferences
	d.VoiceOrder = in.VoiceOrder
	d.Targets.Email = email
	targets := map[string]*string{"wx_uid": &d.Targets.WXUID, "wx_appid": &d.Targets.WXAppID, "telegram_uid": &d.Targets.TelegramUID, "msgrobot_url": &d.Targets.MsgRobotURL, "qqbot_appid": &d.Targets.QQBotAppID, "qqbot_appsecret": &d.Targets.QQBotAppSecret, "qqbot_openid": &d.Targets.QQBotOpenID, "voice_devid": &d.Targets.VoiceDeviceID}
	patches := map[string]*string{"wx_uid": in.Targets.WXUID, "wx_appid": in.Targets.WXAppID, "telegram_uid": in.Targets.TelegramUID, "msgrobot_url": in.Targets.MsgRobotURL, "qqbot_appid": in.Targets.QQBotAppID, "qqbot_appsecret": in.Targets.QQBotAppSecret, "qqbot_openid": in.Targets.QQBotOpenID, "voice_devid": in.Targets.VoiceDeviceID}
	cleared := map[string]bool{}
	for _, name := range in.ClearFields {
		target, ok := targets[name]
		if !ok || cleared[name] {
			return profileData{}, ErrInvalid
		}
		cleared[name] = true
		*target = ""
	}
	for name, value := range patches {
		if value == nil {
			continue
		}
		if cleared[name] {
			if *value != "" {
				return profileData{}, ErrInvalid
			}
			continue
		}
		if *value == "" {
			return profileData{}, ErrInvalid
		}
		*targets[name] = *value
	}
	if !d.valid() {
		return profileData{}, ErrInvalid
	}
	return d, nil
}
func maskTarget(value string) string {
	if value == "" {
		return ""
	}
	letters := []rune(value)
	if len(letters) < 8 {
		return "***"
	}
	return "***" + string(letters[len(letters)-4:])
}
func profileView(id, mid, revision int64, d profileData) SettingsView {
	configured := map[string]bool{"email": d.Targets.Email != "", "wx_uid": d.Targets.WXUID != "", "wx_appid": d.Targets.WXAppID != "", "telegram_uid": d.Targets.TelegramUID != "", "msgrobot_url": d.Targets.MsgRobotURL != "", "qqbot_appid": d.Targets.QQBotAppID != "", "qqbot_appsecret": d.Targets.QQBotAppSecret != "", "qqbot_openid": d.Targets.QQBotOpenID != "", "voice_devid": d.Targets.VoiceDeviceID != ""}
	email := ""
	if d.Targets.Email != "" {
		parts := strings.SplitN(d.Targets.Email, "@", 2)
		email = maskTarget(parts[0]) + "@" + parts[1]
	}
	masks := map[string]string{"wx_uid": maskTarget(d.Targets.WXUID), "wx_appid": maskTarget(d.Targets.WXAppID), "telegram_uid": maskTarget(d.Targets.TelegramUID), "qqbot_appid": maskTarget(d.Targets.QQBotAppID), "qqbot_openid": maskTarget(d.Targets.QQBotOpenID), "voice_devid": maskTarget(d.Targets.VoiceDeviceID)}
	return SettingsView{ID: id, MerchantID: mid, Revision: revision, MsgConfig: d.Preferences.view(), VoiceOrder: d.VoiceOrder, EmailMasked: email, Configured: configured, TargetMasks: masks}
}
