.PHONY: help build test run clean frontend-install frontend-dev frontend-build frontend-clean frontend-validate install-deps fmt vet lint all

# Go binary path (adjust if Go is installed elsewhere)
GO := go
ifeq ($(shell which go),)
	# Try Homebrew Go 1.23 path
	GO := /opt/homebrew/opt/go@1.23/bin/go
endif

# Binary name
BINARY := arbiter

# Default configuration
BIND_ADDRESS ?= 127.0.0.1:9100
DATABASE_URL ?= sqlite:///tmp/arbiter.db
KILLSWITCH_BASE_URL ?= http://127.0.0.1:8080
GATEKEEPER_BASE_URL ?= http://127.0.0.1:3005

help: ## Show this help message
	@echo "Available targets:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Environment variables:"
	@echo "  BIND_ADDRESS         - Address to bind server (default: 127.0.0.1:9100)"
	@echo "  DATABASE_URL         - SQLite database URL (default: sqlite:///tmp/arbiter.db)"
	@echo "  KILLSWITCH_BASE_URL  - Killswitch service URL (default: http://127.0.0.1:9090)"
	@echo "  GATEKEEPER_BASE_URL  - Gatekeeper service URL (default: http://127.0.0.1:3000)"

build: ## Build the arbiter binary
	@echo "Building $(BINARY)..."
	@$(GO) build -o $(BINARY) ./cmd/arbiter
	@echo "Build complete: ./$(BINARY)"

test: ## Run all tests
	@echo "Running tests..."
	@$(GO) test ./... -v

test-short: ## Run all tests (short output)
	@echo "Running tests..."
	@$(GO) test ./...

run: build ## Run the server
	@echo "Starting $(BINARY) server..."
	@echo "Bind Address: $(BIND_ADDRESS)"
	@echo "Database URL: $(DATABASE_URL)"
	@echo "Killswitch URL: $(KILLSWITCH_BASE_URL)"
	@echo "Gatekeeper URL: $(GATEKEEPER_BASE_URL)"
	@echo ""
	@ARBITER_BIND_ADDR=$(BIND_ADDRESS) \
	 DATABASE_URL=$(DATABASE_URL) \
	 KILLSWITCH_BASE_URL=$(KILLSWITCH_BASE_URL) \
	 GATEKEEPER_BASE_URL=$(GATEKEEPER_BASE_URL) \
	 ./$(BINARY)

clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -f $(BINARY)
	@echo "Clean complete"

install-deps: ## Download Go dependencies
	@echo "Downloading dependencies..."
	@$(GO) mod download
	@echo "Dependencies downloaded"

fmt: ## Format Go code
	@echo "Formatting code..."
	@$(GO) fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	@$(GO) vet ./...

lint: vet ## Run linters

all: fmt vet test build ## Format, vet, test, and build

frontend-install: ## Install frontend dependencies
	@echo "Installing frontend dependencies..."
	@cd frontend && npm install
	@echo "Frontend dependencies installed"

frontend-dev: ## Run frontend dev server
	@echo "Starting frontend dev server..."
	@if [ ! -d "frontend/node_modules" ]; then \
		echo "Dependencies not installed. Installing..." ; \
		cd frontend && npm install ; \
	fi
	@cd frontend && npm run dev

frontend-build: ## Build frontend for production
	@echo "Building frontend..."
	@if [ ! -d "frontend/node_modules" ]; then \
		echo "Dependencies not installed. Installing..." ; \
		cd frontend && npm install ; \
	fi
	@cd frontend && npm run build
	@echo "Frontend build complete: frontend/dist/"

frontend-clean: ## Clean frontend build artifacts
	@echo "Cleaning frontend artifacts..."
	@rm -rf frontend/dist frontend/node_modules
	@echo "Frontend clean complete"

frontend-validate: ## Type check frontend code
	@echo "Validating frontend code..."
	@if [ ! -d "frontend/node_modules" ]; then \
		echo "Dependencies not installed. Installing..." ; \
		cd frontend && npm install ; \
	fi
	@cd frontend && npm run validate
