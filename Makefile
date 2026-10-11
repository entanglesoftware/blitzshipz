.PHONY: all fmt vet test test-race cover lint tidy check

all: check

fmt:
	gofmt -l -w .

vet:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint not installed; skipping"

tidy:
	go mod tidy

# check runs the full local verification suite.
check: fmt vet test-race
