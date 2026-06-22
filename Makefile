BINARY := factuarea
PKG := github.com/factuarea/factuarea-cli

.PHONY: build test lint fmt run
build:
	go build -o $(BINARY) ./cmd/factuarea
test:
	go test ./...
fmt:
	gofmt -s -w .
lint:
	go vet ./...
run:
	go run ./cmd/factuarea $(ARGS)
