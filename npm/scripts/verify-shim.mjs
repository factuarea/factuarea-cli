#!/usr/bin/env node
// Verificación LOCAL determinista del shim bin/factuarea.js.
//
// Monta un node_modules temporal con un paquete fake de la plataforma actual
// (@factuarea/cli-<platform-arch>) cuyo "binario" es un script ejecutable que
// imprime OK-SHIM y sale con un código conocido. Coloca una copia del shim en
// la raíz del temp para que su require.resolve encuentre ese node_modules.
// Comprueba que el shim (a) resuelve y ejecuta el binario de la plataforma
// actual y (b) propaga el exit code.

import { mkdtempSync, mkdirSync, writeFileSync, copyFileSync, rmSync, chmodSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { join, dirname, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const NPM_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const SHIM_SRC = join(NPM_DIR, "bin", "factuarea.js");

const platformKey = `${process.platform}-${process.arch}`;
const isWin = process.platform === "win32";

function fail(msg) {
  console.error(`verify-shim: FALLO — ${msg}`);
  process.exit(1);
}

const tmp = mkdtempSync(join(tmpdir(), "factuarea-shim-"));
try {
  // 1) Copia del shim en la raíz del temp (require.resolve parte de aquí).
  const shimCopy = join(tmp, "factuarea.js");
  copyFileSync(SHIM_SRC, shimCopy);

  // 2) Paquete fake de la plataforma actual dentro de node_modules.
  const pkgName = `@factuarea/cli-${platformKey}`;
  const pkgDir = join(tmp, "node_modules", pkgName);
  mkdirSync(pkgDir, { recursive: true });

  const binaryName = isWin ? "factuarea.exe" : "factuarea";
  const fakeBinaryPath = join(pkgDir, binaryName);

  // En POSIX el binario fake es un script con shebang sh; en Windows un .cmd.
  const EXIT_CODE = 42; // código no trivial para verificar propagación
  if (isWin) {
    writeFileSync(fakeBinaryPath, `@echo off\r\necho OK-SHIM\r\nexit /b ${EXIT_CODE}\r\n`);
  } else {
    writeFileSync(fakeBinaryPath, `#!/bin/sh\necho "OK-SHIM"\nexit ${EXIT_CODE}\n`);
    chmodSync(fakeBinaryPath, 0o755);
  }

  writeFileSync(
    join(pkgDir, "package.json"),
    JSON.stringify({ name: pkgName, version: "0.0.0", files: ["factuarea*"] }, null, 2) + "\n"
  );

  // 3) Ejecuta el shim copiado y captura salida + exit code.
  const res = spawnSync(process.execPath, [shimCopy, "ping", "--flag"], {
    encoding: "utf8",
    cwd: tmp,
  });

  if (res.error) fail(`el shim no se pudo ejecutar: ${res.error.message}`);

  const stdout = res.stdout ?? "";
  const stderr = res.stderr ?? "";

  // (a) resolvió y ejecutó el binario de la plataforma actual.
  if (!stdout.includes("OK-SHIM")) {
    fail(
      `el shim no ejecutó el binario fake de ${platformKey}.\n` +
        `stdout: ${JSON.stringify(stdout)}\nstderr: ${JSON.stringify(stderr)}`
    );
  }

  // (b) propagó el exit code del binario.
  if (res.status !== EXIT_CODE) {
    fail(`exit code no propagado: esperado ${EXIT_CODE}, recibido ${res.status}. stderr: ${stderr}`);
  }

  console.log(`verify-shim: OK — plataforma ${platformKey}`);
  console.log(`  (a) resolvió y ejecutó el binario empaquetado (OK-SHIM)`);
  console.log(`  (b) propagó el exit code (${res.status})`);
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
