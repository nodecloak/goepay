package backoffice

import (
	"encoding/json"
	"errors"
	"fmt"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"net/http"
	"strconv"
)

func requestJSON(v payment.LedgerRequest) map[string]any {
	out := map[string]any{"id": strconv.FormatInt(v.ID, 10), "merchant_id": strconv.FormatInt(v.MerchantID, 10), "external_no": v.ExternalNo, "kind": v.Kind, "status": v.Status, "amount": ledger.Format(v.Amount), "amount_cents": strconv.FormatInt(v.Amount, 10), "created_at": v.CreatedAt, "external_payout": false}
	if v.Kind == "settlements" {
		out["settlement"] = v.Settlement
		out["settlement_funding"] = v.Funding
		out["settlement_recipient"] = v.SettlementRecipient
	}
	if v.RecipientID != nil {
		out["recipient_id"] = strconv.FormatInt(*v.RecipientID, 10)
	}
	return out
}
func (s *Service) ServeWorkflows(w http.ResponseWriter, r *http.Request, p Principal, parts []string) {
	if p.SubjectID <= 0 || (p.Role != "admin" && p.Role != "merchant") {
		writeError(w, ErrForbidden)
		return
	}
	if s.serveFundsAvailability(w, r, p, parts) {
		return
	}
	if s.serveSettlementQuote(w, r, p, parts) {
		return
	}
	if s.serveSettlementRecipient(w, r, p, parts) {
		return
	}
	if s.serveProfitRule(w, r, p, parts) {
		return
	}
	if s.serveBusiness(w, r, p, parts) {
		return
	}
	if len(parts) == 0 || (parts[0] != "settlements" && parts[0] != "internal-transfers" && parts[0] != "profitsharing") {
		writeError(w, ErrNotFound)
		return
	}
	kind := parts[0]
	actor := fmt.Sprintf("%s:%d:%s", p.Role, p.SubjectID, p.Username)
	if len(parts) == 1 && r.Method == "POST" {
		in, ok := body(w, r)
		if !ok {
			return
		}
		for k := range in {
			if k != "merchant_id" && k != "recipient_id" && k != "amount" && k != "external_no" && k != "expected_fee_cents" && k != "expected_net_cents" && k != "recipient_config_id" {
				writeError(w, ErrInvalid)
				return
			}
		}
		v := decodeFields(mustJSON(in))
		merchantID := p.SubjectID
		if p.Role == "admin" {
			var e error
			merchantID, e = strconv.ParseInt(fmt.Sprint(v["merchant_id"]), 10, 64)
			if e != nil || merchantID <= 0 {
				writeError(w, ErrInvalid)
				return
			}
		} else if raw, ok := v["merchant_id"]; ok && fmt.Sprint(raw) != strconv.FormatInt(merchantID, 10) {
			writeError(w, ErrForbidden)
			return
		}
		recipientID := int64(0)
		if raw, ok := v["recipient_id"]; ok {
			recipientID, _ = strconv.ParseInt(fmt.Sprint(raw), 10, 64)
		}
		amount, e := ledger.Parse(fmt.Sprint(v["amount"]))
		if e != nil {
			writeError(w, ErrInvalid)
			return
		}
		externalNo, ok := v["external_no"].(string)
		if !ok {
			writeError(w, ErrInvalid)
			return
		}
		var expected *payment.SettlementExpectation
		fee, hasFee := v["expected_fee_cents"]
		net, hasNet := v["expected_net_cents"]
		if hasFee || hasNet {
			feeText, feeString := fee.(string)
			netText, netString := net.(string)
			feeValue, fe := strconv.ParseInt(feeText, 10, 64)
			netValue, ne := strconv.ParseInt(netText, 10, 64)
			if kind != "settlements" || !hasFee || !hasNet || !feeString || !netString || fe != nil || ne != nil || feeValue < 0 || netValue <= 0 || strconv.FormatInt(feeValue, 10) != feeText || strconv.FormatInt(netValue, 10) != netText {
				writeError(w, ErrInvalid)
				return
			}
			expected = &payment.SettlementExpectation{FeeAmount: feeValue, NetAmount: netValue}
		}
		var configID *int64
		if raw, exists := v["recipient_config_id"]; exists {
			text, ok := raw.(string)
			id, err := strconv.ParseInt(text, 10, 64)
			if kind != "settlements" || !ok || err != nil || id <= 0 || strconv.FormatInt(id, 10) != text {
				writeError(w, ErrInvalid)
				return
			}
			configID = &id
		}
		out, e := s.store.RequestFundsWithSettlementRecipient(r.Context(), kind, merchantID, recipientID, externalNo, amount, actor, expected, configID)
		if e != nil {
			writeError(w, e)
			return
		}
		write(w, 201, requestJSON(out), "funds reserved; no external payout")
		return
	}
	if len(parts) == 1 && r.Method == "GET" {
		page, limit, mid, e := workflowListFilters(r, p)
		if e != nil {
			writeError(w, e)
			return
		}
		var total int64
		if e := s.pool.QueryRow(r.Context(), "SELECT count(*) FROM ledger_requests WHERE kind=$1 AND ($2::bigint=0 OR merchant_id=$2)", kind, mid).Scan(&total); e != nil {
			writeError(w, e)
			return
		}
		rows, e := s.pool.Query(r.Context(), "SELECT id,merchant_id,recipient_id,external_no,amount,status,created_at,"+payment.SettlementTermsJSON+" FROM ledger_requests WHERE kind=$1 AND ($2::bigint=0 OR merchant_id=$2) ORDER BY id DESC LIMIT $3 OFFSET $4", kind, mid, limit, (page-1)*limit)
		if e != nil {
			writeError(w, e)
			return
		}
		defer rows.Close()
		items := []map[string]any{}
		requests := []payment.LedgerRequest{}
		for rows.Next() {
			v := payment.LedgerRequest{Kind: kind}
			var terms []byte
			if e = rows.Scan(&v.ID, &v.MerchantID, &v.RecipientID, &v.ExternalNo, &v.Amount, &v.Status, &v.CreatedAt, &terms); e != nil {
				writeError(w, e)
				return
			}
			if len(terms) > 0 {
				if e = json.Unmarshal(terms, &v.Settlement); e != nil {
					writeError(w, e)
					return
				}
			}
			requests = append(requests, v)
		}
		if e = rows.Err(); e != nil {
			writeError(w, e)
			return
		}
		rows.Close()
		for _, v := range requests {
			var payout *payment.SettlementPayout
			if kind == "settlements" {
				v, e = s.store.SettlementRequest(r.Context(), v.MerchantID, v.ID)
				if e != nil {
					writeError(w, e)
					return
				}
				original, payoutErr := s.store.SettlementPayout(r.Context(), v.ID)
				if payoutErr == nil {
					payout = &original
				} else if !errors.Is(payoutErr, payment.ErrNotFound) {
					writeError(w, payoutErr)
					return
				}
			}
			item := requestJSON(v)
			if kind == "settlements" {
				item["payout"] = payout
			}
			items = append(items, item)
		}
		write(w, 200, map[string]any{"items": items, "total": total, "page": page, "limit": limit}, "")
		return
	}
	if len(parts) == 3 && r.Method == "POST" && (parts[2] == "approve" || parts[2] == "reject") {
		if p.Role != "admin" {
			writeError(w, ErrForbidden)
			return
		}
		id, e := strconv.ParseInt(parts[1], 10, 64)
		if e != nil {
			writeError(w, ErrInvalid)
			return
		}
		var actual string
		if e = s.pool.QueryRow(r.Context(), "SELECT kind FROM ledger_requests WHERE id=$1", id).Scan(&actual); e != nil {
			writeError(w, businessError(e))
			return
		}
		if actual != kind {
			writeError(w, ErrNotFound)
			return
		}
		out, e := s.store.ReviewFundsRequest(r.Context(), id, parts[2] == "approve", actor)
		if e != nil {
			writeError(w, e)
			return
		}
		write(w, 200, requestJSON(out), "ledger review recorded; no external payout")
		return
	}
	write(w, 405, nil, "method not allowed")
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
