# Factuarea CLI — Plan 4: Distribución y release

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Que el CLI sea instalable por el mayor número de personas: binarios firmados multiplataforma vía **GoReleaser** (GitHub Releases + Homebrew tap + wrapper npm) con **cosign** y verificación de checksum, instalador `curl | sh`, completions y manpages, y CI de build/test + release.

**Architecture:** Todo es config + scripts, NO código de app. `.goreleaser.yaml` compila la matriz (darwin/linux/windows × amd64/arm64, `CGO_ENABLED=0` porque `go-keyring` es Go puro), genera archives/checksums/SBOM, firma con cosign keyless (OIDC en CI), publica formula en `factuarea/homebrew-tap` y los binarios en GitHub Releases. Un wrapper npm `@factuarea/cli` con `optionalDependencies` por plataforma + un shim `bin`. Workflows de GitHub Actions para CI (build/test/vet/gofmt + drift) y release (en tag). El binario estampa `buildinfo.Version`/`Commit` vía ldflags.

**Tech Stack:** GoReleaser v2, cosign (keyless OIDC), GitHub Actions, npm (Node 20+), Cobra (completions/manpages).

## Global Constraints

- Módulo `github.com/factuarea/factuarea-cli`. Binario `factuarea`. Repo PÚBLICO; **CERO comentarios explicativos en código Go** (los YAML/scripts SÍ pueden documentarse con comentarios — son config, no código auto-documentado de app).
- Repo destino: `github.com/factuarea/factuarea-cli`. Tap: `github.com/factuarea/homebrew-tap`. npm: scope `@factuarea` (ya nuestro: existe `@factuarea/sdk`).
- **Canales MVP:** GitHub Releases (binarios + `checksums.txt` + cosign + SBOM), Homebrew tap, wrapper npm. **Fase 2:** Scoop, WinGet, deb/rpm, Docker, instalador en `get.factuarea.com` (mientras, `install.sh` baja de GitHub Releases).
- **Firma de plataforma (macOS notarización / Windows Authenticode): Fase 2** (sin certs de pago ahora). En MVP los binarios van sin firmar; cosign+checksums cubren integridad de la cadena de descarga.
- **Verificación de checksum OBLIGATORIA** en `install.sh` y en el shim npm.
- `CGO_ENABLED=0` (cross-compile puro; `go-keyring` no usa CGO).
- **Realidad de verificación:** lo verificable en LOCAL (`goreleaser check`, `goreleaser release --snapshot --clean`, `shellcheck`, stamping de versión, shim npm) se verifica de verdad. Lo que requiere el remoto/credenciales (publicar en GitHub Releases, push al tap, `npm publish`, OIDC de cosign) NO se ejecuta aquí: se deja escrito y se ACTIVA al hacer push + montar infra. Cada task marca qué se verifica y qué se difiere.

**Out-of-scope (Fase 2 / infra del usuario):** crear los repos `factuarea-cli` + `homebrew-tap` en GitHub, el token `NPM_TOKEN`, el DNS de `get.factuarea.com`, certs de firma macOS/Windows, scoop/winget/deb/rpm/docker.

---

### Task 1: Versionado (ldflags) + generación de completions y manpages

**Files:**
- Create: `tools/gendocs/main.go` (`//go:build ignore` — genera completions + manpages)
- Modify: `Makefile` (targets `completions`, `manpages`, `dist-assets`)
- Modify: `go.mod`/`go.sum` (añade `github.com/spf13/cobra/doc` — ya viene con cobra)

**Interfaces:** Produces `completions/` (bash/zsh/fish/powershell) y `manpages/` (`factuarea*.1`) a partir del root command, para que GoReleaser los empaquete.

- [ ] **Step 1: Tool de generación**

Create `tools/gendocs/main.go`:
```go
//go:build ignore

package main

import (
	"log"
	"os"

	"github.com/factuarea/factuarea-cli/internal/cmd"
	"github.com/spf13/cobra/doc"
)

func main() {
	root := cmd.NewRootCmd()
	root.DisableAutoGenTag = true

	if err := os.MkdirAll("completions", 0o755); err != nil {
		log.Fatal(err)
	}
	for _, sh := range []string{"bash", "zsh", "fish", "powershell"} {
		f, err := os.Create("completions/factuarea." + sh)
		if err != nil {
			log.Fatal(err)
		}
		switch sh {
		case "bash":
			err = root.GenBashCompletionV2(f, true)
		case "zsh":
			err = root.GenZshCompletion(f)
		case "fish":
			err = root.GenFishCompletion(f, true)
		case "powershell":
			err = root.GenPowerShellCompletionWithDesc(f)
		}
		f.Close()
		if err != nil {
			log.Fatal(err)
		}
	}

	if err := os.MkdirAll("manpages", 0o755); err != nil {
		log.Fatal(err)
	}
	hdr := &doc.GenManHeader{Title: "FACTUAREA", Section: "1"}
	if err := doc.GenManTree(root, hdr, "manpages"); err != nil {
		log.Fatal(err)
	}
}
```
> `cmd.NewRootCmd()` es exportado (Plan 1). `cobra/doc` ya está disponible vía la dependencia cobra; `go mod tidy` lo fija si hace falta.

- [ ] **Step 2: Makefile**

Add:
```makefile
VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w \
  -X github.com/factuarea/factuarea-cli/internal/buildinfo.Version=$(VERSION) \
  -X github.com/factuarea/factuarea-cli/internal/buildinfo.Commit=$(COMMIT)

completions manpages: ## generados por dist-assets
dist-assets:
	go run tools/gendocs/main.go

build-release:
	go build -ldflags '$(LDFLAGS)' -o factuarea ./cmd/factuarea
```
Añade `/completions/` y `/manpages/` a `.gitignore` (son artefactos generados).

- [ ] **Step 3: Verificar**

Run:
```bash
cd /Users/chelu/Personal/factuarea-cli
go run tools/gendocs/main.go && ls completions manpages
make build-release VERSION=v0.1.0-test && ./factuarea version
```
Expected: `completions/factuarea.{bash,zsh,fish,powershell}` y `manpages/factuarea.1` (+ subcomandos) creados; `factuarea version` imprime `factuarea v0.1.0-test (commit <hash>, spec <12hex>)`.

- [ ] **Step 4: Commit.**
```bash
git add tools/gendocs Makefile .gitignore
git commit -m "build: generate shell completions and manpages; version ldflags"
```

---

### Task 2: `.goreleaser.yaml` (builds, archives, checksums, cosign, brew)

**Files:**
- Create: `.goreleaser.yaml`

- [ ] **Step 1: Config**

Create `.goreleaser.yaml` (GoReleaser v2; el implementador ajusta el schema EXACTO a la versión instalada hasta que `goreleaser check` pase):
```yaml
version: 2
project_name: factuarea

before:
  hooks:
    - go mod tidy
    - go run tools/gendocs/main.go

builds:
  - id: factuarea
    main: ./cmd/factuarea
    binary: factuarea
    env:
      - CGO_ENABLED=0
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w
      - -X github.com/factuarea/factuarea-cli/internal/buildinfo.Version={{.Version}}
      - -X github.com/factuarea/factuarea-cli/internal/buildinfo.Commit={{.ShortCommit}}

archives:
  - id: default
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        formats: [zip]
    files:
      - LICENSE
      - README.md
      - completions/*
      - manpages/*

checksum:
  name_template: "checksums.txt"

sboms:
  - artifacts: archive

signs:
  - cmd: cosign
    certificate: "${artifact}.pem"
    args:
      - sign-blob
      - "--yes"
      - "--output-signature=${signature}"
      - "--output-certificate=${certificate}"
      - "${artifact}"
    artifacts: checksum
    output: true

brews:
  - repository:
      owner: factuarea
      name: homebrew-tap
    homepage: "https://factuarea.com"
    description: "CLI oficial de Factuarea para la API pública v1"
    license: "MIT"
    install: |
      bin.install "factuarea"
      bash_completion.install "completions/factuarea.bash" => "factuarea"
      zsh_completion.install "completions/factuarea.zsh" => "_factuarea"
      fish_completion.install "completions/factuarea.fish"
      man1.install Dir["manpages/*.1"]
    test: |
      system "#{bin}/factuarea version"

release:
  github:
    owner: factuarea
    name: factuarea-cli
  draft: false
  prerelease: auto
```

- [ ] **Step 2: Verificar en LOCAL (sin publicar)**

Run:
```bash
cd /Users/chelu/Personal/factuarea-cli
goreleaser check                                  # valida el schema
goreleaser release --snapshot --clean --skip=sign,sbom   # compila TODAS las plataformas + archives + checksums + brew, sin firmar/SBOM (cosign/syft no están en local)
ls dist/                                           # binarios por plataforma + checksums.txt + *.tar.gz/.zip + formula brew
./dist/factuarea_*_darwin_arm64*/factuarea version 2>/dev/null || tar -xzf dist/factuarea_*_darwin_arm64.tar.gz -C /tmp && /tmp/factuarea version
```
Expected: `goreleaser check` OK; el snapshot produce binarios para linux/darwin/windows × amd64/arm64, `checksums.txt`, archives, y la formula de Homebrew en `dist/homebrew/`. El binario darwin/arm64 corre y estampa la versión del snapshot. (Firma y SBOM se omiten en local con `--skip`; en CI corren keyless.)

- [ ] **Step 3: Commit.**
```bash
git add .goreleaser.yaml && git commit -m "build: GoReleaser config (multi-platform, checksums, cosign, brew tap)"
```

---

### Task 3: Wrapper npm `@factuarea/cli` (optionalDependencies + shim)

**Files:**
- Create: `npm/package.json` (paquete principal `@factuarea/cli`)
- Create: `npm/bin/factuarea.js` (shim que resuelve el binario por plataforma)
- Create: `npm/scripts/build-packages.mjs` (genera los paquetes por plataforma desde `dist/` de GoReleaser)
- Test: `npm/scripts/verify-shim.mjs` (verifica la resolución del shim en local)

**Diseño (modelo esbuild/turbo/biome, NO postinstall-download):** el paquete principal declara `optionalDependencies` con un paquete por plataforma (`@factuarea/cli-darwin-arm64`, etc.); npm instala SOLO el que casa por `os`/`cpu`. El `bin` es un shim Node que resuelve el binario empaquetado y lo ejecuta. Sin `postinstall`, sobrevive a `--ignore-scripts`/pnpm/`npx`.

- [ ] **Step 1: Paquete principal**

Create `npm/package.json`:
```json
{
  "name": "@factuarea/cli",
  "version": "0.0.0",
  "description": "CLI oficial de Factuarea para la API pública v1",
  "license": "MIT",
  "bin": { "factuarea": "bin/factuarea.js" },
  "files": ["bin/factuarea.js"],
  "optionalDependencies": {
    "@factuarea/cli-darwin-arm64": "0.0.0",
    "@factuarea/cli-darwin-x64": "0.0.0",
    "@factuarea/cli-linux-arm64": "0.0.0",
    "@factuarea/cli-linux-x64": "0.0.0",
    "@factuarea/cli-win32-x64": "0.0.0"
  },
  "engines": { "node": ">=20" }
}
```
(La versión `0.0.0` la reescribe CI al publicar, igualándola al tag.)

Create `npm/bin/factuarea.js`:
```js
#!/usr/bin/env node
"use strict";
const { spawnSync } = require("node:child_process");

const platformKey = `${process.platform}-${process.arch}`;
const pkg = `@factuarea/cli-${platformKey}`;

let binary;
try {
  binary = require.resolve(`${pkg}/factuarea${process.platform === "win32" ? ".exe" : ""}`);
} catch {
  console.error(
    `factuarea: no se encontró el binario para tu plataforma (${platformKey}).\n` +
      `Instala el paquete específico o usa otro método: https://github.com/factuarea/factuarea-cli`
  );
  process.exit(1);
}

const res = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });
if (res.error) {
  console.error(res.error.message);
  process.exit(1);
}
process.exit(res.status === null ? 1 : res.status);
```

- [ ] **Step 2: Generador de paquetes por plataforma**

Create `npm/scripts/build-packages.mjs` — toma `dist/` de GoReleaser + una versión, y emite en `npm/dist-packages/<pkg>/` un `package.json` (con `os`/`cpu` y `files:["factuarea*"]`) + el binario, listos para `npm publish`. Mapea `goos/goarch` → `process.platform/process.arch` (`darwin→darwin`, `linux→linux`, `windows→win32`; `amd64→x64`, `arm64→arm64`). El implementador lo escribe siguiendo ese mapeo.

- [ ] **Step 3: Verificación local del shim**

Create `npm/scripts/verify-shim.mjs` que: crea un `node_modules/@factuarea/cli-<thisPlatform>/factuarea` falso (un script ejecutable que imprime "OK"), y comprueba que `require.resolve` del shim lo encuentra para la plataforma actual. Run:
```bash
cd /Users/chelu/Personal/factuarea-cli/npm && node scripts/verify-shim.mjs
```
Expected: el shim resuelve el binario de la plataforma actual; mensaje de éxito.

- [ ] **Step 4: Commit.**
```bash
git add npm && git commit -m "build(npm): @factuarea/cli wrapper with per-platform optionalDependencies"
```

---

### Task 4: Instalador `install.sh` (curl | sh)

**Files:**
- Create: `install.sh`

- [ ] **Step 1: Script endurecido**

Create `install.sh` — contrato: toda la lógica en funciones + `main "$@"` en la ÚLTIMA línea (evita ejecución parcial por corte de stream), `set -euo pipefail`, detección OS/arch, descarga del archive correcto desde GitHub Releases (`https://github.com/factuarea/factuarea-cli/releases/download/<tag>/...`), **verificación de checksum** contra `checksums.txt`, instalación en `${INSTALL_DIR:-$HOME/.local/bin}` sin sudo, aviso si ese dir no está en `PATH`. Soporta `FACTUAREA_VERSION` (default: latest vía la API de releases). El implementador lo escribe completo con esa estructura.

- [ ] **Step 2: Verificar**

Run:
```bash
cd /Users/chelu/Personal/factuarea-cli
shellcheck install.sh                 # 0 findings
sh -n install.sh                      # sintaxis OK
FACTUAREA_DRY_RUN=1 sh install.sh     # (si implementas un dry-run) detecta os/arch e imprime la URL sin descargar
```
Expected: `shellcheck` limpio; el dry-run detecta correctamente la plataforma actual e imprime la URL del archive (sin descargar, porque aún no hay releases publicados).

- [ ] **Step 3: Commit.**
```bash
git add install.sh && git commit -m "build: hardened curl|sh installer with checksum verification"
```

---

### Task 5: GitHub Actions — CI + release

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: CI workflow**

Create `.github/workflows/ci.yml` — en push/PR: `go build ./...`, `go vet ./...`, `gofmt -l .` (falla si no vacío), `go test ./... -race`, y un job/step de **drift** con `FACTUAREA_CHECK_DRIFT=1 go test ./internal/spec/ -run TestSpecNotDrifted` (o el nombre real del test de drift). Matriz: ubuntu + macos. Go 1.26.

- [ ] **Step 2: Release workflow**

Create `.github/workflows/release.yml` — `on: push: tags: ['v*']`. Permisos `contents: write`, `id-token: write` (OIDC para cosign keyless), `packages: write`. Pasos: checkout (fetch-depth 0), setup-go, instalar cosign (`sigstore/cosign-installer`) y syft, `goreleaser/goreleaser-action` con `GITHUB_TOKEN` + un `HOMEBREW_TAP_TOKEN` (PAT con acceso al tap). Job posterior de **npm publish**: reescribe versiones a `${GITHUB_REF_NAME#v}`, corre `npm/scripts/build-packages.mjs`, y publica los paquetes por plataforma + el principal con `NPM_TOKEN` y `--access public`.

- [ ] **Step 3: Validar sintaxis**

Run (si `actionlint` está disponible; si no, validación manual de YAML):
```bash
cd /Users/chelu/Personal/factuarea-cli
command -v actionlint >/dev/null 2>&1 && actionlint || python3 -c "import yaml,sys; [yaml.safe_load(open(f)) for f in ['.github/workflows/ci.yml','.github/workflows/release.yml']]; print('YAML OK')"
```
Expected: YAML válido. (Los workflows NO se ejecutan hasta que el repo esté en GitHub; se documentan los secrets requeridos: `HOMEBREW_TAP_TOKEN`, `NPM_TOKEN`.)

- [ ] **Step 4: Commit.**
```bash
git add .github && git commit -m "ci: build/test/drift CI + tag-triggered release (goreleaser, cosign, npm)"
```

---

### Task 6: Verificación integral + README de instalación

- [ ] **Step 1: Snapshot completo local**

Run:
```bash
cd /Users/chelu/Personal/factuarea-cli
goreleaser release --snapshot --clean --skip=sign,sbom
# verifica: binarios de las 5 plataformas + checksums.txt + archives + formula brew + completions/manpages dentro de los archives
tar -tzf dist/factuarea_*_linux_amd64.tar.gz | grep -E 'completions|manpages|factuarea$'
```
Expected: cada archive incluye el binario + completions + manpages; `checksums.txt` presente; formula brew generada.

- [ ] **Step 2: README — sección de instalación** (actualizar la sección "Instalación" del README): brew (`brew install factuarea/tap/factuarea`), npm (`npm i -g @factuarea/cli`), `curl -fsSL https://github.com/factuarea/factuarea-cli/releases/latest/download/install.sh | sh` (o `get.factuarea.com` cuando exista), y binarios de Releases. Marca que la firma de plataforma (notarización) llega en Fase 2.

- [ ] **Step 3: Documentar el "go-live"** (un `docs/RELEASING.md` corto): qué secrets configurar (`HOMEBREW_TAP_TOKEN`, `NPM_TOKEN`), crear el repo `homebrew-tap`, y que el release se dispara con `git tag vX.Y.Z && git push --tags`. 

- [ ] **Step 4: Gate final + commit.**
```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... -count=1
git add README.md docs/RELEASING.md && git commit -m "docs: install instructions and releasing guide"
```

---

## Self-Review (rellenado tras escribir el plan)

**Cobertura:** versionado+completions+manpages T1 · goreleaser (multiplataforma, checksums, cosign, SBOM, brew) T2 · wrapper npm optionalDependencies+shim T3 · install.sh endurecido+checksum T4 · CI+release workflows (cosign OIDC, drift, npm publish) T5 · verificación integral+docs T6. **Fuera (infra/Fase 2, documentado):** crear repos GitHub/tap, NPM_TOKEN, get.factuarea.com, firma macOS/Win, scoop/winget/deb/rpm/docker.

**Verificable en LOCAL (de verdad):** `goreleaser check`, `goreleaser release --snapshot` (cross-compile de las 5 plataformas + archives + checksums + brew formula), stamping de versión por ldflags, generación de completions/manpages, `shellcheck install.sh`, shim npm. **Diferido a push/infra:** publicar releases, push al tap, `npm publish`, firma cosign keyless (OIDC), ejecución de los workflows.

**Placeholders:** `build-packages.mjs`, `verify-shim.mjs`, `install.sh` y los dos workflows se describen con su contrato exacto; el implementador escribe el cuerpo. El resto (goreleaser yaml, package.json, shim, gendocs) es contenido completo. El implementador ajusta el schema de goreleaser v2 a la versión instalada hasta que `goreleaser check` pase.

**Riesgo principal:** el schema EXACTO de GoReleaser v2.16 (claves `signs`/`sboms`/`brews`/`archives.formats`) puede diferir del starting config; `goreleaser check` + `--snapshot` son la red que obliga a dejarlo correcto. CGO_ENABLED=0 asume `go-keyring` Go-puro (cierto) → cross-compile sin toolchain C.
