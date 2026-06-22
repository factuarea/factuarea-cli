#!/usr/bin/env node
// Genera los paquetes npm por plataforma desde el dist/ de GoReleaser.
//
// Para cada combo del MVP emite npm/dist-packages/<pkg>/ con:
//   - package.json (name, version, os:[...], cpu:[...], files:["factuarea*"])
//   - el binario copiado desde dist/
// y reescribe la versión del paquete principal (npm/package.json) y sus
// optionalDependencies a la versión dada.
//
// Uso: node scripts/build-packages.mjs <version>   (o env FACTUAREA_CLI_VERSION)

import { readFileSync, writeFileSync, mkdirSync, copyFileSync, existsSync, rmSync, chmodSync } from "node:fs";
import { createHash } from "node:crypto";
import { join, dirname, resolve, basename } from "node:path";
import { fileURLToPath } from "node:url";

const NPM_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const REPO_ROOT = resolve(NPM_DIR, "..");
const DIST_DIR = join(REPO_ROOT, "dist");
const OUT_DIR = join(NPM_DIR, "dist-packages");

// goos/goarch (Go) -> process.platform/process.arch (Node)
const OS_MAP = { darwin: "darwin", linux: "linux", windows: "win32" };
const ARCH_MAP = { amd64: "x64", arm64: "arm64" };

// Las 5 combos del MVP (windows solo x64), expresadas en términos Node.
const MVP = [
  { platform: "darwin", arch: "arm64" },
  { platform: "darwin", arch: "x64" },
  { platform: "linux", arch: "arm64" },
  { platform: "linux", arch: "x64" },
  { platform: "win32", arch: "x64" },
];

function fail(msg) {
  console.error(`build-packages: ${msg}`);
  process.exit(1);
}

const version = process.argv[2] ?? process.env.FACTUAREA_CLI_VERSION;
if (!version) {
  fail("falta la versión. Uso: node scripts/build-packages.mjs <version>");
}

if (!existsSync(DIST_DIR)) {
  fail(`no existe ${DIST_DIR}. Corre 'goreleaser release --snapshot --clean' primero.`);
}

// Index de binarios desde dist/artifacts.json (fuente autoritativa de goos/goarch/path).
const artifactsPath = join(DIST_DIR, "artifacts.json");
if (!existsSync(artifactsPath)) {
  fail(`no existe ${artifactsPath}. ¿GoReleaser terminó correctamente?`);
}
const artifacts = JSON.parse(readFileSync(artifactsPath, "utf8"));
const binaries = artifacts.filter((a) => a.type === "Binary" && a.goos && a.goarch);
const archives = artifacts.filter((a) => a.type === "Archive" && a.goos && a.goarch);

// Mapa "platform-arch" (Node) -> ruta absoluta del binario en dist/.
const binByKey = new Map();
for (const b of binaries) {
  const platform = OS_MAP[b.goos];
  const arch = ARCH_MAP[b.goarch];
  if (!platform || !arch) continue; // combo no soportado por el mapeo
  binByKey.set(`${platform}-${arch}`, resolve(REPO_ROOT, b.path));
}

// Mapa "platform-arch" (Node) -> ruta absoluta del archive en dist/. checksums.txt
// cubre los ARCHIVES (no los binarios crudos), así que el gate de integridad opera
// sobre el archive de cada plataforma.
const archiveByKey = new Map();
for (const a of archives) {
  const platform = OS_MAP[a.goos];
  const arch = ARCH_MAP[a.goarch];
  if (!platform || !arch) continue;
  archiveByKey.set(`${platform}-${arch}`, resolve(REPO_ROOT, a.path));
}

// Parsea dist/checksums.txt (formato `<hash>  <archive_name>`) -> Map<archive_name, hash>.
const checksumsPath = join(DIST_DIR, "checksums.txt");
if (!existsSync(checksumsPath)) {
  fail(`no existe ${checksumsPath}. ¿GoReleaser terminó correctamente?`);
}
const checksumByArchive = new Map();
for (const line of readFileSync(checksumsPath, "utf8").split("\n")) {
  const trimmed = line.trim();
  if (!trimmed) continue;
  const [hash, ...rest] = trimmed.split(/\s+/);
  const name = rest.join(" ");
  if (hash && name) checksumByArchive.set(name, hash.toLowerCase());
}

// Verifica el sha256 del archive de `key` contra checksums.txt; aborta si falta
// la entrada o el hash no casa (dist/ corrupto o manipulado entre jobs).
function verifyArchiveChecksum(key) {
  const archivePath = archiveByKey.get(key);
  if (!archivePath || !existsSync(archivePath)) {
    fail(`falta el archive para ${key} en dist/ (esperado vía artifacts.json).`);
  }
  const archiveName = basename(archivePath);
  const expected = checksumByArchive.get(archiveName);
  if (!expected) {
    fail(`el archive '${archiveName}' no está en checksums.txt; integridad de dist/ no verificable. Instalación abortada.`);
  }
  const actual = createHash("sha256").update(readFileSync(archivePath)).digest("hex");
  if (actual !== expected) {
    fail(`checksum del archive '${archiveName}' no coincide. Esperado: ${expected}. Obtenido: ${actual}. dist/ corrupto o manipulado; abortado.`);
  }
}

// Regenera el dir de salida desde cero.
rmSync(OUT_DIR, { recursive: true, force: true });
mkdirSync(OUT_DIR, { recursive: true });

const optionalDependencies = {};
const produced = [];

for (const { platform, arch } of MVP) {
  const key = `${platform}-${arch}`;
  const pkgName = `@factuarea/cli-${key}`;
  optionalDependencies[pkgName] = version;

  // Gate de integridad: verifica el archive (lo que cubre checksums.txt) ANTES de
  // copiar el binario crudo, que goreleaser produjo del mismo build (byte-idéntico).
  verifyArchiveChecksum(key);

  const srcBinary = binByKey.get(key);
  if (!srcBinary || !existsSync(srcBinary)) {
    fail(`falta el binario para ${key} en dist/ (esperado vía artifacts.json).`);
  }

  const binaryName = platform === "win32" ? "factuarea.exe" : "factuarea";
  const pkgDir = join(OUT_DIR, key);
  mkdirSync(pkgDir, { recursive: true });

  const destBinary = join(pkgDir, binaryName);
  copyFileSync(srcBinary, destBinary);
  if (platform !== "win32") {
    // Asegura el bit de ejecución (copyFileSync preserva, pero lo fijamos por si acaso).
    try {
      chmodSync(destBinary, 0o755);
    } catch {
      /* no-op en plataformas que no lo soporten */
    }
  }

  const pkgManifest = {
    name: pkgName,
    version,
    description: `Binario de Factuarea CLI para ${platform}-${arch}`,
    license: "MIT",
    homepage: "https://github.com/factuarea/factuarea-cli",
    repository: { type: "git", url: "git+https://github.com/factuarea/factuarea-cli.git" },
    os: [platform],
    cpu: [arch],
    files: ["factuarea*"],
  };
  writeFileSync(join(pkgDir, "package.json"), JSON.stringify(pkgManifest, null, 2) + "\n");

  produced.push(`${pkgName} -> ${join("dist-packages", key)}/{package.json,${binaryName}} (archive checksum verificado)`);
}

// Reescribe la versión del paquete principal y sus optionalDependencies.
const mainPkgPath = join(NPM_DIR, "package.json");
const mainPkg = JSON.parse(readFileSync(mainPkgPath, "utf8"));
mainPkg.version = version;
mainPkg.optionalDependencies = optionalDependencies;
writeFileSync(mainPkgPath, JSON.stringify(mainPkg, null, 2) + "\n");

console.log(`build-packages: versión ${version}`);
for (const line of produced) console.log(`  ${line}`);
console.log(`  paquete principal @factuarea/cli reescrito a ${version}`);
