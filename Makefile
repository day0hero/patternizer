# Container-related variables
NAME := patternizer
TAG := latest
CONTAINER ?= $(NAME):$(TAG)
REGISTRY ?= localhost
UPLOADREGISTRY ?= quay.io/validatedpatterns

# Go-related variables
GO_CMD := go
GO_BUILD := $(GO_CMD) build
GO_TEST := $(GO_CMD) test
GO_CLEAN := $(GO_CMD) clean
GO_VET := $(GO_CMD) vet
GO_FMT := gofmt
GO_VERSION := 1.26.3
GOLANGCI_LINT_VERSION := v2.12.2
GINKGO_VERSION := v2.32.0
SRC_DIR := src

# Default target
.DEFAULT_GOAL := help

##@ Help-related tasks
.PHONY: help
help: ## Help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^(\s|[a-zA-Z_0-9-])+:.*?##/ { printf "  \033[36m%-35s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Go-related tasks
.PHONY: build
build: ## Build the patternizer binary
	@echo "Building patternizer..."
	cd $(SRC_DIR) && $(GO_BUILD) -v -o $(NAME) .
	@echo "Build complete: $(SRC_DIR)/$(NAME)"

.PHONY: clean
clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	cd $(SRC_DIR) && $(GO_CLEAN)
	rm -f $(SRC_DIR)/$(NAME)
	rm -f $(SRC_DIR)/coverage.out
	@echo "Clean complete"

.PHONY: deps
deps: ## Download and install Go dependencies
	@echo "Installing dependencies..."
	cd $(SRC_DIR) && $(GO_CMD) mod download
	cd $(SRC_DIR) && $(GO_CMD) mod tidy
	@echo "Dependencies installed"

.PHONY: test-unit
test-unit: ## Run unit tests
	@echo "Running unit tests..."
	ginkgo -v ./...

.PHONY: test-coverage
test-coverage: ## Run unit tests with coverage report
	@echo "Running unit tests with coverage..."
	cd $(SRC_DIR) && $(GO_TEST) ./... -coverprofile=coverage.out
	cd $(SRC_DIR) && $(GO_CMD) tool cover -func=coverage.out

.PHONY: test
test: test-unit ## Run all tests (unit + integration)

.PHONY: lint
lint: lint-fmt lint-vet lint-golangci ## Run all linting checks

.PHONY: lint-fmt
lint-fmt: ## Check Go formatting
	@echo "Checking Go formatting..."
	@cd $(SRC_DIR) && if [ "$$($(GO_FMT) -s -l . | wc -l)" -gt 0 ]; then \
		echo "The following files are not formatted:"; \
		$(GO_FMT) -s -l .; \
		exit 1; \
	fi
	@echo "Go formatting check passed"

.PHONY: lint-vet
lint-vet: ## Run go vet
	@echo "Running go vet..."
	cd $(SRC_DIR) && $(GO_VET) ./...
	@echo "Go vet passed"

.PHONY: lint-golangci
lint-golangci: ## Run golangci-lint
	@echo "Running golangci-lint..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $$(go env GOPATH)/bin $(GOLANGCI_LINT_VERSION); \
	fi
	cd $(SRC_DIR) && $$(go env GOPATH)/bin/golangci-lint run
	@echo "golangci-lint passed"

.PHONY: fmt
fmt: ## Format Go code
	@echo "Formatting Go code..."
	cd $(SRC_DIR) && $(GO_FMT) -s -w .
	@echo "Go code formatted"

.PHONY: ci
ci: lint build test ## Run the full CI pipeline locally

.PHONY: dev-setup
dev-setup: deps ## Set up development environment
	@echo "Setting up development environment..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $$(go env GOPATH)/bin $(GOLANGCI_LINT_VERSION); \
	fi
	@if ! command -v ginkgo >/dev/null 2>&1; then \
		echo "Installing ginkgo CLI..."; \
		go install github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_VERSION); \
	fi
	@echo "Development environment ready"

.PHONY: check
check: lint-fmt lint-vet build test-unit ## Quick check (format, vet, build, unit tests)

.PHONY: all
all: clean deps lint build test podman-build ## Run everything

##@ Conatiner-related tasks
.PHONY: manifest
manifest: ## creates the buildah manifest for multi-arch images
	# The rm is needed due to bug https://www.github.com/containers/podman/issues/19757
	buildah manifest rm "${REGISTRY}/${CONTAINER}" || /bin/true
	buildah manifest create "${REGISTRY}/${CONTAINER}"

.PHONY: amd64
amd64: manifest podman-build-amd64 ## Build the container on amd64

.PHONY: arm64
arm64: manifest podman-build-arm64 ## Build the container on arm64

.PHONY: podman-build
podman-build: manifest podman-build-amd64 podman-build-arm64 ## Build both amd64 and arm64

.PHONY: podman-build-amd64
podman-build-amd64: ## build the container in amd64
	@echo "Building the patternizer amd64"
	buildah bud --platform linux/amd64 --format docker -f Containerfile -t "${CONTAINER}-amd64"
	buildah manifest add --arch=amd64 "${REGISTRY}/${CONTAINER}" "${REGISTRY}/${CONTAINER}-amd64"

.PHONY: podman-build-arm64
podman-build-arm64: ## build the container in arm64
	@echo "Building the patternizer arm64"
	buildah bud --platform linux/arm64 --build-arg GOARCH="arm64" --format docker -f Containerfile -t "${CONTAINER}-arm64"
	buildah manifest add --arch=arm64 "${REGISTRY}/${CONTAINER}" "${REGISTRY}/${CONTAINER}-arm64"

.PHONY: upload
upload: ## Uploads the container to quay.io/validatedpatterns/${CONTAINER}
	@echo "Uploading the ${REGISTRY}/${CONTAINER} container to ${UPLOADREGISTRY}/${CONTAINER}"
	buildah manifest push --all --format v2s2 "${REGISTRY}/${CONTAINER}" "docker://${UPLOADREGISTRY}/${CONTAINER}"
