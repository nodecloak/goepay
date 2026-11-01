package backoffice

import "testing"

func TestGroupCombineSettingsPersistWithGroupAndRejectBadPolicy(t *testing.T) {
	s := setup(t)
	group, e := s.Create(t.Context(), admin, "groups", input(`{"name":"combine overrides","enabled":true,"wxcombine_open":"1","wxcombine_minmoney":"100.01","wxcombine_submoney":"0"}`))
	if e != nil {
		t.Fatal("group combine fields unavailable", e)
	}
	if group.Fields["wxcombine_minmoney"] != "100.01" || group.Fields["wxcombine_submoney"] != "0" {
		t.Fatal("group combine values were not atomically persisted", group.Fields)
	}
	for _, body := range []string{`{"name":"bad combine","wxcombine_open":"2"}`, `{"name":"bad combine","wxcombine_minmoney":"-1"}`, `{"name":"bad combine","wxcombine_submoney":"0.001"}`, `{"name":"bad combine","wxcombine_minmoney":"92233720368547758.08"}`} {
		if _, e := s.Create(t.Context(), admin, "groups", input(body)); e == nil {
			t.Error("invalid group combine policy was accepted", body)
		}
	}
}
