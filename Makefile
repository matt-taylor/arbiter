.PHONY: help build build-consumer test run run-consumer clean frontend-install frontend-dev frontend-build frontend-clean frontend-validate install-deps fmt vet lint all redis-up redis-down redis-status redis-cli mariadb-up mariadb-down mariadb-status mariadb-cli telemetry-partitions test-telemetry test-telemetry-loop test-telemetry-e2e test-telemetry-e2e-persist test-telemetry-api

# Load .env file if it exists (- prefix suppresses error when missing)
-include .env
export

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
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' Makefile | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
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
	 ARB_TELEMETRY_API_ENABLED=true \
	 ARB_TELEMETRY_API_DB_DSN="$${MYSQL_USER:-arbiter}:$${MYSQL_PASSWORD:-arbiter_dev}@tcp(127.0.0.1:$${MYSQL_PORT:-3306})/$${MYSQL_DATABASE:-arbiter_telemetry}?parseTime=true&loc=UTC" \
	 ./$(BINARY)

clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -f $(BINARY) arbiter-telemetry-consumer
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

# --- Telemetry Consumer ---

CONSUMER_BINARY := arbiter-telemetry-consumer

build-consumer: ## Build the telemetry consumer binary
	@echo "Building $(CONSUMER_BINARY)..."
	@$(GO) build -o $(CONSUMER_BINARY) ./cmd/arbiter-telemetry-consumer
	@echo "Build complete: ./$(CONSUMER_BINARY)"

run-consumer: build-consumer ## Build and run the telemetry consumer
	@echo "Starting $(CONSUMER_BINARY)..."
	@ARB_TELEMETRY_CONSUMER_ENABLED=true \
	 ARB_TELEMETRY_REDIS_URL=redis://localhost:6379/0 \
	 ARB_TELEMETRY_DB_DSN="$${MYSQL_USER:-arbiter}:$${MYSQL_PASSWORD:-arbiter_dev}@tcp(127.0.0.1:$${MYSQL_PORT:-3306})/$${MYSQL_DATABASE:-arbiter_telemetry}?parseTime=true&loc=UTC" \
	 ./$(CONSUMER_BINARY)

# --- Redis (dev telemetry) ---

redis-up: ## Start dev Redis for telemetry
	@docker compose -f docker-compose.dev.yml up -d
	@echo ""
	@echo "  Redis is running."
	@echo "  Endpoint:  redis://localhost:6379/0"
	@echo "  CLI:       make redis-cli"
	@echo "  Stream:    redis-cli XRANGE arb:v1:events - +"
	@echo ""
	@echo "  To enable telemetry in Arbiter, export:"
	@echo "    export ARB_TELEMETRY_ENABLED=true"
	@echo "    export ARB_TELEMETRY_REDIS_URL=redis://localhost:6379/0"
	@echo ""

redis-down: ## Stop dev Redis
	@docker compose -f docker-compose.dev.yml down
	@echo "Redis stopped."

redis-status: ## Show dev Redis container status
	@docker compose -f docker-compose.dev.yml ps

redis-cli: ## Open redis-cli to the dev Redis
	@docker compose -f docker-compose.dev.yml exec redis redis-cli

# --- MariaDB (dev telemetry) ---

mariadb-up: ## Start dev MariaDB for telemetry
	@docker compose -f docker-compose.dev.yml up -d mariadb
	@echo ""
	@echo "  MariaDB is starting..."
	@echo "  Endpoint:  127.0.0.1:$${MYSQL_PORT:-3306}"
	@echo "  Database:  $${MYSQL_DATABASE:-arbiter_telemetry}"
	@echo "  CLI:       make mariadb-cli"
	@echo ""

mariadb-down: ## Stop dev MariaDB
	@docker compose -f docker-compose.dev.yml stop mariadb
	@echo "MariaDB stopped."

mariadb-status: ## Show dev MariaDB container status
	@docker compose -f docker-compose.dev.yml ps mariadb

mariadb-cli: ## Open mariadb CLI to the dev MariaDB
	@docker compose -f docker-compose.dev.yml exec mariadb mariadb -u root -p$${MYSQL_ROOT_PASSWORD} $${MYSQL_DATABASE}

telemetry-partitions: ## Run partition maintenance script against local MariaDB
	@MYSQL_HOST=127.0.0.1 \
	 MYSQL_PORT=$${MYSQL_PORT:-3306} \
	 MYSQL_USER=root \
	 MYSQL_ROOT_PASSWORD=$${MYSQL_ROOT_PASSWORD} \
	 MYSQL_DATABASE=$${MYSQL_DATABASE:-arbiter_telemetry} \
	 bash scripts/telemetry_partitions.sh

# --- Telemetry integration test ---

test-telemetry: ## Run telemetry integration test (single pass)
	@if [ ! -f scripts/.venv/bin/python3 ]; then \
		echo "Setting up Python venv..." ; \
		python3 -m venv scripts/.venv ; \
		scripts/.venv/bin/pip install -q -r scripts/requirements.txt ; \
	fi
	@scripts/.venv/bin/python3 scripts/test_telemetry.py

test-telemetry-loop: ## Run telemetry integration test (continuous)
	@if [ ! -f scripts/.venv/bin/python3 ]; then \
		echo "Setting up Python venv..." ; \
		python3 -m venv scripts/.venv ; \
		scripts/.venv/bin/pip install -q -r scripts/requirements.txt ; \
	fi
	@scripts/.venv/bin/python3 scripts/test_telemetry.py --loop

test-telemetry-e2e: build-consumer ## Run consumer end-to-end integration test (one command)
	@echo "=== Telemetry Consumer E2E Test ==="
	@echo ""
	@# 1. Ensure venv + deps
	@if [ ! -f scripts/.venv/bin/python3 ]; then \
		echo "[setup] Creating Python venv..." ; \
		python3 -m venv scripts/.venv ; \
	fi
	@scripts/.venv/bin/pip install -q -r scripts/requirements.txt
	@echo ""
	@# 2. Start containers
	@echo "[setup] Starting Redis + MariaDB containers..."
	@docker compose -f docker-compose.dev.yml up -d
	@echo "[setup] Waiting for MariaDB to be healthy..."
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do \
		if docker compose -f docker-compose.dev.yml exec -T mariadb mariadb-admin ping -h localhost -u root -p$${MYSQL_ROOT_PASSWORD} >/dev/null 2>&1; then \
			echo "[setup] MariaDB ping OK, verifying database is ready..." ; \
			if docker compose -f docker-compose.dev.yml exec -T mariadb mariadb -u "$${MYSQL_USER:-arbiter}" -p"$${MYSQL_PASSWORD:-arbiter_dev}" -e "SELECT 1" "$${MYSQL_DATABASE:-arbiter_telemetry}" >/dev/null 2>&1; then \
				echo "[setup] MariaDB is ready." ; \
				break ; \
			fi ; \
		fi ; \
		if [ $$i -eq 20 ]; then \
			echo "[setup] MariaDB did not become ready in time." ; \
			exit 1 ; \
		fi ; \
		sleep 2 ; \
	done
	@echo ""
	@# 3. Start consumer in background, capture PID
	@#    Consumer runs migrations on boot, creating tables if needed.
	@echo "[setup] Starting telemetry consumer in background..."
	@ARB_TELEMETRY_CONSUMER_ENABLED=true \
	 ARB_TELEMETRY_REDIS_URL=redis://localhost:6379/0 \
	 ARB_TELEMETRY_FLUSH_MS=500 \
	 ARB_TELEMETRY_DB_DSN="$${MYSQL_USER:-arbiter}:$${MYSQL_PASSWORD:-arbiter_dev}@tcp(127.0.0.1:$${MYSQL_PORT:-3306})/$${MYSQL_DATABASE:-arbiter_telemetry}?parseTime=true&loc=UTC" \
	 ./$(CONSUMER_BINARY) & echo $$! > .consumer.pid
	@sleep 3
	@if ! kill -0 $$(cat .consumer.pid) 2>/dev/null; then \
		echo "[setup] ERROR: Consumer exited unexpectedly. Check logs above." ; \
		rm -f .consumer.pid ; \
		exit 1 ; \
	fi
	@echo "[setup] Consumer started (PID: $$(cat .consumer.pid))"
	@echo ""
	@# 4. Run partition maintenance (tables now exist from migration)
	@echo "[setup] Running partition maintenance..."
	@MYSQL_HOST=127.0.0.1 \
	 MYSQL_PORT=$${MYSQL_PORT:-3306} \
	 MYSQL_USER=root \
	 MYSQL_ROOT_PASSWORD=$${MYSQL_ROOT_PASSWORD} \
	 MYSQL_DATABASE=$${MYSQL_DATABASE:-arbiter_telemetry} \
	 bash scripts/telemetry_partitions.sh
	@echo ""
	@# 5. Run the test; capture exit code
	@echo "[test] Running E2E verification..."
	@scripts/.venv/bin/python3 scripts/test_telemetry_consumer.py \
		--redis redis://localhost:6379/0 \
		--mariadb-host 127.0.0.1 \
		--mariadb-port $${MYSQL_PORT:-3306} \
		--mariadb-user $${MYSQL_USER:-arbiter} \
		--mariadb-password $${MYSQL_PASSWORD:-arbiter_dev} \
		--mariadb-db $${MYSQL_DATABASE:-arbiter_telemetry} \
		--flush-wait 3 ; \
	TEST_EXIT=$$? ; \
	echo "" ; \
	echo "[teardown] Stopping consumer (PID: $$(cat .consumer.pid))..." ; \
	kill $$(cat .consumer.pid) 2>/dev/null || true ; \
	rm -f .consumer.pid ; \
	exit $$TEST_EXIT

test-telemetry-e2e-persist: build-consumer ## Run consumer E2E test WITHOUT cleanup (data stays in MariaDB)
	@echo "=== Telemetry Consumer E2E Test (persist mode — no cleanup) ==="
	@echo ""
	@if [ ! -f scripts/.venv/bin/python3 ]; then \
		echo "[setup] Creating Python venv..." ; \
		python3 -m venv scripts/.venv ; \
	fi
	@scripts/.venv/bin/pip install -q -r scripts/requirements.txt
	@echo ""
	@echo "[setup] Starting Redis + MariaDB containers..."
	@docker compose -f docker-compose.dev.yml up -d
	@echo "[setup] Waiting for MariaDB to be healthy..."
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do \
		if docker compose -f docker-compose.dev.yml exec -T mariadb mariadb-admin ping -h localhost -u root -p$${MYSQL_ROOT_PASSWORD} >/dev/null 2>&1; then \
			echo "[setup] MariaDB ping OK, verifying database is ready..." ; \
			if docker compose -f docker-compose.dev.yml exec -T mariadb mariadb -u "$${MYSQL_USER:-arbiter}" -p"$${MYSQL_PASSWORD:-arbiter_dev}" -e "SELECT 1" "$${MYSQL_DATABASE:-arbiter_telemetry}" >/dev/null 2>&1; then \
				echo "[setup] MariaDB is ready." ; \
				break ; \
			fi ; \
		fi ; \
		if [ $$i -eq 20 ]; then \
			echo "[setup] MariaDB did not become ready in time." ; \
			exit 1 ; \
		fi ; \
		sleep 2 ; \
	done
	@echo ""
	@echo "[setup] Starting telemetry consumer in background..."
	@ARB_TELEMETRY_CONSUMER_ENABLED=true \
	 ARB_TELEMETRY_REDIS_URL=redis://localhost:6379/0 \
	 ARB_TELEMETRY_FLUSH_MS=500 \
	 ARB_TELEMETRY_DB_DSN="$${MYSQL_USER:-arbiter}:$${MYSQL_PASSWORD:-arbiter_dev}@tcp(127.0.0.1:$${MYSQL_PORT:-3306})/$${MYSQL_DATABASE:-arbiter_telemetry}?parseTime=true&loc=UTC" \
	 ./$(CONSUMER_BINARY) & echo $$! > .consumer.pid
	@sleep 3
	@if ! kill -0 $$(cat .consumer.pid) 2>/dev/null; then \
		echo "[setup] ERROR: Consumer exited unexpectedly. Check logs above." ; \
		rm -f .consumer.pid ; \
		exit 1 ; \
	fi
	@echo "[setup] Consumer started (PID: $$(cat .consumer.pid))"
	@echo ""
	@echo "[setup] Running partition maintenance..."
	@MYSQL_HOST=127.0.0.1 \
	 MYSQL_PORT=$${MYSQL_PORT:-3306} \
	 MYSQL_USER=root \
	 MYSQL_ROOT_PASSWORD=$${MYSQL_ROOT_PASSWORD} \
	 MYSQL_DATABASE=$${MYSQL_DATABASE:-arbiter_telemetry} \
	 bash scripts/telemetry_partitions.sh
	@echo ""
	@echo "[test] Running E2E verification (--no-cleanup)..."
	@scripts/.venv/bin/python3 scripts/test_telemetry_consumer.py \
		--redis redis://localhost:6379/0 \
		--mariadb-host 127.0.0.1 \
		--mariadb-port $${MYSQL_PORT:-3306} \
		--mariadb-user $${MYSQL_USER:-arbiter} \
		--mariadb-password $${MYSQL_PASSWORD:-arbiter_dev} \
		--mariadb-db $${MYSQL_DATABASE:-arbiter_telemetry} \
		--flush-wait 3 \
		--no-cleanup ; \
	TEST_EXIT=$$? ; \
	echo "" ; \
	echo "[teardown] Stopping consumer (PID: $$(cat .consumer.pid))..." ; \
	kill $$(cat .consumer.pid) 2>/dev/null || true ; \
	rm -f .consumer.pid ; \
	exit $$TEST_EXIT

test-telemetry-api: build ## Run telemetry API end-to-end test
	@echo "=== Telemetry API E2E Test ==="
	@echo ""
	@# 1. Ensure venv + deps
	@if [ ! -f scripts/.venv/bin/python3 ]; then \
		echo "[setup] Creating Python venv..." ; \
		python3 -m venv scripts/.venv ; \
	fi
	@scripts/.venv/bin/pip install -q -r scripts/requirements.txt
	@echo ""
	@# 2. Start containers
	@echo "[setup] Starting Redis + MariaDB containers..."
	@docker compose -f docker-compose.dev.yml up -d
	@echo "[setup] Waiting for MariaDB to be healthy..."
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do \
		if docker compose -f docker-compose.dev.yml exec -T mariadb mariadb-admin ping -h localhost -u root -p$${MYSQL_ROOT_PASSWORD} >/dev/null 2>&1; then \
			echo "[setup] MariaDB ping OK, verifying database is ready..." ; \
			if docker compose -f docker-compose.dev.yml exec -T mariadb mariadb -u "$${MYSQL_USER:-arbiter}" -p"$${MYSQL_PASSWORD:-arbiter_dev}" -e "SELECT 1" "$${MYSQL_DATABASE:-arbiter_telemetry}" >/dev/null 2>&1; then \
				echo "[setup] MariaDB is ready." ; \
				break ; \
			fi ; \
		fi ; \
		if [ $$i -eq 20 ]; then \
			echo "[setup] MariaDB did not become ready in time." ; \
			exit 1 ; \
		fi ; \
		sleep 2 ; \
	done
	@echo ""
	@# 3. Run partition maintenance (tables must exist from consumer migration)
	@echo "[setup] Running partition maintenance..."
	@MYSQL_HOST=127.0.0.1 \
	 MYSQL_PORT=$${MYSQL_PORT:-3306} \
	 MYSQL_USER=root \
	 MYSQL_ROOT_PASSWORD=$${MYSQL_ROOT_PASSWORD} \
	 MYSQL_DATABASE=$${MYSQL_DATABASE:-arbiter_telemetry} \
	 bash scripts/telemetry_partitions.sh
	@echo ""
	@# 4. Start Arbiter in background with telemetry API enabled
	@echo "[setup] Starting Arbiter with telemetry API enabled..."
	@ARBITER_BIND_ADDR=127.0.0.1:9199 \
	 DATABASE_URL=sqlite:///tmp/arbiter-test-api.db \
	 KILLSWITCH_BASE_URL=http://127.0.0.1:19090 \
	 GATEKEEPER_BASE_URL=http://127.0.0.1:13005 \
	 ARB_TELEMETRY_API_ENABLED=true \
	 ARB_TELEMETRY_API_DB_DSN="$${MYSQL_USER:-arbiter}:$${MYSQL_PASSWORD:-arbiter_dev}@tcp(127.0.0.1:$${MYSQL_PORT:-3306})/$${MYSQL_DATABASE:-arbiter_telemetry}?parseTime=true&loc=UTC" \
	 ./$(BINARY) & echo $$! > .arbiter-api.pid
	@sleep 2
	@if ! kill -0 $$(cat .arbiter-api.pid) 2>/dev/null; then \
		echo "[setup] ERROR: Arbiter exited unexpectedly. Check logs above." ; \
		rm -f .arbiter-api.pid ; \
		exit 1 ; \
	fi
	@echo "[setup] Arbiter started (PID: $$(cat .arbiter-api.pid))"
	@echo ""
	@# 5. Run the E2E test; capture exit code
	@echo "[test] Running telemetry API E2E verification..."
	@scripts/.venv/bin/python3 scripts/test_telemetry_api.py \
		--arbiter-url http://127.0.0.1:9199 \
		--mariadb-host 127.0.0.1 \
		--mariadb-port $${MYSQL_PORT:-3306} \
		--mariadb-user $${MYSQL_USER:-arbiter} \
		--mariadb-password $${MYSQL_PASSWORD:-arbiter_dev} \
		--mariadb-db $${MYSQL_DATABASE:-arbiter_telemetry} ; \
	TEST_EXIT=$$? ; \
	echo "" ; \
	echo "[teardown] Stopping Arbiter (PID: $$(cat .arbiter-api.pid))..." ; \
	kill $$(cat .arbiter-api.pid) 2>/dev/null || true ; \
	rm -f .arbiter-api.pid ; \
	rm -f /tmp/arbiter-test-api.db ; \
	exit $$TEST_EXIT

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
