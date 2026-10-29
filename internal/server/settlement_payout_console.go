package server

import (
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"strconv"
)

func settlementRequestView(v payment.LedgerRequest) map[string]any {
	return map[string]any{"id": fmt.Sprint(v.ID), "merchant_id": fmt.Sprint(v.MerchantID), "status": v.Status, "amount_cents": fmt.Sprint(v.Amount), "settlement": v.Settlement, "funding": v.Funding, "recipient": v.SettlementRecipient}
}

// The shared console middleware validates original roles, current accounts and
// CSRF. Read-only ownership checks remain here before every payout lookup.
func (s *Server) settlementPayoutConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "settlement-payouts" {
		return false
	}
	if len(parts) < 3 || len(parts) > 4 {
		response(w, 404, -1, "settlement payout not found", nil)
		return true
	}
	if s.Pool == nil || s.Store == nil {
		response(w, 503, -1, "settlement storage unavailable", nil)
		return true
	}
	id, e := strconv.ParseInt(parts[2], 10, 64)
	if e != nil || id <= 0 {
		response(w, 404, -1, "settlement payout not found", nil)
		return true
	}
	scope := session.User.ID
	if role == "admin" {
		scope = 0
	}
	var mid int64
	e = s.Pool.QueryRow(r.Context(), "SELECT merchant_id FROM ledger_requests WHERE id=$1 AND kind='settlements' AND ($2::bigint=0 OR merchant_id=$2)", id, scope).Scan(&mid)
	if errors.Is(e, pgx.ErrNoRows) {
		response(w, 404, -1, "settlement payout not found", nil)
		return true
	}
	if e != nil {
		s.fail(w, e)
		return true
	}
	request, e := s.Store.SettlementRequest(r.Context(), mid, id)
	if e != nil {
		s.fail(w, e)
		return true
	}
	fail := func(err error) {
		if errors.Is(err, payment.ErrLiveDisabled) || errors.Is(err, adapter.ErrUnsupported) {
			response(w, 503, -1, "settlement capability or writes unavailable", nil)
		} else {
			s.fail(w, err)
		}
	}
	if len(parts) == 3 && r.Method == "GET" {
		payout, err := s.Store.SettlementPayout(r.Context(), id)
		var out any
		confirmURL := ""
		if err == nil {
			out = payout
			// 原 settle.php 已完成结算的 jumpurl 确认收款二维码：微信转账待确认时可用。
			if payout.TransferID > 0 {
				links, e := s.transferConfirmURLs(r.Context(), []int64{payout.TransferID})
				if e != nil {
					s.fail(w, e)
					return true
				}
				confirmURL = links[payout.TransferID]
			}
		} else if !errors.Is(err, payment.ErrNotFound) {
			fail(err)
			return true
		}
		response(w, 200, 0, "approval and external payout are separate states", map[string]any{"request": settlementRequestView(request), "payout": out, "confirm_url": confirmURL})
		return true
	}
	if len(parts) != 4 {
		response(w, 405, -1, "method unavailable", nil)
		return true
	}
	if parts[3] == "capabilities" && r.Method == "GET" {
		if role != "admin" {
			response(w, 403, -1, "administrator required", nil)
			return true
		}
		s.settlementPayoutCapabilities(w, r, request)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "POST required", nil)
		return true
	}
	if role != "admin" && parts[3] != "query" {
		response(w, 403, -1, "administrator required", nil)
		return true
	}
	if s.ProviderFlow == nil {
		response(w, 503, -1, "settlement provider unavailable", nil)
		return true
	}
	actor := payment.TransferBatchActor{Role: role, Class: session.User.AdminClass, ID: session.User.ID, Name: session.User.Username}
	var payout payment.SettlementPayout
	switch parts[3] {
	case "prepare":
		var in struct {
			ChannelID string `json:"channel_id"`
		}
		if !decode(w, r, &in) {
			return true
		}
		channel, err := strconv.ParseInt(in.ChannelID, 10, 64)
		if err != nil || channel <= 0 || fmt.Sprint(channel) != in.ChannelID {
			response(w, 422, -1, "canonical channel_id required", nil)
			return true
		}
		payout, e = s.ProviderFlow.PrepareSettlementPayout(r.Context(), id, channel, actor, s.batchChannelCheck(mid))
	case "execute", "query", "cancel-unsent":
		var in struct{}
		if !decode(w, r, &in) {
			return true
		}
		switch parts[3] {
		case "execute":
			payout, e = s.ProviderFlow.ExecuteSettlementPayout(r.Context(), id, actor, s.batchChannelCheck(mid))
		case "query":
			payout, e = s.ProviderFlow.QuerySettlementPayout(r.Context(), id, actor)
		case "cancel-unsent":
			payout, e = s.ProviderFlow.CancelUnsentSettlementPayout(r.Context(), id, actor)
		}
	default:
		response(w, 404, -1, "settlement operation not found", nil)
		return true
	}
	if e != nil {
		// Only an actual claimed original intent may be reported as pending.
		// Missing/expired authorization remains an error before any send claim.
		var unknown *adapter.UnknownOutcome
		if errors.As(e, &unknown) && payout.TransferID > 0 && payout.SendState == "claimed" {
			response(w, 202, 0, "original payout outcome pending; query it without resubmitting", payout)
		} else {
			fail(e)
		}
		return true
	}
	response(w, 200, 0, "inspect payout state; approval alone is not paid", payout)
	return true
}

func (s *Server) settlementPayoutCapabilities(w http.ResponseWriter, r *http.Request, v payment.LedgerRequest) {
	channels := []map[string]string{}
	enabled := false
	if e := s.Pool.QueryRow(r.Context(), "SELECT enabled AND COALESCE(profile->>'transfer_api_enabled','false')='true' FROM merchants WHERE id=$1", v.MerchantID).Scan(&enabled); e != nil {
		s.fail(w, e)
		return
	}
	policy, e := s.Store.TransferPolicy(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	writeEnabled := s.ProviderFlow != nil && s.ProviderFlow.AllowLive && enabled && policy.Enabled
	reason := ""
	canPrepare := writeEnabled && v.Status == "approved" && v.Funding != nil && v.Funding.State == "reserved" && v.Funding.ReservedAmount == v.Amount && v.Settlement != nil && v.SettlementRecipient != nil && v.SettlementRecipient.ConfigID > 0
	if s.ProviderFlow != nil && v.SettlementRecipient != nil {
		for _, id := range s.MerchantChannels[v.MerchantID] {
			p := s.ProviderFlow.Providers[id]
			if p == nil {
				continue
			}
			caps, ok := p.(adapter.TransferCapabilities)
			if !ok {
				continue
			}
			if _, ok = p.(adapter.TransferProvider); !ok {
				continue
			}
			identity, ok := p.(adapter.ApplicationIdentity)
			if !ok || identity.ApplicationID() == "" {
				continue
			}
			if v.SettlementRecipient.AppID != "" && v.SettlementRecipient.AppID != identity.ApplicationID() {
				continue
			}
			for _, typ := range caps.TransferTypes() {
				if (typ == "alipay" || typ == "wxpay") && typ == v.SettlementRecipient.Type {
					channels = append(channels, map[string]string{"id": fmt.Sprint(id), "type": typ, "app_id": identity.ApplicationID()})
					break
				}
				if typ == "bank" && v.SettlementRecipient.Type == "bank" {
					product, ok := p.(adapter.BankTransferProduct)
					if !ok {
						continue
					}
					bank := product.BankTransferProduct()
					if !(p.ID() == "wxpay" && bank == adapter.BankProductWechat || p.ID() == "alipay" && bank == adapter.BankProductAlipay) {
						continue
					}
					channels = append(channels, map[string]string{"id": fmt.Sprint(id), "type": "bank", "app_id": identity.ApplicationID(), "bank_product": bank, "fee_policy": "hold-and-return-v1"})
					break
				}
			}
		}
	}
	canPrepare = canPrepare && len(channels) > 0
	if !writeEnabled {
		reason = "payment writes or merchant transfers disabled"
	} else if v.Status != "approved" {
		reason = "approved settlement required"
	} else if v.Funding == nil || v.Funding.State != "reserved" {
		reason = "original dedicated gross reservation required"
	} else if v.SettlementRecipient == nil || v.SettlementRecipient.ConfigID == 0 {
		reason = "original recipient snapshot required"
	} else if len(channels) == 0 {
		reason = "no matching payment application/product and permitted channel"
	}
	if _, e = s.Store.SettlementPayout(r.Context(), v.ID); e == nil {
		canPrepare = false
		reason = "original payout already prepared; use its existing transfer"
	} else if !errors.Is(e, payment.ErrNotFound) {
		s.fail(w, e)
		return
	}
	response(w, 200, 0, "local capabilities only; no provider request made", map[string]any{"request_id": fmt.Sprint(v.ID), "can_prepare": canPrepare, "writes_enabled": writeEnabled, "channels": channels, "reason": reason})
}
