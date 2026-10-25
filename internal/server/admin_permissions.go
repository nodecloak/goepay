package server

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
