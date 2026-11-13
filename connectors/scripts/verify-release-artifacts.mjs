import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const packageJson = JSON.parse(await readFile(path.join(root, "package.json"), "utf8"));
const artifactArg = process.argv[2];

if (!artifactArg) {
  throw new Error("usage: npm run release:verify -- <artifact.tgz>");
}

const artifactPath = path.resolve(root, artifactArg);
const version = packageJson.version;
const connectorIndex = JSON.parse(await readFile(path.join(root, "connectors/index.json"), "utf8"));
const manifestPath = path.join(path.dirname(artifactPath), `opensoha-connectors-${version}.release-manifest.json`);
const checksumPath = `${artifactPath}.sha256`;
const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
const checksumText = (await readFile(checksumPath, "utf8")).trim();
const actualSha256 = await sha256File(artifactPath);

if (manifest.schemaVersion !== "opensoha.dev/connectors-release/v1") {
  throw new Error(`unexpected release manifest schemaVersion ${manifest.schemaVersion}`);
}
if (manifest.packageName !== packageJson.name || manifest.version !== version) {
  throw new Error("release manifest package identity does not match package.json");
}
if (manifest.npmPublish !== false || manifest.publishTarget !== "github-release") {
  throw new Error("release manifest must declare GitHub release packaging, not npm publish");
}
if (manifest.artifact !== path.basename(artifactPath)) {
  throw new Error(`release manifest artifact ${manifest.artifact} does not match ${path.basename(artifactPath)}`);
}
if (manifest.artifactSha256 !== actualSha256) {
  throw new Error(`release manifest sha256 mismatch, expected ${manifest.artifactSha256}, got ${actualSha256}`);
}
if (checksumText !== `${actualSha256}  ${path.basename(artifactPath)}`) {
  throw new Error(`${checksumPath} does not match artifact sha256/name`);
}
if (manifest.provenance?.sourceRepository !== "github.com/opensoha/soha-connectors") {
  throw new Error("release manifest must declare the source repository provenance");
}
if (manifest.provenance?.attestationProvider !== "github-actions-attest-build-provenance") {
  throw new Error("release manifest must require GitHub build provenance attestation");
}
if (manifest.provenance?.signingRequired !== true) {
  throw new Error("release manifest provenance.signingRequired must be true");
}

const expectedConnectorManifests = await connectorManifestEntries(connectorIndex);
if (JSON.stringify(manifest.connectorManifests ?? []) !== JSON.stringify(expectedConnectorManifests)) {
  throw new Error("release manifest connectorManifests do not match current connector manifest checksums");
}

const { stdout } = await execFileAsync("tar", ["-tzf", artifactPath], { maxBuffer: 1024 * 1024 * 10 });
const members = new Set(
  stdout
    .split(/\r?\n/)
    .filter(Boolean)
    .map((member) => member.replace(/^package\//, ""))
);

const missing = [];
for (const file of manifest.files ?? []) {
  if (!members.has(file.path)) {
    missing.push(file.path);
  }
}
if (missing.length > 0) {
  throw new Error(`release artifact is missing manifest files:\n${missing.map((file) => `- ${file}`).join("\n")}`);
}

console.log(`verified ${path.relative(root, artifactPath)} sha256=${actualSha256} files=${manifest.files.length}`);

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
