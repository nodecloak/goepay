package backoffice

import (
	"context"
	"testing"
)

// The 支付方式 console shows all 7 original pre_type rows with their effective
// 状态 even before any record exists (merged by List -> listPayTypes), and the
// 状态 switch POSTs a new record for a type with none or PATCHes the existing
// one — matching the original console and payTypeCatalogOpen's defaults.
func TestPayTypesListMergesCatalogDefaultsAndRecords(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	// Empty store: all 7 originals present, alipay/wxpay open by default, the
	// rest closed, and none has a record yet (id 0).
	page, e := s.List(ctx, admin, "pay-types", Query{})
	if e != nil {
		t.Fatal(e)
	}
	if page.Total != 7 || len(page.Items) != 7 {
		t.Fatalf("expected the 7 original types with no record, got %d", page.Total)
	}
	defaults := map[string]bool{"alipay": true, "wxpay": true, "qqpay": false, "bank": false, "jdpay": false, "paypal": false, "douyinpay": false}
	gotOrder := []string{"alipay", "wxpay", "qqpay", "bank", "jdpay", "paypal", "douyinpay"}
	for i, it := range page.Items {
		kind, _ := it.Fields["type"].(string)
		if kind != gotOrder[i] {
			t.Fatalf("catalog order broke at %d: want %s got %s", i, gotOrder[i], kind)
		}
		if it.ID != 0 {
			t.Fatalf("%s showed a record id %d before any record exists", kind, it.ID)
		}
		if enabled, _ := it.Fields["enabled"].(bool); enabled != defaults[kind] {
			t.Fatalf("%s effective state %v, want default %v", kind, enabled, defaults[kind])
		}
	}

	// Enabling qqpay from the switch (no record yet) POSTs a record.
	created, e := s.Create(ctx, admin, "pay-types", input(`{"name":"QQ钱包","type":"qqpay","enabled":true}`))
	if e != nil {
		t.Fatal("switch-create for a defaultless type rejected", e)
	}
	// Closing alipay (open by default, still no record) POSTs a disabling record.
	if _, e = s.Create(ctx, admin, "pay-types", input(`{"name":"支付宝","type":"alipay","enabled":false}`)); e != nil {
		t.Fatal("switch-create disabling a default-open type rejected", e)
	}
	page, e = s.List(ctx, admin, "pay-types", Query{})
	if e != nil {
		t.Fatal(e)
	}
	if page.Total != 7 {
		t.Fatalf("records must not add extra catalog rows, got %d", page.Total)
	}
	state := map[string]struct {
		id      int64
		enabled bool
	}{}
	for _, it := range page.Items {
		kind, _ := it.Fields["type"].(string)
		enabled, _ := it.Fields["enabled"].(bool)
		state[kind] = struct {
			id      int64
			enabled bool
		}{it.ID, enabled}
	}
	if state["qqpay"].id == 0 || !state["qqpay"].enabled {
		t.Fatalf("qqpay record not reflected: %+v", state["qqpay"])
	}
	if state["alipay"].id == 0 || state["alipay"].enabled {
		t.Fatalf("alipay record not reflected (should be closed): %+v", state["alipay"])
	}
	if state["bank"].id != 0 || state["bank"].enabled {
		t.Fatalf("bank should still be a default row (id 0, closed): %+v", state["bank"])
	}

	// Toggling the existing qqpay record off PATCHes it (no new row).
	if _, e = s.Update(ctx, admin, "pay-types", created.ID, input(`{"enabled":false}`)); e != nil {
		t.Fatal("switch-update of an existing record rejected", e)
	}
	page, e = s.List(ctx, admin, "pay-types", Query{})
	if e != nil {
		t.Fatal(e)
	}
	if page.Total != 7 {
		t.Fatalf("update must not change the catalog row count, got %d", page.Total)
	}
	for _, it := range page.Items {
		if kind, _ := it.Fields["type"].(string); kind == "qqpay" {
			if it.ID != created.ID {
				t.Fatalf("qqpay toggled via a new record instead of PATCH: id %d", it.ID)
			}
			if enabled, _ := it.Fields["enabled"].(bool); enabled {
				t.Fatal("qqpay still open after disabling PATCH")
			}
		}
	}
}

// A record for a type outside the 7 originals (ecny) is listed after the
// catalog, so nothing is hidden, and the 7 originals still all show.
func TestPayTypesListKeepsNonCatalogRecordsAfterTheSeven(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, e := s.Create(ctx, admin, "pay-types", input(`{"name":"数字人民币","type":"ecny","enabled":true}`)); e != nil {
		t.Fatal(e)
	}
	page, e := s.List(ctx, admin, "pay-types", Query{})
	if e != nil {
		t.Fatal(e)
	}
	if page.Total != 8 {
		t.Fatalf("expected 7 originals + ecny, got %d", page.Total)
	}
	last := page.Items[len(page.Items)-1]
	if kind, _ := last.Fields["type"].(string); kind != "ecny" {
		t.Fatalf("non-catalog record should follow the seven, got %s last", kind)
	}
	seen := map[string]bool{}
	for _, it := range page.Items {
		kind, _ := it.Fields["type"].(string)
		seen[kind] = true
	}
	for _, t2 := range []string{"alipay", "wxpay", "qqpay", "bank", "jdpay", "paypal", "douyinpay"} {
		if !seen[t2] {
			t.Fatalf("original type %s missing from the merged list", t2)
		}
	}
}
