import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import test from "node:test";

const script = fileURLToPath(new URL("./check-version-sync.mjs", import.meta.url));
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "contracts-root-lock-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const sdk = join(root, "contracts");
  const write = (path, contents) => {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    writeFileSync(join(root, path), contents);
  };
  const packageJson = { name: "@opensoha/contracts", version: "0.2.0", repository: { type: "git", url: "https://github.com/opensoha/soha", directory: "contracts" } };
  write("contracts/package.json", JSON.stringify(packageJson));
  write("package-lock.json", JSON.stringify({ version: "9.9.9", packages: {
    "": { version: "9.9.9", workspaces: ["contracts", "web"] },
    contracts: { name: "@opensoha/contracts", version: "0.2.0" },
  } }));
  write("contracts/openapi/soha-api.yaml", 'info:\n  version: 0.2.0\n');
  write("contracts/openapi/soha-api.json", '{"info":{"version":"0.2.0"}}');
  write("contracts/go.mod", "module github.com/opensoha/soha/contracts\n");
  write("contracts/gen/go/sohaapi/client.go", 'const defaultUserAgent = "opensoha-contracts/0.2.0"\n');
  return { root, sdk, write, packageJson };
}
function check(f, tag = "", env = {}) {
  return spawnSync(process.execPath, [script], { cwd: f.sdk, encoding: "utf8", env: {
    ...process.env, GITHUB_REF_TYPE: tag ? "tag" : "branch", GITHUB_REF_NAME: tag, GITHUB_REF: tag ? `refs/tags/${tag}` : "", ...env,
  } });
}
test("version sync binds root lock's SDK entry, independently of root project version", (t) => {
  const f = fixture(t); const result = check(f);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.equal(check(f, "contracts/v0.2.0").status, 0);
  assert.notEqual(check(f, "contracts/v0.2.1").status, 0);
  for (const tag of ["v0.2.1", "cli/v0.2.1", "agent/v0.2.1"]) assert.equal(check(f, tag).status, 0);
  assert.equal(check(f, "", { GITHUB_REF: "refs/tags/contracts/v0.2.0" }).status, 0);
  assert.notEqual(check(f, "", { GITHUB_REF: "refs/tags/contracts/v0.2.1" }).status, 0);
});
test("namespace migration requires the 0.2.x source stream", (t) => {
  const f = fixture(t);
  f.write("contracts/package.json", JSON.stringify({ ...f.packageJson, version: "0.1.29" }));
  const result = check(f);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /version must stay on the 0\.2\.x stream/);
});
for (const invalid of ["missing-root", "missing-sdk", "wrong-name", "wrong-version", "wrong-sdk-name", "yaml", "json", "module", "client", "repository-url", "repository-directory"]) {
  test(`version sync rejects ${invalid} without accepting a stale child lock`, (t) => {
    const f = fixture(t);
    f.write("contracts/package-lock.json", '{"version":"0.2.0","packages":{"":{"name":"@opensoha/contracts","version":"0.2.0"}}}');
    if (invalid === "missing-root") rmSync(join(f.root, "package-lock.json"));
    if (invalid === "missing-sdk") f.write("package-lock.json", '{"packages":{}}');
    if (invalid === "wrong-name") f.write("package-lock.json", '{"packages":{"contracts":{"name":"other-sdk","version":"0.2.0"}}}');
    if (invalid === "wrong-version") f.write("package-lock.json", '{"packages":{"contracts":{"name":"@opensoha/contracts","version":"0.2.1"}}}');
    if (invalid === "wrong-sdk-name") {
      f.write("contracts/package.json", JSON.stringify({ ...f.packageJson, name: "other-sdk" }));
      f.write("package-lock.json", '{"packages":{"contracts":{"name":"other-sdk","version":"0.2.0"}}}');
    }
    if (invalid === "yaml") f.write("contracts/openapi/soha-api.yaml", 'info:\n  version: 0.2.1\n');
    if (invalid === "json") f.write("contracts/openapi/soha-api.json", '{"info":{"version":"0.2.1"}}');
    if (invalid === "module") f.write("contracts/go.mod", 'module invalid.example/contracts\n');
    if (invalid === "client") f.write("contracts/gen/go/sohaapi/client.go", 'const defaultUserAgent = "opensoha-contracts/0.2.1"\n');
    if (invalid === "repository-url") f.write("contracts/package.json", JSON.stringify({ ...f.packageJson, repository: { ...f.packageJson.repository, url: "https://github.com/opensoha/soha-contracts" } }));
    if (invalid === "repository-directory") f.write("contracts/package.json", JSON.stringify({ ...f.packageJson, repository: { ...f.packageJson.repository, directory: "" } }));
    assert.notEqual(check(f).status, 0);
  });
}
