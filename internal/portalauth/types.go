package portalauth

// Account represents a student portal account exported for the stateless server.
type Account struct {
	StudentID          int    `json:"studentId"`
	Username           string `json:"username"`
	PasswordSalt       string `json:"passwordSalt"`
	PasswordHash       string `json:"passwordHash"`
	MustChangePassword bool   `json:"mustChangePassword"`
	PasswordChangedAt  string `json:"passwordChangedAt"`
}

// AccountList is the top-level structure written to accounts.json.
type AccountList struct {
	Version     int       `json:"version"`
	PublishedAt string    `json:"publishedAt"`
	Accounts    []Account `json:"accounts"`
}
