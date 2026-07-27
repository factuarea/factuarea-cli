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
	$(MAKE) --no-print-directory normalize-spec
	go run internal/gen/main.go
# `public-api:export-spec`, NO `scramble:export`: el primero aplica
# WebhooksBlockTransformer y el segundo no, así que con scramble el spec embebido
# perdía el bloque `webhooks` que el endpoint vivo SÍ sirve.
generate-dev:
	docker exec factuarea-backend php artisan public-api:export-spec --api=public-api --path=/tmp/openapi.json
	docker cp factuarea-backend:/tmp/openapi.json internal/spec/openapi.json
	$(MAKE) --no-print-directory normalize-spec
	go run internal/gen/main.go
# El endpoint vivo sirve JSON compacto y el export de PHP lo sirve indentado: sin
# normalizar, alternar fuentes produce un diff de ~98k líneas que oculta el cambio real.
normalize-spec:
	@python3 -c "import json,sys;p='internal/spec/openapi.json';d=json.load(open(p));open(p,'w').write(json.dumps(d,indent=4)+chr(10))" \
		|| echo "AVISO: python3 no disponible; el spec queda sin normalizar y su diff será ilegible."
completions manpages: ## generados por dist-assets
dist-assets:
	go run tools/gendocs/main.go
build-release:
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/factuarea
