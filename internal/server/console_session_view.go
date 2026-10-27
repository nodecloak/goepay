package server

import (
	"strconv"

	"goepay/internal/identity"
)

// Project console transport identities separately from internal User encoding.
// JavaScript must not round a subject ID before an owner-bound component sees it.
// Persistent sessions, authority checks and existing proof formats are unchanged.
func consoleSessionUser(user identity.User) any {
	return struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Role       string `json:"role"`
		AdminClass string `json:"admin_class,omitempty"`
	}{strconv.FormatInt(user.ID, 10), user.Name, user.Role, user.AdminClass}
}
