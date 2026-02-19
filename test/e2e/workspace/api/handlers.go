// Package main implements the HTTP handlers for the todo app workspace.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

var (
	todos   = make(map[string]*Todo)
	todosMu sync.RWMutex
	nextID  = 1
)

// ListTodos returns all todos as JSON.
func ListTodos(w http.ResponseWriter, _ *http.Request) {
	todosMu.RLock()
	defer todosMu.RUnlock()
	list := make([]*Todo, 0, len(todos))
	for _, t := range todos {
		list = append(list, t)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// CreateTodo creates a new todo from the request body.
func CreateTodo(w http.ResponseWriter, r *http.Request) {
	var t Todo
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	todosMu.Lock()
	t.ID = fmt.Sprintf("%d", nextID)
	nextID++
	todos[t.ID] = &t
	todosMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

// UpdateTodo updates an existing todo by ID.
func UpdateTodo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	todosMu.Lock()
	defer todosMu.Unlock()

	existing, ok := todos[id]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var updates Todo
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if updates.Title != "" {
		existing.Title = updates.Title
	}
	if updates.Description != "" {
		existing.Description = updates.Description
	}
	existing.Completed = updates.Completed

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(existing)
}

// DeleteTodo removes a todo by ID.
func DeleteTodo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	todosMu.Lock()
	defer todosMu.Unlock()

	if _, ok := todos[id]; !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	delete(todos, id)
	w.WriteHeader(http.StatusNoContent)
}
