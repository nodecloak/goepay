package server

import (
	"encoding/json"
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
