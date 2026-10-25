package server

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

var sharingConsoleNo = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type sharingConsoleInput struct {
	MerchantID        string `json:"merchant_id,omitempty"`
	ChannelID         string `json:"channel_id,omitempty"`
	ExternalNo        string `json:"external_no,omitempty"`
	TradeNo           string `json:"trade_no,omitempty"`
	SharingExternalNo string `json:"sharing_external_no,omitempty"`
	ReceiverID        string `json:"receiver_id,omitempty"`
	Amount            string `json:"amount,omitempty"`
	Type              string `json:"type,omitempty"`
	Account           string `json:"account,omitempty"`
	Name              string `json:"name,omitempty"`
	RelationType      string `json:"relation_type,omitempty"`
	CustomRelation    string `json:"custom_relation,omitempty"`
	Expected          string `json:"expected,omitempty"`
	HasMerchant       bool   `json:"-"`
	payment.ChannelSharingRuleInput
}

func sharingConsoleMerchant(role string, session identity.Session, raw string) (int64, error) {
	if role == "merchant" && raw == "" && session.User.ID > 0 {
		return session.User.ID, nil
	}
	if role != "admin" || session.User.Role != "admin" {
		return 0, payment.ErrInvalid
	}
	id, ok := identityPositiveID(raw)
	if !ok {
		return 0, payment.ErrInvalid
	}
	return id, nil
}

func sharingConsoleQuery(r *http.Request, allowed ...string) bool {
	keys := map[string]bool{}
	for _, k := range allowed {
		keys[k] = true
	}
	for k, values := range r.URL.Query() {
		if !keys[k] || len(values) != 1 {
			return false
		}
	}
	return true
}

func sharingConsoleBody(w http.ResponseWriter, r *http.Request, parts []string, out *sharingConsoleInput) bool {
	var fields map[string]json.RawMessage
	if !decodeFundsJSON(w, r, &fields) {
		return false
	}
	allowed := []string{"merchant_id"}
	if d, ok := channelSharingFundsTarget(parts[1:]); ok {
		allowed = d.keys
	} else if len(parts) == 3 && parts[2] == "receivers" {
		allowed = append(allowed, "channel_id", "external_no", "type", "account", "name", "relation_type", "custom_relation")
	}
	keys := map[string]bool{}
	for _, key := range allowed {
		keys[key] = true
	}
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			response(w, 400, -1, "sharing fields cannot be null", nil)
			return false
		}
		if !keys[key] {
			response(w, 400, -1, "unexpected sharing field", nil)
			return false
		}
	}
	if d, ok := channelSharingFundsTarget(parts[1:]); ok {
		for _, key := range d.keys {
			if key != "merchant_id" {
				if _, exists := fields[key]; !exists {
					response(w, 400, -1, "required sharing field missing", nil)
					return false
				}
			}
		}
	}
	raw, _ := json.Marshal(fields)
	if strictFundsJSON(raw, out) != nil {
		response(w, 400, -1, "invalid sharing fields", nil)
		return false
	}
	_, out.HasMerchant = fields["merchant_id"]
	return true
}

func (s *Server) sharingConsoleChannel(mid, channel int64) bool {
	for _, id := range s.MerchantChannels[mid] {
		if id == channel {
			return true
		}
	}
	return false
}

// Shared console middleware verifies the durable account and administrator role.
// This hook also checks role/CSRF locally, including direct handler tests.
func (s *Server) channelSharingConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "channel-sharing" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if len(parts) < 3 || len(parts) > 5 {
		response(w, 404, -1, "channel sharing operation not found", nil)
		return true
	}
	if role != session.User.Role || parts[0] != role || session.User.ID <= 0 || role != "admin" && role != "merchant" || role == "admin" && !adminOperationAllowed(session.User.AdminClass, parts) {
		response(w, 403, -1, "channel sharing permission required", nil)
		return true
	}
	if r.Method != "GET" && (session.CSRF == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "channel sharing unavailable", nil)
		return true
	}
	actor := payment.TransferBatchActor{Role: role, Class: session.User.AdminClass, ID: session.User.ID, Name: session.User.Username}
	fail := func(e error) {
		var unknown *adapter.UnknownOutcome
		var rejected *adapter.ProviderError
		switch {
		case errors.As(e, &unknown):
			response(w, 409, -1, "upstream outcome unknown; query the original request only", nil)
		case errors.As(e, &rejected):
			response(w, 422, -1, "upstream rejected this request; inspect the original operation before any explicit new intent", nil)
		case errors.Is(e, adapter.ErrUnsupported), errors.Is(e, payment.ErrLiveDisabled):
			response(w, 503, -1, "current channel does not support channel sharing or writes are disabled", nil)
		default:
			s.fail(w, e)
		}
	}
	var in sharingConsoleInput
	if r.Method != "GET" {
		if !sharingConsoleQuery(r) {
			response(w, 400, -1, "query parameters are not accepted for sharing writes", nil)
			return true
		}
		// Strict duplicate/unknown-member rejection also applies to nested rules.
		if !sharingConsoleBody(w, r, parts, &in) {
			return true
		}
	} else {
		allowed := []string{"merchant_id"}
		if len(parts) == 3 && (parts[2] == "receivers" || parts[2] == "orders" || parts[2] == "returns") {
			allowed = append(allowed, "cursor", "limit")
		}
		if len(parts) == 3 && parts[2] == "finishes" {
			allowed = append(allowed, "limit")
		}
		if !sharingConsoleQuery(r, allowed...) {
			response(w, 400, -1, "invalid sharing query", nil)
			return true
		}
	}
	rawMerchant := in.MerchantID
	if r.Method == "GET" {
		rawMerchant = r.URL.Query().Get("merchant_id")
	}
	if role == "merchant" && (in.HasMerchant || r.URL.Query().Has("merchant_id")) {
		response(w, 403, -1, "merchant scope is determined by the session", nil)
		return true
	}
	mid, e := sharingConsoleMerchant(role, session, rawMerchant)
	if e != nil {
		fail(e)
		return true
	}
	respond := func(v any, e error) {
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "", v)
		}
	}
	resource := parts[2]
	if resource == "finishes" || resource == "finish-preview" {
		s.channelSharingFinishConsole(w, r, mid, actor, parts, in, fail)
		return true
	}
	if resource == "returns" {
		s.channelSharingReturnConsole(w, r, mid, actor, parts, in, fail)
		return true
	}
	if resource == "channels" && len(parts) == 3 && r.Method == "GET" {
		channels := []payment.ChannelSharingChannelView{}
		reason := "当前未配置支持渠道分账的通道；模拟通道不执行真实分账"
		if s.ProviderFlow != nil && !s.Sandbox && !s.ProviderFlow.LocalSimulation && s.ProviderFlow.AllowLive {
			all, err := s.ProviderFlow.ChannelSharingChannels(r.Context(), mid, actor)
			if err != nil {
				fail(err)
				return true
			}
			for _, ch := range all {
				if s.sharingConsoleChannel(mid, ch.ChannelID) {
					channels = append(channels, ch)
				}
			}
		}
		if len(channels) > 0 {
			reason = ""
		}
		response(w, 200, 0, "", map[string]any{"merchant_id": strconv.FormatInt(mid, 10), "available": len(channels) > 0, "reason": reason, "channels": channels})
		return true
	}
	if len(parts) == 3 && r.Method == "GET" && (resource == "receivers" || resource == "orders") {
		cursor, limit := int64(0), 20
		if raw := r.URL.Query().Get("cursor"); raw != "" {
			cursor, e = strconv.ParseInt(raw, 10, 64)
			if e != nil || cursor < 0 || strconv.FormatInt(cursor, 10) != raw {
				fail(payment.ErrInvalid)
				return true
			}
		}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, e = strconv.Atoi(raw)
			if e != nil || limit < 1 || limit > 100 || strconv.Itoa(limit) != raw {
				fail(payment.ErrInvalid)
				return true
			}
		}
		if resource == "receivers" {
			v, err := s.Store.ListChannelSharingReceivers(r.Context(), mid, cursor, limit, actor)
			respond(v, err)
		} else {
			v, err := s.Store.ListChannelSharingOrders(r.Context(), mid, cursor, limit, actor)
			respond(v, err)
		}
		return true
	}
	if resource == "payment" && len(parts) == 4 && r.Method == "GET" {
		v, err := s.Store.ChannelSharingPayment(r.Context(), mid, parts[3], actor)
		respond(v, err)
		return true
	}
	if resource == "orders" && len(parts) == 4 && r.Method == "GET" {
		v, err := s.Store.ChannelSharingOrder(r.Context(), mid, parts[3], actor)
		respond(v, err)
		return true
	}
	if resource == "receivers" && len(parts) == 4 && r.Method == "GET" {
		id, ok := identityPositiveID(parts[3])
		if !ok {
			fail(payment.ErrInvalid)
			return true
		}
		v, err := s.Store.ChannelSharingReceiver(r.Context(), mid, id, actor)
		respond(v, err)
		return true
	}
	if resource == "rules" && len(parts) >= 4 {
		channel, ok := identityPositiveID(parts[3])
		if !ok {
			fail(payment.ErrInvalid)
			return true
		}
		if r.Method == "GET" && len(parts) == 4 {
			v, err := s.Store.ChannelSharingRule(r.Context(), mid, channel, actor)
			respond(v, err)
			return true
		}
		if r.Method == "POST" {
			expected, err := strconv.ParseInt(in.Expected, 10, 64)
			if err != nil || expected < 0 || strconv.FormatInt(expected, 10) != in.Expected {
				fail(payment.ErrInvalid)
				return true
			}
			if len(parts) == 5 && parts[4] == "disable" {
				v, err := s.Store.DisableChannelSharingRule(r.Context(), mid, channel, expected, actor)
				respond(v, err)
				return true
			}
			if len(parts) == 4 && s.ProviderFlow != nil && !s.Sandbox && !s.ProviderFlow.LocalSimulation && s.sharingConsoleChannel(mid, channel) {
				v, err := s.ProviderFlow.SetChannelSharingRule(r.Context(), mid, channel, in.ChannelSharingRuleInput, expected, actor)
				respond(v, err)
				return true
			}
		}
	}
	if s.ProviderFlow == nil || s.Sandbox || s.ProviderFlow.LocalSimulation {
		fail(adapter.ErrUnsupported)
		return true
	}
	if resource == "receivers" && len(parts) == 3 && r.Method == "POST" {
		channel, ok := identityPositiveID(in.ChannelID)
		if !ok || !s.sharingConsoleChannel(mid, channel) {
			fail(payment.ErrInvalid)
			return true
		}
		v, err := s.ProviderFlow.CreateChannelSharingReceiver(r.Context(), mid, channel, in.ExternalNo, adapter.SharingReceiver{Type: in.Type, Account: in.Account, Name: in.Name, RelationType: in.RelationType, CustomRelation: in.CustomRelation}, actor)
		respond(v, err)
		return true
	}
	if resource == "receivers" && len(parts) == 5 && r.Method == "POST" {
		id, ok := identityPositiveID(parts[3])
		if !ok {
			fail(payment.ErrInvalid)
			return true
		}
		original, err := s.Store.ChannelSharingReceiver(r.Context(), mid, id, actor)
		if err != nil {
			fail(err)
			return true
		}
		if !s.sharingConsoleChannel(mid, original.ChannelID) {
			fail(payment.ErrConflict)
			return true
		}
		if parts[4] == "bind" {
			v, err := s.ProviderFlow.BindChannelSharingReceiver(r.Context(), mid, id, in.ExternalNo, actor)
			respond(v, err)
			return true
		}
		if parts[4] == "unbind" {
			v, err := s.ProviderFlow.UnbindChannelSharingReceiver(r.Context(), mid, id, in.ExternalNo, actor)
			respond(v, err)
			return true
		}
	}
	if resource == "orders" && len(parts) == 3 && r.Method == "POST" {
		original, err := s.Store.ChannelSharingPayment(r.Context(), mid, in.TradeNo, actor)
		if err != nil {
			fail(err)
			return true
		}
		if !s.sharingConsoleChannel(mid, original.ChannelID) {
			fail(payment.ErrConflict)
			return true
		}
		v, err := s.ProviderFlow.SubmitChannelSharing(r.Context(), mid, in.TradeNo, in.ExternalNo, actor)
		respond(v, err)
		return true
	}
	if resource == "orders" && len(parts) == 5 && parts[4] == "query" && r.Method == "POST" {
		v, err := s.ProviderFlow.QueryChannelSharing(r.Context(), mid, parts[3], actor)
		respond(v, err)
		return true
	}
	response(w, 405, -1, "channel sharing operation or method unavailable", nil)
	return true
}
