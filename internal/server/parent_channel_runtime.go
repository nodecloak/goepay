package server

import (
	"context"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
)

func (s *Server) InstallParentRuntimeAuthorization() error {
	if s.ProviderFlow == nil {
		return nil
	}
	*s.ProviderFlow = s.ProviderFlow.WithCurrentWritePolicy(func() bool { return s.paymentExecutionWriteAllowed() })
	return s.ProviderFlow.InstallParentOperatorLookup(func(mid, source int64) bool {
		for _, id := range s.MerchantChannels[mid] {
			if id == source {
				return true
			}
		}
		return false
	})
}

// A request owns its restored catalog; no mutable global provider registry or
// authenticated operator scope is rewritten by a GET or configuration save.
func (s *Server) withParentRuntime(ctx context.Context, mid int64, no string, payTypes ...string) (*Server, error) {
	if s.ProviderFlow == nil || len(s.ProviderFlow.ParentFactories) == 0 {
		return s, nil
	}
	// A provider attempt fixes an original external operation. Current vault
	// corruption/rotation must not block its query or callback restoration. A
	// sealed order without an attempt still needs current first-send admission.
	historical := false
	if no != "" {
		if e := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders o JOIN provider_attempts a ON a.order_id=o.id WHERE o.merchant_id=$1 AND o.out_trade_no=$2)`, mid, no).Scan(&historical); e != nil {
			return nil, e
		}
	}
	root := s
	for root.subchannelPolicySource != nil {
		root = root.subchannelPolicySource
	}
	base := s.ProviderFlow.WithCurrentWritePolicy(func() bool { return root.paymentExecutionWriteAllowed() })
	flow, allowed, e := base.RuntimeParentChannels(ctx, mid, root.MerchantChannels, func(merchant, source int64) bool {
		for _, id := range root.MerchantChannels[merchant] {
			if id == source {
				return true
			}
		}
		return false
	})
	if e != nil {
		return nil, e
	}
	if no != "" {
		flow, e = flow.ParentPaymentIntent(ctx, mid, no)
		if e != nil {
			return nil, e
		}
	}
	if !historical {
		kind := ""
		if len(payTypes) > 0 {
			kind = payTypes[0]
		}
		dependencies, err := flow.ManagedWeixinApplicationDependencies(kind)
		if err != nil {
			return nil, err
		}
		if len(dependencies) > 0 {
			resolved, err := s.managedWeixinRuntimeIDs(ctx, dependencies, true)
			if err != nil {
				return nil, err
			}
			s = resolved
			base = s.ProviderFlow.WithCurrentWritePolicy(func() bool { return root.paymentExecutionWriteAllowed() })
			flow, allowed, err = base.RuntimeParentChannels(ctx, mid, root.MerchantChannels, func(merchant, source int64) bool {
				for _, id := range root.MerchantChannels[merchant] {
					if id == source {
						return true
					}
				}
				return false
			})
			if err != nil {
				return nil, err
			}
			if no != "" {
				flow, err = flow.ParentPaymentIntent(ctx, mid, no)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	out := s.withRestoredParentFlow(flow)
	out.MerchantChannels = make(map[int64][]int64, len(s.MerchantChannels))
	for id, ids := range s.MerchantChannels {
		out.MerchantChannels[id] = ids
	}
	out.MerchantChannels[mid] = allowed
	return out, nil
}

// Restore execution profiles from the same sealed account as the SDK, without
// reloading a current catalog during an original-order read.
func (s *Server) withRestoredParentFlow(flow payment.ProviderFlow) *Server {
	out := s.executionView()
	out.ProviderFlow = &flow
	root := s
	for root.subchannelPolicySource != nil {
		root = root.subchannelPolicySource
	}
	out.MerchantChannels = make(map[int64][]int64, len(root.MerchantChannels))
	for mid, ids := range root.MerchantChannels {
		out.MerchantChannels[mid] = append([]int64(nil), ids...)
	}
	for mid, aliases := range flow.ParentExecutionPermissionOrigins() {
		for alias, origin := range aliases {
			originAllowed, already := false, false
			for _, id := range root.MerchantChannels[mid] {
				originAllowed = originAllowed || id == origin
				already = already || id == alias
			}
			if originAllowed && !already {
				out.MerchantChannels[mid] = append(out.MerchantChannels[mid], alias)
			}
		}
	}
	out.OAuthProviders = make(map[int64]adapter.WebOAuthProvider, len(s.OAuthProviders))
	for id, p := range s.OAuthProviders {
		if flow.Providers[id] != nil {
			out.OAuthProviders[id] = p
		}
	}
	out.MiniOAuthProviders = make(map[int64]adapter.MiniOAuthProvider, len(s.MiniOAuthProviders))
	for id, p := range s.MiniOAuthProviders {
		if flow.Providers[id] != nil {
			out.MiniOAuthProviders[id] = p
		}
	}
	web, mini := flow.ParentExecutionProfiles()
	for id, p := range web {
		delete(out.OAuthProviders, id)
		if p != nil {
			out.OAuthProviders[id] = p
		}
	}
	for id, p := range mini {
		delete(out.MiniOAuthProviders, id)
		if p != nil {
			out.MiniOAuthProviders[id] = p
		}
	}
	return out
}
