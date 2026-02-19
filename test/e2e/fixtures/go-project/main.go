// Package main is the entry point for the go-project E2E test fixture.
package main

import (
	"fmt"

	"example.com/testproject/internal/auth"
)

func main() {
	user, err := auth.Authenticate("admin", "secret")
	if err != nil {
		fmt.Println("Auth failed:", err)
		return
	}
	fmt.Println("Welcome", user.Email)

	token, err := auth.RefreshToken("old-refresh-token")
	if err != nil {
		fmt.Println("Refresh failed:", err)
		return
	}
	fmt.Println("New token expires at:", token.ExpiresAt)
}
