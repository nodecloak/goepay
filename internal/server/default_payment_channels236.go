package server

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"goepay/internal/backoffice"
	"goepay/internal/grouppolicy"
	"goepay/internal/merchantpolicy"
	"goepay/internal/payment"
)

// Private request state; never a grant to transfer/settlement workers.
type defaultPaymentScope236 struct {
	merchant int64
	origins  []int64
	info     grouppolicy.Info
}

type originalPaymentScope236 struct {
	merchant   int64
	outTradeNo string
	channel    int64
}

func (s *Server) withDefaultPaymentAdmission236(ctx context.Context, mid int64) (*Server, error) {
	root := s
	for root.subchannelPolicySource != nil {
		root = root.subchannelPolicySource
	}
	if _, explicit := root.MerchantChannels[mid]; explicit || len(root.DefaultPaymentChannels) == 0 || s.ProviderFlow == nil {
		return s, nil
	}
	if s.defaultPaymentScope236 != nil && s.defaultPaymentScope236.merchant == mid {
		return s, nil
	}
	if s.Pool == nil || mid <= 0 {
		return nil, payment.ErrInvalid
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	var pay int
	if err = tx.QueryRow(ctx, `SELECT enabled,pay FROM merchants WHERE id=$1`, mid).Scan(&enabled, &pay); err != nil {
		return nil, err
	}
	scope := &defaultPaymentScope236{merchant: mid, origins: []int64{}, info: grouppolicy.Info{}}
	if enabled && pay == 1 {
		group, e := merchantpolicy.ReadEffectiveGroup(ctx, tx, mid, false)
		if e != nil && !errors.Is(e, merchantpolicy.ErrGroupPolicy) {
			return nil, e
		}
		if e == nil {
			var fields struct {
				Info json.RawMessage `json:"info"`
			}
			if json.Unmarshal(group.Body, &fields) != nil {
				return nil, payment.ErrInvalid
			}
			if len(fields.Info) == 0 {
				fields.Info = json.RawMessage(`{}`)
			}
			scope.info, e = grouppolicy.ParseInfo(fields.Info)
			if e != nil {
				return nil, payment.ErrInvalid
			}
			for _, id := range root.DefaultPaymentChannels {
				kind := ""
				if p := s.ProviderFlow.Providers[id]; p != nil {
					kind = p.ID()
				} else if f := s.ProviderFlow.ParentFactories[id]; f != nil {
					kind = f.PayType()
				}
				if id <= 0 || kind == "" || slices.Contains(scope.origins, id) {
					continue
				}
				open, e := payTypeCatalogOpen(ctx, tx, kind)
				if e != nil {
					return nil, e
				}
				if open && scope.info[kind].RoutingChoice().Kind() != grouppolicy.Disabled {
					scope.origins = append(scope.origins, id)
				}
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	view := s.executionView()
	view.MerchantChannels = make(map[int64][]int64, len(root.MerchantChannels)+1)
	for merchant, ids := range root.MerchantChannels {
		view.MerchantChannels[merchant] = ids
	}
	view.MerchantChannels[mid] = append([]int64{}, scope.origins...)
	view.defaultPaymentScope236 = scope
	return view, nil
}

func (s *Server) collectionOperatorAllowed236(mid, source int64) bool {
	root := s
	for root.subchannelPolicySource != nil {
		root = root.subchannelPolicySource
	}
	if ids, explicit := root.MerchantChannels[mid]; explicit {
		return slices.Contains(ids, source)
	}
	scope := s.defaultPaymentScope236
	return scope != nil && scope.merchant == mid && slices.Contains(root.DefaultPaymentChannels, source) && slices.Contains(scope.origins, source)
}

// Retain origin permissions for restoring granted aliases, then narrow visible
// candidates to the current group's selection. No private grant is fabricated.
func (s *Server) projectDefaultPaymentCandidates236(ctx context.Context, mid int64) (*Server, error) {
	scope := s.defaultPaymentScope236
	if scope == nil || scope.merchant != mid {
		return s, nil
	}
	ids := []int64{}
	for _, id := range s.MerchantChannels[mid] {
		p := s.ProviderFlow.Providers[id]
		if p == nil {
			continue
		}
		ordinary := false
		for _, method := range p.Capabilities().Methods {
			if method != "preauth" {
				ordinary = true
			}
		}
		if !ordinary {
			continue
		}
		choice := scope.info[p.ID()].RoutingChoice()
		if choice.Kind() == grouppolicy.Disabled || choice.Kind() == grouppolicy.Channel && choice.ID() != id {
			continue
		}
		if choice.Kind() == grouppolicy.Roll {
			var raw []byte
			if e := s.Pool.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE resource='pay-rolls' AND id=$1`, choice.ID()).Scan(&raw); e != nil {
				return nil, e
			}
			var fields map[string]any
			if json.Unmarshal(raw, &fields) != nil {
				return nil, payment.ErrInvalid
			}
			roll, e := backoffice.ParsePayRoll(fields)
			if e != nil || !roll.Enabled || roll.MerchantID != 0 || roll.Type != p.ID() {
				return nil, payment.ErrState
			}
			found := false
			for _, candidate := range roll.Channels {
				found = found || candidate.ID == id
			}
			if !found {
				continue
			}
		}
		ids = append(ids, id)
	}
	view := s.executionView()
	view.MerchantChannels = make(map[int64][]int64, len(s.MerchantChannels))
	for merchant, allowed := range s.MerchantChannels {
		view.MerchantChannels[merchant] = allowed
	}
	view.MerchantChannels[mid] = ids
	return view, nil
}
