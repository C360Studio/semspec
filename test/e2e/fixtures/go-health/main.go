// Package main is an E2E fixture that serves a minimal HTTP health endpoint.
package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "Hello, World!")
	})

	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
