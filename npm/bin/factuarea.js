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
