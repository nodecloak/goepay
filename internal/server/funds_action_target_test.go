package server

import "testing"

func TestFundsActionTargetsExplicitAllowlist(t *testing.T) {
	for _, target := range []string{"/console/v1/admin/transfers", "/console/v1/admin/transfer-batches", "/console/v1/admin/transfer-batches/1/execute", "/console/v1/admin/transfer-batches/1/cancel-unsent", "/console/v1/admin/orders/T1/refund", "/console/v1/admin/orders/T1/freeze-income", "/console/v1/admin/orders/T1/unfreeze-income", "/console/v1/admin/orders/T1/capture-authorization", "/console/v1/admin/orders/T1/release-authorization", "/console/v1/admin/orders/T1/close", "/console/v1/admin/workflows/internal-transfers", "/console/v1/admin/workflows/settlements", "/console/v1/admin/workflows/profitsharing", "/console/v1/admin/workflows/memberships", "/console/v1/admin/workflows/deposits", "/console/v1/admin/workflows/settlements/1/approve", "/console/v1/admin/workflows/deposits/1/reject", "/console/v1/admin/workflows/profitsharing-rules/1/execute"} {
		if _, ok := fundsTarget(target); !ok {
			t.Fatal("missing protected target", target)
		}
	}
	for _, target := range []string{"/console/v1/merchant/transfers", "/api/transfer/submit", "/console/v1/admin/orders/T1/query", "/console/v1/admin/orders/T1/notify", "/console/v1/admin/transfers/1/proof", "/console/v1/admin/transfer-batches/1/query", "/console/v1/admin/transfer-batches/01/execute", "/console/v1/admin/transfer-batches/1/cancel", "/console/v1/admin/workflows/recharges", "/console/v1/admin/workflows/settlements/1/approve?x=1"} {
		if _, ok := fundsTarget(target); ok {
			t.Fatal("unintended financial target", target)
		}
	}
}
