package merchantpolicy

import "testing"

func TestOriginalTransferPermissionInheritanceAndCanonicalPrecedence(t *testing.T) {
	for _, v := range []struct {
		name              string
		settings          map[string]string
		group             string
		enabled, platform bool
		source            string
	}{
		{"missing", nil, "", false, false, "default"},
		{"legacy", map[string]string{"user_transfer": "1"}, "", true, true, "legacy_global"},
		{"canonical_off", map[string]string{"transfer_enabled": "false", "user_transfer": "1"}, "", false, false, "global"},
		{"canonical_on", map[string]string{"transfer_enabled": "true", "user_transfer": "0"}, "", true, true, "global"},
		{"group_on", map[string]string{"transfer_enabled": "false"}, "1", true, false, "group"},
		{"legacy_zero_inherits", map[string]string{"transfer_enabled": "true"}, "0", true, true, "global"},
		{"underscore_inherits", map[string]string{"transfer_enabled": "false"}, "_", false, false, "global"},
	} {
		t.Run(v.name, func(t *testing.T) {
			a, e := resolve(v.settings, v.group, 9007199254740993)
			if e != nil || a.Enabled != v.enabled || a.PlatformEnabled != v.platform || a.Source != v.source || a.GroupID != 9007199254740993 {
				t.Fatalf("permission mismatch %+v %v", a, e)
			}
		})
	}
	for _, v := range []struct {
		settings map[string]string
		group    string
	}{{nil, "2"}, {nil, "true"}, {nil, "1\n"}, {map[string]string{"transfer_enabled": "1"}, "1"}, {map[string]string{"user_transfer": "true"}, "1"}, {map[string]string{"unknown": "true"}, ""}} {
		if _, e := resolve(v.settings, v.group, 0); e == nil {
			t.Fatalf("malformed permission accepted %+v", v)
		}
	}
}
