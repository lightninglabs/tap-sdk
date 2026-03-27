PKG := github.com/lightninglabs/tap-sdk

TOOLS_DIR := tools
TOOLS_MOD := $(TOOLS_DIR)/go.mod

GOIMPORTS_PKG := github.com/rinchsan/gosimports/cmd/gosimports

GO_BIN := ${GOPATH}/bin
GOIMPORTS_BIN := $(GO_BIN)/gosimports

GOCC := go
GOTOOL := GOWORK=off $(GOCC) tool -modfile=$(TOOLS_MOD)
GOBUILD := go build -v
GOINSTALL := go install -v
GOTEST := go test -v

GOFILES_NOVENDOR = $(shell find . -type f -name '*.go' -not -path "./vendor/*")
GOLIST := go list -deps $(PKG)/... | grep '$(PKG)'| grep -v '/vendor/'

COMMIT := $(shell git describe --abbrev=40 --dirty)
LDFLAGS := -X $(PKG).Commit=$(COMMIT)

RM := rm -f
CP := cp
MAKE := make
XARGS := xargs -L 1

# Linting uses a lot of memory, so keep it under control by limiting the number
# of workers if requested.
ifneq ($(workers),)
LINT_WORKERS = --concurrency=$(workers)
endif

# Use docker by default; allow overrides and detect podman wrapper.
DOCKER ?= docker

# Worktree support: golangci-lint's issues.new-from-rev compares against
# git history, but worktrees keep .git metadata outside the worktree
# root. When lint runs in Docker without those dirs mounted,
# new-from-rev cannot resolve, so we bind-mount the git dir/common dir
# into the container.
GIT_DIR := $(shell git rev-parse --git-dir 2>/dev/null)
GIT_COMMON_DIR := $(shell git rev-parse --git-common-dir 2>/dev/null)
DOCKER_GIT_MOUNTS :=
ifneq ($(filter /%,$(GIT_DIR)),)
DOCKER_GIT_MOUNTS += -v $(GIT_DIR):$(GIT_DIR)
endif
ifneq ($(filter /%,$(GIT_COMMON_DIR)),)
ifneq ($(GIT_COMMON_DIR),$(GIT_DIR))
DOCKER_GIT_MOUNTS += -v $(GIT_COMMON_DIR):$(GIT_COMMON_DIR)
endif
endif

# Docker cache mounting strategy:
# - CI (GitHub Actions): Use bind mounts to host paths that GA caches
#   persist.
# - Local: Use Docker named volumes (much faster on macOS/Windows due
#   to avoiding slow host-syncing overhead).
# Paths inside container must match GOCACHE/GOMODCACHE in
# tools/Dockerfile.
ifdef CI
# CI mode: bind mount to host paths that GitHub Actions caches.
DOCKER_TOOLS = $(DOCKER) run \
  --rm \
  -v $${HOME}/.cache/go-build:/tmp/build/.cache \
  -v $${HOME}/go/pkg/mod:/tmp/build/.modcache \
  -v $${HOME}/.cache/golangci-lint:/root/.cache/golangci-lint \
  $(DOCKER_GIT_MOUNTS) \
  -v $$(pwd):/build tap-sdk-tools
else
# Local mode: Docker named volumes for fast macOS/Windows performance.
DOCKER_TOOLS = $(DOCKER) run \
  --rm \
  -v tapsdk-go-build-cache:/tmp/build/.cache \
  -v tapsdk-go-mod-cache:/tmp/build/.modcache \
  -v tapsdk-go-lint-cache:/root/.cache/golangci-lint \
  $(DOCKER_GIT_MOUNTS) \
  -v $$(pwd):/build tap-sdk-tools
endif

GREEN := "\\033[0;32m"
NC := "\\033[0m"
define print
	echo $(GREEN)$1$(NC)
endef

default: build

all: build check install

# ============
# INSTALLATION
# ============

build:
	@$(call print, "Building tap-sdk.")
	$(GOBUILD) -ldflags="$(LDFLAGS)" $(PKG)

docker-tools:
	@$(call print, "Building tools docker image.")
	docker build -q -t tap-sdk-tools $(TOOLS_DIR)

# =======
# TESTING
# =======

check: unit

unit:
	@$(call print, "Running unit tests.")
	$(GOTEST) ./...

unit-race:
	@$(call print, "Running unit race tests.")
	env CGO_ENABLED=1 GORACE="history_size=7 halt_on_errors=1" $(GOTEST) -race ./...

# =========
# UTILITIES
# =========
fmt:
	@$(call print, "Fixing imports.")
	$(GOTOOL) $(GOIMPORTS_PKG) -w $(GOFILES_NOVENDOR)
	@$(call print, "Formatting source.")
	gofmt -l -w -s $(GOFILES_NOVENDOR)

lint: docker-tools
	@$(call print, "Linting source.")
	$(DOCKER_TOOLS) golangci-lint run -v $(LINT_WORKERS)

lint-fix: docker-tools
	@$(call print, "Linting and fixing source.")
	$(DOCKER_TOOLS) golangci-lint run -v --fix $(LINT_WORKERS)

.PHONY: default \
	build \
	unit \
	unit-race \
	fmt \
	lint \
	lint-fix