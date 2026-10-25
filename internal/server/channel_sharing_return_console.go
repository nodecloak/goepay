package server

import (
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"net/http"
	"strconv"
)

func (s *Server) channelSharingReturnConsole(w http.ResponseWriter, r *http.Request, mid int64, actor payment.TransferBatchActor, parts []string, in sharingConsoleInput, fail func(error)) {
	respond := func(value any, e error) {
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "", value)
		}
	}
	if len(parts) == 3 && r.Method == "GET" {
		cursor, limit := int64(0), 20
		if raw := r.URL.Query().Get("cursor"); raw != "" {
			n, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || n < 0 || strconv.FormatInt(n, 10) != raw {
				fail(payment.ErrInvalid)
				return
			}
			cursor = n
		}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || n > 100 || strconv.Itoa(n) != raw {
				fail(payment.ErrInvalid)
				return
			}
			limit = n
		}
		out, e := s.Store.ListChannelSharingReturns(r.Context(), mid, cursor, limit, actor)
		respond(out, e)
		return
	}
	if len(parts) == 4 && r.Method == "GET" {
		out, e := s.Store.ChannelSharingReturn(r.Context(), mid, parts[3], actor)
		respond(out, e)
		return
	}
	if s.ProviderFlow == nil || s.Sandbox || s.ProviderFlow.LocalSimulation {
		fail(adapter.ErrUnsupported)
		return
	}
	if len(parts) == 3 && r.Method == "POST" {
		receiver, ok := identityPositiveID(in.ReceiverID)
		amount, valid := identityPositiveID(in.Amount)
		if !ok || !valid {
			fail(payment.ErrInvalid)
			return
		}
		original, e := s.Store.ChannelSharingOrder(r.Context(), mid, in.SharingExternalNo, actor)
		if e != nil {
			fail(e)
			return
		}
		if !original.Completed || !original.FundsReleased {
			fail(payment.ErrState)
			return
		}
		receiverView, err := s.Store.ChannelSharingReceiver(r.Context(), mid, receiver, actor)
		if err != nil {
			fail(err)
			return
		}
		if !s.sharingConsoleChannel(mid, receiverView.ChannelID) {
			fail(payment.ErrConflict)
			return
		}
		out, e := s.ProviderFlow.SubmitChannelSharingReturn(r.Context(), mid, in.SharingExternalNo, receiver, in.ExternalNo, amount, actor)
		respond(out, e)
		return
	}
	if len(parts) == 5 && parts[4] == "query" && r.Method == "POST" {
		out, e := s.ProviderFlow.QueryChannelSharingReturn(r.Context(), mid, parts[3], actor)
		respond(out, e)
		return
	}
	response(w, 405, -1, "channel sharing return operation unavailable", nil)
}
