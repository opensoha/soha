import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, existsSync, rmSync, copyFileSync, chmodSync, symlinkSync } from "node:fs";
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
function run(root, args, extra = {}, checker = script) {
  return spawnSync(process.execPath, [checker, "--require-all", "--out", join(root, "result.json"), ...args], {
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
  assert.equal(report.status, "dry-run");
  assert.equal(report.results[0].commands.length, 3);
  assert.ok(report.results[0].commands[0][1].includes("<contracts-tarball>"));
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

function fakeNpm(root, failAt) {
  const preload = join(root, "mock-npm-preload.mjs");
  const trace = join(root, "npm-trace.jsonl");
  writeFileSync(preload, `
import childProcess from "node:child_process";
import { syncBuiltinESMExports } from "node:module";
import { appendFileSync, writeFileSync, readFileSync, existsSync } from "node:fs";
import { join } from "node:path";
const original = childProcess.spawnSync;
childProcess.spawnSync = (command, args, options) => {
  if (command !== "npm") return original(command, args, options);
  appendFileSync(${JSON.stringify(trace)}, JSON.stringify(args)+"\\n");
  if (args[0] === "pack") {
    const version = JSON.parse(readFileSync(join(options.cwd, "package.json"))).version;
    writeFileSync(join(args.at(-1), "opensoha-contracts-" + version + ".tgz"), "unit tarball");
  } else if (args[0] === "install") {
    if (!existsSync(args.at(-1))) throw Error("candidate tarball missing");
    writeFileSync(join(options.cwd, "package.json"), "temporary candidate manifest");
    writeFileSync(join(options.cwd, "package-lock.json"), "temporary candidate lock");
  } else if (readFileSync(join(options.cwd, "package-lock.json"), "utf8") !== "temporary candidate lock") {
    throw Error("ci/build reached declared lock instead of candidate");
  }
  return { status: args[0] === ${JSON.stringify(failAt)} ? 17 : 0 };
};
syncBuiltinESMExports();
`);
  return { NODE_OPTIONS: "--import=" + pathToFileURL(preload).href };
}
for (const failAt of [null, "ci", "install"]) {
  test(`candidate npm restores exact manifests after ${failAt ?? "success"} and records failures`, (t) => {
    const root = fixture(t);
    for (const name of ["package.json", "package-lock.json"]) writeFileSync(join(root, "soha-web", name), "original " + name + "\r\n");
    const result = run(root, ["--consumer", "soha-web"], fakeNpm(root, failAt));
    assert.equal(result.status, failAt ? 1 : 0, result.stderr);
    for (const name of ["package.json", "package-lock.json"]) assert.equal(readFileSync(join(root, "soha-web", name), "utf8"), "original " + name + "\r\n");
    const report = JSON.parse(readFileSync(join(root, "result.json")));
    assert.equal(report.failed, failAt ? 1 : 0);
    assert.equal(report.results[0].status, failAt ? "failed" : "passed");
    const calls = readFileSync(join(root, "npm-trace.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    assert.deepEqual(calls.map((args) => args[0]), failAt === "install" ? ["pack", "install"] : failAt === "ci" ? ["pack", "install", "ci"] : ["pack", "install", "ci", "run"]);
    assert.equal(existsSync(calls[0].at(-1)), false, "candidate package directory must be cleaned");
  });
}
test("candidate identity includes staged, deleted and untracked files from the App repository root", (t) => {
  const root = fixture(t); const app = join(root, "soha-app");
  const git = (...args) => {
    const result = spawnSync("git", ["-C", app, ...args], { encoding: "utf8" });
    assert.equal(result.status, 0, result.stderr);
    return result.stdout.trim();
  };
  git("init");
  writeFileSync(join(app, "frontend", "source.txt"), "base");
  writeFileSync(join(app, "deleted.txt"), "delete this");
  git("add", ".");
  git("-c", "user.name=Unit", "-c", "user.email=unit@example.test", "commit", "-m", "unit baseline");
  writeFileSync(join(app, "frontend", "source.txt"), "staged change");
  git("add", ".");
  writeFileSync(join(app, "untracked.txt"), "untracked change");
  rmSync(join(app, "deleted.txt"));
  const result = run(root, ["--consumer", "soha-app", "--dry-run"]);
  assert.equal(result.status, 0, result.stderr);
  const report = JSON.parse(readFileSync(join(root, "result.json")));
  const identity = report.results[0].revision;
  assert.equal(identity.head, git("rev-parse", "HEAD"));
  assert.deepEqual(identity.files.map((file) => file.path), ["deleted.txt", "frontend/source.txt", "untracked.txt"]);
  assert.equal(identity.files[0].sha256, null);
  assert.equal(identity.files[0].type, "deleted");
  assert.match(identity.sourceHash, /^[0-9a-f]{64}$/);
});
test("missing required consumers produce a failing report and preserve the full matrix", (t) => {
  const root = fixture(t);
  const result = run(root, ["--consumer", "soha-agent", "--consumer", "soha-skills", "--dry-run"]);
  assert.equal(result.status, 1);
  const report = JSON.parse(readFileSync(join(root, "result.json")));
  assert.equal(report.failed, 2);
  assert.equal(report.results.length, 2);
});

function candidateFixture(t) {
  const root = fixture(t);
  const contracts = join(root, "soha-contracts");
  mkdirSync(join(contracts, "scripts"), { recursive: true });
  const checker = join(contracts, "scripts/check-consumers.mjs");
  copyFileSync(script, checker);
  writeFileSync(join(contracts, "package.json"), '{"version":"0.1.0"}');
  const git = (...args) => {
    const result = spawnSync("git", ["-C", contracts, ...args], { encoding: "utf8" });
    assert.equal(result.status, 0, result.stderr);
    return result.stdout.trim();
  };
  git("init"); git("add", ".");
  git("-c", "user.name=Unit", "-c", "user.email=unit@example.test", "commit", "-m", "candidate baseline");
  mkdirSync(join(root, "soha-skills/tools"), { recursive: true });
  writeFileSync(join(root, "soha-skills/tools/validate_assets.py"), 'from pathlib import Path\nPath("ran").write_text("fixture executed")\n');
  return { root, contracts, checker, head: git("rev-parse", "HEAD") };
}

test("full candidate SHA binds a clean checkout before executing a fixture consumer", (t) => {
  const { root, checker, head } = candidateFixture(t);
  const result = run(root, ["--consumer", "soha-skills", "--contracts-sha", head], {}, checker);
  assert.equal(result.status, 0, result.stderr);
  const report = JSON.parse(readFileSync(join(root, "result.json")));
  assert.equal(report.status, "passed");
  assert.equal(report.contractsRevision.head, head);
  assert.equal(report.expectedContracts.head, head);
  assert.equal(existsSync(join(root, "soha-skills/ran")), true);
});

for (const identity of ["wrong-sha", "short-sha", "dirty-sha", "wrong-hash", "hash-without-sha"]) {
  test(`candidate ${identity} fails before consumer execution and records actual/expected input`, (t) => {
    const { root, contracts, checker, head } = candidateFixture(t);
    if (identity === "dirty-sha") writeFileSync(join(contracts, "untracked.txt"), "candidate change");
    const args = ["--consumer", "soha-skills"];
    if (identity !== "hash-without-sha") args.push("--contracts-sha", identity === "wrong-sha" ? "0".repeat(40) : identity === "short-sha" ? head.slice(0, 7) : head);
    if (identity.includes("hash")) args.push("--contracts-source-hash", "0".repeat(64));
    const result = run(root, args, {}, checker);
    assert.equal(result.status, 1);
    assert.equal(existsSync(join(root, "soha-skills/ran")), false);
    const report = JSON.parse(readFileSync(join(root, "result.json")));
    assert.equal(report.status, "failed");
    assert.equal(report.contractsRevision.head, head);
    assert.ok(report.identityError);
    assert.equal(report.results.length, 0);
  });
}

test("candidate digest binds dirty content and rejects a later source change", (t) => {
  const { root, contracts, checker, head } = candidateFixture(t);
  writeFileSync(join(contracts, "untracked.txt"), "candidate change");
  assert.equal(run(root, ["--consumer", "soha-skills", "--dry-run"], {}, checker).status, 0);
  const { sourceHash } = JSON.parse(readFileSync(join(root, "result.json"))).contractsRevision;
  const args = ["--consumer", "soha-skills", "--contracts-sha", head, "--contracts-source-hash", sourceHash];
  assert.equal(run(root, args, {}, checker).status, 0);
  rmSync(join(root, "soha-skills/ran"));
  writeFileSync(join(contracts, "untracked.txt"), "changed after binding");
  const result = run(root, args, {}, checker);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /source hash mismatch/);
  assert.equal(existsSync(join(root, "soha-skills/ran")), false);
});

for (const flag of ["--contracts-sha", "--contracts-source-hash"]) {
  for (const missing of [[flag], [flag, "--dry-run"], [flag + "="]]) {
    test(`explicit missing identity value cannot disable binding: ${missing.join(" ")}`, (t) => {
      const { root, checker } = candidateFixture(t);
      const result = run(root, ["--consumer", "soha-skills", ...missing], {}, checker);
      assert.notEqual(result.status, 0);
      assert.equal(existsSync(join(root, "soha-skills/ran")), false);
    });
  }
}

test("candidate digest includes executable mode even when the repository ignores mode changes", (t) => {
  const { root, contracts, checker, head } = candidateFixture(t);
  const source = join(contracts, "candidate.txt");
  writeFileSync(source, "unchanged bytes"); chmodSync(source, 0o644);
  assert.equal(run(root, ["--consumer", "soha-skills", "--dry-run"], {}, checker).status, 0);
  const before = JSON.parse(readFileSync(join(root, "result.json"))).contractsRevision;
  assert.equal(before.files[0].mode, "100644");
  chmodSync(source, 0o755);
  const result = run(root, ["--consumer", "soha-skills", "--contracts-sha", head, "--contracts-source-hash", before.sourceHash], {}, checker);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /source hash mismatch/);
  const after = JSON.parse(readFileSync(join(root, "result.json"))).contractsRevision;
  assert.equal(after.files[0].mode, "100755");
  assert.equal(after.files[0].sha256, before.files[0].sha256);
  assert.equal(existsSync(join(root, "soha-skills/ran")), false);
  const git = (...args) => assert.equal(spawnSync("git", ["-C", contracts, ...args]).status, 0);
  git("add", "."); git("-c", "user.name=Unit", "-c", "user.email=unit@example.test", "commit", "-m", "track executable source");
  git("config", "core.filemode", "false");
  chmodSync(source, 0o644);
  assert.equal(run(root, ["--consumer", "soha-skills", "--dry-run"], {}, checker).status, 0);
  assert.equal(JSON.parse(readFileSync(join(root, "result.json"))).contractsRevision.files[0].mode, "100644");
});

test("candidate identity hashes symlink text without reading its target and distinguishes file type", (t) => {
  const { root, contracts, checker, head } = candidateFixture(t);
  const link = join(contracts, "candidate.txt");
  writeFileSync(join(root, "outside.txt"), "outside bytes");
  symlinkSync("../outside.txt", link);
  const discover = () => {
    const result = run(root, ["--consumer", "soha-skills", "--dry-run"], {}, checker);
    assert.equal(result.status, 0, result.stderr);
    return JSON.parse(readFileSync(join(root, "result.json"))).contractsRevision;
  };
  const before = discover();
  assert.equal(before.files[0].type, "symlink");
  assert.equal(before.files[0].mode, "120000");
  rmSync(join(root, "outside.txt"));
  assert.equal(discover().sourceHash, before.sourceHash, "a dangling target must not be read or treated as deletion");
  rmSync(link); symlinkSync("../other-target.txt", link);
  assert.notEqual(discover().sourceHash, before.sourceHash);
  rmSync(link); writeFileSync(link, "../outside.txt"); chmodSync(link, 0o644);
  const regular = discover();
  assert.equal(regular.files[0].sha256, before.files[0].sha256, "same bytes still require a different entry type");
  assert.equal(regular.files[0].type, "file");
  assert.notEqual(regular.sourceHash, before.sourceHash);
  const result = run(root, ["--consumer", "soha-skills", "--contracts-sha", head, "--contracts-source-hash", before.sourceHash], {}, checker);
  assert.equal(result.status, 1);
  assert.equal(existsSync(join(root, "soha-skills/ran")), false);
});

test("a failed consumer does not suppress the following consumer or its report", (t) => {
  const { root, checker } = candidateFixture(t);
  mkdirSync(join(root, "soha-agent"));
  const env = fakeGo(root, { Version: "v0.1.0", Replace: { Dir: "/unit-only" } });
  const result = run(root, ["--declared", "--consumer", "soha-agent", "--consumer", "soha-skills"], env, checker);
  assert.equal(result.status, 1);
  const report = JSON.parse(readFileSync(join(root, "result.json")));
  assert.equal(report.failed, 1);
  assert.deepEqual(report.results.map(({ status }) => status), ["failed", "passed"]);
  assert.equal(existsSync(join(root, "soha-skills/ran")), true);
});
