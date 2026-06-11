package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/davidpopovici01/grades/internal/portalauth"
)

func main() {
	hash, salt, err := portalauth.HashPassword("testpass")
	if err != nil {
		panic(err)
	}
	accounts := portalauth.AccountList{
		Version: 1,
		Accounts: []portalauth.Account{
			{
				StudentID:          1,
				Username:           "john.doe",
				PasswordSalt:       salt,
				PasswordHash:       hash,
				MustChangePassword: false,
			},
		},
	}
	data, _ := json.MarshalIndent(accounts, "", "  ")
	os.WriteFile("test-data/accounts.json", append(data, '\n'), 0644)
	fmt.Println("Generated test-data/accounts.json")
	fmt.Println("Login: john.doe / testpass")
}
