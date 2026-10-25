package server

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

var settlementAutoKeys = []string{"expected_revision", "expected_authorization_id", "generation_authorization_id", "valid_from", "expires_at", "max_item_gross", "max_run_gross", "max_day_gross", "max_day_items", "cost_policy", "channels"}

func settlementAutoFundsTarget(p []string) (fundsTargetDefinition, bool) {
	if len(p) != 3 || p[0] != "settlement-plans" || p[2] != "auto-authorizations" {
		return fundsTargetDefinition{}, false
	}
	if _, ok := positiveFundsID(p[1]); !ok {
		return fundsTargetDefinition{}, false
	}
	return fundsTargetDefinition{guard: "settlement-auto-authorize", category: "settlement-auto", id: p[1], verb: p[2], keys: settlementAutoKeys}, true
}

func normalizeSettlementAutoBody(raw []byte) (map[string]json.RawMessage, []byte, error) {
	var fields map[string]json.RawMessage
	if strictFundsJSON(raw, &fields) != nil || len(fields) != len(settlementAutoKeys) {
		return nil, nil, payment.ErrInvalid
	}
	for _, key := range settlementAutoKeys {
		if _, ok := fields[key]; !ok {
			return nil, nil, payment.ErrInvalid
		}
	}
	for _, key := range []string{"expected_revision", "expected_authorization_id", "generation_authorization_id", "max_item_gross", "max_run_gross", "max_day_gross", "max_day_items"} {
		if !settlementAutoDecimal(fields[key], key == "expected_authorization_id") {
			return nil, nil, payment.ErrInvalid
		}
	}
	if _, ok := positiveFundsID(fundsText(fields, "expected_revision")); !ok {
		return nil, nil, payment.ErrInvalid
	}
	var input payment.SettlementAutoInput
	if json.Unmarshal(raw, &input) != nil || payment.ValidateSettlementAutoInput(input) != nil {
		return nil, nil, payment.ErrInvalid
	}
	// The top-level pass rejects duplicate keys recursively. Decode the typed
	// nested whitelist as well, so a channel cannot smuggle a caller fee/account.
	var nested struct {
		Channels []payment.SettlementAutoChannel `json:"channels"`
	}
	if strictFundsJSON(append(append([]byte(`{"channels":`), fields["channels"]...), '}'), &nested) != nil {
		return nil, nil, payment.ErrInvalid
	}
	var channelFields []map[string]json.RawMessage
	if json.Unmarshal(fields["channels"], &channelFields) != nil {
		return nil, nil, payment.ErrInvalid
	}
	for _, ch := range channelFields {
		if len(ch) != 2 || !settlementAutoDecimal(ch["merchant_id"], false) || !settlementAutoDecimal(ch["channel_id"], false) {
			return nil, nil, payment.ErrInvalid
		}
	}
	sort.Slice(input.Channels, func(i, j int) bool { return input.Channels[i].MerchantID < input.Channels[j].MerchantID })
	fields["channels"], _ = json.Marshal(input.Channels)
	fields["valid_from"], _ = json.Marshal(input.ValidFrom.UTC().Format(time.RFC3339Nano))
	fields["expires_at"], _ = json.Marshal(input.ExpiresAt.UTC().Format(time.RFC3339Nano))
	canonical, e := json.Marshal(fields)
	return fields, canonical, e
}

func settlementAutoDecimal(raw json.RawMessage, zero bool) bool {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return false
	}
	n, e := strconv.ParseInt(text, 10, 64)
	return e == nil && n >= 0 && (zero || n > 0) && strconv.FormatInt(n, 10) == text
}

func settlementAutoInput(fields map[string]json.RawMessage) (payment.SettlementAutoInput, int64, error) {
	raw, _ := json.Marshal(fields)
	var in payment.SettlementAutoInput
	e := json.Unmarshal(raw, &in)
	revision, ok := positiveFundsID(fundsText(fields, "expected_revision"))
	if e != nil || !ok {
		return in, 0, payment.ErrInvalid
	}
	return in, revision, nil
}

func (s *Server) fundsSettlementAuto(ctx context.Context, q identity.FundsQuerier, d fundsTargetDefinition, fields map[string]json.RawMessage, summary map[string]string) (json.RawMessage, []payment.SettlementAutoChannel, error) {
	if s.ProviderFlow == nil {
		return nil, nil, payment.ErrLiveDisabled
	}
	id, _ := positiveFundsID(d.id)
	input, revision, e := settlementAutoInput(fields)
	if e != nil {
		return nil, nil, e
	}
	snapshot, e := s.ProviderFlow.SettlementAutoAuthorizationSnapshot(ctx, q, id, revision, input)
	if e != nil {
		return nil, nil, e
	}
	var approved struct {
		Scope struct {
			Static struct {
				Merchants []struct {
					MerchantID string                          `json:"merchant_id"`
					Recipient  payment.SettlementRecipientView `json:"recipient"`
				} `json:"merchants"`
			} `json:"static"`
			Channels []struct {
				MerchantID string `json:"merchant_id"`
				ChannelID  string `json:"channel_id"`
				Provider   string `json:"provider"`
				Product    string `json:"product"`
				AppID      string `json:"app_id"`
			} `json:"channels"`
		} `json:"scope"`
	}
	if e = json.Unmarshal(snapshot, &approved); e != nil {
		return nil, nil, e
	}
	recipients := []string{}
	channels := []string{}
	for _, m := range approved.Scope.Static.Merchants {
		recipients = append(recipients, m.MerchantID+": "+m.Recipient.Type+" "+m.Recipient.AccountMasked)
	}
	for _, c := range approved.Scope.Channels {
		channels = append(channels, fmt.Sprintf("%s: %s/%s 通道%s AppID%s", c.MerchantID, c.Provider, c.Product, c.ChannelID, c.AppID))
	}
	summary["plan_id"], summary["generation_authorization_id"], summary["expected_revision"] = d.id, fmt.Sprint(input.GenerationAuthorizationID), fmt.Sprint(revision)
	summary["recipients"], summary["channels"] = strings.Join(recipients, "; "), strings.Join(channels, "; ")
	summary["max_item_gross"], summary["max_run_gross"], summary["max_day_gross"] = ledger.Format(input.MaxItemGross), ledger.Format(input.MaxRunGross), ledger.Format(input.MaxDayGross)
	summary["max_day_items"], summary["valid_from"], summary["expires_at"] = fmt.Sprint(input.MaxDayItems), input.ValidFrom.UTC().Format(time.RFC3339Nano), input.ExpiresAt.UTC().Format(time.RFC3339Nano)
	summary["cost_policy"] = "上限仅含结算毛额（净额+平台手续费）；支付机构账户另收费不计入此账本，不保证其成本封顶"
	summary["scope"] = "仅原生成授权产生的原run申请；重新授权不重置该计划已消费的当日/本run额度"
	summary["revocation"] = "首次发送前可停止；结果未知只查询原交易，不重发；本授权不代表独立执行器已启动"
	return snapshot, input.Channels, nil
}

func (s *Server) settlementAutoConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, p []string) bool {
	if len(p) < 4 || p[1] != "settlement-plans" || (p[3] != "auto-authorization" && p[3] != "auto-authorizations") {
		return false
	}
	if role != "admin" || session.User.Role != role || session.User.AdminClass != "super" || session.Issuer != nil || session.User.ID <= 0 || session.User.Username == "" {
		response(w, 403, -1, "own super administrator session required", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "automatic settlement unavailable", nil)
		return true
	}
	id, ok := positiveFundsID(p[2])
	if !ok || r.URL.RawQuery != "" {
		response(w, 400, -1, "canonical plan without query required", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	actor := payment.SettlementPlanActor{Role: role, Class: session.User.AdminClass, ID: session.User.ID, Name: session.User.Username}
	if len(p) == 4 && p[3] == "auto-authorization" && r.Method == "GET" {
		view, e := s.Store.SettlementAutoAuthorization(ctx, id, actor)
		if e != nil {
			s.fail(w, e)
			return true
		}
		channels := []payment.SettlementAutoWalletChannel{}
		if s.ProviderFlow != nil {
			channels, e = s.ProviderFlow.SettlementAutoWalletChannelsForPlan(ctx, id, actor)
			if e != nil {
				s.fail(w, e)
				return true
			}
		}
		response(w, 200, 0, "independent authority does not imply executor running", struct {
			payment.SettlementAutoAuthorizationView
			Available      bool                                  `json:"available"`
			WalletChannels []payment.SettlementAutoWalletChannel `json:"wallet_channels"`
		}{view, true, channels})
		return true
	}
	if len(p) == 4 && p[3] == "auto-authorizations" && r.Method == "POST" {
		if s.ProviderFlow == nil {
			response(w, 503, -1, "wallet products unavailable", nil)
			return true
		}
		var fields map[string]json.RawMessage
		if !decodeFundsJSON(w, r, &fields) {
			return true
		}
		raw, _ := json.Marshal(fields)
		fields, _, e := normalizeSettlementAutoBody(raw)
		if e != nil {
			s.fail(w, e)
			return true
		}
		input, revision, e := settlementAutoInput(fields)
		if e != nil {
			s.fail(w, e)
			return true
		}
		view, e := s.ProviderFlow.AuthorizeSettlementAuto(ctx, id, revision, input, actor)
		if e != nil {
			s.fail(w, e)
		} else {
			response(w, 201, 0, "independent bounded payout authority created", view)
		}
		return true
	}
	if len(p) == 6 && p[3] == "auto-authorizations" && p[5] == "revoke" && r.Method == "POST" {
		aid, ok := positiveFundsID(p[4])
		if !ok {
			response(w, 400, -1, "canonical authority ID required", nil)
			return true
		}
		var empty struct{}
		if !decodeFundsJSON(w, r, &empty) {
			return true
		}
		if e := s.Store.RevokeSettlementAutoForPlan(ctx, id, aid, actor); e != nil {
			s.fail(w, e)
		} else {
			response(w, 200, 0, "unclaimed automatic payouts stopped", map[string]string{"authorization_id": strconv.FormatInt(aid, 10)})
		}
		return true
	}
	response(w, 405, -1, "explicit automatic authority method required", nil)
	return true
}
