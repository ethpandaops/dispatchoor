.PHONY: all build build-api build-ui clean test-api lint-api lint-ui dev dev-api dev-ui docker-build docker-build-api docker-build-web docker-up docker-down

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildDate=$(BUILD_DATE)

# Directories
BIN_DIR := bin
UI_DIR := ui

all: build

build: build-api build-ui

build-api:
	@echo "Building API..."
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/dispatchoor ./cmd/dispatchoor

build-ui:
	@echo "Building UI..."
	npm install --prefix $(UI_DIR)
	npm run --prefix $(UI_DIR) build

clean:
	@echo "Cleaning..."
	rm -rf $(BIN_DIR)
	rm -rf $(UI_DIR)/dist
	rm -rf $(UI_DIR)/node_modules

test-api:
	@echo "Running API tests..."
	go test -race -v ./...

lint-api:
	@echo "Linting API..."
	golangci-lint run --new-from-rev="origin/master"

lint-ui:
	@echo "Linting UI..."
	npm run --prefix $(UI_DIR) lint

dev-api:
	@echo "Starting API in development mode..."
	go run ./cmd/dispatchoor server --config config.yaml

dev-ui:
	@echo "Starting UI in development mode..."
	npm run --prefix $(UI_DIR) dev

# Run both API and UI in parallel (requires make -j2)
dev: dev-api dev-ui

# Database migrations
migrate:
	go run ./cmd/dispatchoor migrate --config config.yaml

# Docker
docker-build: docker-build-api docker-build-web

docker-build-api:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f Dockerfile.api \
		-t dispatchoor-api:$(VERSION) \
		-t dispatchoor-api:latest \
		.

docker-build-web:
	docker build \
		-f Dockerfile.web \
		-t dispatchoor-web:$(VERSION) \
		-t dispatchoor-web:latest \
		.

docker-up:
	docker compose up -d

docker-down:
	docker compose down

# Help
help:
	@echo "Available targets:"
	@echo "  all              - Build everything (default)"
	@echo "  build            - Build API and UI"
	@echo "  build-api        - Build Go API"
	@echo "  build-ui         - Build React UI"
	@echo "  clean            - Remove build artifacts"
	@echo "  test-api         - Run API tests"
	@echo "  lint-api         - Run Go linter"
	@echo "  lint-ui          - Run UI linter (eslint)"
	@echo "  dev-api          - Start API in dev mode"
	@echo "  dev-ui           - Start UI in dev mode"
	@echo "  migrate          - Run database migrations"
	@echo "  docker-build     - Build all Docker images"
	@echo "  docker-build-api - Build API Docker image"
	@echo "  docker-build-web - Build Web Docker image"
	@echo "  docker-up        - Start services with docker compose"
	@echo "  docker-down      - Stop services with docker compose"
