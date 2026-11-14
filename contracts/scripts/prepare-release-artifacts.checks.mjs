import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, delimiter, join } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = fileURLToPath(new URL("..", import.meta.url));
const script = fileURLToPath(new URL("./prepare-release-artifacts.mjs", import.meta.url));
const metadata = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
const digest = (body) => createHash("sha256").update(body).digest("hex");
function run(args, extraEnv = {}) {
  const env = { ...process.env, ...extraEnv };
  delete env.GITHUB_OUTPUT;
  return spawnSync(process.execPath, [script, ...args], { cwd: root, encoding: "utf8", env });
}

test("local SDK pack binds Root namespace/source/tag and refuses artifact identity drift", async (t) => {
  const out = mkdtempSync(join(tmpdir(), "contracts-artifact-check-"));
  t.after(() => rmSync(out, { recursive: true, force: true }));
  const releaseSha = spawnSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).stdout.trim();
  const prepared = run(["--out-dir", out], { RELEASE_SHA: releaseSha });
  assert.equal(prepared.status, 0, prepared.stdout + prepared.stderr);
  const manifestPath = join(out, "release-manifest.json");
  const manifestBody = readFileSync(manifestPath, "utf8");
  const manifest = JSON.parse(manifestBody);
  assert.equal(manifest.releaseSource.revision, releaseSha);
  assert.equal(manifest.tag, `contracts/v${metadata.version}`);
  assert.equal(manifest.goModule, "github.com/opensoha/soha/contracts");
  assert.deepEqual(manifest.source, { repository: "opensoha/soha", directory: "contracts" });
  const sbom = JSON.parse(readFileSync(join(out, basename(manifest.sbom.path)), "utf8"));
  const rootMetadata = JSON.parse(readFileSync(join(root, "../package.json"), "utf8"));
  const rootRef = `${rootMetadata.name}@${rootMetadata.version}`;
  assert.equal(sbom.metadata.component.purl, `pkg:npm/%40opensoha/contracts@${metadata.version}`);
  assert.equal(sbom.metadata.component["bom-ref"], `${metadata.name}@${metadata.version}`);
  const refs = new Set([sbom.metadata.component["bom-ref"], ...sbom.components.map((item) => item["bom-ref"])]);
  assert.equal(refs.has(rootRef), false);
  assert.equal(refs.size, sbom.components.length + 1, "SBOM refs remain unique");
  assert.equal(sbom.components.some((item) => item.purl === sbom.metadata.component.purl), false);
  assert.equal(sbom.components.some((item) => item["bom-ref"].startsWith("soha-web@") || item["bom-ref"].startsWith("@midscene/web@")), false);
  for (const name of Object.keys(metadata.devDependencies)) {
    assert(sbom.components.some((item) => item["bom-ref"].startsWith(`${name}@`)), `missing SDK dependency ${name}`);
  }
  for (const item of sbom.dependencies) {
    assert.equal(refs.has(item.ref), true, item.ref);
    for (const ref of item.dependsOn ?? []) assert.equal(refs.has(ref), true, ref);
  }
  const tarball = join(out, manifest.artifacts.find((item) => item.kind === "npm-tarball").name);
  assert.equal(run(["--verify", tarball], { RELEASE_SHA: releaseSha }).status, 0);

  for (const change of [{ releaseSource: undefined }, { releaseSource: { revision: releaseSha, tree: "b".repeat(40) } }]) {
    await t.test("rejects missing or mismatched candidate source tree", () => {
      writeFileSync(manifestPath, JSON.stringify({ ...manifest, ...change }));
      const result = run(["--verify", tarball], { RELEASE_SHA: releaseSha });
      assert.notEqual(result.status, 0);
      assert.match(result.stderr, /release source revision\/tree/);
      writeFileSync(manifestPath, manifestBody);
    });
  }

  await t.test("rejects changed SRI metadata despite unchanged sha256", () => {
    const changed = structuredClone(manifest);
    changed.artifacts.find((item) => item.kind === "npm-tarball").npmIntegrity = "sha512-different";
    writeFileSync(manifestPath, JSON.stringify(changed));
    assert.notEqual(run(["--verify", tarball]).status, 0);
    writeFileSync(manifestPath, manifestBody);
  });

  for (const [field, value] of [["tag", `v${metadata.version}`], ["goModule", "github.com/opensoha/soha-contracts"], ["source", { repository: "opensoha/soha-contracts", directory: "" }]]) {
    await t.test(`rejects manifest ${field} drift`, () => {
      writeFileSync(manifestPath, JSON.stringify({ ...manifest, [field]: value }));
      const result = run(["--verify", tarball]);
      assert.notEqual(result.status, 0);
      assert.match(result.stderr, /version\/tag|SDK source identity/);
      writeFileSync(manifestPath, manifestBody);
    });
  }

  await t.test("rejects a rehashed policy that points at the Core publisher", () => {
    const policyPath = join(out, basename(manifest.provenance.path));
    const checksumPath = join(out, basename(manifest.provenance.checksumFile));
    const body = readFileSync(policyPath, "utf8");
    const checksum = readFileSync(checksumPath, "utf8");
    const policy = JSON.parse(body);
    assert.equal(policy.workflow.path, ".github/workflows/contracts-release.yml");
    assert.equal(policy.attestation.localArtifactsDoNotProveRemoteAttestation, true);
    policy.workflow.path = ".github/workflows/release.yml";
    const changed = JSON.stringify(policy);
    const sha = digest(changed);
    writeFileSync(policyPath, changed);
    writeFileSync(checksumPath, `${sha}  ${basename(policyPath)}\n`);
    writeFileSync(manifestPath, JSON.stringify({ ...manifest, provenance: { ...manifest.provenance, sha256: sha } }));
    const result = run(["--verify", tarball]);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /Root SDK publisher/);
    writeFileSync(policyPath, body);
    writeFileSync(checksumPath, checksum);
    writeFileSync(manifestPath, manifestBody);
  });

  await t.test("rejects changed tarball bytes before extraction", () => {
    const body = readFileSync(tarball);
    writeFileSync(tarball, Buffer.concat([body, Buffer.from("changed")]));
    const result = run(["--verify", tarball]);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /sha256 mismatch/);
    writeFileSync(tarball, body);
  });
  const bin = join(out, "bin");
  mkdirSync(bin);
  const npm = join(bin, "npm");
  const pack = manifest.artifacts.find((item) => item.kind === "npm-tarball");
  const packInfo = { filename: pack.name, integrity: pack.npmIntegrity, shasum: pack.npmShasum, size: pack.sizeBytes, entryCount: pack.fileCount };
  writeFileSync(npm, `#!/usr/bin/env node
const { copyFileSync, readFileSync } = require('node:fs');
const { join } = require('node:path');
const args = process.argv.slice(2);
const info = JSON.parse(process.env.PACK_FIXTURE_INFO);
if (args[0] === 'pack') {
  copyFileSync(process.env.PACK_FIXTURE_TARBALL, join(args[args.indexOf('--pack-destination') + 1], info.filename));
  console.log(JSON.stringify([info]));
} else if (args[0] === 'sbom') {
  console.log(readFileSync(process.env.SBOM_FIXTURE, 'utf8'));
} else process.exit(90);
`, { mode: 0o755 });
  for (const mode of ["missing-sdk", "duplicate-sdk", "root-dependency", "conflicting-artifact", "conflicting-property"]) {
    await t.test(`rejects npm SBOM ${mode}`, () => {
      const fixture = structuredClone(sbom);
      const sdk = fixture.metadata.component;
      fixture.metadata.component = { "bom-ref": rootRef };
      if (mode !== "missing-sdk") fixture.components.push(sdk);
      if (mode === "duplicate-sdk") fixture.components.push(structuredClone(sdk));
      if (mode === "conflicting-artifact") fixture.components.push({ ...structuredClone(fixture.components[0]), hashes: [{ alg: "SHA-512", content: "different" }] });
      if (mode === "conflicting-property") fixture.components.push({ ...structuredClone(fixture.components[0]), properties: [...(fixture.components[0].properties ?? []), { name: "cdx:npm:package:private", value: "true" }] });
      if (mode === "root-dependency") fixture.dependencies.find((item) => item.ref === sdk["bom-ref"]).dependsOn.push(rootRef);
      const path = join(out, `${mode}.json`);
      writeFileSync(path, JSON.stringify(fixture));
      const result = run(["--out-dir", join(out, mode)], { PATH: `${bin}${delimiter}${process.env.PATH}`, PACK_FIXTURE_INFO: JSON.stringify(packInfo), PACK_FIXTURE_TARBALL: tarball, SBOM_FIXTURE: path });
      assert.notEqual(result.status, 0);
      assert.match(result.stderr, /exactly one SDK|Root synthetic component|conflicting artifact identity/);
    });
  }
  console.log(JSON.stringify({ package: metadata.name, version: metadata.version, goModule: manifest.goModule, tag: manifest.tag, source: manifest.source, tarballSha256: digest(readFileSync(tarball)), sbomIdentity: sbom.metadata.component.purl, sbomComponents: sbom.components.length, verification: "local candidate only; no registry/proxy/attestation qualification" }));
});
