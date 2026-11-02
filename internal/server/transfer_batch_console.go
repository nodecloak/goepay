package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/backoffice"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"net/http"
	"strconv"
	"time"
)

type batchConsoleItem struct {
	ChannelID string `json:"channel_id"`
	Type      string `json:"type"`
	Account   string `json:"account"`
	Name      string `json:"name"`
	Amount    string `json:"amount"`
	Remark    string `json:"remark"`
	BankCode  string `json:"bank_code"`
}

func (s *Server) transferBatchConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "transfer-batches" {
		return false
	}
	// Closing the feature stops new preparation and execution. Original lists,
	// details, query and proven-unsent cancellation remain owner/CSRF protected
	// below so an unknown result can still be reconciled without another send.
	newUse := r.Method == "POST" && (len(parts) == 2 || len(parts) == 4 && parts[3] == "execute")
	if role == "merchant" && newUse && s.Pool != nil && s.Store != nil {
		on, e := backoffice.New(s.Pool, s.Store).MerchantFeatureVisibleForMerchant(r.Context(), session.User.ID, "user_transfer_batch")
		if e != nil {
			response(w, 503, -1, "merchant feature unavailable", nil)
			return true
		}
		if !on {
			response(w, 403, -1, "未开启批量代付功能", nil)
			return true
		}
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "transfer service unavailable", nil)
		return true
	}
	actor := payment.TransferBatchActor{Role: role, Class: session.User.AdminClass, ID: session.User.ID, Name: session.User.Username}
	fail := func(e error) {
		if errors.Is(e, payment.ErrLiveDisabled) || errors.Is(e, adapter.ErrUnsupported) {
			response(w, 503, -1, "transfer capability or writes unavailable", nil)
		} else {
			s.fail(w, e)
		}
	}
	if len(parts) == 2 {
		mid := session.User.ID
		if r.Method == "GET" {
			q := r.URL.Query()
			for _, key := range []string{"merchant_id", "external_no", "before_id", "limit"} {
				if len(q[key]) > 1 {
					response(w, 400, -1, "ambiguous list query", nil)
					return true
				}
			}
			if role == "admin" {
				var e error
				mid, e = strconv.ParseInt(q.Get("merchant_id"), 10, 64)
				if e != nil || mid <= 0 {
					response(w, 422, -1, "merchant_id required", nil)
					return true
				}
			} else if q.Get("merchant_id") != "" && q.Get("merchant_id") != fmt.Sprint(mid) {
				response(w, 403, -1, "merchant ownership required", nil)
				return true
			}
			before, limit := int64(0), 20
			var e error
			if raw := q.Get("before_id"); raw != "" {
				before, e = strconv.ParseInt(raw, 10, 64)
			}
			if raw := q.Get("limit"); e == nil && raw != "" {
				limit, e = strconv.Atoi(raw)
			}
			if e != nil || before < 0 || limit < 1 || limit > 100 {
				response(w, 400, -1, "invalid pagination", nil)
				return true
			}
			out, e := s.Store.ListTransferBatches(r.Context(), mid, q.Get("external_no"), before, limit)
			if e != nil {
				fail(e)
			} else {
				response(w, 200, 0, "", out)
			}
			return true
		}
		if r.Method != "POST" {
			response(w, 405, -1, "method unavailable", nil)
			return true
		}
		if s.ProviderFlow == nil {
			response(w, 503, -1, "transfer service unavailable", nil)
			return true
		}
		var in struct {
			MerchantID string             `json:"merchant_id"`
			ExternalNo string             `json:"external_no"`
			Items      []batchConsoleItem `json:"items"`
		}
		if !decode(w, r, &in) {
			return true
		}
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
		if len(in.Items) < 1 || len(in.Items) > 100 {
			response(w, 422, -1, "1 to 100 items required", nil)
			return true
		}
		items := make([]payment.TransferBatchInput, 0, len(in.Items))
		var batchID int64
		err := s.Pool.QueryRow(r.Context(), `SELECT id FROM transfer_batches WHERE merchant_id=$1 AND external_no=$2`, mid, in.ExternalNo).Scan(&batchID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			fail(err)
			return true
		}
		for pos, item := range in.Items {
			no := ""
			if batchID > 0 {
				no = fmt.Sprintf("TB%d-%d", batchID, pos+1)
			}
			view, channel, routing, e := s.resolveTransferRouting228(r.Context(), s.Pool, mid, no, compat.Params{"type": item.Type, "channel_id": item.ChannelID})
			amount, ae := ledger.Parse(item.Amount)
			if e != nil || ae != nil || amount <= 0 {
				response(w, 422, -1, "invalid batch item or channel", nil)
				return true
			}
			s = view
			r = r.WithContext(transferRoutingContext228(r.Context(), routing))
			req := adapter.TransferRequest{Type: item.Type, Account: item.Account, Name: item.Name, Amount: amount, Remark: item.Remark, BankCode: item.BankCode}
			if item.Type == "alipay" {
				req.Title = req.Remark
				req.Remark = ""
			}
			items = append(items, payment.TransferBatchInput{ChannelID: channel, Request: req})
		}
		out, e := s.ProviderFlow.PrepareTransferBatchChecked(r.Context(), mid, in.ExternalNo, items, actor, s.batchChannelCheck(mid))
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "batch prepared; no items sent", out)
		}
		return true
	}
	if len(parts) < 3 || len(parts) > 4 {
		response(w, 404, -1, "batch not found", nil)
		return true
	}
	id, e := strconv.ParseInt(parts[2], 10, 64)
	if e != nil || id <= 0 {
		response(w, 404, -1, "batch not found", nil)
		return true
	}
	mid := session.User.ID
	scope := mid
	if role == "admin" {
		scope = 0
	}
	e = s.Pool.QueryRow(r.Context(), "SELECT merchant_id FROM transfer_batches WHERE id=$1 AND ($2::bigint=0 OR merchant_id=$2)", id, scope).Scan(&mid)
	if errors.Is(e, pgx.ErrNoRows) {
		response(w, 404, -1, "batch not found", nil)
		return true
	}
	if e != nil {
		fail(e)
		return true
	}
	if len(parts) == 3 && r.Method == "GET" {
		out, e := s.Store.TransferBatch(r.Context(), mid, id)
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "", out)
		}
		return true
	}
	if len(parts) != 4 || r.Method != "POST" {
		response(w, 405, -1, "method unavailable", nil)
		return true
	}
	var out payment.TransferBatch
	switch parts[3] {
	case "execute":
		if s.ProviderFlow == nil {
			response(w, 503, -1, "transfer service unavailable", nil)
			return true
		}
		var in struct {
			Limit int `json:"limit"`
		}
		if !decode(w, r, &in) {
			return true
		}
		if in.Limit < 1 || in.Limit > 100 {
			response(w, 422, -1, "limit 1 to 100 required", nil)
			return true
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		view, err := s.transferBatchOriginalView228(ctx, s.Pool, mid, id)
		if err != nil {
			fail(err)
			return true
		}
		out, e = view.ProviderFlow.ExecuteTransferBatch(ctx, mid, id, in.Limit, actor, view.batchChannelCheck(mid))
	case "query", "cancel-unsent":
		var in struct{}
		if !decode(w, r, &in) {
			return true
		}
		if parts[3] == "query" {
			if s.ProviderFlow == nil {
				response(w, 503, -1, "transfer service unavailable", nil)
				return true
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			out, e = s.ProviderFlow.QueryTransferBatch(ctx, mid, id, actor)
		} else {
			flow := s.ProviderFlow
			if flow == nil {
				// This exact recovery operation needs only the existing Store,
				// unsent proof and funds guard. Never use it for a provider action.
				flow = &payment.ProviderFlow{Store: s.Store}
			}
			out, e = flow.CancelUnsentTransferBatch(r.Context(), mid, id, actor)
		}
	default:
		response(w, 404, -1, "batch operation unavailable", nil)
		return true
	}
	if e != nil {
		fail(e)
		return true
	}
	response(w, 200, 0, "inspect individual results; response is not proof all items succeeded", out)
	return true
}

func (s *Server) batchChannelCheck(mid int64) payment.TransferBatchCheck {
	return func(ctx context.Context, channel int64) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		for _, allowed := range s.MerchantChannels[mid] {
			if allowed == channel {
				return nil
			}
		}
		return payment.ErrState
	}
}
