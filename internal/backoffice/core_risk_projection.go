package backoffice

import "context"

func (s *Service) attachMerchantCoreRisk(ctx context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	states, e := s.store.CoreRiskSubjectStatuses(ctx, "merchant", ids)
	if e != nil {
		return e
	}
	for i := range items {
		items[i].Fields["core_risk"] = states[items[i].ID]
	}
	return nil
}
func init() {
	d := definitions["merchants"]
	d.fields = append(d.fields, Field{Name: "core_risk", Label: "核心风控有效状态", Type: "json", ReadOnly: true})
	definitions["merchants"] = d
}
