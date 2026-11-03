package server

import (
	"context"
	"strconv"
)

type PaymentAdmissionChannel236 struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}
type PaymentAdmissionReport236 struct {
	MerchantID       string                       `json:"merchant_id"`
	Exists           bool                         `json:"exists"`
	Source           string                       `json:"source"`
	PaymentChannels  []PaymentAdmissionChannel236 `json:"payment_channels"`
	OperatorChannels []string                     `json:"operator_channels"`
}

// InspectPaymentAdmission236 is an offline host diagnostic. It reads the same
// collection view as checkout without generating orders or calling providers.
func (s *Server) InspectPaymentAdmission236(ctx context.Context, mid int64) (PaymentAdmissionReport236, error) {
	out := PaymentAdmissionReport236{MerchantID: strconv.FormatInt(mid, 10), Source: "none", PaymentChannels: []PaymentAdmissionChannel236{}, OperatorChannels: []string{}}
	root := s
	for root.subchannelPolicySource != nil {
		root = root.subchannelPolicySource
	}
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchants WHERE id=$1)`, mid).Scan(&out.Exists); err != nil {
		return out, err
	}
	if ids, explicit := root.MerchantChannels[mid]; explicit {
		out.Source = "explicit"
		for _, id := range ids {
			out.OperatorChannels = append(out.OperatorChannels, strconv.FormatInt(id, 10))
		}
	} else if len(root.DefaultPaymentChannels) > 0 {
		out.Source = "default"
	}
	if !out.Exists {
		return out, nil
	}
	view, err := s.withParentRuntime(ctx, mid, "")
	if err != nil {
		return out, err
	}
	if view.ProviderFlow != nil {
		for _, id := range view.MerchantChannels[mid] {
			if p := view.ProviderFlow.Providers[id]; p != nil {
				out.PaymentChannels = append(out.PaymentChannels, PaymentAdmissionChannel236{ID: strconv.FormatInt(id, 10), Type: p.ID()})
			}
		}
	}
	return out, nil
}
