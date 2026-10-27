package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) transferConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "transfers" {
		return false
	}
	if len(parts) == 2 && r.Method == "GET" {
		return false
	} // common paginated list
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "database unavailable", nil)
		return true
	}
	if len(parts) == 3 && parts[2] == "capabilities" && r.Method == "GET" {
		mid := session.User.ID
		if role == "admin" {
			var e error
			mid, e = strconv.ParseInt(r.URL.Query().Get("merchant_id"), 10, 64)
			if e != nil || mid <= 0 {
				response(w, 422, -1, "merchant_id required", nil)
				return true
			}
		}
		var enabled, merchantEnabled bool
		var rate string
		e := s.Pool.QueryRow(r.Context(), "SELECT enabled,COALESCE(profile->>'transfer_api_enabled','false')='true',COALESCE(profile->>'transfer_rate','0') FROM merchants WHERE id=$1", mid).Scan(&enabled, &merchantEnabled, &rate)
		if e != nil {
			response(w, 404, -1, "merchant not found", nil)
			return true
		}
		channels := []map[string]any{}
		if s.ProviderFlow != nil {
			for _, id := range s.MerchantChannels[mid] {
				p := s.ProviderFlow.Providers[id]
				if caps, ok := p.(adapter.TransferCapabilities); ok {
					if _, ok = p.(adapter.TransferProvider); ok && len(caps.TransferTypes()) > 0 {
						_, proof := p.(adapter.TransferProofProvider)
						ali := false
						bankProduct := ""
						for _, typ := range caps.TransferTypes() {
							if typ == "alipay" {
								ali = true
							}
							if typ == "bank" {
								bankProduct = adapter.BankProductWechat
								if bank, ok := p.(adapter.BankTransferProduct); ok {
									bankProduct = bank.BankTransferProduct()
								}
							}
						}
						proof = proof && ali
						channels = append(channels, map[string]any{"id": fmt.Sprint(id), "types": caps.TransferTypes(), "proof": proof, "bank_product": bankProduct, "bank_code_required": bankProduct == adapter.BankProductWechat})
					}
				}
			}
		}
		b, e := s.Store.FundsAvailability(r.Context(), mid)
		if e != nil {
			s.fail(w, e)
			return true
		}
		policy, access, e := s.Store.ManualTransferPolicy(r.Context(), mid)
		if e != nil {
			s.fail(w, e)
			return true
		}
		writes := s.ProviderFlow != nil && s.ProviderFlow.AllowLive && enabled && merchantEnabled && access.Enabled && len(channels) > 0
		response(w, 200, 0, "", map[string]any{"merchant_id": fmt.Sprint(mid), "writes_enabled": writes, "platform_enabled": access.PlatformEnabled, "effective_enabled": access.Enabled, "permission_source": access.Source, "effective_group_id": fmt.Sprint(access.GroupID), "merchant_enabled": merchantEnabled && enabled, "transfer_rate": rate, "channels": channels, "available_money": ledger.Format(b.Available), "ledger_available_money": ledger.Format(b.LedgerAvailable), "today_blocked_money": ledger.Format(b.Blocked), "settle_type": b.SettleType, "minimum_money": ledger.Format(policy.MinimumAmount), "maximum_money": ledger.Format(policy.MaximumAmount), "daily_recipient_limit": policy.DailyRecipientLimit, "default_remark": policy.DefaultRemark, "default_title": policy.DefaultTitle})
		return true
	}
	if len(parts) == 2 && r.Method == "POST" {
		var in struct {
			MerchantID string `json:"merchant_id"`
			ChannelID  string `json:"channel_id"`
			Type       string `json:"type"`
			Account    string `json:"account"`
			Name       string `json:"name"`
			Amount     string `json:"amount"`
			Remark     string `json:"remark"`
			ExternalNo string `json:"external_no"`
			BankCode   string `json:"bank_code"`
		}
		if !decode(w, r, &in) {
			return true
		}
		mid := session.User.ID
		if role == "admin" {
			var e error
			mid, e = strconv.ParseInt(in.MerchantID, 10, 64)
			if e != nil || mid <= 0 {
				response(w, 422, -1, "merchant_id required", nil)
				return true
			}
		} else if in.MerchantID != "" && in.MerchantID != fmt.Sprint(mid) {
			response(w, 403, -1, "merchant ownership required", nil)
			return true
		}
		if s.ProviderFlow == nil {
			response(w, 503, -1, "transfer provider unavailable", nil)
			return true
		}
		amount, e := ledger.Parse(in.Amount)
		if e != nil || amount <= 0 || in.ExternalNo == "" || len(in.ExternalNo) > 128 {
			response(w, 422, -1, "valid amount and external_no required", nil)
			return true
		}
		channel, e := s.transferChannel(mid, compat.Params{"type": in.Type, "channel_id": in.ChannelID})
		if e != nil {
			response(w, 422, -1, "transfer channel not permitted or unsupported", nil)
			return true
		}
		req := adapter.TransferRequest{Type: in.Type, Account: in.Account, Name: in.Name, Amount: amount, Remark: in.Remark, BankCode: in.BankCode}
		if req.Type == "alipay" {
			req.Title = req.Remark
			req.Remark = ""
		}
		finish, e := s.transferActionAudit(r.Context(), role, session, "create", payment.Transfer{MerchantID: mid, ChannelID: channel, TransferNo: in.ExternalNo, Amount: amount})
		if e != nil {
			s.fail(w, e)
			return true
		}
		var transfer payment.Transfer
		defer func() {
			state := "rejected"
			if transfer.ID > 0 {
				state = transfer.Status
			}
			finish(transfer, state)
		}()
		transfer, e = s.ProviderFlow.Transfer(r.Context(), mid, channel, in.ExternalNo, req)
		if isFundsAuthorizationError(e) {
			fundsHTTPError(w, e)
			return true
		}
		if e != nil && transfer.ID > 0 && transfer.Status == "failed" {
			response(w, 200, 0, "transfer definitively rejected; funds released", transfer)
			return true
		}
		if e != nil && transfer.ID > 0 {
			response(w, 202, 0, "transfer intent persisted; query the same transfer", transfer)
			return true
		}
		if e != nil {
			if errors.Is(e, payment.ErrLiveDisabled) {
				response(w, 503, -1, "transfer writes disabled", nil)
			} else {
				s.fail(w, e)
			}
			return true
		}
		response(w, 200, 0, "transfer result", transfer)
		return true
	}
	if len(parts) < 3 || len(parts) > 4 {
		response(w, 404, -1, "transfer operation unavailable", nil)
		return true
	}
	transferID, e := strconv.ParseInt(parts[2], 10, 64)
	if e != nil || transferID <= 0 {
		response(w, 404, -1, "transfer not found", nil)
		return true
	}
	mid := session.User.ID
	if role == "admin" {
		mid = 0
	}
	var owner int64
	var no string
	e = s.Pool.QueryRow(r.Context(), "SELECT merchant_id,transfer_no FROM transfers WHERE id=$1 AND ($2::bigint=0 OR merchant_id=$2)", transferID, mid).Scan(&owner, &no)
	if e != nil {
		response(w, 404, -1, "transfer not found", nil)
		return true
	}
	transfer, e := s.Store.Transfer(r.Context(), owner, no)
	if e != nil {
		s.fail(w, e)
		return true
	}
	if len(parts) == 3 && r.Method == "GET" {
		response(w, 200, 0, "", map[string]any{"transfer": transfer, "recipient": map[string]any{"type": transfer.Type, "account": transfer.Account, "name": transfer.Name, "title": transfer.Title, "bank_code": transfer.BankCode}})
		return true
	}
	if len(parts) != 4 || r.Method != "POST" {
		response(w, 405, -1, "method unavailable", nil)
		return true
	}
	if s.ProviderFlow == nil {
		response(w, 503, -1, "transfer provider unavailable", nil)
		return true
	}
	switch parts[3] {
	case "query":
		finish, ae := s.transferActionAudit(r.Context(), role, session, "query", transfer)
		if ae != nil {
			s.fail(w, ae)
			return true
		}
		outcome := "unknown"
		defer func() { finish(transfer, outcome) }()
		current, qe := s.ProviderFlow.QueryTransfer(r.Context(), owner, no)
		if qe != nil {
			if current.ID > 0 {
				transfer = current
			}
			response(w, 202, 0, "verified query unresolved; no unverified balance credit applied", transfer)
			return true
		}
		transfer = current
		outcome = current.Status
		response(w, 200, 0, "verified provider transfer result", current)
	case "proof":
		finish, ae := s.transferActionAudit(r.Context(), role, session, "proof", transfer)
		if ae != nil {
			s.fail(w, ae)
			return true
		}
		outcome := "unknown"
		defer func() { finish(transfer, outcome) }()
		proof, pe := s.ProviderFlow.TransferProof(r.Context(), owner, no)
		if pe != nil {
			var unknown *adapter.UnknownOutcome
			if errors.As(pe, &unknown) {
				response(w, 202, 0, "proof application unresolved; it will not be submitted again", nil)
			} else {
				response(w, 503, -1, "proof unavailable or not enabled", nil)
			}
			return true
		}
		outcome = proof.State
		response(w, 200, 0, "", map[string]any{"file_id": proof.FileID, "state": proof.State, "download_url": proof.DownloadURL})
	default:
		response(w, 404, -1, "transfer operation unavailable", nil)
	}
	return true
}

// Audit a bounded allowlist before any provider call. Recipient identities and
// receipt bearer URLs deliberately never enter the actor audit.
func (s *Server) transferActionAudit(ctx context.Context, role string, session identity.Session, operation string, t payment.Transfer) (func(payment.Transfer, string), error) {
	changes := map[string]any{"merchant_id": fmt.Sprint(t.MerchantID), "channel_id": fmt.Sprint(t.ChannelID), "external_no": t.TransferNo, "amount": ledger.Format(t.Amount)}
	raw, e := json.Marshal(changes)
	if e != nil {
		return nil, e
	}
	var intent int64
	e = s.Pool.QueryRow(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'transfers',$4,$5,$6) RETURNING id", role, session.User.ID, session.User.Username, t.ID, operation+"-intent", raw).Scan(&intent)
	if e != nil {
		return nil, e
	}
	return func(current payment.Transfer, state string) {
		changes["intent_id"] = fmt.Sprint(intent)
		changes["state"] = state
		changes["transfer_id"] = fmt.Sprint(current.ID)
		changes["biz_no"] = current.BizNo
		raw, e := json.Marshal(changes)
		if e != nil {
			return
		}
		bounded, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, e = s.Pool.Exec(bounded, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'transfers',$4,$5,$6)", role, session.User.ID, session.User.Username, current.ID, operation+"-result", raw); e != nil {
			slog.Error("console transfer outcome audit failed", "intent_id", intent)
		}
	}, nil
}

// transferAPI is reached only after the shared merchant RSA authentication.
func (s *Server) transferAPI(w http.ResponseWriter, r *http.Request, id int64, p compat.Params, route string) {
	if route == "/api/transfer/balance" {
		if e := s.Store.CheckMerchantAPI(r.Context(), id, "transfer"); e != nil {
			s.wire(w, false, nil, "merchant transfer API unavailable")
			return
		}
		s.transferRead(w, r, id, p, route)
		return
	}
	if route == "/api/transfer/submit" {
		if s.ProviderFlow == nil || !s.ProviderFlow.AllowLive {
			s.wire(w, false, nil, "transfer writes disabled")
			return
		}
		if p["bookid"] != "" {
			s.wire(w, false, nil, "book transfer capability unavailable")
			return
		}
		amount, e := ledger.Parse(p["money"])
		if e != nil || amount <= 0 {
			s.wire(w, false, nil, "invalid transfer amount")
			return
		}
		channel, e := s.transferChannel(id, p)
		if e != nil {
			s.wire(w, false, nil, "transfer channel not permitted or unsupported")
			return
		}
		no := p["out_biz_no"]
		if no == "" {
			if e = s.Pool.QueryRow(r.Context(), "SELECT nextval('trade_no_seq')::text").Scan(&no); e != nil {
				s.wire(w, false, nil, "transfer unavailable")
				return
			}
		}
		req := adapter.TransferRequest{Type: p["type"], Account: p["account"], Name: p["name"], Amount: amount, Remark: p["remark"], BankCode: p["bank_code"]}
		if req.Type == "alipay" {
			req.Title = req.Remark
			req.Remark = ""
		}
		transfer, e := s.ProviderFlow.TransferFromAPI(r.Context(), id, channel, no, req)
		if transfer.ID == 0 {
			s.wire(w, false, nil, "transfer rejected")
			return
		}
		// An uncertain upstream outcome retains its original identifier and reserve.
		// Returning it allows the client to query instead of creating another intent.
		out := transferWire(transfer)
		if transfer.Status == "failed" {
			s.wire(w, false, out, "transfer failed; funds released")
			return
		}
		if e != nil {
			out["query_required"] = true
		}
		s.wire(w, false, out, "")
		return
	}
	var transfer payment.Transfer
	var e error
	if p["out_biz_no"] != "" {
		transfer, e = s.Store.Transfer(r.Context(), id, p["out_biz_no"])
	} else if p["biz_no"] != "" {
		transfer, e = s.Store.TransferByBizNo(r.Context(), id, p["biz_no"])
	} else {
		s.wire(w, false, nil, "transfer number required")
		return
	}
	if e != nil || (p["biz_no"] != "" && p["biz_no"] != transfer.BizNo && p["biz_no"] != fmt.Sprint(transfer.ID)) {
		s.wire(w, false, nil, "transfer not found")
		return
	}
	if route == "/api/transfer/proof" {
		if s.ProviderFlow == nil {
			s.wire(w, false, nil, "transfer proof unavailable")
			return
		}
		proof, e := s.ProviderFlow.TransferProof(r.Context(), id, transfer.TransferNo)
		if e != nil {
			s.wire(w, false, nil, "transfer proof requires query or is unavailable")
			return
		}
		s.wire(w, false, map[string]any{"biz_no": transfer.BizNo, "out_biz_no": transfer.TransferNo, "file_id": proof.FileID, "status": proof.State, "url": proof.DownloadURL}, "")
		return
	}
	if transfer.ChannelID > 0 && s.ProviderFlow != nil {
		updated, qe := s.ProviderFlow.QueryTransfer(r.Context(), id, transfer.TransferNo)
		if qe == nil {
			transfer = updated
		} else {
			if updated.ID > 0 {
				transfer = updated
			}
			out := transferWire(transfer)
			out["query_required"] = true
			s.wire(w, false, out, "")
			return
		}
	}
	s.wire(w, false, transferWire(transfer), "")
}
func transferWire(t payment.Transfer) map[string]any {
	status := 0
	if t.Status == "succeeded" {
		status = 1
	} else if t.Status == "failed" || t.Status == "reversed" {
		status = 2
	}
	out := map[string]any{"status": status, "biz_no": t.BizNo, "out_biz_no": t.TransferNo, "orderid": t.ProviderTransferNo, "paydate": t.PayDate, "amount": ledger.Format(t.Amount), "cost_money": ledger.Format(t.CostAmount), "remark": t.Remark}
	if t.BankLifecycle != "" {
		out["bank_product"] = t.BankProduct
		out["bank_estimated_fee_amount"] = ledger.Format(t.BankEstimatedFeeAmount)
		out["bank_estimated_fee_known"] = t.BankEstimatedFeeKnown
		out["bank_lifecycle"] = t.BankLifecycle
		out["ledger_status"] = t.LedgerStatus
		out["bank_fee_amount"] = ledger.Format(t.BankFeeAmount)
		out["bank_fee_known"] = t.BankFeeKnown
		out["bank_service_fee_held"] = ledger.Format(t.BankServiceFeeHeld)
		out["bank_returned_principal"] = ledger.Format(t.BankReturnedPrincipal)
		out["bank_returned_service_fee"] = ledger.Format(t.BankReturnedServiceFee)
		out["bank_returned_fee"] = ledger.Format(t.BankReturnedFee)
		out["bank_reversal_journals"] = t.BankReversalJournals
	}
	if status == 2 {
		out["errmsg"] = "transfer failed"
	}
	return out
}
func (s *Server) transferChannel(mid int64, p compat.Params) (int64, error) {
	requested := int64(0)
	if p["channel_id"] != "" {
		var e error
		requested, e = strconv.ParseInt(p["channel_id"], 10, 64)
		if e != nil || requested <= 0 {
			return 0, payment.ErrInvalid
		}
	}
	for _, id := range s.MerchantChannels[mid] {
		if requested != 0 && requested != id {
			continue
		}
		candidate := s.ProviderFlow.Providers[id]
		capabilities, ok := candidate.(adapter.TransferCapabilities)
		if !ok {
			continue
		}
		if _, ok = candidate.(adapter.TransferProvider); !ok {
			continue
		}
		for _, typ := range capabilities.TransferTypes() {
			if typ == p["type"] {
				return id, nil
			}
		}
	}
	return 0, payment.ErrInvalid
}
