import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const npm = process.platform === "win32" ? "npm.cmd" : "npm";

const packageJson = JSON.parse(await readFile(path.join(root, "package.json"), "utf8"));
const connectorIndex = JSON.parse(await readFile(path.join(root, "connectors/index.json"), "utf8"));
const version = optionValue("--version") ?? packageJson.version;
const outputDir = path.resolve(root, optionValue("--out-dir") ?? "dist/release");

await mkdir(outputDir, { recursive: true });

const { stdout } = await execFileAsync(
  npm,
  ["pack", "--json", "--pack-destination", outputDir, "--ignore-scripts"],
  {
    cwd: root,
    maxBuffer: 1024 * 1024 * 10
  }
);
const pack = JSON.parse(stdout)[0];
const artifactPath = path.join(outputDir, pack.filename);
const artifactSha256 = await sha256File(artifactPath);
const checksumPath = `${artifactPath}.sha256`;
await writeFile(checksumPath, `${artifactSha256}  ${pack.filename}\n`);

const manifest = {
  schemaVersion: "opensoha.dev/connectors-release/v1",
  packageName: packageJson.name,
  version,
  privatePackage: packageJson.private === true,
  publishTarget: "github-release",
  npmPublish: false,
  artifact: pack.filename,
  artifactSha256,
  checksumFile: path.basename(checksumPath),
  provenance: {
    sourceRepository: "github.com/opensoha/soha-connectors",
    buildType: "github-actions",
    attestationProvider: "github-actions-attest-build-provenance",
    signingRequired: true,
    verificationCommand: "gh attestation verify <artifact> --repo opensoha/soha-connectors"
  },
  connectorManifests: await connectorManifestEntries(connectorIndex),
  files: (pack.files ?? []).map((file) => ({
    path: file.path,
    size: file.size,
    mode: file.mode
  }))
};

const manifestPath = path.join(outputDir, `opensoha-connectors-${version}.release-manifest.json`);
await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);

console.log(`release artifact ${path.relative(root, artifactPath)} sha256=${artifactSha256}`);
console.log(`release manifest ${path.relative(root, manifestPath)}`);

function optionValue(name) {
  const index = process.argv.indexOf(name);
  if (index === -1) {
    return undefined;
  }
  return process.argv[index + 1];
}

async function sha256File(file) {
  return createHash("sha256").update(await readFile(file)).digest("hex");
}

async function connectorManifestEntries(index) {
  const entries = [];
  for (const connector of index.connectors ?? []) {
    if (typeof connector.path !== "string" || !connector.path.endsWith(".json")) {
      continue;
    }
    entries.push({
      id: connector.id,
      path: connector.path,
      sha256: await sha256File(path.join(root, connector.path))
    });
  }
  return entries.sort((left, right) => left.id.localeCompare(right.id));
}
