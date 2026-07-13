# Siphon build automation.
#
# `make build` produces the real, cluster-capable binary: it enables CGO and
# links the Ceph client libraries (librados/librbd), which must be installed
# along with their development headers (see README). `make build-mock` produces
# a pure-Go binary that talks only to the in-memory mock client, for development
# or demos on machines without the Ceph libraries.

BIN_DIR := bin
BINARY  := $(BIN_DIR)/siphon
VERSION_PKG := github.com/cinpol/siphon/internal/version

# Version metadata stamped into the binary. Override on the command line if
# needed, e.g. `make build VERSION=1.2.3`.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).Date=$(DATE)

.PHONY: build build-mock test vet fmt tidy clean version help

## build: build the real binary (needs CGO + librados/librbd; see README)
build:
	CGO_ENABLED=1 go build -tags goceph -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/siphon

## build-mock: build a pure-Go binary (mock client only; no Ceph libraries needed)
build-mock:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY)-mock ./cmd/siphon

## test: run the test suite
test:
	go test ./...

## vet: run go vet
vet:
	go vet ./...

## fmt: format all Go code
fmt:
	gofmt -w .

## tidy: tidy go.mod / go.sum
tidy:
	go mod tidy

## clean: remove build output
clean:
	rm -rf $(BIN_DIR)

## version: print the version metadata that would be stamped into a build
version:
	@echo "$(VERSION) ($(COMMIT), $(DATE))"

## help: list available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
