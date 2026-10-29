package backoffice

import "testing"

// The blacklist console mirrors Pro's labels (黑名单类型 / 黑名单内容 / 备注) and
// shows 添加时间 as a read-only column, while keeping the 启用 switch and the
// read-only 到期时间. The kind options stay ip/domain/buyer.
func TestBlacklistSchemaMirrorsProLabels(t *testing.T) {
	p := Principal{Role: "admin", AdminClass: "super", SubjectID: 1, Username: "local"}
	schema, err := New(nil, nil).Schema(p, "blacklist")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name, label, typ string
		readonly         bool
	}{
		{"kind", "黑名单类型", "select", false},
		{"value", "黑名单内容", "string", false},
		{"reason", "备注", "string", false},
		{"enabled", "启用", "boolean", false},
		{"expires_at", "到期时间（空为永久）", "string", true},
		{"created_at", "添加时间", "string", true},
	}
	if len(schema.Fields) != len(want) {
		t.Fatalf("blacklist field count = %d, want %d: %+v", len(schema.Fields), len(want), schema.Fields)
	}
	for i, w := range want {
		f := schema.Fields[i]
		if f.Name != w.name || f.Label != w.label || f.Type != w.typ || f.ReadOnly != w.readonly {
			t.Fatalf("field %d = {%s %s %s ro=%v}, want {%s %s %s ro=%v}", i, f.Name, f.Label, f.Type, f.ReadOnly, w.name, w.label, w.typ, w.readonly)
		}
	}
	kind := schema.Fields[0]
	gotOpts := make([]string, 0, len(kind.Options))
	for _, o := range kind.Options {
		gotOpts = append(gotOpts, o.Value)
	}
	for i, v := range []string{"ip", "domain", "buyer"} {
		if i >= len(gotOpts) || gotOpts[i] != v {
			t.Fatalf("kind options = %v, want ip/domain/buyer", gotOpts)
		}
	}
	// 添加时间 and 到期时间 are read-only, so a write never carries them.
	if !schema.Fields[5].ReadOnly || !schema.Fields[4].ReadOnly {
		t.Fatal("created_at / expires_at must be read-only")
	}
}
