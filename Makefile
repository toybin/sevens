SHELL := /bin/sh

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
BIN_DIR := $(ROOT_DIR)/bin
BINARY := $(BIN_DIR)/sevens
INSTALL_DIR ?= $(HOME)/.local/bin
INSTALL_PATH := $(INSTALL_DIR)/sevens
GO_REQUIRED := $(shell awk '/^go / {print $$2; exit}' go.mod)
GOCACHE ?= /tmp/sevens-gocache

.PHONY: check-go build install test clean

check-go:
	@actual="$$(go env GOVERSION 2>/dev/null | sed 's/^go//')"; \
	if [ -z "$$actual" ]; then \
		echo "go is not available in PATH"; \
		exit 1; \
	fi; \
	if [ "$$actual" != "$(GO_REQUIRED)" ]; then \
		echo "go version $$actual does not match required $(GO_REQUIRED)"; \
		exit 1; \
	fi

build: check-go $(BINARY)

$(BINARY): go.mod
	@mkdir -p "$(BIN_DIR)"
	GOCACHE="$(GOCACHE)" go build -o "$(BINARY)" ./cmd/sevens

install: build
	@mkdir -p "$(INSTALL_DIR)"
	@rm -f "$(INSTALL_PATH)"
	install -m 0755 "$(BINARY)" "$(INSTALL_PATH)"
	@echo "installed $(INSTALL_PATH)"

test: check-go
	GOCACHE="$(GOCACHE)" go test ./...

clean:
	rm -rf "$(BIN_DIR)"
