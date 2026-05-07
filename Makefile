.DEFAULT_GOAL := help
.PHONY: build test lint clean install integration-test run help

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
CMD ?= chat
PROVIDER ?= ollama
PORT := "8080"
SKIP ?= deepseek

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Variables:"
	@echo "  CMD        Subcommand: chat (default), serve, ask"
	@echo "  PROVIDER   Provider: ollama (default), bedrock, openai, anthropic, deepseek"
	@echo "  MODEL      Model name (provider-specific)"
	@echo "  ARGS       Extra flags passed to forge (e.g. --yes, --log file.txt, --port 9090)"

build: ## Build binary to bin/forge
	go build -ldflags "-X main.version=$(VERSION)" -o bin/forge ./cmd/forge

run: ## Build and run (CMD=chat|serve|ask PROVIDER=ollama|bedrock MODEL= ARGS=)
	go run -ldflags "-X main.version=$(VERSION)" ./cmd/forge $(CMD) --provider $(PROVIDER) $(if $(MODEL),--model $(MODEL)) $(ARGS)

test: ## Run tests with race detector
	go test -race -count=1 -v ./...

cover: ## Run tests with coverage report
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint: ## Run go vet
	go vet ./...

clean: ## Remove build artifacts
	rm -rf bin/ coverage.out coverage.html

install: build ## Build and install to /usr/local/bin
	cp bin/forge /usr/local/bin/forge

integration-test: ## Run integration tests (PORT= SKIP= MODELS=)
	@echo "Running integration tests (server must be running on port $(PORT))..."
	$(eval OUTPUT ?= out/integration-tests-$(shell date +%Y%m%d-%H%M%S).txt)
	FORGE_PORT=$(PORT) ./scripts/integration_test.sh -o $(OUTPUT) $(if $(SKIP),--skip $(SKIP)) $(MODELS)
	@echo "Results saved to $(OUTPUT)"

kill-port: ## Kill process on PORT
	@echo "Killing process on port $(PORT)..."
	-kill -9 $$(lsof -ti:$(PORT)) 2>/dev/null || true
