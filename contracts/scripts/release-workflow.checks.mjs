import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";
import YAML from "yaml";
import { validateStageRun } from "./verify-stage-run.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const workflow = YAML.parse(readFileSync(join(root, "../.github/workflows/contracts-release.yml"), "utf8"));
const steps = workflow.jobs.release.steps;
const publish = { ...steps.find((step) => step.id === "publish"), run: readFileSync(join(root, "scripts/stage-release.sh"), "utf8") };
const stageId = "12345678-1234-4321-8321-123456789abc";
const secret = "SYNTHETIC_PUBLISH_TOKEN_NEVER_LOG";

const fakeNpm = `#!/usr/bin/env node
const { appendFileSync, readFileSync } = require('node:fs');
const { createHash } = require('node:crypto');
const args = process.argv.slice(2);
appendFileSync(process.env.NPM_CALLS, JSON.stringify(args) + '\\n');
if (args[0] === 'view') {
  if (!process.env.MOCK_PUBLIC_INTEGRITY) { console.error('npm error code ' + (process.env.MOCK_VIEW_ERROR || 'E404')); process.exit(1); }
  console.log(process.env.MOCK_PUBLIC_INTEGRITY);
} else if (args[0] === 'stage' && args[1] === 'publish') {
  if (process.env.MOCK_STAGE_EXIT) process.exit(Number(process.env.MOCK_STAGE_EXIT));
  const integrity = 'sha512-' + createHash('sha512').update(readFileSync(process.env.CONTRACTS_NPM_TARBALL)).digest('base64');
  const receipt = { id: '@opensoha/contracts@' + process.env.CONTRACTS_NPM_VERSION, name: '@opensoha/contracts', version: process.env.MOCK_STAGE_VERSION || process.env.CONTRACTS_NPM_VERSION, integrity: process.env.MOCK_STAGE_INTEGRITY || integrity, stageId: process.env.MOCK_STAGE_ID || '${stageId}' };
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

// npm 12.2.2 publish.js adds stageId; tar.js emits the package-name-keyed map.
for (const format of ["map", "object", "array"]) {
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

test("Root publisher preserves CI, protected OIDC stage and exact source handoff gates", () => {
  assert.deepEqual(workflow.on.push.tags, ["contracts/v*"]);
  assert.equal(workflow.on.workflow_dispatch.inputs.publish_mode.default, "stage");
  assert.deepEqual(workflow.on.workflow_dispatch.inputs.publish_mode.options, ["stage", "recover"]);
  assert.deepEqual(workflow.permissions, { contents: "read" });
  assert.equal(workflow.jobs.verify.uses, "./.github/workflows/contracts-ci.yml");
  assert.equal(workflow.jobs.verify.with.contracts_sha, "${{ needs.identity.outputs.sha }}");
  assert.deepEqual(workflow.jobs.release.needs, ["identity", "verify", "artifacts"]);
  assert.equal(workflow.jobs.release.environment, "release-contracts");
  assert.deepEqual(workflow.jobs.release.permissions, { actions: "read", contents: "write", "id-token": "write" });
  assert.ok(steps.some((step) => step.run === "npm install --global npm@12.2.2"));
  assert.equal(steps.find((step) => step.uses === "actions/setup-node@v6").with["node-version"], "22.23.4");
  assert.equal(JSON.stringify(workflow).includes("NPM_TOKEN"), false);
  assert.equal(JSON.stringify(workflow.jobs.artifacts).includes("npm@12"), false);
  assert.ok(workflow.jobs.artifacts.steps.some((step) => step.run?.includes("release:artifacts")));
  assert.equal(steps.some((step) => step.run?.includes("release:artifacts")), false);
  const prepare = workflow.jobs.artifacts.steps.find((step) => step.run?.includes("release:artifacts"));
  assert.equal(prepare.if, "inputs.publish_mode != 'recover'");
  const restore = workflow.jobs.artifacts.steps.findIndex((step) => step.with?.["run-id"]);
  assert(restore > workflow.jobs.artifacts.steps.findIndex((step) => step.run === "node scripts/verify-stage-run.mjs"));
  assert.equal(workflow.jobs.artifacts.steps[restore].with.name, "contracts-staged-${{ needs.identity.outputs.sha }}");
  const upload = steps.find((step) => step.name === "GitHub release").run;
  assert(upload.includes('python3 scripts/publish-release-assets.py --component contracts --tag "$RELEASE_TAG" --sha "$RELEASE_SHA" --files'));
  assert.equal(/--directory|--latest|--clobber|npm-stage|npm-view|\*/.test(upload), false);
  assert.equal(JSON.stringify(workflow).includes("softprops/action-gh-release"), false);
  const guard = steps.findIndex((step) => step.run?.includes("release-control.py verify"));
  assert(guard >= 0 && guard < steps.findIndex((step) => step.id === "publish"));
  assert.equal(steps[guard].if, undefined, "recover must never skip the external preflight");
  for (const name of ["Verify published npm bytes and official Go proxy with sumDB", "GitHub release", "Download published release assets and verify"]) {
    assert.equal(steps.find((step) => step.name === name).if, "steps.publish.outputs.published == 'true'");
  }
  assert.equal(steps.find((step) => step.name === "Save pending stage receipt and release candidate").if, "steps.publish.outputs.published == 'false'");
  for (const job of Object.values(workflow.jobs)) for (const step of job.steps ?? []) {
    if (step.run) assert.equal(step.run.includes("${{"), false, "shell inputs must flow through env");
  }
  const publicCheck = steps.find((step) => step.name === "Verify published npm bytes and official Go proxy with sumDB");
  assert.deepEqual(publicCheck.env, { GOWORK: "off", GOPROXY: "https://proxy.golang.org", GOSUMDB: "sum.golang.org", GOPRIVATE: "", GONOPROXY: "", GONOSUMDB: "" });
});

test("Root release input validation rejects unknown modes and injected/mismatched tags", () => {
  const directory = mkdtempSync(join(tmpdir(), "contracts-tag-test-"));
  try {
    const validation = workflow.jobs.identity.steps.find((step) => step.name === "Validate release inputs as data").run;
    const run = (tag, mode = "stage", runId = "") => spawnSync("bash", ["-e", "-o", "pipefail", "-c", validation], {
      cwd: directory, encoding: "utf8", env: { ...process.env, RELEASE_TAG: tag, NPM_PUBLISH_MODE: mode, STAGE_RUN_ID: runId },
    });
    assert.equal(run("contracts/v0.2.0").status, 0);
    assert.equal(run("contracts/v0.2.0", "recover", "123").status, 0);
    assert.notEqual(run("contracts/v0.2.0", "recover").status, 0);
    assert.notEqual(run("contracts/v0.2.0", "recover", "123; false").status, 0);
    assert.notEqual(run("contracts/v0.2.0", "stage", "123").status, 0);
    assert.notEqual(run("contracts/v0.2.0", "direct").status, 0);
    const marker = join(directory, "must-not-exist");
    for (const tag of ["v0.2.0", "cli/v0.2.0", "contracts/v0.1.29", "contracts/v0.2.0\ninvalid", "contracts/v0.2.0 ", `contracts/v0.2.0$(touch "${marker}")`]) {
      assert.notEqual(run(tag).status, 0);
      assert.equal(existsSync(marker), false);
    }
    const identity = workflow.jobs.identity.steps.find((step) => step.id === "identity").run;
    assert(identity.includes('test "$GITHUB_REF" = "refs/tags/$RELEASE_TAG"'));
    assert(identity.includes('test "$GITHUB_SHA" = "$RELEASE_SHA"'));
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

test("registry errors fail closed instead of creating another stage", () => {
  const result = runPublish({ mock: { MOCK_VIEW_ERROR: "E401" } });
  assert.notEqual(result.status, 0);
  assert.equal(result.output, "");
  assert.deepEqual(result.calls.map((call) => call[0]), ["view"]);
});

test("recovery accepts only a successful original Root publisher run for the same tag and SHA", () => {
  const sha = "a".repeat(40);
  const identity = { repository: "opensoha/soha", tag: "contracts/v0.2.0", sha, runId: "123" };
  const workflow = { id: 456, path: ".github/workflows/contracts-release.yml" };
  const run = { id: 123, repository: { full_name: identity.repository }, head_repository: { full_name: identity.repository }, workflow_id: workflow.id, path: workflow.path, head_sha: sha, head_branch: identity.tag, event: "push", status: "completed", conclusion: "success" };
  validateStageRun(run, workflow, identity);
  validateStageRun({ ...run, event: "workflow_dispatch" }, workflow, identity);
  for (const change of [{ id: 124 }, { workflow_id: 457 }, { path: ".github/workflows/release.yml" }, { head_sha: "b".repeat(40) }, { head_branch: "main" }, { event: "pull_request" }, { status: "in_progress" }, { conclusion: "failure" }, { repository: { full_name: "opensoha/soha-contracts" } }, { head_repository: { full_name: "someone/soha" } }]) {
    assert.throws(() => validateStageRun({ ...run, ...change }, workflow, identity));
  }
});
