GO ?= go
BIN_DIR ?= bin
INSTALL_DIR ?= $(shell $(GO) env GOBIN)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf dev)
LDFLAGS := -X droid-switcher/internal/switcher.version=$(VERSION)

ifeq ($(strip $(INSTALL_DIR)),)
INSTALL_DIR := $(shell $(GO) env GOPATH)/bin
endif

.PHONY: all build clean fmt fmt-check help install lint test test-race tidy verify vet vuln

all: build

build:
	mkdir -p "$(BIN_DIR)"
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN_DIR)/droid-switcher" .
	cp "$(BIN_DIR)/droid-switcher" "$(BIN_DIR)/drsw"

clean:
	rm -rf "$(BIN_DIR)" coverage.out

fmt:
	$(GO) tool golangci-lint fmt ./...

fmt-check:
	$(GO) tool golangci-lint fmt --diff ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race -shuffle=on ./...

vet:
	$(GO) vet ./...

lint:
	$(GO) tool golangci-lint run ./...

vuln:
	$(GO) tool govulncheck ./...

tidy:
	$(GO) mod tidy

verify: fmt-check test-race vet lint vuln

install: build
	install -d "$(INSTALL_DIR)"
	install -m 0755 "$(BIN_DIR)/droid-switcher" "$(INSTALL_DIR)/droid-switcher"
	install -m 0755 "$(BIN_DIR)/drsw" "$(INSTALL_DIR)/drsw"

help:
	@printf '%s\n' \
		'build       Build droid-switcher and drsw into bin/' \
		'fmt         Format Go source with the pinned lint toolchain' \
		'test        Run the unit test suite' \
		'test-race   Run tests with race detection and shuffled order' \
		'lint        Run pinned golangci-lint checks' \
		'vuln        Scan reachable code for known vulnerabilities' \
		'verify      Run formatting, race tests, vet, lint, and vulnerability checks' \
		'install     Install both command names into GOBIN (or GOPATH/bin)'
