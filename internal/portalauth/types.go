package portalauth

// Account represents a student portal account published to the portal server.
type Account struct {
	StudentID          int    `json:"studentId"`
	Username           string `json:"username"`
	PasswordSalt       string `json:"passwordSalt"`
	PasswordHash       string `json:"passwordHash"`
	MustChangePassword bool   `json:"mustChangePassword"`
	PasswordChangedAt  string `json:"passwordChangedAt"`
}
