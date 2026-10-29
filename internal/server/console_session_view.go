package server

import (
	"encoding/json"
	"strconv"

	"goepay/internal/identity"
)

// Project console transport identities separately from internal User encoding.
// JavaScript must not round a subject ID before an owner-bound component sees it.
// Persistent sessions, authority checks and existing proof formats are unchanged.
func consoleSessionUser(user identity.User) any {
	// permissions: nil when unrestricted (NULL column / super), else the granted
	// menu-key set. The web hides any admin menu whose key is not granted; a
	// null value (absent field) means no narrowing. The server remains the
	// authority — this only drives navigation visibility.
	var permissions []string
	if user.Role == "admin" && user.AdminClass != "super" {
		if granted, restricted := parsePermissions(user.Permissions); restricted {
			permissions = []string{}
			for k := range granted {
				permissions = append(permissions, k)
			}
			sortStrings(permissions)
		}
	}
	return struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Role        string   `json:"role"`
		AdminClass  string   `json:"admin_class,omitempty"`
		Permissions []string `json:"permissions,omitempty"`
	}{strconv.FormatInt(user.ID, 10), user.Name, user.Role, user.AdminClass, permissions}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// adminPermissionCatalog is served so the admin-user editor and the web menu can
// render the grantable-page tree without duplicating the Go catalog.
func adminPermissionCatalogJSON() json.RawMessage {
	b, _ := json.Marshal(adminMenuCatalog)
	return b
}
