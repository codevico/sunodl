#!/usr/bin/env node
"use strict";

const { spawnSync } = require("node:child_process");

const platformPackage = `sunodl-${process.platform}-${process.arch}`;
const executable = process.platform === "win32" ? "sunodl.exe" : "sunodl";

function resolveBinary() {
  try {
    return require.resolve(`${platformPackage}/bin/${executable}`);
  } catch {
    console.error(
      `sunodl: no prebuilt binary for ${process.platform}-${process.arch}.\n` +
        `Expected the optional dependency "${platformPackage}" to be installed; ` +
        "reinstall without --no-optional / --omit=optional, or download a binary from " +
        "https://github.com/codevico/sunodl/releases",
    );
    process.exit(1);
  }
}

const result = spawnSync(resolveBinary(), process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  throw result.error;
}
if (result.signal) {
  process.kill(process.pid, result.signal);
}
process.exit(result.status ?? 1);
