package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"time"

	"goepay/internal/identity"
	"goepay/internal/printing"
)

func (s *Server) printingConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, p []string) bool {
	if len(p) < 2 {
		return false
	}
	platform := p[1] == "printing"
	settings := p[1] == "account" && len(p) >= 3 && p[2] == "printing"
	order := p[1] == "orders" && len(p) >= 4 && (p[3] == "receipt" || p[3] == "print-jobs")
	jobs := p[1] == "print-jobs"
	if !platform && !settings && !order && !jobs {
		return false
	}
	if session.User.ID <= 0 || session.User.Username == "" || session.User.Role != role || p[0] != role || session.Issuer != nil || platform && (role != "admin" || session.User.AdminClass != "super") || !platform && role != "merchant" {
		response(w, 403, -1, "own merchant or super administrator session required", nil)
		return true
	}
	if r.Method != "GET" && (session.CSRF == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	var after int64
	limit := 20
	if (jobs && len(p) == 2 || order && len(p) == 4 && p[3] == "print-jobs") && r.Method == "GET" {
		var ok bool
		after, limit, ok = settlementPlanPaging(w, r)
		if !ok {
			return true
		}
	} else if r.URL.RawQuery != "" {
		response(w, 400, -1, "printing tenant and scope come from the original session and path", nil)
		return true
	}
	recoverJob := jobs && len(p) == 4 && p[3] == "recover"
	validPath := platform && len(p) == 3 && p[2] == "settings" || settings && len(p) == 3 || order && len(p) == 4 || jobs && (len(p) == 2 || len(p) == 3 || recoverJob)
	if !validPath {
		response(w, 404, -1, "printing operation unavailable", nil)
		return true
	}
	if (platform || settings) && r.Method != "GET" && r.Method != "PUT" || order && p[3] == "receipt" && r.Method != "GET" || order && p[3] == "print-jobs" && r.Method != "POST" && r.Method != "GET" || jobs && !recoverJob && r.Method != "GET" || recoverJob && r.Method != "POST" {
		response(w, 405, -1, "printing method unavailable", nil)
		return true
	}
	if s.Printing == nil {
		response(w, 503, -1, "printing service unavailable", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	a := printing.Actor{Role: role, Class: session.User.AdminClass, ID: session.User.ID, Name: session.User.Username}
	mid := session.User.ID
	var out any
	var err error
	status := http.StatusOK
	switch {
	case platform && r.Method == "GET":
		out, err = s.Printing.Platform(ctx, a)
	case platform:
		var in printing.PlatformInput
		if !printingBody(w, r, "platform", &in) {
			return true
		}
		out, err = s.Printing.SetPlatform(ctx, in, a)
	case settings && r.Method == "GET":
		out, err = s.Printing.Merchant(ctx, mid, a)
	case settings:
		var in printing.MerchantInput
		if !printingBody(w, r, "merchant", &in) {
			return true
		}
		out, err = s.Printing.SetMerchant(ctx, mid, in, a)
	case order && p[3] == "receipt":
		out, err = s.Printing.Receipt(ctx, mid, p[2], a)
	case order && r.Method == "GET":
		var items []printing.JobView
		items, err = s.Printing.OrderJobs(ctx, mid, p[2], after, limit, a)
		if items == nil {
			items = []printing.JobView{}
		}
		next := ""
		if len(items) == limit {
			next = fmt.Sprint(items[len(items)-1].ID)
		}
		out = map[string]any{"items": items, "next_cursor": next}
	case order:
		var in printing.ManualInput
		if !printingBody(w, r, "manual", &in) {
			return true
		}
		out, err = s.Printing.CreateManual(ctx, mid, p[2], in, a)
		status = http.StatusAccepted
	case recoverJob:
		id, ok := positiveFundsID(p[2])
		if !ok {
			response(w, 400, -1, "canonical print job id required", nil)
			return true
		}
		var in struct {
			ExpectedTaskID int64 `json:"expected_task_id,string"`
		}
		if !printingBody(w, r, "recover", &in) {
			return true
		}
		out, err = s.Printing.Recover(ctx, mid, id, in.ExpectedTaskID, a)
	case jobs && len(p) == 3:
		id, ok := positiveFundsID(p[2])
		if !ok {
			response(w, 400, -1, "canonical print job id required", nil)
			return true
		}
		out, err = s.Printing.Job(ctx, mid, id, a)
	case jobs:
		var items []printing.JobView
		items, err = s.Printing.Jobs(ctx, mid, after, limit, a)
		if items == nil {
			items = []printing.JobView{}
		}
		next := ""
		if len(items) == limit {
			next = fmt.Sprint(items[len(items)-1].ID)
		}
		out = map[string]any{"items": items, "next_cursor": next}
	}
	if err != nil {
		s.printingError(w, err)
		return true
	}
	response(w, status, 0, "printing state recorded; accepted is not physical printing confirmation", out)
	return true
}

func (s *Server) printingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, printing.ErrInvalid):
		response(w, 422, -1, "invalid printing input", nil)
	case errors.Is(err, printing.ErrConflict):
		response(w, 409, -1, "printing revision or original evidence conflict", nil)
	case errors.Is(err, printing.ErrState):
		response(w, 409, -1, "original printing state does not permit this operation", nil)
	case errors.Is(err, printing.ErrNotFound):
		response(w, 404, -1, "printing record not found", nil)
	case errors.Is(err, printing.ErrUnsupported):
		response(w, 503, -1, "external printing execution unavailable", nil)
	default:
		s.fail(w, err)
	}
}
