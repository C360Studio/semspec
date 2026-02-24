.PHONY: build test run lint

build:
	go build -o bin/server ./cmd/server

test:
	go test ./... -race -coverprofile=coverage.out

lint:
	golangci-lint run ./...

run:
	go run ./cmd/server

coverage:
	go tool cover -html=coverage.out
