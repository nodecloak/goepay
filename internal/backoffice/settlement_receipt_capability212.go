package backoffice

import (
	"context"
	"goepay/internal/payment"
)

// Only project availability from the host's actual payee-link lookup. The
// scoped list has already selected the caller's requests; signed URLs remain
// in the separately authorized detail endpoint and are not exposed here.
func (s *Service) attachSettlementReceiptCapabilities(ctx context.Context, items []map[string]any) error {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		item["can_confirm_receipt"] = false
		if p, ok := item["payout"].(*payment.SettlementPayout); ok && p != nil && p.TransferID > 0 {
			ids = append(ids, p.TransferID)
		}
	}
	if s.SettlementConfirmURLs == nil || len(ids) == 0 {
		return nil
	}
	links, e := s.SettlementConfirmURLs(ctx, ids)
	if e != nil {
		return e
	}
	for _, item := range items {
		if p, ok := item["payout"].(*payment.SettlementPayout); ok && p != nil {
			item["can_confirm_receipt"] = links[p.TransferID] != ""
		}
	}
	return nil
}
