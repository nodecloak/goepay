package backoffice

import "context"

// The resource service authorizes the administrator before selecting these
// merchant rows. This batch decorates only those rows with existing safe masks;
// it cannot configure a recipient or grant any funds action.
func (s *Service) attachMerchantRecipients(ctx context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	views, err := s.store.SettlementRecipientSummaries(ctx, ids)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Fields["settlement_recipient"] = views[items[i].ID]
	}
	return nil
}
