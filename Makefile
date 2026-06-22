BINARY := factuarea
PKG := github.com/factuarea/factuarea-cli
SPEC_URL ?= https://api.factuarea.com/v1/openapi.json

VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w \
  -X github.com/factuarea/factuarea-cli/internal/buildinfo.Version=$(VERSION) \
  -X github.com/factuarea/factuarea-cli/internal/buildinfo.Commit=$(COMMIT)

.PHONY: build test lint fmt run generate generate-dev completions manpages dist-assets build-release
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
	go run internal/gen/main.go
generate-dev:
	docker exec factuarea-backend php artisan scramble:export --api=public-api --path=/tmp/openapi.json
	docker cp factuarea-backend:/tmp/openapi.json internal/spec/openapi.json
	go run internal/gen/main.go
completions manpages: ## generados por dist-assets
dist-assets:
	go run tools/gendocs/main.go
build-release:
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/factuarea
