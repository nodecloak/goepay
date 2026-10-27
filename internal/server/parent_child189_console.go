package server

import (
	"context"
	"goepay/internal/payment"
)

func (s *Server) projectConsoleSubchannels(ctx context.Context, actor payment.SubchannelActor, items []payment.PaymentSubchannel) ([]payment.PaymentSubchannel, error) {
	if s.ProviderFlow == nil {
		return items, nil
	}
	if len(s.ProviderFlow.ParentFactories) == 0 {
		return s.ProviderFlow.ProjectSubchannels(ctx, actor, items)
	}
	out := make([]payment.PaymentSubchannel, len(items))
	groups := map[int64][]int{}
	for i, item := range items {
		groups[item.MerchantID] = append(groups[item.MerchantID], i)
	}
	for mid, indices := range groups {
		view, err := s.withParentRuntime(ctx, mid, "")
		if err != nil {
			return nil, err
		}
		subset := make([]payment.PaymentSubchannel, len(indices))
		for j, i := range indices {
			subset[j] = items[i]
		}
		projected, err := view.ProviderFlow.ProjectSubchannels(ctx, actor, subset)
		if err != nil {
			return nil, err
		}
		for j, i := range indices {
			out[i] = projected[j]
		}
	}
	return out, nil
}
