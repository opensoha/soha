import assert from "node:assert/strict";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { basename, join } from "node:path";
import YAML from "yaml";

const [input, output] = process.argv.slice(2);
const version = JSON.parse(readFileSync("package.json", "utf8")).version;
const workflow = YAML.parse(readFileSync("../.github/workflows/contracts-ci.yml", "utf8"));
const consumers = workflow.jobs.consumers.strategy.matrix.consumer;
const results = [];
for (const consumer of consumers) {
  for (const mode of ["declared", "candidate"]) {
    const report = readReport(join(input, `consumer-${consumer}-reports`), `${mode}.json`);
    assert.equal(report.results.length, 1);
    assert.equal(report.results[0].consumer, consumer);
    assert(mode === "declared" ? report.dependencyMode === "declared" : ["candidate-source", "internal-source"].includes(report.dependencyMode));
    results.push({ ...report.results[0], dependencyMode: report.dependencyMode });
  }
}
const internal = readReport(join(input, "internal-source-report"), "internal-source-report.json");
assert.equal(internal.dependencyMode, "internal-source");
results.push(...internal.results.map((item) => ({ ...item, dependencyMode: "internal-source" })));
writeFileSync(output, `${JSON.stringify({ schemaVersion: 1, contractsVersion: version, contractsRevision: process.env.RELEASE_SHA, checked: results.length, skipped: 0, results }, null, 2)}\n`);

function readReport(directory, name) {
  // Native evidence can change upload-artifact's common root; locate one exact report per artifact.
  const paths = readdirSync(directory, { recursive: true }).filter((path) => basename(path) === name);
  assert.equal(paths.length, 1, `${directory} must contain exactly one ${name}`);
  const path = join(directory, paths[0]);
  const report = JSON.parse(readFileSync(path, "utf8"));
  assert.equal(report.contractsVersion, version, path);
  assert.match(process.env.RELEASE_SHA ?? "", /^[0-9a-f]{40}$/);
  assert.equal(report.contractsRevision?.head, process.env.RELEASE_SHA, path);
  assert.equal(report.status, "passed", path);
  assert.equal(report.requireAll, true, path);
  assert.equal(report.dryRun, false, path);
  assert.equal(report.skipped, 0, path);
  assert.equal(report.failed, 0, path);
  assert(report.checked > 0 && report.checked === report.results.length, path);
  assert(report.results.every((item) => item.status === "passed"), path);
  return report;
}
