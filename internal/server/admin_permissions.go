package server

import "goepay/internal/identity"

// R_ADMIN operators retain business-management access while R_SUPER is required
// for security-sensitive system configuration and worker control.
func adminOperationAllowed(class string, parts []string) bool {
	if class == "super" {
		return true
	}
	if class != "operator" || len(parts) < 2 {
		return false
	}
	if len(parts) < 3 {
		return true
	}
	switch parts[1] {
	case "resources", "manage":
		switch parts[2] {
		case "settings", "admin-users", "cron-tasks", "plugin-configs", "template-configs", "addon-configs":
			return false
		}
	case "operations":
		switch parts[2] {
		case "jobs", "system-info", "cleanup":
			return false
		}
	}
	return true
}

// adminRequestAllowed decides whether an administrator session may reach the
// endpoint named by parts. It layers per-menu permissions (migration 000132) on
// top of the coarse super/operator floor:
//
//   - super: always allowed.
//   - operator with NO explicit permission set (NULL column): the original
//     operator access, unchanged (adminOperationAllowed).
//   - operator WITH an explicit permission set: allowed only when the route maps
//     to a granted menu key; routes with no grantable menu (super-only pages and
//     system operations) are refused. Self-service routes (session/account/
//     security/logout) are always allowed so a restricted admin can still manage
//     their own password and MFA.
func adminRequestAllowed(user identity.User, parts []string) bool {
	if user.AdminClass == "super" {
		return true
	}
	if !adminOperationAllowed(user.AdminClass, parts) {
		return false
	}
	granted, restricted := parsePermissions(user.Permissions)
	if !restricted {
		return true
	}
	if len(parts) >= 2 && alwaysAllowedAdminFamily(parts[1]) {
		return true
	}
	// Both channel-sharing pages need the read-only channel capability list.
	// The handler itself only implements GET for this exact route.
	if len(parts) == 3 && parts[1] == "channel-sharing" && parts[2] == "channels" {
		return granted["PsReceiver"] || granted["PsOrder"]
	}
	key := routeMenuKey(parts)
	if key == "" {
		// Unmapped routes (super-only pages, system operations) are denied to a
		// restricted operator.
		return false
	}
	return granted[key]
}

// alwaysAllowedAdminFamily lists route families every signed-in administrator
// may use regardless of menu grants: own session, own account/password/MFA, and
// logout. Denying these would lock a restricted operator out of changing their
// own password.
func alwaysAllowedAdminFamily(family string) bool {
	switch family {
	case "session", "logout", "account", "security":
		return true
	}
	return false
}
