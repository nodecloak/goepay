package server

import "testing"

func TestWorkflowIntent212LookupRequiresTheOriginalWorkflowMenuGrant(t *testing.T) {
	for _, tc := range []struct {
		kind, grant string
		want        bool
	}{
		{"settlements", "SettleList", true},
		{"settlements", "InternalTransfer", false},
		{"internal-transfers", "InternalTransfer", true},
		{"internal-transfers", "SettleList", false},
	} {
		parts := []string{"admin", "workflows", tc.kind, "lookup"}
		if got := adminRequestAllowed(adminUser("operator", []string{tc.grant}), parts); got != tc.want {
			t.Fatalf("%s grant=%s: allowed=%v want=%v", tc.kind, tc.grant, got, tc.want)
		}
		if adminRequestAllowed(adminUser("readonly", nil), parts) {
			t.Fatal("lookup bypassed administrator class")
		}
	}
}
