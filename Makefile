.PHONY: build
LINTER_VERSION ?= v2.7.2

all: lint test build

build-dir:
	@echo "Creating build directory..."
	@mkdir -p build/

lint:
	@echo "Running linters..."
	@golangci-lint run ./...

test: build-dir
	@echo "Running tests..."
	@go test -v ./... -coverprofile=build/coverage.out
	@go tool cover -html=build/coverage.out -o build/coverage.html

build: casd cas-test cas-balancer

casd:
	@echo "Building the casd binary..."
	@go build -o build/casd cmd/casd/*.go

cas-test:
	@echo "Building the test tool..."
	@go build -o build/cas-test cmd/cas-test/*.go

cas-balancer:
	@echo "Building the load balancer..."
	@go build -o build/cas-balancer cmd/cas-balancer/*.go

tools:
	@echo "Running tools..."
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(shell go env GOPATH)/bin $(LINTER_VERSION)
