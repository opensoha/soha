import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";
import YAML from "yaml";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const source = readFileSync(join(root, "../.github/workflows/contracts-ci.yml"), "utf8");
const workflow = YAML.parse(source);
const steps = workflow.jobs.consumers.steps;
const declared = steps.find((step) => step.id === "declared");
const candidate = steps.find((step) => step.id === "candidate");
const summary = steps.find((step) => step.name === "Report both dependency modes");
const shaExpression = "${{ inputs.contracts_sha || github.sha }}";

test("explicit candidate SHA is validated before either checkout and never supplies publication credentials", () => {
  assert.equal(workflow.on.workflow_dispatch.inputs.contracts_sha.type, "string");
  assert.deepEqual(workflow.permissions, { contents: "read" });
  assert.equal(source.includes("secrets."), false);
  for (const job of Object.values(workflow.jobs)) {
    const validation = job.steps[0];
    assert.equal(validation.name, "Validate Contracts candidate SHA");
    assert.equal(validation.env.CONTRACTS_SHA, shaExpression);
    const checkout = job.steps[1];
    assert.equal(checkout.with.ref, shaExpression);
    assert.equal(checkout.with["persist-credentials"], false);
    for (const sha of ["a".repeat(40), "main", "abcdef0", "a".repeat(40) + "\n", '$(exit 0)']) {
      const result = spawnSync("bash", ["-e", "-c", validation.run], { env: { ...process.env, CONTRACTS_SHA: sha } });
      assert.equal(result.status === 0, sha === "a".repeat(40));
    }
    assert.ok(job.steps.some((step) => step.run?.includes('rev-parse HEAD)" = "$CONTRACTS_SHA"')));
  }
  assert.equal(candidate.env.CONTRACTS_SHA, shaExpression);
  assert.ok(candidate.run.includes('--contracts-sha "$CONTRACTS_SHA"'));
});

test("existing required consumer jobs and pinned selection remain; neither mode can mask a failure", () => {
  assert.deepEqual(workflow.jobs.consumers.strategy.matrix.consumer, ["soha", "soha-web", "soha-app", "soha-cli", "soha-agent", "soha-skills"]);
  assert.equal(workflow.jobs.consumers.strategy["fail-fast"], false);
  assert.equal(workflow.jobs.consumers["continue-on-error"], undefined);
  assert.equal(steps.find((step) => step.id === "consumer-ref").env.CONSUMER_REVISION_MODE, "${{ inputs.consumer_mode || 'pinned' }}");
  assert.equal(candidate.if, "${{ !cancelled() && steps.consumer-setup.outcome == 'success' }}");
  assert.ok(steps.findIndex((step) => step.id === "consumer-setup") < steps.indexOf(declared));
  assert.ok(steps.indexOf(declared) < steps.indexOf(candidate));
  for (const step of [declared, candidate]) {
    assert.equal(step["continue-on-error"], undefined);
    assert.equal(step.run.includes("||"), false);
  }
  assert.ok(declared.run.includes("--declared --out ../../consumer-reports/declared.json"));
  assert.ok(candidate.run.includes("--out ../../consumer-reports/candidate.json"));
  assert.equal(summary.if, "always()");
  const artifacts = steps.find((step) => step.uses?.startsWith("actions/upload-artifact@"));
  assert.equal(artifacts.if, "always()");
  assert.equal(artifacts.with.path, "consumer-reports/*.json");
});

for (const failures of [[true, false], [false, true], [true, true], [false, false]]) {
  test(`workflow commands keep both mode results visible (declared fails=${failures[0]}, candidate fails=${failures[1]})`, (t) => {
    const directory = mkdtempSync(join(tmpdir(), "consumer-ci-unit-"));
    t.after(() => rmSync(directory, { recursive: true, force: true }));
    const trace = join(directory, "trace");
    const report = join(directory, "summary");
    writeFileSync(join(directory, "npm"), '#!/bin/sh\nprintf "%s\\n" "$*" >> "$TRACE"\ncase "$*" in *--declared*) exit "$DECLARED_EXIT";; *) exit "$CANDIDATE_EXIT";; esac\n', { mode: 0o755 });
    const env = { ...process.env, PATH: `${directory}:${process.env.PATH}`, TRACE: trace, CONSUMER: "soha-web", CONTRACTS_SHA: "a".repeat(40), GITHUB_WORKSPACE: directory, DECLARED_EXIT: failures[0] ? "17" : "0", CANDIDATE_EXIT: failures[1] ? "23" : "0" };
    const results = [declared, candidate].map((step) => spawnSync("bash", ["-e", "-o", "pipefail", "-c", step.run], { env }));
    assert.deepEqual(results.map((result) => result.status !== 0), failures);
    assert.equal(readFileSync(trace, "utf8").trim().split("\n").length, 2);
    const outcomes = results.map((result) => result.status === 0 ? "success" : "failure");
    const result = spawnSync("bash", ["-e", "-c", summary.run], { env: { ...env, GITHUB_STEP_SUMMARY: report, DECLARED_RESULT: outcomes[0], CANDIDATE_RESULT: outcomes[1] } });
    assert.equal(result.status, 0);
    assert.equal(readFileSync(report, "utf8"), `Declared dependency: \`${outcomes[0]}\`\nCandidate source: \`${outcomes[1]}\`\n`);
  });
}


test("external baselines are outside the workspace and internal-source is a separate check", () => {
  const checkout = steps.find((step) => step.with?.path === "consumers/${{ matrix.consumer }}");
  assert.ok(checkout.with.repository.includes("github.repository"));
  assert.ok(checkout.with.repository.includes("matrix.consumer == 'soha-web'"));
  assert.equal(checkout.with.ref, "${{ steps.consumer-ref.outputs.ref }}");
  assert.equal(steps.find((step) => step.id === "consumer-ref").run, "node scripts/select-consumer-revision.mjs");
  assert.deepEqual(workflow.on.workflow_dispatch.inputs.consumer_mode.options, ["pinned"]);
  for (const step of [declared, candidate]) {
    assert.equal(step.env.SOHA_CONSUMER_ROOT, "${{ github.workspace }}/consumers");
    assert.equal(step["working-directory"], "source/contracts");
  }
  for (const flag of ["--contracts-skill-schema", "--contracts-mcp-preset-schema", "--contracts-agent-profile-schema", "--contracts-permission-catalog"])
    assert.ok(candidate.run.includes(flag));
  assert.ok(workflow.jobs["internal-source"].steps.some((step) => step.run?.includes("--internal-source --require-all")));
  assert.equal(workflow.jobs["internal-source"]["continue-on-error"], undefined);
});

test("active router waits for every selected workflow and preserves the original gates", () => {
  const readWorkflow = (name) => YAML.parse(readFileSync(join(root, `../.github/workflows/${name}.yml`), "utf8"));
  const ci = readWorkflow("ci");
  assert.deepEqual(Object.keys(ci.on), ["pull_request", "push", "workflow_dispatch"]);
  assert.equal(ci.jobs.summary.if, "always()");
  assert.deepEqual(ci.jobs.summary.needs, ["route", "core", "web", "contracts", "deletion"]);
  assert.equal(ci.jobs.summary.steps.at(-1).env.CI_NEEDS, "${{ toJSON(needs) }}");
  assert.equal(ci.jobs.summary.steps.at(-1).run, "python3 scripts/ci-impact.py summary");
  for (const name of ["core", "web", "contracts", "deletion"]) {
    assert.equal(ci.jobs[name].needs, "route");
    assert.equal(ci.jobs[name].if, `needs.route.outputs.${name} == 'true'`);
  }
  for (const name of ["Test", "Race test", "Vet", "Production complexity gate", "Govulncheck", "Golangci-lint", "Build application binaries", "Verify network test assets", "Validate optional runtime Compose profiles", "Docker build", "Verify container storage permissions"])
    assert.ok(ci.jobs.core.steps.some((step) => step.name === name), `missing Core ${name}`);
  assert.equal(ci.jobs.core.steps.find((step) => step.name === "Docker build").run.split("\n").filter((line) => line.startsWith("docker build ")).length, 7);
  const web = readWorkflow("web-ci");
  assert.deepEqual(Object.keys(web.on), ["workflow_call"]);
  for (const name of ["Release workflow guard", "Enhanced workflow trust guard", "E2E typecheck", "Route architecture guard", "Frontend boundary guard", "Shared table import guard", "Lint", "Format check", "Typecheck", "Test", "Coverage", "Build", "Bundle budget", "Browser regression including migrated Kubernetes baseline"])
    assert.ok(web.jobs.verify.steps.some((step) => step.name === name), `missing Web ${name}`);
  const deletion = readWorkflow("deletion-integration");
  assert.deepEqual(Object.keys(deletion.on), ["workflow_call", "workflow_dispatch"]);
  assert.ok(deletion.jobs["postgres-deletion"].services.postgres);
  assert.ok(deletion.jobs["postgres-deletion"].steps.find((step) => step.name === "Run actual PostgreSQL deletion scenarios").run.includes("scripts/check-go-test-evidence.py"));
  assert.equal(source.includes("npm test"), true);
  assert.equal(workflow.jobs.verify.steps.find((step) => step.run === "npm test").env.GOWORK, "off");
});
