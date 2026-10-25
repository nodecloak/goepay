package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (s *Server) settlementBatchConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, p []string) bool {
	if len(p) < 2 || p[1] != "settlement-batches" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if role != "admin" || session.User.Role != "admin" || session.User.ID <= 0 || session.User.Username == "" || session.Issuer != nil || session.User.AdminClass != "super" && session.User.AdminClass != "operator" {
		response(w, 403, -1, "own authorized administrator required", nil)
		return true
	}
	if r.Method != "GET" && (session.CSRF == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if len(p) > 4 {
		response(w, 404, -1, "settlement batch not found", nil)
		return true
	}
	id := int64(0)
	var err error
	if len(p) >= 3 {
		id, err = strconv.ParseInt(p[2], 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != p[2] {
			response(w, 404, -1, "settlement batch not found", nil)
			return true
		}
	}
	export := len(p) == 4 && p[3] == "export"
	if export && session.User.AdminClass != "super" {
		response(w, 403, -1, "original super administrator required for recipient export", nil)
		return true
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		response(w, 400, -1, "canonical batch query required", nil)
		return true
	}
	before, limit := int64(0), 20
	format, external := "", ""
	batchFilter, statusFilter := "", ""
	for key, values := range query {
		if len(values) != 1 {
			response(w, 400, -1, "ambiguous batch query", nil)
			return true
		}
		value := values[0]
		if export && r.Method == "GET" && key == "format" {
			if value != "common" && value != "mybank" && value != "alipay" && value != "wxpay" {
				response(w, 400, -1, "original export format required", nil)
				return true
			}
			format = value
			continue
		}
		if len(p) != 2 || r.Method != "GET" {
			response(w, 400, -1, "unexpected batch query", nil)
			return true
		}
		switch key {
		case "batch":
			batchFilter = value
		case "status":
			statusFilter = value
		case "external_no":
			external = value
		case "before_id", "limit":
			n, e := strconv.ParseInt(value, 10, 64)
			if e != nil || n < 0 || strconv.FormatInt(n, 10) != value || key == "limit" && (n < 1 || n > 100) {
				response(w, 400, -1, "canonical batch pagination required", nil)
				return true
			}
			if key == "limit" {
				limit = int(n)
			} else {
				before = n
			}
		default:
			response(w, 400, -1, "unknown batch filter", nil)
			return true
		}
	}
	if export && format == "" {
		response(w, 400, -1, "export format required", nil)
		return true
	}
	if len(p) == 4 && !export && p[3] != "archive" && p[3] != "complete" {
		response(w, 404, -1, "batch operation unavailable", nil)
		return true
	}
	if len(p) == 3 && r.Method != "GET" || len(p) == 4 && (export && r.Method != "GET" || !export && r.Method != "POST") || len(p) == 2 && r.Method != "GET" && r.Method != "POST" {
		response(w, 405, -1, "batch method unavailable", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "settlement batches unavailable", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	fail := func(e error) {
		if errors.Is(e, payment.ErrState) {
			response(w, 409, -1, "original settlement state does not permit this batch action", nil)
		} else {
			s.fail(w, e)
		}
	}
	actor := payment.TransferBatchActor{Role: role, Class: session.User.AdminClass, ID: session.User.ID, Name: session.User.Username}
	if export {
		file, e := s.Store.ExportSettlementBatch(ctx, id, format, actor)
		if e != nil {
			fail(e)
			return true
		}
		w.Header().Set("Content-Type", file.ContentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", file.Filename))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Length", strconv.Itoa(len(file.Data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(file.Data)
		return true
	}
	var out any
	if len(p) == 2 && r.Method == "GET" {
		out, err = s.Store.ListSettlementBatchesFiltered(ctx, payment.SettlementBatchQuery{ExternalNo: external, Batch: batchFilter, Status: statusFilter, BeforeID: before, Limit: limit})
	} else if len(p) == 2 {
		var input struct {
			ExternalNo string   `json:"external_no"`
			RequestIDs []string `json:"request_ids"`
		}
		if !decode(w, r, &input) {
			return true
		}
		ids := make([]int64, 0, len(input.RequestIDs))
		for _, raw := range input.RequestIDs {
			n, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || n <= 0 || strconv.FormatInt(n, 10) != raw {
				response(w, 422, -1, "original settlement request IDs required", nil)
				return true
			}
			ids = append(ids, n)
		}
		out, err = s.Store.CreateSettlementBatch(ctx, payment.SettlementBatchInput{ExternalNo: input.ExternalNo, RequestIDs: ids}, actor)
	} else if len(p) == 3 {
		out, err = s.Store.SettlementBatch(ctx, id)
	} else {
		var input struct{}
		if !decode(w, r, &input) {
			return true
		}
		if p[3] == "archive" {
			out, err = s.Store.ArchiveSettlementBatch(ctx, id, actor)
		} else {
			out, err = s.Store.CompleteSettlementBatch(ctx, id, actor)
		}
	}
	if err != nil {
		fail(err)
	} else {
		response(w, 200, 0, "", out)
	}
	return true
}
