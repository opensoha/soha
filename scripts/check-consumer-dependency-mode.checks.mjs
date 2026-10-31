import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

const script = resolve(dirname(fileURLToPath(import.meta.url)), "check-consumers.mjs");
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "consumer-mode-unit-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const name of ["soha", "soha-web", "soha-app/frontend"]) mkdirSync(join(root, name), { recursive: true });
  writeFileSync(join(root, "soha", "keep.txt"), "owned fixture\n");
  return root;
}
function run(root, args, extra = {}) {
  return spawnSync(process.execPath, [script, "--require-all", "--out", join(root, "result.json"), ...args], {
    encoding: "utf8", env: { ...process.env, SOHA_CONSUMER_ROOT: root, ...extra },
  });
}
// This isolated child-process preload is a portable unit stub, never a real consumer gate.
function fakeGo(root, module, downloaded = { Version: "v0.1.0", Sum: "h1:unit-only" }) {
  const trace = join(root, "trace.jsonl");
  const preload = join(root, "mock-go-preload.mjs");
  writeFileSync(preload, `
import childProcess from "node:child_process";
import { syncBuiltinESMExports } from "node:module";
import { appendFileSync } from "node:fs";
const original = childProcess.spawnSync;
childProcess.spawnSync = (command, args, options) => {
  if (command !== "go") return original(command, args, options);
  if (options.env.GOWORK !== "off" || options.env.GOFLAGS !== "-mod=readonly") throw Error("source substitution enabled");
  appendFileSync(${JSON.stringify(trace)}, JSON.stringify(args)+"\\n");
  if(args[0] === "list") return { status: 0, stdout: ${JSON.stringify(JSON.stringify(module))} };
  if(args[0] === "mod") return { status: 0, stdout: ${JSON.stringify(JSON.stringify(downloaded))} };
  if(args[0] === "test") return { status: 0, stdout: "" };
  throw Error("unexpected Go invocation");
};
syncBuiltinESMExports();
`);
  return { NODE_OPTIONS: "--import=" + pathToFileURL(preload).href };
}

test("declared npm mode retains npm ci/build and omits the candidate tarball install", (t) => {
  const root = fixture(t);
  const result = run(root, ["--declared", "--dry-run", "--consumer", "soha-web", "--consumer", "soha-app"]);
  assert.equal(result.status, 0, result.stderr);
  const report = JSON.parse(readFileSync(join(root, "result.json")));
  assert.equal(report.dependencyMode, "declared");
  for (const entry of report.results) assert.deepEqual(entry.commands, [["npm", ["ci"]], ["npm", ["run", "build"]]]);
  assert.equal(existsSync(join(root, "soha/keep.txt")), true);
});
test("candidate source binding remains the explicit default with its SDK tarball", (t) => {
  const root = fixture(t); const result = run(root, ["--dry-run", "--consumer", "soha-web"]);
  assert.equal(result.status, 0, result.stderr);
  const report = JSON.parse(readFileSync(join(root, "result.json")));
  assert.equal(report.dependencyMode, "candidate-source");
  assert.equal(report.results[0].commands.length, 3);
  assert.ok(report.results[0].commands[1][1].includes("<contracts-tarball>"));
});
test("declared Go mode disables workspaces and preserves the consumer directory", (t) => {
  const root = fixture(t); const env = fakeGo(root, { Version: "v0.1.0", Sum: "h1:unit-only" });
  const result = run(root, ["--declared", "--consumer", "soha"], env);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(existsSync(join(root, "soha/keep.txt")), true);
  const calls = readFileSync(join(root, "trace.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
  assert.deepEqual(calls.map((args) => args[0]), ["list", "mod", "test"]);
});
test("declared Go mode rejects a replacement before any test command", (t) => {
  const root = fixture(t); const env = fakeGo(root, { Version: "v0.1.0", Replace: { Dir: "/unit-only" } });
  const result = run(root, ["--declared", "--consumer", "soha"], env);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /without replacement/);
  assert.equal(existsSync(join(root, "soha/keep.txt")), true);
  assert.equal(readFileSync(join(root, "trace.jsonl"), "utf8").trim().split("\n").length, 1);
});
test("declared Go mode rejects a checksum mismatch before the suite", (t) => {
  const root = fixture(t); const env = fakeGo(root, { Version: "v0.1.0", Sum: "h1:unit-only" }, { Version: "v0.1.0", Sum: "h1:other-unit-value" });
  const result = run(root, ["--declared", "--consumer", "soha"], env);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /checksum\/version mismatch/);
  assert.equal(existsSync(join(root, "soha/keep.txt")), true);
});
