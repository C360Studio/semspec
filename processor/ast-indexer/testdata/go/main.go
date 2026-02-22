package main

// User represents a user in the system.
type User struct {
	ID    int
	Name  string
	Email string
}

// NewUser creates a new user with the given name.
func NewUser(name string) *User {
	return &User{Name: name}
}

// Greet returns a greeting for the user.
func (u *User) Greet() string {
	return "Hello, " + u.Name
}

// MaxUsers is the maximum number of users.
const MaxUsers = 100
