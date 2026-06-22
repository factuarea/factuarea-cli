BINARY := factuarea
PKG := github.com/factuarea/factuarea-cli
SPEC_URL ?= https://api.factuarea.com/v1/openapi.json

.PHONY: build test lint fmt run generate generate-dev
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
generate:
	curl -fsSL $(SPEC_URL) -o internal/spec/openapi.json
	go generate ./...
generate-dev:
	docker exec factuarea-backend php artisan scramble:export --api=public-api --path=/tmp/openapi.json
	docker cp factuarea-backend:/tmp/openapi.json internal/spec/openapi.json
	go generate ./...
