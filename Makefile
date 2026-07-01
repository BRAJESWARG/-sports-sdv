.PHONY: run build test tidy fmt vet

# Loads .env into the environment for `make run` (ignore if absent).
-include .env
export

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...
