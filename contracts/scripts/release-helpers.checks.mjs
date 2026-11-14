import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, existsSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

const sha = "a".repeat(40);
const root = fileURLToPath(new URL("..", import.meta.url));
const script = (name) => join(root, "scripts", name);
const body = Buffer.from("identical public candidate");
const integrity = `sha512-${createHash("sha512").update(body).digest("base64")}`;
function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "contracts-release-check-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const write = (path, contents) => {
    mkdirSync(dirname(join(directory, path)), { recursive: true });
    writeFileSync(join(directory, path), contents);
  };
  return { directory, write };
}

test("consumer report merger requires declared, candidate and internal evidence for the exact source", (t) => {
  const f = fixture(t);
  const cwd = join(f.directory, "contracts");
  f.write("contracts/package.json", '{"version":"0.2.0"}');
  f.write(".github/workflows/contracts-ci.yml", "jobs:\n  consumers:\n    strategy:\n      matrix:\n        consumer: [soha]\n");
  const report = { contractsVersion: "0.2.0", contractsRevision: { head: sha }, requireAll: true, dryRun: false, status: "passed", skipped: 0, failed: 0, checked: 1, results: [{ consumer: "soha", status: "passed" }] };
  for (const [path, mode] of [["consumer-soha-reports/work/source/declared.json", "declared"], ["consumer-soha-reports/candidate.json", "internal-source"], ["internal-source-report/internal-source-report.json", "internal-source"]]) f.write(`contracts/reports/${path}`, JSON.stringify({ ...report, dependencyMode: mode }));
  const run = () => spawnSync(process.execPath, [script("merge-release-reports.mjs"), "reports", "matrix.json"], { cwd, encoding: "utf8", env: { ...process.env, RELEASE_SHA: sha } });
  assert.equal(run().status, 0);
  assert.equal(JSON.parse(readFileSync(join(cwd, "matrix.json"))).checked, 3);
  for (const patch of [{ contractsRevision: { head: "b".repeat(40) } }, { status: "failed" }, { dryRun: true }, { skipped: 1 }, { checked: 0 }, { dependencyMode: "candidate-source" }]) {
    f.write("contracts/reports/consumer-soha-reports/work/source/declared.json", JSON.stringify({ ...report, dependencyMode: "declared", ...patch }));
    assert.notEqual(run().status, 0, JSON.stringify(patch));
  }
  f.write("contracts/reports/consumer-soha-reports/declared.json", JSON.stringify({ ...report, dependencyMode: "declared" }));
  assert.notEqual(run().status, 0, "duplicate reports cannot mask a failed source report");
  rmSync(join(cwd, "reports/consumer-soha-reports/work/source/declared.json"));
  rmSync(join(cwd, "reports/consumer-soha-reports/declared.json"));
  assert.notEqual(run().status, 0);
});

for (const mode of ["success", "bytes", "no-provenance", "workflow", "tag", "source", "go-failure", "go-replace", "audit-failure"]) {
  test(`public verification ${mode} uses downloaded bytes, signed source and normal Go sumDB`, (t) => {
    const f = fixture(t);
    const digest = createHash("sha256").update(body).digest("hex");
    f.write("dist/release/release-manifest.json", JSON.stringify({ version: "0.2.0", releaseSource: { revision: sha }, artifacts: [{ kind: "npm-tarball", npmIntegrity: integrity, sha256: digest }] }));
    f.write("fetch.mjs", `
const body = Buffer.from(${JSON.stringify(body.toString())});
globalThis.fetch = async (url) => ({ ok: true, json: async () => ({ name: '@opensoha/contracts', version: '0.2.0', dist: { integrity: ${JSON.stringify(integrity)}, tarball: 'https://registry.npmjs.org/candidate.tgz', attestations: process.env.CHECK_MODE === 'no-provenance' ? undefined : {url:'https://registry.npmjs.org/attestation'} } }), arrayBuffer: async () => process.env.CHECK_MODE === 'bytes' ? Buffer.from('different') : body });
`);
    const fake = `#!/usr/bin/env node
const {appendFileSync, writeFileSync} = require('node:fs');
const {join, basename} = require('node:path');
const args = process.argv.slice(2), mode = process.env.CHECK_MODE;
const command = basename(process.argv[1]);
appendFileSync(process.env.CALL_LOG, JSON.stringify({command,args,cwd:process.cwd(),GOWORK:process.env.GOWORK,GOPROXY:process.env.GOPROXY,GOSUMDB:process.env.GOSUMDB,GONOSUMDB:process.env.GONOSUMDB})+'\\n');
if(command==='npm') {
 if(args[0]==='install') process.exit(0);
 if(mode==='audit-failure') process.exit(1);
 const statement = {predicate:{buildDefinition:{externalParameters:{workflow:{repository:'https://github.com/opensoha/soha',path:mode==='workflow'?'.github/workflows/release.yml':'.github/workflows/contracts-release.yml',ref:mode==='tag'?'refs/tags/contracts/v0.2.1':'refs/tags/contracts/v0.2.0'}},resolvedDependencies:[{uri:'git+https://github.com/opensoha/soha@refs/tags/contracts/v0.2.0',digest:{gitCommit:mode==='source'?'b'.repeat(40):process.env.RELEASE_SHA}}]}}};
 console.log(JSON.stringify({invalid:[],missing:[],verified:[{name:'@opensoha/contracts',version:'0.2.0',attestationBundles:[{predicateType:'https://slsa.dev/provenance/v1',bundle:{dsseEnvelope:{payload:Buffer.from(JSON.stringify(statement)).toString('base64')}}}]}]}));
} else {
 if(mode==='go-failure') process.exit(1);
 const goMod=join(process.cwd(),'downloaded.mod');
 writeFileSync(goMod,'module github.com/opensoha/soha/contracts\\n'+(mode==='go-replace'?'replace example.org/x => ../x\\n':''));
 console.log(JSON.stringify({Path:'github.com/opensoha/soha/contracts',Version:'v0.2.0',Sum:'h1:module',GoModSum:'h1:gomod',GoMod:goMod,Origin:{Hash:process.env.RELEASE_SHA}}));
}
`;
    for (const name of ["npm", "go"]) { f.write(`bin/${name}`, fake); chmodSync(join(f.directory, "bin", name), 0o755); }
    const log = join(f.directory, "calls.jsonl");
    const result = spawnSync(process.execPath, ["--import", join(f.directory, "fetch.mjs"), script("verify-public-release.mjs")], { cwd: f.directory, encoding: "utf8", env: { ...process.env, PATH: `${join(f.directory, "bin")}:${process.env.PATH}`, RELEASE_SHA: sha, RELEASE_VERSION: "0.2.0", RELEASE_TAG: "contracts/v0.2.0", CHECK_MODE: mode, CALL_LOG: log, GONOSUMDB: "must-be-cleared" } });
    const receipt = join(f.directory, "dist/release/public-verification.json");
    if (mode === "success") {
      assert.equal(result.status, 0, result.stdout + result.stderr);
      assert.equal(JSON.parse(readFileSync(receipt)).sha, sha);
      assert.deepEqual(JSON.parse(readFileSync(receipt)).npm, { integrity, sha256: digest, provenance: "npm audit signatures passed" });
      const calls = readFileSync(log, "utf8").trim().split("\n").map(JSON.parse);
      assert(calls[0].args.includes("--ignore-scripts"));
      const go = calls.find((item) => item.command === "go");
      assert.equal(go.GOWORK, "off");
      assert.equal(go.GOPROXY, "https://proxy.golang.org");
      assert.equal(go.GOSUMDB, "sum.golang.org");
      assert.equal(go.GONOSUMDB, "");
    } else {
      assert.notEqual(result.status, 0, result.stdout + result.stderr);
      assert.equal(existsSync(receipt), false);
    }
    if (existsSync(log)) for (const call of readFileSync(log, "utf8").trim().split("\n").map(JSON.parse)) assert.equal(existsSync(call.cwd), false, "verification temp directories must be cleaned on success and failure");
  });
}
