package backoffice

import "testing"

// Pro's 授权域名 list shows 添加时间 (and the admin reviews 审核通过). GOEPAY surfaces
// created_at as a read-only 添加时间 column, like the blacklist console, without
// fabricating a 审核时间 it does not track. merchant_id/approved stay admin-only.
func TestDomainsSchemaShowsAddedTime(t *testing.T) {
	admin := Principal{Role: "admin", AdminClass: "super", SubjectID: 1, Username: "local"}
	schema, err := New(nil, nil).Schema(admin, "domains")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name, label, typ string
		readonly         bool
	}{
		{"merchant_id", "商户 ID", "string", false},
		{"domain", "域名", "string", false},
		{"approved", "审核通过", "boolean", false},
		{"enabled", "启用", "boolean", false},
		{"created_at", "添加时间", "string", true},
	}
	if len(schema.Fields) != len(want) {
		t.Fatalf("domains field count = %d, want %d: %+v", len(schema.Fields), len(want), schema.Fields)
	}
	for i, w := range want {
		f := schema.Fields[i]
		if f.Name != w.name || f.Label != w.label || f.Type != w.typ || f.ReadOnly != w.readonly {
			t.Fatalf("field %d = {%s %s %s ro=%v}, want {%s %s %s ro=%v}", i, f.Name, f.Label, f.Type, f.ReadOnly, w.name, w.label, w.typ, w.readonly)
		}
	}

	// For a merchant the admin-only fields (merchant_id, approved) become read-only,
	// and 添加时间 stays read-only.
	m := Principal{Role: "merchant", SubjectID: 101, Username: "m"}
	ms, err := New(nil, nil).Schema(m, "domains")
	if err != nil {
		t.Fatal(err)
	}
	if !ms.Fields[0].ReadOnly || !ms.Fields[2].ReadOnly || !ms.Fields[4].ReadOnly {
		t.Fatalf("merchant domains: merchant_id/approved/created_at must be read-only: %+v", ms.Fields)
	}
}
