import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

export async function verifyNpmRelease({ name, version, tag, sha, workflow, integrity, sha256 }) {
  assert.match(name, /^(?:@[a-z0-9-]+\/)?[a-z0-9][a-z0-9._-]*$/);
  const metadata = await get(`https://registry.npmjs.org/${encodeURIComponent(name)}/${version}`, true);
  assert.equal(metadata.name, name);
  assert.equal(metadata.version, version);
  assert.equal(metadata.dist.integrity, integrity);
  assert.equal(new URL(metadata.dist.tarball).origin, "https://registry.npmjs.org");
  assert(metadata.dist.attestations?.url, "npm provenance attestation is required");
  const body = await get(metadata.dist.tarball, false);
  assert.equal(`sha512-${createHash("sha512").update(body).digest("base64")}`, integrity);
  assert.equal(createHash("sha256").update(body).digest("hex"), sha256);

  const directory = mkdtempSync(join(tmpdir(), "npm-public-release-"));
  try {
    writeFileSync(join(directory, "package.json"), '{"name":"npm-public-verification","version":"0.0.0","private":true}\n');
    run("npm", ["install", "--ignore-scripts", "--no-audit", "--no-fund", `${name}@${version}`]);
    const audit = JSON.parse(run("npm", ["audit", "signatures", "--json", "--include-attestations"]));
    assert.deepEqual(audit.invalid, []);
    assert.deepEqual(audit.missing, []);
    const verified = audit.verified?.find((item) => item.name === name && item.version === version);
    const provenance = verified?.attestationBundles?.find((item) => item.predicateType === "https://slsa.dev/provenance/v1");
    assert(provenance, "a cryptographically verified SLSA provenance statement is required");
    const statement = JSON.parse(Buffer.from(provenance.bundle.dsseEnvelope.payload, "base64").toString("utf8"));
    const definition = statement.predicate.buildDefinition;
    assert.equal(definition.externalParameters.workflow.repository, "https://github.com/opensoha/soha");
    assert.equal(definition.externalParameters.workflow.path, workflow);
    assert.equal(definition.externalParameters.workflow.ref, `refs/tags/${tag}`);
    assert(definition.resolvedDependencies.some((item) => item.uri === `git+https://github.com/opensoha/soha@refs/tags/${tag}` && item.digest?.gitCommit === sha));
    return { integrity, sha256, provenance: "npm audit signatures passed" };
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }

  function run(command, args) {
    const result = spawnSync(command, args, { cwd: directory, encoding: "utf8", env: process.env });
    if (result.status !== 0) throw new Error(`${command} ${args.join(" ")} failed: ${result.stdout}\n${result.stderr}`);
    return result.stdout;
  }
}

async function get(url, json) {
  const response = await fetch(url, { signal: AbortSignal.timeout(60_000) });
  if (!response.ok) throw new Error(`public registry retrieval failed: ${response.status} ${url}`);
  return json ? response.json() : Buffer.from(await response.arrayBuffer());
}
