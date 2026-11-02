package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func (s *Server) transferRuntimeSnapshot228(ctx context.Context, q payment.ParentRuntimeReader, mid int64) (*Server, error) {
	if s.ProviderFlow == nil {
		return nil, payment.ErrLiveDisabled
	}
	if len(s.ProviderFlow.ParentFactories) == 0 {
		return s, nil
	}
	root := s
	for root.subchannelPolicySource != nil {
		root = root.subchannelPolicySource
	}
	flow, allowed, err := root.ProviderFlow.RuntimeParentChannelsSnapshot(ctx, q, mid, root.MerchantChannels, func(m, source int64) bool {
		for _, id := range root.MerchantChannels[m] {
			if id == source {
				return true
			}
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	view := s.withRestoredParentFlow(flow)
	view.MerchantChannels[mid] = allowed
	return view, nil
}

// An existing durable intent always retains its original channel/account. The
// payment store still compares the complete request hash before returning it.
func (s *Server) resolveTransferRouting228(ctx context.Context, q payment.ParentRuntimeReader, mid int64, no string, p compat.Params) (*Server, int64, payment.TransferRoutingState228, error) {
	var state payment.TransferRoutingState228
	if no != "" {
		if s.ProviderFlow == nil {
			return nil, 0, state, payment.ErrLiveDisabled
		}
		flow, old, err := s.ProviderFlow.TransferRoutingOriginal228(ctx, q, mid, no)
		if err == nil {
			if p["channel_id"] != "" && p["channel_id"] != strconv.FormatInt(old.ChannelID, 10) {
				return nil, 0, state, payment.ErrConflict
			}
			return s.withRestoredParentFlow(flow), old.ChannelID, state, nil
		}
		if !errors.Is(err, payment.ErrNotFound) {
			return nil, 0, state, err
		}
	}
	state, route, err := s.Store.TransferRouteSnapshot228(ctx, q, p["type"])
	if err != nil {
		return nil, 0, state, err
	}
	if err = payment.CheckTransferRoutingSelection228(ctx, state); err != nil {
		return nil, 0, state, err
	}
	if route.Mode == "manual" || route.Mode == "disabled" {
		return nil, 0, state, payment.ErrState
	}
	view, err := s.transferRuntimeSnapshot228(ctx, q, mid)
	if err != nil {
		return nil, 0, state, err
	}
	selected := compat.Params{"type": p["type"], "channel_id": p["channel_id"]}
	if selected["channel_id"] == "" && route.Mode == "channel" {
		selected["channel_id"] = strconv.FormatInt(route.ChannelID, 10)
	}
	id, err := view.transferChannel(mid, selected)
	return view, id, state, err
}

func transferRoutingContext228(ctx context.Context, state payment.TransferRoutingState228) context.Context {
	if state.Fingerprint == "" {
		return ctx
	}
	return payment.WithTransferRoutingSelection228(ctx, state.Fingerprint)
}

func (s *Server) transferBatchOriginalView228(ctx context.Context, q payment.ParentRuntimeReader, mid, batchID int64) (*Server, error) {
	rows, err := q.Query(ctx, `SELECT t.transfer_no FROM transfer_batch_items i JOIN transfers t ON t.id=i.transfer_id JOIN transfer_batches b ON b.id=i.batch_id WHERE b.id=$1 AND b.merchant_id=$2 ORDER BY i.position`, batchID, mid)
	if err != nil {
		return nil, err
	}
	numbers := []string{}
	for rows.Next() {
		var no string
		if err = rows.Scan(&no); err != nil {
			break
		}
		numbers = append(numbers, no)
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(numbers) == 0 || s.ProviderFlow == nil {
		return nil, payment.ErrNotFound
	}
	flow := *s.ProviderFlow
	for _, no := range numbers {
		flow, _, err = flow.TransferRoutingOriginal228(ctx, q, mid, no)
		if err != nil {
			return nil, err
		}
	}
	return s.withRestoredParentFlow(flow), nil
}

func (s *Server) redpackOriginalView228(ctx context.Context, p payment.Redpack) (*Server, error) {
	if p.Status != "claiming" {
		return s, nil
	}
	flow, _, err := s.ProviderFlow.TransferRoutingOriginal228(ctx, s.Pool, p.MerchantID, payment.RedpackTransferNo(p.ID))
	if errors.Is(err, payment.ErrNotFound) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	return s.withRestoredParentFlow(flow), nil
}

func (s *Server) transferRoutingConsole228(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "settings" || parts[2] != "transfer-routes" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if r.Method != "GET" && r.Method != "PATCH" {
		w.Header().Set("Allow", "GET, PATCH")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if s.Store == nil || s.ProviderFlow == nil {
		response(w, 503, -1, "transfer settings unavailable", nil)
		return true
	}
	choices, err := s.ProviderFlow.TransferRoutingChoices228(r.Context())
	if err != nil {
		s.fail(w, err)
		return true
	}
	var state payment.TransferRoutingState228
	if r.Method == "PATCH" {
		var in struct {
			Fingerprint string            `json:"expected_fingerprint"`
			Routes      map[string]string `json:"routes"`
		}
		if !decode(w, r, &in) {
			return true
		}
		for typ, raw := range in.Routes {
			route, err := payment.ParseTransferRoute228(raw)
			if err != nil {
				response(w, 422, -1, "invalid transfer route", nil)
				return true
			}
			if route.Mode == "channel" {
				found := false
				for _, choice := range choices {
					if choice.ID == fmt.Sprint(route.ChannelID) {
						for _, t := range choice.Types {
							found = found || t == typ
						}
					}
				}
				if !found {
					response(w, 422, -1, "transfer channel unavailable; reload choices", nil)
					return true
				}
			}
		}
		state, err = s.Store.SaveTransferRouting228(r.Context(), payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF}, in.Fingerprint, in.Routes)
	} else {
		state, err = s.Store.TransferRouting228(r.Context())
	}
	if err != nil {
		s.fail(w, err)
		return true
	}
	response(w, 200, 0, "", map[string]any{"state": state, "channels": choices})
	return true
}
