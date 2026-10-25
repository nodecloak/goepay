package backoffice

import (
	"context"
	"fmt"
	"testing"
)

func TestTransferPolicySettingsAreRegisteredAndValidated(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, pair := range [][2]string{{"transfer_minmoney", "1.00"}, {"transfer_maxmoney", "500.00"}, {"transfer_maxlimit", "3"}, {"transfer_desc", "默认转账"}, {"transfer_name", "商家付款"}} {
		if _, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); e != nil {
			t.Fatal(pair, e)
		}
	}
	for _, pair := range [][2]string{{"transfer_minmoney", "-1"}, {"transfer_maxmoney", "1.001"}, {"transfer_maxlimit", "1.5"}, {"transfer_maxlimit", "1000001"}, {"transfer_desc", "unsafe\nremark"}} {
		if _, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); e == nil {
			t.Fatal("invalid transfer setting accepted", pair)
		}
	}
}
