import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";
import YAML from "yaml";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const workflow = YAML.parse(readFileSync(join(root, ".github/workflows/release.yml"), "utf8"));
const steps = workflow.jobs.release.steps;
const publish = steps.find((step) => step.id === "publish");
const stageId = "12345678-1234-4321-8321-123456789abc";
const secret = "SYNTHETIC_PUBLISH_TOKEN_NEVER_LOG";

const fakeNpm = `#!/usr/bin/env node
const { appendFileSync, readFileSync } = require('node:fs');
const { createHash } = require('node:crypto');
const args = process.argv.slice(2);
appendFileSync(process.env.NPM_CALLS, JSON.stringify(args) + '\\n');
if (args[0] === 'view') {
  if (!process.env.MOCK_PUBLIC_INTEGRITY) process.exit(1);
  console.log(process.env.MOCK_PUBLIC_INTEGRITY);
} else if (args[0] === 'stage' && args[1] === 'publish') {
  if (process.env.MOCK_STAGE_EXIT) process.exit(Number(process.env.MOCK_STAGE_EXIT));
  const integrity = 'sha512-' + createHash('sha512').update(readFileSync(process.env.CONTRACTS_NPM_TARBALL)).digest('base64');
  const receipt = { name: '@opensoha/contracts', version: process.env.MOCK_STAGE_VERSION || process.env.CONTRACTS_NPM_VERSION, integrity: process.env.MOCK_STAGE_INTEGRITY || integrity, stageId: process.env.MOCK_STAGE_ID || '${stageId}' };
  const result = process.env.MOCK_STAGE_FORMAT === 'array' ? [receipt] : process.env.MOCK_STAGE_FORMAT === 'map' ? { '@opensoha/contracts': receipt } : receipt;
  console.log(JSON.stringify(result));
} else {
  console.error('Unexpected npm command');
  process.exit(90);
}
`;

function runPublish(options = {}) {
  const directory = mkdtempSync(join(tmpdir(), "contracts-publish-test-"));
  try {
    const bin = join(directory, "bin");
    const release = join(directory, "dist/release");
    mkdirSync(bin);
    mkdirSync(release, { recursive: true });
    writeFileSync(join(bin, "npm"), fakeNpm, { mode: 0o755 });
    const tarball = join(release, "candidate.tgz");
    const body = Buffer.from("fixed release candidate bytes");
    writeFileSync(tarball, body);
    const integrity = `sha512-${createHash("sha512").update(body).digest("base64")}`;
    const output = join(directory, "output");
    const summary = join(directory, "summary");
    const callsFile = join(directory, "calls");
    writeFileSync(output, "");
    writeFileSync(summary, "");
    writeFileSync(callsFile, "");
    const result = spawnSync("bash", ["-e", "-o", "pipefail", "-c", publish.run], {
      cwd: directory,
      encoding: "utf8",
      env: {
        ...process.env,
        PATH: `${bin}:${process.env.PATH}`,
        NODE_AUTH_TOKEN: secret,
        NPM_PUBLISH_MODE: options.mode || "stage",
        CONTRACTS_NPM_VERSION: "0.1.27",
        CONTRACTS_NPM_TARBALL: tarball,
        GITHUB_OUTPUT: output,
        GITHUB_STEP_SUMMARY: summary,
        NPM_CALLS: callsFile,
        ...(options.published ? { MOCK_PUBLIC_INTEGRITY: options.published === "same" ? integrity : "sha512-different" } : {}),
        ...(options.mock || {}),
      },
    });
    const data = {
      ...result,
      output: readFileSync(output, "utf8"),
      summary: readFileSync(summary, "utf8"),
      calls: readFileSync(callsFile, "utf8").trim().split("\n").filter(Boolean).map(JSON.parse),
    };
    for (const value of [data.stdout, data.stderr, data.output, data.summary]) {
      assert.equal(value.includes(secret), false, "publisher must not log its credential");
    }
    return data;
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
}

test("identical public package completes recovery without staging", () => {
  const result = runPublish({ mode: "recover", published: "same" });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.output, "published=true\n");
  assert.deepEqual(result.calls.map((call) => call[0]), ["view"]);
});

test("different public bytes fail without overwriting or staging", () => {
  const result = runPublish({ published: "different" });
  assert.notEqual(result.status, 0);
  assert.equal(result.output, "");
  assert.deepEqual(result.calls.map((call) => call[0]), ["view"]);
});

test("recovery refuses an unpublished version", () => {
  const result = runPublish({ mode: "recover" });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /approve its stage/);
  assert.equal(result.output, "");
  assert.deepEqual(result.calls.map((call) => call[0]), ["view"]);
});

for (const format of ["object", "array", "map"]) {
  test(`stage mode records a verified pending receipt (${format})`, () => {
    const result = runPublish({ mock: { MOCK_STAGE_FORMAT: format } });
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.output, `published=false\nstage_id=${stageId}\n`);
    assert.match(result.summary, /staged, not public/);
    assert.match(result.summary, /2FA/);
    assert.equal(result.calls.length, 2);
    assert.deepEqual(result.calls[1].slice(0, 2), ["stage", "publish"]);
    assert.ok(result.calls[1].includes("--provenance"));
    assert.ok(result.calls[1].includes("--json"));
    assert.equal(result.calls.some((call) => call[0] === "publish" || call[1] === "approve"), false);
  });
}

test("stage errors propagate without a success receipt", () => {
  const result = runPublish({ mock: { MOCK_STAGE_EXIT: "17" } });
  assert.equal(result.status, 17);
  assert.equal(result.output, "");
  assert.equal(result.summary, "");
});

for (const mock of [
  { MOCK_STAGE_ID: `${stageId}\nevil=true` },
  { MOCK_STAGE_ID: `${stageId}\n` },
  { MOCK_STAGE_VERSION: "0.1.26" },
  { MOCK_STAGE_INTEGRITY: "sha512-different" },
]) {
  test(`invalid stage receipt cannot produce outputs (${Object.keys(mock)[0]})`, () => {
    const result = runPublish({ mock });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /Stage receipt does not match/);
    assert.equal(result.output, "");
    assert.equal(result.summary, "");
  });
}

test("unknown modes fail before contacting npm", () => {
  const result = runPublish({ mode: "direct" });
  assert.notEqual(result.status, 0);
  assert.equal(result.calls.length, 0);
});

test("workflow gates all public-release steps on actual publication", () => {
  assert.equal(workflow.on.workflow_dispatch.inputs.publish_mode.default, "stage");
  assert.deepEqual(workflow.on.workflow_dispatch.inputs.publish_mode.options, ["stage", "recover"]);
  assert.equal(workflow.jobs.release.env.NPM_PUBLISH_MODE, "${{ inputs.publish_mode || 'stage' }}");
  assert.ok(steps.some((step) => step.run === "npm install --global npm@11.21.1"));
  for (const name of ["Verify published npm package and Go module tag", "GitHub release", "Download published release assets and verify"]) {
    assert.equal(steps.find((step) => step.name === name).if, "steps.publish.outputs.published == 'true'");
  }
  assert.equal(steps.find((step) => step.name === "Save pending stage receipt and release candidate").if, "steps.publish.outputs.published == 'false'");
  assert.equal(publish.env.NODE_AUTH_TOKEN, "${{ secrets.NPM_TOKEN }}");
});


test("release tag is validated as data before publication", () => {
  const directory = mkdtempSync(join(tmpdir(), "contracts-tag-test-"));
  try {
    writeFileSync(join(directory, "package.json"), JSON.stringify({ version: "0.1.27" }));
    const validation = steps.find((step) => step.name === "Validate release tag").run;
    assert.equal(validation.includes("${{"), false);
    const run = (tag) => spawnSync("bash", ["-e", "-o", "pipefail", "-c", validation], {
      cwd: directory,
      encoding: "utf8",
      env: { ...process.env, CONTRACTS_RELEASE_TAG: tag },
    });
    assert.equal(run("v0.1.27").status, 0);
    assert.notEqual(run("v0.1.26").status, 0);
    const marker = join(directory, "must-not-exist");
    for (const tag of [
      `v0.1.27"; touch "${marker}"; #`,
      `v0.1.27$(touch "${marker}")`,
      "v0.1.27\ninvalid",
      "v0.1.27 ",
    ]) {
      assert.notEqual(run(tag).status, 0);
      assert.equal(existsSync(marker), false);
    }
    for (const step of steps) {
      if (step.run) assert.equal(step.run.includes("inputs.tag"), false);
    }
    assert.ok(steps.find((step) => step.name === "Download published release assets and verify").run.includes('gh release download "${CONTRACTS_RELEASE_TAG}"'));
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
