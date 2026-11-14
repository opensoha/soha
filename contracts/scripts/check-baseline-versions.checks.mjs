import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { assertBaselineVersion } from "./baseline-version.mjs";

test("historical baseline versions use SemVer precedence without requiring release metadata equality", () => {
  for (const [before, after] of [["0.1.29", "0.2.0"], ["0.2.0", "0.2.0"], ["0.1.9", "0.1.10"], ["0.2.0-rc.9", "0.2.0-rc.10"], ["0.2.0-1", "0.2.0-alpha"], ["0.2.0-alpha", "0.2.0-alpha.1"], ["0.2.0-alpha", "0.2.0-beta"], ["0.2.0-rc.1", "0.2.0"], ["0.2.0+old", "0.2.0+new"]]) assertBaselineVersion(before, after);
  for (const [before, after] of [["0.2.0", "0.1.29"], ["0.1.10", "0.1.9"], ["0.2.0", "0.2.0-rc.1"], ["0.2.0-rc.10", "0.2.0-rc.9"], ["0.2.0-alpha", "0.2.0-1"], ["0.2.0-alpha.1", "0.2.0-alpha"], ["0.2.0-beta", "0.2.0-alpha"]]) assert.throws(() => assertBaselineVersion(before, after), /must not be newer/);
  for (const invalid of [undefined, 29, "", "0.1", "0.01.29", "0.1.29\n", "0.2.0-01", "0.2.0+", "0.2.0-rc..1"]) {
    assert.throws(() => assertBaselineVersion(invalid, "0.2.0"), /baseline version must be valid SemVer/);
    assert.throws(() => assertBaselineVersion("0.1.29", invalid), /current version must be valid SemVer/);
  }
});

function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "contracts-baseline-version-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const write = (path, value) => {
    mkdirSync(dirname(join(directory, path)), { recursive: true });
    writeFileSync(join(directory, path), JSON.stringify(value));
  };
  const run = (name) => spawnSync(process.execPath, [fileURLToPath(new URL(name, import.meta.url))], { cwd: directory, encoding: "utf8" });
  return { write, run };
}

test("loose boundaries retain historical metadata while rejecting malformed/future baselines and boundary drift", (t) => {
  const f = fixture(t);
  const document = { info: { version: "0.2.0" }, components: { schemas: { Payload: { $ref: "#/components/schemas/GenericObject" } } } };
  const baseline = { version: "0.1.29", entries: [{ kind: "schemaRef", path: "components/schemas/Payload", target: "#/components/schemas/GenericObject" }] };
  const run = () => f.run("check-openapi-loose-boundaries.mjs");
  f.write("openapi/soha-api.yaml", document);
  f.write("compat/openapi-loose-boundaries-0.1.0.json", baseline);
  assert.equal(run().status, 0);
  for (const version of ["invalid", "0.3.0"]) {
    f.write("compat/openapi-loose-boundaries-0.1.0.json", { ...baseline, version });
    assert.notEqual(run().status, 0);
  }
  f.write("compat/openapi-loose-boundaries-0.1.0.json", baseline);
  f.write("openapi/soha-api.yaml", { ...document, extra: { additionalProperties: true } });
  const added = run();
  assert.notEqual(added.status, 0);
  assert.match(added.stderr, /new loose OpenAPI boundary/);
  f.write("openapi/soha-api.yaml", { info: document.info });
  const removed = run();
  assert.notEqual(removed.status, 0);
  assert.match(removed.stderr, /loose OpenAPI boundary removed/);
});

test("JSON schema compatibility retains historical metadata and every existing shape guard", (t) => {
  const f = fixture(t);
  const baseline = { version: "0.1.29", schemas: { "schema.json": { rootProperties: ["name"], rootRequired: [], enums: [{ path: "properties/status", values: ["running"] }] } } };
  const run = () => f.run("check-json-schema-compat.mjs");
  f.write("package.json", { version: "0.2.0" });
  f.write("schema.json", { properties: { name: { type: "string" }, status: { enum: ["running"] } } });
  f.write("compat/json-schema-ecosystem-0.1.0.json", baseline);
  assert.equal(run().status, 0);
  for (const version of ["invalid", "0.3.0"]) {
    f.write("compat/json-schema-ecosystem-0.1.0.json", { ...baseline, version });
    assert.notEqual(run().status, 0);
  }
  f.write("compat/json-schema-ecosystem-0.1.0.json", baseline);
  f.write("schema.json", { properties: { status: { enum: ["stopped"] } }, required: ["name"] });
  const result = run();
  assert.notEqual(result.status, 0);
  for (const message of [/removed root property name/, /added root required property name/, /removed enum value running/]) assert.match(result.stderr, message);
});
