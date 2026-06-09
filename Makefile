# Formae Plugin Makefile
#
# Targets:
#   build   - Build the plugin binary
#   test    - Run tests
#   lint    - Run linter
#   clean   - Remove build artifacts
#   install - Build and install plugin locally (binary + schema + manifest)

# Plugin metadata - extracted from formae-plugin.pkl
PLUGIN_NAME := $(shell pkl eval -x 'name' formae-plugin.pkl 2>/dev/null || echo "example")
PLUGIN_VERSION := $(shell pkl eval -x 'version' formae-plugin.pkl 2>/dev/null || echo "0.0.0")
PLUGIN_NAMESPACE := $(shell pkl eval -x 'namespace' formae-plugin.pkl 2>/dev/null || echo "EXAMPLE")

# Build settings
GO := go
GOFLAGS := -trimpath
BINARY := $(PLUGIN_NAME)

# Installation paths
# Plugin discovery expects lowercase directory names matching the plugin name
PLUGIN_BASE_DIR := $(HOME)/.pel/formae/plugins
INSTALL_DIR := $(PLUGIN_BASE_DIR)/$(PLUGIN_NAME)/v$(PLUGIN_VERSION)

.PHONY: all build test test-unit test-integration lint verify-schema clean install help clean-environment vllm-up conformance-test conformance-test-aws

all: build

## build: Build the plugin binary and update manifest
build:
	@mkdir -p schema/pkl && echo "$(PLUGIN_VERSION)" > schema/pkl/VERSION
	$(GO) build $(GOFLAGS) -o bin/$(BINARY) .
	@# minFormaeVersion is pinned manually in formae-plugin.pkl (currently 0.86.0).
	@# The SDK auto-stamp was removed because the SDK floor (pkg/plugin MinFormaeVersion)
	@# lags the agent's stable release (still 0.84.0 at pkg/plugin@v0.3.0); re-enable it
	@# once the SDK floor catches up to the targeted stable agent.

## test: Run all tests
test:
	$(GO) test -v ./...

## test-unit: Run unit tests only (tests with //go:build unit tag)
test-unit:
	$(GO) test -v -tags=unit ./...

## test-integration: Run integration tests (requires cloud credentials)
## Add tests with //go:build integration tag
test-integration:
	$(GO) test -v -tags=integration ./...

## lint: Run golangci-lint
lint:
	golangci-lint run

## verify-schema: Validate PKL schema files
## Checks that schema files are well-formed and follow formae conventions.
verify-schema:
	$(GO) run github.com/platform-engineering-labs/formae/pkg/plugin/testutil/cmd/verify-schema --namespace $(PLUGIN_NAMESPACE) ./schema/pkl

## clean: Remove build artifacts
clean:
	rm -rf bin/ dist/

## install: Build and install plugin locally (binary + schema + manifest)
## Installs to ~/.pel/formae/plugins/<name>/v<version>/
## Removes any existing versions of the plugin first to ensure clean state.
install: build
	@echo "Installing $(PLUGIN_NAME) v$(PLUGIN_VERSION) (namespace: $(PLUGIN_NAMESPACE))..."
	@rm -rf $(PLUGIN_BASE_DIR)/$(PLUGIN_NAME)
	@mkdir -p $(INSTALL_DIR)/schema
	@cp bin/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	@cp -r schema/* $(INSTALL_DIR)/schema/
	@cp formae-plugin.pkl $(INSTALL_DIR)/
	@echo "Installed to $(INSTALL_DIR)"
	@echo "  - Binary: $(INSTALL_DIR)/$(BINARY)"
	@echo "  - Schema: $(INSTALL_DIR)/schema/"
	@echo "  - Manifest: $(INSTALL_DIR)/formae-plugin.pkl"

## help: Show this help message
help:
	@echo "Available targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'

## clean-environment: Tear down conformance test resources (the real vLLM container)
## Called before and after conformance tests. See scripts/ci/clean-environment.sh.
clean-environment:
	@./scripts/ci/clean-environment.sh

# Conformance runs against a REAL vLLM (CPU container). The in-process fake
# (internal/fakevllm) backs the integration tests ONLY — never conformance:
# idempotency, provider-populated id/parent/root and path normalization can only
# be proven against real vLLM.
#
# Host port the real vLLM container publishes. Override if taken, e.g.
# `make conformance-test VLLM_PORT=8200`.
VLLM_PORT ?= 8100
export VLLM_URL ?= http://127.0.0.1:$(VLLM_PORT)
# Per-run id used by testdata for unique adapter names.
export FORMAE_TEST_RUN_ID ?= local-$(shell date +%s)

## vllm-up: boot a real CPU vLLM server for conformance (idempotent)
vllm-up:
	@./scripts/ci/vllm-up.sh

## conformance-test-aws: DOGFOOD real-vLLM conformance on AWS. formae's AWS
## plugin provisions a g4dn (T4) GPU box running vLLM, conformance runs against
## it, then formae destroys everything (guaranteed teardown). This is the
## authoritative real-vLLM gate — a GPU is REQUIRED (CPU-only hosts hit a vLLM
## LoRA pin_memory bug). Billable; needs AWS creds + the formae CLI.
conformance-test-aws:
	@./scripts/ci/conformance-aws.sh

## conformance-test: Boot a real vLLM (CPU container), run CRUD + discovery
## conformance against it, then tear it down. Requires Docker.
## NOTE: vLLM v0.22.1 has a CPU LoRA bug (is_pin_memory_available wrongly True
## off-WSL) so this CPU path only works on WSL/GPU hosts; the AWS GPU dogfood
## (`make conformance-test-aws`) is the portable gate. Use this for quick local
## checks on a WSL/GPU workstation.
## Usage: make conformance-test [TEST=Name] [TIMEOUT=30m]
## Set VLLM_EXTERNAL=1 (and VLLM_URL=...) to target an already-running vLLM and
## skip the managed container (e.g. a real GPU box).
conformance-test: install
	@if [ -z "$(VLLM_EXTERNAL)" ]; then ./scripts/ci/vllm-up.sh; else echo "Using external vLLM at $(VLLM_URL)"; fi
	@echo ""
	@echo "Running conformance tests (VLLM_URL=$(VLLM_URL))..."
	@$(GO) test -tags=conformance -v -timeout $(or $(TIMEOUT),30m) $(if $(TEST),-run $(TEST),) ./...; \
	TEST_EXIT=$$?; \
	echo ""; \
	if [ -z "$(VLLM_EXTERNAL)" ]; then echo "Tearing down vLLM..."; ./scripts/ci/clean-environment.sh || true; fi; \
	exit $$TEST_EXIT
