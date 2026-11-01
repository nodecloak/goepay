package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/backoffice"
	"goepay/internal/identity"
)

func adminUser(class string, perms []string) identity.User {
	u := identity.User{Role: "admin", AdminClass: class, ID: 7, Username: "op"}
	if perms != nil {
		b, _ := json.Marshal(perms)
		u.Permissions = b
	}
	return u
}

func TestWeixinApplicationsUsesDedicatedMenuGrant(t *testing.T) {
	for _, sub := range []string{"", "capabilities", "requests", "7"} {
		parts := []string{"admin", "weixin-applications"}
		if sub != "" {
			parts = append(parts, sub)
		}
		if routeMenuKey(parts) != "PayWeixin" {
			t.Fatalf("application route is not bound to its own menu grant: %v", parts)
		}
		if !adminRequestAllowed(adminUser("operator", []string{"PayWeixin"}), parts) {
			t.Fatal("application reader cannot enter its granted node")
		}
		if adminRequestAllowed(adminUser("operator", []string{"PayChannel", "UserList"}), parts) {
			t.Fatal("unrelated menu grants allow application access")
		}
	}
}

func TestChannelSharingHTTPEnforcesPageGrantBeforeFinancialOperation(t *testing.T) {
	s := &Server{} // An authorized request stops at the unavailable local store.
	for _, tc := range []struct {
		grant, path string
		parts       []string
		want        int
	}{
		{"PsReceiver", "/console/v1/admin/channel-sharing/orders", []string{"admin", "channel-sharing", "orders"}, 403},
		{"PsOrder", "/console/v1/admin/channel-sharing/orders", []string{"admin", "channel-sharing", "orders"}, 503},
		{"PsOrder", "/console/v1/admin/channel-sharing/returns", []string{"admin", "channel-sharing", "returns"}, 503},
		{"PsReceiver", "/console/v1/admin/channel-sharing/returns", []string{"admin", "channel-sharing", "returns"}, 403},
		{"PsOrder", "/console/v1/admin/channel-sharing/receivers", []string{"admin", "channel-sharing", "receivers"}, 403},
	} {
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(`{}`))
		r.Header.Set("X-CSRF-Token", "csrf")
		w := httptest.NewRecorder()
		session := identity.Session{User: adminUser("operator", []string{tc.grant}), CSRF: "csrf"}
		if !s.channelSharingConsole(w, r, "admin", session, tc.parts) || w.Code != tc.want {
			t.Errorf("grant %s POST %s: status %d, want %d", tc.grant, tc.path, w.Code, tc.want)
		}
	}
}

func TestAdminRequestAllowedSuperAlwaysAllowed(t *testing.T) {
	for _, parts := range [][]string{
		{"admin", "resources", "admin-users"},
		{"admin", "operations", "cleanup"},
		{"admin", "resources", "orders"},
	} {
		if !adminRequestAllowed(adminUser("super", nil), parts) {
			t.Fatalf("super denied %v", parts)
		}
	}
}

func TestAdminRequestAllowedUnrestrictedOperatorKeepsLegacyAccess(t *testing.T) {
	// NULL permissions = pre-migration operator: business pages allowed,
	// super-only pages denied — exactly the old adminOperationAllowed floor.
	op := adminUser("operator", nil)
	allow := [][]string{{"admin", "orders"}, {"admin", "reports", "daily"}, {"admin", "operations", "login-log"}}
	for _, parts := range allow {
		if !adminRequestAllowed(op, parts) {
			t.Fatalf("unrestricted operator denied %v", parts)
		}
	}
	deny := [][]string{{"admin", "resources", "admin-users"}, {"admin", "resources", "settings"}, {"admin", "operations", "cleanup"}, {"admin", "operations", "jobs"}}
	for _, parts := range deny {
		if adminRequestAllowed(op, parts) {
			t.Fatalf("unrestricted operator wrongly allowed %v", parts)
		}
	}
}

func TestAdminRequestAllowedRestrictedOperatorHonoursGrants(t *testing.T) {
	// An operator granted only orders + daily report may reach those pages and
	// nothing else; self-service families remain open.
	op := adminUser("operator", []string{"OrderList", "DailyStat"})
	allow := [][]string{
		{"admin", "orders"},
		{"admin", "orders", "T123", "refund"},
		{"admin", "reports", "daily"},
		{"admin", "session"},
		{"admin", "account", "password"},
		{"admin", "security", "payment-password"},
		{"admin", "logout"},
	}
	for _, parts := range allow {
		if !adminRequestAllowed(op, parts) {
			t.Fatalf("granted operator denied %v", parts)
		}
	}
	deny := [][]string{
		{"admin", "resources", "merchants"},
		{"admin", "reports", "channels"},
		{"admin", "resources", "groups"},
		{"admin", "operations", "login-log"},
		{"admin", "resources", "admin-users"},
	}
	for _, parts := range deny {
		if adminRequestAllowed(op, parts) {
			t.Fatalf("granted operator wrongly allowed %v", parts)
		}
	}
}

func TestAdminRequestAllowedEmptyGrantSetIsFullyRestricted(t *testing.T) {
	// An explicit empty set ([]), distinct from NULL, grants no business page
	// but still allows self-service.
	op := adminUser("operator", []string{})
	if adminRequestAllowed(op, []string{"admin", "orders"}) {
		t.Fatal("empty grant set reached orders")
	}
	if !adminRequestAllowed(op, []string{"admin", "account", "password"}) {
		t.Fatal("empty grant set blocked own password")
	}
}

func TestAdminRequestAllowedMalformedPermissionsFailClosed(t *testing.T) {
	op := identity.User{Role: "admin", AdminClass: "operator", ID: 7, Permissions: []byte("not json")}
	if adminRequestAllowed(op, []string{"admin", "orders"}) {
		t.Fatal("malformed permissions reached a business page")
	}
}

func TestRouteMenuKeyUnmappedIsSuperOnly(t *testing.T) {
	// Routes with no grantable menu return "" so a restricted operator is denied.
	for _, parts := range [][]string{
		{"admin", "operations", "cleanup"},
		{"admin", "resources", "settings"},
		{"admin", "system-update"},
		{"admin", "scheduled-tasks"},
	} {
		if routeMenuKey(parts) != "" {
			t.Fatalf("expected unmapped (super-only) for %v, got %q", parts, routeMenuKey(parts))
		}
	}
}

func TestAdminPermissionCatalogKeysAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range adminMenuCatalog {
		if seen[p.Key] {
			t.Fatalf("duplicate catalog key %s", p.Key)
		}
		seen[p.Key] = true
		if p.Title == "" || p.Group == "" {
			t.Fatalf("catalog key %s missing title/group", p.Key)
		}
	}
	// No catalog key may also be super-only.
	for _, k := range superOnlyMenuKeys {
		if adminMenuKeys[k] {
			t.Fatalf("super-only key %s also in grantable catalog", k)
		}
	}
}

func TestAdminPermissionCatalogMatchesBackoffice(t *testing.T) {
	// The backoffice validator and the server enforcement catalog must grant
	// exactly the same key set, or the editor could store a key the server
	// never honours (or vice versa).
	bo := backoffice.AdminPermissionKeySet()
	if len(bo) != len(adminMenuKeys) {
		t.Fatalf("catalog size mismatch: server=%d backoffice=%d", len(adminMenuKeys), len(bo))
	}
	for k := range adminMenuKeys {
		if !bo[k] {
			t.Fatalf("key %s in server catalog but not backoffice", k)
		}
	}
	for k := range bo {
		if !adminMenuKeys[k] {
			t.Fatalf("key %s in backoffice but not server catalog", k)
		}
	}
}

func TestAdminPageGrantsAreIsolated(t *testing.T) {
	cases := []struct {
		grant string
		allow [][]string
		deny  [][]string
	}{
		{"PsReceiver", [][]string{{"admin", "channel-sharing", "rules", "1"}, {"admin", "channel-sharing", "receivers"}, {"admin", "channel-sharing", "channels"}}, [][]string{{"admin", "channel-sharing", "orders"}, {"admin", "channel-sharing", "returns"}, {"admin", "resources", "profitsharing-rules"}}},
		{"PsOrder", [][]string{{"admin", "channel-sharing", "orders"}, {"admin", "channel-sharing", "returns"}, {"admin", "channel-sharing", "payment", "T1"}, {"admin", "channel-sharing", "channels"}}, [][]string{{"admin", "channel-sharing", "rules", "1"}, {"admin", "channel-sharing", "receivers"}, {"admin", "resources", "profitsharing-rules"}}},
		{"PsInternal", [][]string{{"admin", "resources", "profitsharing-rules"}, {"admin", "workflows", "profitsharing-rules", "1", "execute"}}, [][]string{{"admin", "channel-sharing", "orders"}, {"admin", "channel-sharing", "rules", "1"}}},
		{"PayChannel", [][]string{{"admin", "payment-channels"}}, [][]string{{"admin", "payment-subchannels"}}},
		{"SubChannelManage", [][]string{{"admin", "payment-subchannels"}, {"admin", "payment-subchannels", "schemas"}}, [][]string{{"admin", "payment-channels"}}},
		{"Record", [][]string{{"admin", "ledger"}, {"admin", "ledger", "summary"}}, [][]string{{"admin", "settlement-batches"}}},
		{"BatchSettle", [][]string{{"admin", "settlement-batches"}, {"admin", "settlement-batches", "1", "approve"}}, [][]string{{"admin", "workflows", "settlements"}}},
		{"SettleList", [][]string{{"admin", "workflows", "settlements"}}, [][]string{{"admin", "settlement-batches"}}},
	}
	for _, tc := range cases {
		t.Run(tc.grant, func(t *testing.T) {
			u := adminUser("operator", []string{tc.grant})
			for _, p := range tc.allow {
				if !adminRequestAllowed(u, p) {
					t.Errorf("grant %s denied %v", tc.grant, p)
				}
			}
			for _, p := range tc.deny {
				if adminRequestAllowed(u, p) {
					t.Errorf("grant %s wrongly allows %v", tc.grant, p)
				}
			}
		})
	}
}

func TestConsoleSessionUserPreservesEmptyPermissionSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		user identity.User
		want string
	}{
		{"unrestricted", adminUser("operator", nil), "absent"},
		{"empty", adminUser("operator", []string{}), "[]"},
		{"granted", adminUser("operator", []string{"OrderList"}), `["OrderList"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(consoleSessionUser(tc.user))
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(b, &fields); err != nil {
				t.Fatal(err)
			}
			got, present := fields["permissions"]
			if tc.want == "absent" {
				if present {
					t.Fatalf("expected absent permissions, got %s", got)
				}
			} else if !present || string(got) != tc.want {
				t.Fatalf("permissions = %s, present = %v, want %s", got, present, tc.want)
			}
		})
	}
}
