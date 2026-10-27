package backoffice

import "context"

func (s *Service) attachGroupCoreRisk(ctx context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	states, e := s.store.CoreRiskGroupSummaries(ctx, ids)
	if e != nil {
		return e
	}
	for i := range items {
		items[i].Fields["core_risk_config"] = states[items[i].ID]
	}
	return nil
}
func init() {
	d := definitions["groups"]
	d.fields = append(d.fields, Field{Name: "core_risk_config", Label: "用户组风控（实际继承与覆盖）", Type: "json", ReadOnly: true})
	definitions["groups"] = d
}
