package server

import (
	"goepay/internal/backoffice"
	"strconv"
)

// Metadata is derived from the loaded provider registry. It never reveals
// credentials, consults the editable directory, or calls a provider.
func (s *Server) groupPaymentChannels() ([]backoffice.GroupPaymentChannel, error) {
	items := []backoffice.GroupPaymentChannel{}
	if s.ProviderFlow == nil {
		return items, nil
	}
	if len(s.ProviderFlow.Providers) > 1000 {
		return nil, backoffice.ErrInvalid
	}
	for id, provider := range s.ProviderFlow.Providers {
		if id <= 0 || provider == nil {
			return nil, backoffice.ErrInvalid
		}
		kind := provider.ID()
		if kind != "alipay" && kind != "wxpay" {
			continue
		}
		methods := append([]string(nil), provider.Capabilities().Methods...)
		if len(methods) == 0 {
			continue
		}
		items = append(items, backoffice.GroupPaymentChannel{ID: strconv.FormatInt(id, 10), Type: kind, Methods: methods})
	}
	return items, nil
}
