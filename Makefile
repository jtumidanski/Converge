SHELL := /bin/bash
.DEFAULT_GOAL := help

ROOT      := $(shell git rev-parse --show-toplevel)
BACKEND   := $(ROOT)/apps/backend
FRONTEND  := $(ROOT)/apps/frontend
VERSION   ?= $(shell $(ROOT)/tools/version.sh)
GIT_SHA   ?= $(shell git rev-parse HEAD)
LDFLAGS   := -s -w -X github.com/jtumidanski/converge/internal/buildinfo.Version=$(VERSION)

NPM := export NVM_DIR="$$HOME/.nvm" && . "$$NVM_DIR/nvm.sh" >/dev/null && nvm use 22 >/dev/null && npm

.PHONY: help version lint test test-integration build

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

version: ## Print the build version
	@echo $(VERSION)

lint: ## Lint backend and frontend
	cd $(BACKEND) && go vet ./... && go tool golangci-lint run
	cd $(FRONTEND) && $(NPM) run lint && $(NPM) run format:check

test: ## Unit tests, both apps
	cd $(BACKEND) && go test -race -count=1 ./...
	cd $(FRONTEND) && $(NPM) test

test-integration: ## Integration tests (local git only, no network)
	cd $(BACKEND) && go test -race -count=1 -tags integration ./...

build: ## Build the frontend into the backend embed dir, then the binaries
	cd $(FRONTEND) && $(NPM) ci && $(NPM) run build
	touch $(BACKEND)/internal/ui/dist/.gitkeep
	cd $(BACKEND) && CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" ./...
	$(ROOT)/tools/build-backend.sh
