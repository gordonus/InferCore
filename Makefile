# InferCore — https://infercore.dev
.PHONY: help all fmt vet test build run smoke docker-build clean info

BIN_DIR := bin
BINARY := $(BIN_DIR)/infercore
CONFIG ?= configs/infercore.example.yaml

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

info: ## Print project website URL
	@echo "Website: https://infercore.dev"

all: fmt vet test ## Format, vet, and test

fmt: ## go fmt
	go fmt ./...

vet: ## go vet
	go vet ./...

test: ## Run all tests
	go test ./...

build: ## Build binary to $(BINARY)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -o $(BINARY) ./cmd/infercore

run: ## Run server (INFERCORE_CONFIG=$(CONFIG))
	INFERCORE_CONFIG=$(CONFIG) go run ./cmd/infercore

smoke: ## HTTP smoke against BASE_URL (default http://localhost:8080); start server separately
	bash ./scripts/smoke.sh "$(BASE_URL)"

load-infer: ## Load test POST /infer (needs hey; use configs/infercore.loadtest.yaml for server)
	bash ./scripts/load-infer.sh

docker-build: ## Build OCI image infercore:local
	docker build -t infercore:local .

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)
