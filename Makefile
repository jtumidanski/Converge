SHELL := /bin/bash
.DEFAULT_GOAL := help

ROOT      := $(shell git rev-parse --show-toplevel)
BACKEND   := $(ROOT)/apps/backend
FRONTEND  := $(ROOT)/apps/frontend
VERSION   ?= $(shell $(ROOT)/tools/version.sh)
GIT_SHA   ?= $(shell git rev-parse HEAD)
LDFLAGS   := -s -w -X github.com/jtumidanski/converge/internal/buildinfo.Version=$(VERSION)

.PHONY: help version lint test test-integration build

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

version: ## Print the build version
	@echo $(VERSION)

lint: ## Lint backend (frontend added in Task 21)
	cd $(BACKEND) && go vet ./... && go tool golangci-lint run

test: ## Unit tests
	cd $(BACKEND) && go test -race -count=1 ./...

test-integration: ## Integration tests (local git only, no network)
	cd $(BACKEND) && go test -race -count=1 -tags integration ./...

build: ## Build backend binaries into dist/ (frontend build wired in Task 21)
	cd $(BACKEND) && CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" ./...
