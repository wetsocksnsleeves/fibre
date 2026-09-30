VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/fibre ./cmd/fibre

test:
	go vet ./...
	go test -race ./...
