VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

.PHONY: build test dist

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/rivet ./cmd/rivet

test:
	go vet ./...
	go test -race ./...

# Release binaries named as install.sh expects: dist/rivet_<os>_<arch>.
dist:
	rm -rf dist
	for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		GOOS=$$os GOARCH=$$arch go build -ldflags "-X main.version=$(VERSION)" -o dist/rivet_$${os}_$${arch} ./cmd/rivet || exit 1; \
	done
