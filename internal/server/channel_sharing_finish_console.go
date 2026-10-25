package server

import (
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"net/http"
	"strconv"
)

func (s *Server) channelSharingFinishConsole(w http.ResponseWriter, r *http.Request, mid int64, actor payment.TransferBatchActor, parts []string, in sharingConsoleInput, fail func(error)) {
	respond := func(out any, e error) {
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "", out)
		}
	}
	if parts[2] == "finishes" && len(parts) == 3 && r.Method == "GET" {
		limit := 20
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || n > 100 || strconv.Itoa(n) != raw {
				fail(payment.ErrInvalid)
				return
			}
			limit = n
		}
		out, e := s.Store.ChannelSharingFinishes(r.Context(), mid, limit, actor)
		respond(map[string]any{"items": out}, e)
		return
	}
	if parts[2] == "finishes" && len(parts) == 4 && r.Method == "GET" {
		out, e := s.Store.ChannelSharingFinish(r.Context(), mid, parts[3], actor)
		respond(out, e)
		return
	}
	if s.ProviderFlow == nil || s.Sandbox || s.ProviderFlow.LocalSimulation {
		fail(adapter.ErrUnsupported)
		return
	}
	if parts[2] == "finish-preview" && len(parts) == 4 && r.Method == "GET" {
		out, e := s.ProviderFlow.PreviewChannelSharingFinish(r.Context(), mid, parts[3], actor)
		respond(out, e)
		return
	}
	if parts[2] == "finishes" && len(parts) == 3 && r.Method == "POST" {
		original, e := s.Store.ChannelSharingPayment(r.Context(), mid, in.TradeNo, actor)
		if e != nil {
			fail(e)
			return
		}
		if !s.sharingConsoleChannel(mid, original.ChannelID) {
			fail(payment.ErrConflict)
			return
		}
		out, e := s.ProviderFlow.SubmitChannelSharingFinish(r.Context(), mid, in.TradeNo, in.ExternalNo, actor)
		respond(out, e)
		return
	}
	if parts[2] == "finishes" && len(parts) == 5 && parts[4] == "query" && r.Method == "POST" {
		out, e := s.ProviderFlow.QueryChannelSharingFinish(r.Context(), mid, parts[3], actor)
		respond(out, e)
		return
	}
	response(w, 405, -1, "channel sharing finish operation unavailable", nil)
}
