.PHONY: build test lint clean install integration-test

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PORT := "8080"

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/forge ./cmd/forge

test:
	go test -race -count=1 -v ./...

cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	go vet ./...

clean:
	rm -rf bin/ coverage.out coverage.html

install: build
	cp bin/forge /usr/local/bin/forge

integration-test:
	@echo "Running integration tests (server must be running on port $(PORT))..."
	FORGE_PORT=$(PORT) ./scripts/integration_test.sh $(MODELS)

kill-port:
	@echo "Killing process on port $(PORT)..."
	-kill -9 $$(lsof -ti:$(PORT)) 2>/dev/null || true
