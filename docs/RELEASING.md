# Releasing

La distribución está automatizada con GoReleaser + GitHub Actions. Una release se dispara empujando un tag `vX.Y.Z`.

## Requisitos previos (una sola vez)

1. **Repos en GitHub:**
   - `github.com/factuarea/factuarea-cli` (este repo).
   - `github.com/factuarea/homebrew-tap` (vacío; GoReleaser empuja ahí el cask de Homebrew).
2. **Secrets** del repo `factuarea-cli` (Settings → Secrets → Actions):
   - `HOMEBREW_TAP_TOKEN` — PAT (classic o fine-grained) con `contents: write` sobre `factuarea/homebrew-tap`.
   - `NPM_TOKEN` — automation token de npm con permiso de publish en el scope `@factuarea`. Debe ser un **automation token** (no de tipo "publish" interactivo con 2FA) para que `npm publish --provenance` (firma Sigstore vía OIDC `id-token: write`) funcione en CI.
   - `GITHUB_TOKEN` es automático (no hay que crearlo).
3. **npm scope** `@factuarea` debe existir y permitir publicar `@factuarea/cli` y `@factuarea/cli-<os>-<arch>`.

## Publicar una release

```bash
git tag v0.1.0
git push origin v0.1.0
```

El workflow `release.yml` (on tag `v*`):
1. **goreleaser**: compila las 6 plataformas, genera `checksums.txt`, **firma con cosign keyless** (OIDC, `id-token: write`), SBOM, sube los binarios + `install.sh` a GitHub Releases, y empuja el **cask** a `homebrew-tap`.
2. **npm-publish**: reescribe las versiones a la del tag, genera los paquetes por plataforma desde `dist/` (`npm/scripts/build-packages.mjs`, que **verifica el checksum de cada archive** contra `dist/checksums.txt` antes de copiar el binario) y publica `@factuarea/cli` + los `@factuarea/cli-<os>-<arch>` con `--provenance --access public` (firma Sigstore).

CI (`ci.yml`) corre en cada push/PR: build/vet/gofmt/test `-race` (ubuntu+macos) y un job de **drift** (`FACTUAREA_CHECK_DRIFT=1`) que compara el spec embebido con el vivo de `api.factuarea.com`.

## Verificar tras publicar

```bash
brew install --cask factuarea/tap/factuarea && factuarea version
npm i -g @factuarea/cli && factuarea version
curl -fsSL https://github.com/factuarea/factuarea-cli/releases/latest/download/install.sh | sh
```

## Ensayo en local (sin publicar)

```bash
goreleaser check
goreleaser release --snapshot --clean --skip=sign,sbom   # cosign/syft solo en CI
```

## Actualizar el spec antes de una release

```bash
make generate        # baja el openapi vivo de prod y regenera resources_gen.go
# o, contra el backend local, para incluir superficie aún no desplegada:
make generate-dev
```

> **Elige la fuente a conciencia.** `GET https://api.factuarea.com/v1/openapi.json` (el `SPEC_URL` por defecto) sirve la spec completa que el backend genera en runtime, así que `make generate` es fiable. Lo que da es la superficie **de producción**, y producción va por detrás de `develop`: si el CLI ya embebe recursos que aún no se han desplegado, regenerar contra prod los BORRA. Para una release que deba adelantarse al deploy, usa `make generate-dev` (`php artisan public-api:export-spec` del backend local, **no** `scramble:export`: el primero aplica el transformador de webhooks y el segundo deja el spec embebido sin el bloque `webhooks` que el endpoint vivo sí sirve).
>
> El `spec drift-guard` de CI compara lo embebido con el spec vivo, pero es `continue-on-error: true` y trata "develop adelantado a prod" como resultado normal: su verde **no** demuestra paridad con producción. Si necesitas saber qué falta en prod, lee su log.

### Prerrequisito vigente: la superficie del ERP omnicanal (2026-09-18)

El spec embebido de `internal/spec/openapi.json` está fijado al **contrato
congelado** de la ola de sincronización del ERP omnicanal: **654 operaciones en
532 paths**, exportado una sola vez desde el backend con la superficie del ERP
ya fusionada (sha256 del artefacto de origen
`687f70a156a90cd41c453e87cb6627b3cb3ff85c21db0ff81b392c7b1945f49c`). La foto
anterior declaraba 470 operaciones en 386 paths: el delta es **+184**.

**Mientras esa superficie no esté desplegada en producción, `make generate` es
destructivo**: `https://api.factuarea.com/v1/openapi.json` sigue sirviendo el
contrato anterior, así que regenerar contra él retiraría del binario las 184
operaciones nuevas —los 166 comandos del ERP y los 18 de contactos— sin que
nada se ponga rojo salvo el contraste del árbol. El orden correcto es:

1. desplegar el ERP a producción;
2. comprobar que el contrato publicado ya declara 654 operaciones
   (`curl -fsSL https://api.factuarea.com/v1/openapi.json | jq '[.paths[] | keys[]] | length'`);
3. sólo entonces `make generate`, y volver a dejar en verde el contraste del
   árbol con
   `go test ./internal/cmd/ -run TestCommandTreeMatchesGolden -update-command-tree`;
4. etiquetar la release.

Si el paso 2 mide menos de 654, **para**: la release se corta con
`make generate-dev` (o con el artefacto congelado) y el paso 3 se pospone. El
`spec drift-guard` de CI **no** protege de esto: es `continue-on-error` y su
rojo es el resultado esperado mientras dure la ventana.

## Fase 2 (pendiente)

- Notarización macOS (Apple Developer ID) y firma Authenticode (Windows).
- Canales adicionales: Scoop, WinGet, deb/rpm, Docker.
- Instalador en dominio propio `get.factuarea.com`.
- Relay WebSocket para `listen` (requiere cambio de backend).
