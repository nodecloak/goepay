package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"goepay/internal/identity"
	"goepay/internal/ledger"
	notice "goepay/internal/merchantnotice"
)

func parseMerchantNoticeBody(raw []byte) (notice.SettingsWrite, int64, error) {
	var empty notice.SettingsWrite
	var top map[string]json.RawMessage
	if strictFundsJSON(raw, &top) != nil || len(top) != 5 {
		return empty, 0, notice.ErrInvalid
	}
	for _, key := range []string{"expected_profile_id", "msgconfig", "voice_order", "targets", "clear_fields"} {
		if v, ok := top[key]; !ok || string(v) == "null" {
			return empty, 0, notice.ErrInvalid
		}
	}
	var expected string
	if json.Unmarshal(top["expected_profile_id"], &expected) != nil {
		return empty, 0, notice.ErrInvalid
	}
	id, err := strconv.ParseInt(expected, 10, 64)
	if err != nil || id < 0 || strconv.FormatInt(id, 10) != expected {
		return empty, 0, notice.ErrInvalid
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(top["msgconfig"], &config) != nil || len(config) != 11 {
		return empty, 0, notice.ErrInvalid
	}
	modes := make([]notice.Mode, 9)
	for i, key := range []string{"order", "settle", "login", "complain", "mchrisk", "balance", "group", "domain_audit", "daily_report"} {
		value, ok := config[key]
		if !ok || string(value) == "null" || json.Unmarshal(value, &modes[i]) != nil || modes[i] > 6 {
			return empty, 0, notice.ErrInvalid
		}
	}
	var orderMoney, balanceMoney string
	if json.Unmarshal(config["order_money"], &orderMoney) != nil || json.Unmarshal(config["balance_money"], &balanceMoney) != nil {
		return empty, 0, notice.ErrInvalid
	}
	order, err := ledger.Parse(orderMoney)
	if err != nil {
		return empty, 0, notice.ErrInvalid
	}
	balance, err := ledger.Parse(balanceMoney)
	if err != nil {
		return empty, 0, notice.ErrInvalid
	}
	if err = json.Unmarshal(top["voice_order"], &empty.VoiceOrder); err != nil {
		return empty, 0, notice.ErrInvalid
	}
	empty.Preferences = notice.Preferences{Order: modes[0], Settle: modes[1], Login: modes[2], Complain: modes[3], MchRisk: modes[4], Balance: modes[5], Group: modes[6], DomainAudit: modes[7], DailyReport: modes[8], OrderMoney: order, BalanceMoney: balance}
	var targets map[string]json.RawMessage
	if json.Unmarshal(top["targets"], &targets) != nil || targets == nil {
		return empty, 0, notice.ErrInvalid
	}
	fields := map[string]**string{"wx_uid": &empty.Targets.WXUID, "wx_appid": &empty.Targets.WXAppID, "telegram_uid": &empty.Targets.TelegramUID, "msgrobot_url": &empty.Targets.MsgRobotURL, "qqbot_appid": &empty.Targets.QQBotAppID, "qqbot_appsecret": &empty.Targets.QQBotAppSecret, "qqbot_openid": &empty.Targets.QQBotOpenID, "voice_devid": &empty.Targets.VoiceDeviceID}
	for key, value := range targets {
		p, ok := fields[key]
		var text string
		if !ok || string(value) == "null" || json.Unmarshal(value, &text) != nil {
			return empty, 0, notice.ErrInvalid
		}
		*p = &text
	}
	if json.Unmarshal(top["clear_fields"], &empty.ClearFields) != nil || empty.ClearFields == nil {
		return empty, 0, notice.ErrInvalid
	}
	return empty, id, nil
}

func (s *Server) merchantNoticeConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, p []string) bool {
	if len(p) < 2 {
		return false
	}
	own := len(p) >= 3 && p[1] == "account" && p[2] == "notice"
	managed := len(p) >= 4 && p[1] == "merchants" && p[3] == "notice"
	if !own && !managed {
		return false
	}
	write := r.Method != "GET"
	actor := notice.Actor{Role: role, ID: session.User.ID, Name: session.User.Username, AdminClass: session.User.AdminClass}
	// Original console_users rows carry an operator default even for merchants.
	// That classification grants permissions only when the verified role is admin.
	if role == "merchant" {
		actor.AdminClass = ""
	}
	adminAllowed := actor.AdminClass == "super" || actor.AdminClass == "operator" || !write && actor.AdminClass == "auditor"
	if session.User.ID <= 0 || session.User.Username == "" || session.User.Role != role || p[0] != role || session.Issuer != nil || own && role != "merchant" || managed && (role != "admin" || !adminAllowed) {
		response(w, 403, -1, "own merchant or authorized administrator required", nil)
		return true
	}
	if write && (session.CSRF == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	events := own && len(p) == 4 && p[3] == "events" || managed && len(p) == 5 && p[4] == "events"
	if !events && (own && len(p) != 3 || managed && len(p) != 4) {
		response(w, 404, -1, "notice operation unavailable", nil)
		return true
	}
	mid := session.User.ID
	if managed {
		var ok bool
		mid, ok = positiveFundsID(p[2])
		if !ok {
			response(w, 400, -1, "canonical merchant id required", nil)
			return true
		}
	}
	before, limit := int64(0), 20
	if events {
		query, queryErr := url.ParseQuery(r.URL.RawQuery)
		if queryErr != nil {
			response(w, 400, -1, "canonical notice pagination required", nil)
			return true
		}
		for key, values := range query {
			if len(values) != 1 || key != "before_id" && key != "limit" {
				response(w, 400, -1, "canonical notice pagination required", nil)
				return true
			}
			n, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil || n < 0 || strconv.FormatInt(n, 10) != values[0] || key == "limit" && (n < 1 || n > 100) {
				response(w, 400, -1, "canonical notice pagination required", nil)
				return true
			}
			if key == "limit" {
				limit = int(n)
			} else {
				before = n
			}
		}
	} else if r.URL.RawQuery != "" {
		response(w, 400, -1, "notice owner comes from original session and path", nil)
		return true
	}
	if events && r.Method != "GET" || !events && r.Method != "GET" && r.Method != "PUT" {
		response(w, 405, -1, "notice method unavailable", nil)
		return true
	}
	var in notice.SettingsWrite
	var expected int64
	if write {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 65537))
		if err != nil || len(raw) > 65536 {
			response(w, 400, -1, "bounded notice body required", nil)
			return true
		}
		in, expected, err = parseMerchantNoticeBody(raw)
		if err != nil {
			response(w, 422, -1, "explicit original notice fields required", nil)
			return true
		}
	}
	if s.MerchantNotice == nil {
		response(w, 503, -1, "merchant notice configuration unavailable", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var out any
	var err error
	if events {
		var items []notice.EventView
		items, err = s.MerchantNotice.Events(ctx, mid, before, limit, actor)
		cursor := ""
		if len(items) == limit {
			cursor = strconv.FormatInt(items[len(items)-1].ID, 10)
		}
		out = struct {
			Items      []notice.EventView `json:"items"`
			NextCursor string             `json:"next_cursor"`
		}{items, cursor}
	} else if write {
		out, err = s.MerchantNotice.SaveSettings(ctx, mid, expected, in, actor)
	} else {
		out, err = s.MerchantNotice.Settings(ctx, mid, actor)
	}
	if err != nil {
		switch {
		case errors.Is(err, notice.ErrForbidden):
			response(w, 403, -1, "notice access denied", nil)
		case errors.Is(err, notice.ErrInvalid):
			response(w, 422, -1, "invalid notice configuration", nil)
		case errors.Is(err, notice.ErrConflict), errors.Is(err, notice.ErrState):
			response(w, 409, -1, "notice version or original proof conflict; reload before editing", nil)
		case errors.Is(err, notice.ErrNotFound):
			response(w, 404, -1, "merchant unavailable", nil)
		default:
			s.fail(w, err)
		}
		return true
	}
	identityHeaders(w)
	response(w, 200, 0, "", out)
	return true
}
