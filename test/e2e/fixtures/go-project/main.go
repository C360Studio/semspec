// Package main is the entry point for the go-project E2E test fixture.
// This is a simple HTTP service with authentication support.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	resp := map[string]string{"status": "ok"}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Error encoding health response: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, "Hello, World!")
	})

	mux.HandleFunc("/health", healthHandler)

	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
