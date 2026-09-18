package portalauth

import "strings"

// ReservedDemoUsername is the portal server's built-in demo account. The CLI
// must never assign it to a real student: the server refuses to seed the demo
// account while a real account owns the name, and while the demo is enabled
// the name gets read-only treatment.
const ReservedDemoUsername = "demo"

// IsReservedUsername reports whether a username is reserved for server-managed
// accounts and must not be generated for a student.
func IsReservedUsername(username string) bool {
	return strings.EqualFold(strings.TrimSpace(username), ReservedDemoUsername)
}

// Account represents a student portal account published to the portal server.
type Account struct {
	StudentID          int    `json:"studentId"`
	Username           string `json:"username"`
	PasswordSalt       string `json:"passwordSalt"`
	PasswordHash       string `json:"passwordHash"`
	MustChangePassword bool   `json:"mustChangePassword"`
	PasswordChangedAt  string `json:"passwordChangedAt"`
}
