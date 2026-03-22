BINARY     := mitm
BUILD_DIR  := bin
MODULE     := github.com/mitm/mitm
GO         := go
GOFLAGS    := -trimpath
LDFLAGS    := -s -w

# Angular UI
UI_DIR     := ui
UI_DIST    := internal/api/static
NG         := npx ng

.PHONY: all build test lint clean ui embed run run-dev install-ca help

##@ General

all: build  ## Default target: build the binary

help:        ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

run:        ## Run proxy + API (no UI build, uses last embedded build)
	$(GO) run ./cmd/mitm --v

run-dev:    ## Run Go backend; Angular served separately by ng serve
	$(GO) run ./cmd/mitm --v &
	cd $(UI_DIR) && $(NG) serve --proxy-config proxy.conf.json

install-ca: ## Print CA trust instructions
	$(GO) run ./cmd/mitm --install-ca

##@ Build

ui:         ## Build the Angular UI into internal/api/static
	cd $(UI_DIR) && npm ci && $(NG) build --configuration production --output-path ../$(UI_DIST)

embed: ui   ## Alias: build UI then build Go binary
	$(MAKE) build

build:      ## Build the Go binary (assumes UI already built if embedding)
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/mitm
	@echo "  → $(BUILD_DIR)/$(BINARY)"

build-all:  ## Cross-compile for Linux, macOS (amd64 + arm64) and Windows
	@mkdir -p $(BUILD_DIR)
	GOOS=linux   GOARCH=amd64  $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64    ./cmd/mitm
	GOOS=linux   GOARCH=arm64  $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64    ./cmd/mitm
	GOOS=darwin  GOARCH=amd64  $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-amd64   ./cmd/mitm
	GOOS=darwin  GOARCH=arm64  $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-arm64   ./cmd/mitm
	GOOS=windows GOARCH=amd64  $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe ./cmd/mitm
	@ls -lh $(BUILD_DIR)/

##@ Testing

test:       ## Run all Go tests
	$(GO) test ./... -race -count=1

test-v:     ## Run tests with verbose output
	$(GO) test ./... -race -count=1 -v

test-cover: ## Run tests with coverage report
	$(GO) test ./... -race -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "  → coverage.html"

##@ Quality

lint:       ## Run golangci-lint (install via brew install golangci-lint)
	golangci-lint run ./...

vet:        ## Run go vet
	$(GO) vet ./...

tidy:       ## Tidy and verify go.mod
	$(GO) mod tidy
	$(GO) mod verify

##@ Cleanup

clean:      ## Remove build artefacts
	rm -rf $(BUILD_DIR) coverage.out coverage.html
	rm -rf $(UI_DIST)
