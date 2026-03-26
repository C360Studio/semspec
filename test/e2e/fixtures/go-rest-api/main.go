// Package main is an E2E fixture that serves a minimal REST API for testing.
package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "User Service v0.1.0")
	})

	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
