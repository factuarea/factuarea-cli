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
import { join, dirname, resolve } from "node:path";
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

// Mapa "platform-arch" (Node) -> ruta absoluta del binario en dist/.
const binByKey = new Map();
for (const b of binaries) {
  const platform = OS_MAP[b.goos];
  const arch = ARCH_MAP[b.goarch];
  if (!platform || !arch) continue; // combo no soportado por el mapeo
  binByKey.set(`${platform}-${arch}`, resolve(REPO_ROOT, b.path));
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
    os: [platform],
    cpu: [arch],
    files: ["factuarea*"],
  };
  writeFileSync(join(pkgDir, "package.json"), JSON.stringify(pkgManifest, null, 2) + "\n");

  produced.push(`${pkgName} -> ${join("dist-packages", key)}/{package.json,${binaryName}}`);
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
