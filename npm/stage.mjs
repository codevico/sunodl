import { chmodSync, copyFileSync, cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const targets = [
  { os: "win32", cpu: "x64", asset: "sunodl-windows-x64.exe" },
  { os: "win32", cpu: "arm64", asset: "sunodl-windows-arm64.exe" },
  { os: "linux", cpu: "x64", asset: "sunodl-linux-x64" },
  { os: "linux", cpu: "arm64", asset: "sunodl-linux-arm64" },
  { os: "darwin", cpu: "x64", asset: "sunodl-darwin-x64" },
  { os: "darwin", cpu: "arm64", asset: "sunodl-darwin-arm64" },
];

const [version, distDir, outDir] = process.argv.slice(2);
if (!version || !distDir || !outDir) {
  console.error("usage: node npm/stage.mjs <version> <dist dir> <out dir>");
  process.exit(2);
}

const npmDir = dirname(fileURLToPath(import.meta.url));
const mainSource = join(npmDir, "sunodl");
const licenseFile = join(npmDir, "..", "LICENSE");
const mainManifest = JSON.parse(readFileSync(join(mainSource, "package.json"), "utf8"));

function writeJson(path, value) {
  writeFileSync(path, JSON.stringify(value, null, 2) + "\n");
}

function stagePlatformPackage({ os, cpu, asset }) {
  const name = `@codevico/sunodl-${os}-${cpu}`;
  const packageDir = join(outDir, name);
  const executable = os === "win32" ? "sunodl.exe" : "sunodl";
  const binary = join(packageDir, "bin", executable);

  mkdirSync(dirname(binary), { recursive: true });
  copyFileSync(join(distDir, asset), binary);
  chmodSync(binary, 0o755);
  copyFileSync(licenseFile, join(packageDir, "LICENSE"));
  writeJson(join(packageDir, "package.json"), {
    name,
    version,
    description: `${mainManifest.name} binary for ${os}-${cpu}`,
    license: mainManifest.license,
    repository: mainManifest.repository,
    os: [os],
    cpu: [cpu],
    files: ["bin"],
    preferUnplugged: true,
  });
  return name;
}

function stageMainPackage(platformPackages) {
  const packageDir = join(outDir, mainManifest.name);
  cpSync(mainSource, packageDir, { recursive: true });
  chmodSync(join(packageDir, "bin", "sunodl.js"), 0o755);
  copyFileSync(licenseFile, join(packageDir, "LICENSE"));
  writeJson(join(packageDir, "package.json"), {
    ...mainManifest,
    version,
    optionalDependencies: Object.fromEntries(platformPackages.map((name) => [name, version])),
  });
  return mainManifest.name;
}

rmSync(outDir, { recursive: true, force: true });
const platformPackages = targets.map(stagePlatformPackage);
const mainPackage = stageMainPackage(platformPackages);

for (const name of [...platformPackages, mainPackage]) {
  console.log(join(outDir, name));
}
