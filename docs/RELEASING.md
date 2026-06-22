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
make generate        # baja el openapi vivo y regenera resources_gen.go
# o, contra develop local:
make generate-dev
```

## Fase 2 (pendiente)

- Notarización macOS (Apple Developer ID) y firma Authenticode (Windows).
- Canales adicionales: Scoop, WinGet, deb/rpm, Docker.
- Instalador en dominio propio `get.factuarea.com`.
- Relay WebSocket para `listen` (requiere cambio de backend).
