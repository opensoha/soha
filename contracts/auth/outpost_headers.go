package auth

import "strings"

// IsOutpostIdentityHeader identifies the response headers supported by the v1
// Outpost protocol. Transport, cookie and internal authentication headers are
// deliberately excluded, including when returned by a configured claim mapping.
func IsOutpostIdentityHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "x-auth-request-user", "x-auth-request-email", "x-auth-request-groups",
		"x-soha-user", "x-soha-user-id", "x-soha-email", "x-soha-roles",
		"x-soha-teams", "x-soha-groups", "x-soha-projects", "x-soha-tags":
		return true
	default:
		return false
	}
}
