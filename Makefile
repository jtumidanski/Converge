SHELL := /bin/bash
.DEFAULT_GOAL := help

ROOT      := $(shell git rev-parse --show-toplevel)
BACKEND   := $(ROOT)/apps/backend
FRONTEND  := $(ROOT)/apps/frontend
VERSION   ?= $(shell $(ROOT)/tools/version.sh)
GIT_SHA   ?= $(shell git rev-parse HEAD)
LDFLAGS   := -s -w -X github.com/jtumidanski/converge/internal/buildinfo.Version=$(VERSION)
IMAGE     ?= $(if $(IMAGE_REPOSITORY),$(IMAGE_REPOSITORY),$(if $(CI_REGISTRY_IMAGE),$(CI_REGISTRY_IMAGE),converge))
PLATFORM  ?= linux/amd64
NPM       := export NVM_DIR="$$HOME/.nvm" && [ -s "$$NVM_DIR/nvm.sh" ] && . "$$NVM_DIR/nvm.sh" >/dev/null && nvm use 22 >/dev/null; npm

.PHONY: help version lint test test-integration build docker-build docker-push release-github dev clean

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

test-integration: ## Reconstruction integration tests (local git only, no network)
	cd $(BACKEND) && go test -race -count=1 -tags integration ./...

build: ## Build the UI into the embed directory, then cross-compile the binaries
	cd $(FRONTEND) && $(NPM) ci && $(NPM) run build
	touch $(BACKEND)/internal/ui/dist/.gitkeep
	VERSION=$(VERSION) $(ROOT)/tools/build-backend.sh

docker-build: ## Build the container image
	docker buildx build \
		--platform $(PLATFORM) \
		--build-arg VERSION=$(VERSION) \
		--tag $(IMAGE):$(VERSION) \
		--tag $(IMAGE):$(GIT_SHA) \
		$(if $(filter 1,$(MAINLINE)),--tag $(IMAGE):latest,) \
		--load \
		$(ROOT)

docker-push: ## Push the image tags (registry from CI variables)
	VERSION=$(VERSION) GIT_SHA=$(GIT_SHA) $(ROOT)/tools/docker-push.sh

release-github: ## Attach dist artifacts to the current tag's GitHub Release
	gh release create $(VERSION) $(ROOT)/dist/*.tar.gz --generate-notes || \
		gh release upload $(VERSION) $(ROOT)/dist/*.tar.gz --clobber

dev: ## Run the backend and the Vite dev server together
	cd $(BACKEND) && go run ./cmd/converge & \
	backend_pid=$$!; \
	cd $(FRONTEND) && $(NPM) run dev & \
	frontend_pid=$$!; \
	trap "kill $$backend_pid $$frontend_pid 2>/dev/null" EXIT INT TERM; \
	wait -n $$backend_pid $$frontend_pid; \
	status=$$?; \
	kill $$backend_pid $$frontend_pid 2>/dev/null; \
	exit $$status

clean: ## Remove build artifacts
	rm -rf $(ROOT)/dist $(FRONTEND)/dist
	find $(BACKEND)/internal/ui/dist -mindepth 1 ! -name .gitkeep -delete
