package server

import (
	"fmt"
	provider "goepay/internal/adapters/payment"
	"strconv"
)

// The runtime registry identifies actual configured channels. Generic channel
// directory records do not establish that binding, so their row IDs cannot be
// used here as provider IDs or as names for unrelated imported channels.
func (s *Server) merchantMatrixConfiguredDimensions(a *merchantMatrixAccumulator, method string, transfer bool) error {
	if s.ProviderFlow == nil {
		return nil
	}
	for id, p := range s.ProviderFlow.Providers {
		if id <= 0 || p == nil {
			continue
		}
		var types []string
		if transfer {
			_, canTransfer := p.(provider.TransferProvider)
			capabilities, ok := p.(provider.TransferCapabilities)
			if !canTransfer || !ok {
				continue
			}
			types = capabilities.TransferTypes()
		} else if len(p.Capabilities().Methods) > 0 {
			types = []string{p.ID()}
		}
		admitted := false
		for _, kind := range types {
			switch kind {
			case "alipay", "wxpay", "sandbox":
			case "bank":
				if !transfer {
					continue
				}
			default:
				continue
			}
			admitted = true
			if method == "type" {
				if err := a.dimension("type_" + kind); err != nil {
					return err
				}
			}
		}
		if admitted && method == "channel" {
			if err := a.dimension("channel_" + strconv.FormatInt(id, 10)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *merchantMatrixAccumulator) dimension(key string) error {
	if !a.Dimensions[key] && len(a.Dimensions) >= 128 {
		return fmt.Errorf("matrix exceeds 128 configured or historical dimensions")
	}
	a.Dimensions[key] = true
	return nil
}
