import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, existsSync, rmSync, copyFileSync, chmodSync, symlinkSync, realpathSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

const script = resolve(dirname(fileURLToPath(import.meta.url)), "check-consumers.mjs");
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "consumer-mode-unit-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const name of ["soha", "soha-web", "soha-app/frontend"]) mkdirSync(join(root, name), { recursive: true });
  writeFileSync(join(root, "soha", "keep.txt"), "owned fixture\n");
  writeFileSync(join(root, "soha", "go.mod"), "module fixture\nrequire github.com/opensoha/soha-contracts v0.1.0\n");
  return root;
}
function run(root, args, extra = {}, checker = script) {
  return spawnSync(process.execPath, [checker, "--require-all", "--out", join(root, "result.json"), ...args], {
    encoding: "utf8", env: { ...process.env, SOHA_CONSUMER_ROOT: root, ...extra },
  });
}
function nativeArtifactRoot() {
  const root = realpathSync(process.env.RUNNER_TEMP || tmpdir());
  const automaticRoot = process.env.OPENSOHA_VERIFY_DIR && realpathSync(process.env.OPENSOHA_VERIFY_DIR);
  return automaticRoot && (root === automaticRoot || root.startsWith(automaticRoot + sep)) ? dirname(automaticRoot) : root;
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
  assert.equal(JSON.parse(readFileSync(join(root, "result.json"))).results[0].nativeQualification.status, "not-applicable");
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


test("candidate Skills binds every producer schema while declared preserves its own selection", (t) => {
  const root = fixture(t);
  mkdirSync(join(root, "soha-skills"));
  for (const declared of [false, true]) {
    const result = run(root, ["--consumer", "soha-skills", "--dry-run", ...(declared ? ["--declared"] : [])]);
    assert.equal(result.status, 0, result.stderr);
    const command = JSON.parse(readFileSync(join(root, "result.json"))).results[0].commands[0];
    assert.equal(command[0], "python3");
    if (declared) assert.deepEqual(command[1], ["tools/validate_assets.py"]);
    else {
      const producer = dirname(dirname(script));
      assert.deepEqual(command[1], ["tools/validate_assets.py",
        "--contracts-skill-schema", join(producer, "skills/skill-manifest.schema.json"),
        "--contracts-mcp-preset-schema", join(producer, "presets/mcp-preset.schema.json"),
        "--contracts-agent-profile-schema", join(producer, "profiles/agent-profile.schema.json"),
        "--contracts-permission-catalog", join(producer, "auth/permission-catalog.json")]);
    }
  }
});

// Real, network-disabled Go/npm processes in an owned tiny source workspace.
// Copy the canonical checker/helper, never replace their checks with a test implementation.
function internalFixture(t) {
  const root = mkdtempSync(join(tmpdir(), "internal-source-unit-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const write = (path, body) => {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    writeFileSync(join(root, path), body);
  };
  const writeJSON = (path, value) => write(path, JSON.stringify(value) + "\n");
  write(".gitignore", "node_modules/\nresult.json\nskills/ran\n");
  for (const [path, name] of [["", "soha"], ["cli/", "soha/cli"], ["agent/", "soha/agent"]]) {
    write(path + "go.mod", "module github.com/opensoha/" + name + "\n\ngo 1.27.4\n\nrequire github.com/opensoha/soha/contracts v0.1.29\n");
    write(path + "go.sum", "");
    write(path + "probe_test.go", 'package source\nimport("testing"; "github.com/opensoha/soha/contracts/b2unpublished")\nfunc TestLocalSDK(t *testing.T) { if b2unpublished.SourceOnly != "B2_SOURCE_ONLY_FIXTURE" { t.Fatal("wrong SDK") } }\n');
  }
  write("go.work", "go 1.27.4\n\nuse (\n .\n ./contracts\n ./cli\n ./agent\n)\nreplace github.com/opensoha/soha/contracts v0.1.29 => ./contracts\n");
  write("contracts/go.mod", "module github.com/opensoha/soha/contracts\n\ngo 1.27.4\n");
  write("contracts/go.sum", "");
  write("contracts/b2unpublished/probe.go", 'package b2unpublished\nconst SourceOnly = "B2_SOURCE_ONLY_FIXTURE"\n');
  const workspacePaths = ["contracts", "web", "connectors", "cli/npm/cli"];
  writeJSON("package.json", { private: true, workspaces: workspacePaths });
  writeJSON("contracts/package.json", { name: "@opensoha/contracts", version: "0.1.29", type: "module", exports: {
    "./gen/ts/sohaapi": "./gen/ts/sohaapi/index.ts", "./b2unpublished": "./b2unpublished/index.js"
  } });
  write("contracts/gen/ts/sohaapi/index.ts", 'export const sourceOnly = "B2_SOURCE_ONLY_FIXTURE"\n');
  write("contracts/b2unpublished/index.js", 'export const sourceOnly = "B2_SOURCE_ONLY_FIXTURE"\n');
  writeJSON("web/package.json", { name: "soha-web", version: "0.0.0", type: "module", dependencies: { "@opensoha/contracts": "file:../contracts" }, scripts: { build: "node build.mjs" } });
  write("web/build.mjs", 'import assert from "node:assert/strict"; import { sourceOnly } from "@opensoha/contracts/b2unpublished"; assert.equal(sourceOnly, "B2_SOURCE_ONLY_FIXTURE");\n');
  writeJSON("connectors/package.json", { name: "@opensoha/connectors", version: "0.1.0" });
  writeJSON("cli/npm/cli/package.json", { name: "@opensoha/cli", version: "0.1.8" });
  const packages = { "": { workspaces: workspacePaths } };
  for (const part of workspacePaths) {
    const manifest = JSON.parse(readFileSync(join(root, part, "package.json")));
    packages[part] = manifest;
    packages["node_modules/" + manifest.name] = { resolved: part, link: true };
    const installed = join(root, "node_modules", manifest.name);
    mkdirSync(dirname(installed), { recursive: true });
    symlinkSync(join(root, part), installed, "dir");
  }
  // Link-only fixture: no registry package or invented checksum.
  writeJSON("package-lock.json", { lockfileVersion: 3, packages });
  const required = ["contracts/skills/skill-manifest.schema.json", "contracts/presets/mcp-preset.schema.json",
    "contracts/profiles/agent-profile.schema.json", "contracts/auth/permission-catalog.json",
    "internal/application/aigateway/catalog.go", "internal/domain/cluster/capabilities.go"];
  for (const path of required) write(path, path.endsWith(".go") ? "package fixture\n" : "{}\n");
  write("skills/tools/validate_assets.py", 'from pathlib import Path\nroot = Path(__file__).resolve().parents[2]\nrequired = ' + JSON.stringify(required) + '\nassert all((root / p).is_file() for p in required), "missing same-source schema/Core input"\nPath("ran").write_text("strict fixture validated")\n');
  mkdirSync(join(root, "contracts/scripts"));
  copyFileSync(script, join(root, "contracts/scripts/check-consumers.mjs"));
  mkdirSync(join(root, "scripts"));
  copyFileSync(resolve(dirname(script), "../../scripts/source-workspace.py"), join(root, "scripts/source-workspace.py"));
  copyFileSync(resolve(dirname(script), "../../scripts/check-module-boundaries.go"), join(root, "scripts/check-module-boundaries.go"));
  const git = (...args) => {
    const result = spawnSync("git", ["-C", root, ...args], { encoding: "utf8" });
    assert.equal(result.status, 0, result.stderr);
    return result.stdout.trim();
  };
  git("init"); git("add", ".");
  git("-c", "user.name=Unit", "-c", "user.email=unit@example.invalid", "commit", "-m", "source fixture");
  return { root, write, writeJSON, checker: join(root, "contracts/scripts/check-consumers.mjs"), head: git("rev-parse", "HEAD"), test: t };
}
function frozenInputs(root) {
  return Object.fromEntries(["package.json", "package-lock.json", "go.work", "go.mod", "go.sum", "contracts/package.json", "contracts/go.mod", "contracts/go.sum", "web/package.json", "cli/go.mod", "cli/go.sum", "agent/go.mod", "agent/go.sum", "connectors/package.json", "cli/npm/cli/package.json"].map((name) => [name,
    existsSync(join(root, name)) ? readFileSync(join(root, name), "utf8") : null]));
}
function internalRun(f, args = []) {
  return run(f.root, ["--internal-source", ...args], { GOPROXY: "off", GOSUMDB: "off", GOTOOLCHAIN: "local", GOMAXPROCS: "2", npm_config_offline: "true" }, f.checker);
}
test("internal source compiles unpublished Go and Web SDK imports offline without touching inputs", (t) => {
  const f = internalFixture(t); const before = frozenInputs(f.root);
  const result = internalRun(f, ["--contracts-sha", f.head]);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  const report = JSON.parse(readFileSync(join(f.root, "result.json")));
  assert.equal(report.dependencyMode, "internal-source");
  assert.equal(report.status, "passed");
  assert.deepEqual(report.results.map(({ consumer, status }) => [consumer, status]), [["soha", "passed"], ["soha-web", "passed"], ["soha-cli", "passed"], ["soha-agent", "passed"], ["soha-skills", "passed"]]);
  assert.equal(report.results[0].nativeQualification.status, "not-applicable");
  assert.equal(report.results[0].nativeQualification.reason, "fixed source has no native suite");
  assert.equal(report.contractsRevision.head, f.head);
  assert.match(report.workspaceInputs["package-lock.json"], /^[a-f0-9]{64}$/);
  assert.deepEqual(frozenInputs(f.root), before);
  assert.ok(existsSync(join(f.root, "contracts/scripts/check-consumers.mjs")), "cleanup must preserve source root");
});
for (const failing of ["soha", "soha-web", "soha-cli", "soha-agent", "soha-skills"]) test(`internal ${failing} failure preserves root/workspace/lock and does not hide the other result`, (t) => {
  const f = internalFixture(t);
  if (failing === "soha") f.write("probe_test.go", 'package source\nimport "testing"\nfunc TestFailure(t *testing.T) { t.Fatal("fixture failure") }\n');
  else if (failing === "soha-web") f.write("web/build.mjs", 'process.exit(17)\n');
  else if (failing === "soha-skills") f.write("skills/tools/validate_assets.py", 'raise SystemExit(17)\n');
  else f.write((failing === "soha-cli" ? "cli" : "agent") + "/probe_test.go", 'package source\nimport "testing"\nfunc TestFailure(t *testing.T) { t.Fatal("fixture failure") }\n');
  const before = frozenInputs(f.root); const result = internalRun(f);
  assert.notEqual(result.status, 0);
  const report = JSON.parse(readFileSync(join(f.root, "result.json")));
  assert.equal(report.results.find((r) => r.consumer === failing).status, "failed");
  assert.ok(report.results.filter((r) => r.consumer !== failing).every((r) => r.status === "passed"));
  assert.deepEqual(frozenInputs(f.root), before);
});
for (const invalid of ["missing-go", "wrong-module", "work-use", "work-replace", "missing-lock", "child-lock", "missing-link", "redirect-link", "redirect-web-resolution", "missing-web"]) {
  test(`internal source rejects ${invalid} and preserves actual inputs`, (t) => {
    const f = internalFixture(t);
    const web = ["missing-lock", "child-lock", "missing-link", "redirect-link", "redirect-web-resolution", "missing-web"].includes(invalid);
    if (invalid === "missing-go") rmSync(join(f.root, "contracts/go.mod"));
    if (invalid === "wrong-module") f.write("contracts/go.mod", "module invalid.example/wrong\n\ngo 1.27.4\n");
    if (invalid === "work-use") f.write("go.work", "go 1.27.4\nuse .\n");
    if (invalid === "work-replace") f.write("go.work", 'go 1.27.4\nuse (\n.\n./contracts\n)\nreplace github.com/opensoha/soha/contracts => ./contracts\n');
    if (invalid === "missing-lock") rmSync(join(f.root, "package-lock.json"));
    if (invalid === "child-lock") f.write("web/package-lock.json", "{}\n");
    if (invalid === "missing-link") rmSync(join(f.root, "node_modules/@opensoha/contracts"));
    if (["redirect-link", "redirect-web-resolution"].includes(invalid)) {
      f.writeJSON("other-sdk/package.json", { name: "@opensoha/contracts", version: "0.1.29", exports: { "./gen/ts/sohaapi": "./entry.js" } });
      f.write("other-sdk/entry.js", "module.exports = {}\n");
      if (invalid === "redirect-link") {
        rmSync(join(f.root, "node_modules/@opensoha/contracts"));
        symlinkSync(join(f.root, "other-sdk"), join(f.root, "node_modules/@opensoha/contracts"), "dir");
      } else {
        mkdirSync(join(f.root, "web/node_modules/@opensoha"), { recursive: true });
        symlinkSync(join(f.root, "other-sdk"), join(f.root, "web/node_modules/@opensoha/contracts"), "dir");
      }
    }
    if (invalid === "missing-web") rmSync(join(f.root, "web"), { recursive: true });
    const before = frozenInputs(f.root);
    const result = internalRun(f, ["--consumer", web ? "soha-web" : "soha"]);
    assert.notEqual(result.status, 0, result.stdout + result.stderr);
    assert.equal(JSON.parse(readFileSync(join(f.root, "result.json"))).status, "failed");
    assert.deepEqual(frozenInputs(f.root), before);
    assert.ok(existsSync(join(f.root, "scripts/source-workspace.py")));
  });
}
for (const [consumer, path] of [["soha-cli", "cli/go.mod"], ["soha-agent", "agent/go.mod"], ["soha-skills", "contracts/skills/skill-manifest.schema.json"], ["soha-skills", "internal/domain/cluster/capabilities.go"]]) {
  test(`internal ${consumer} rejects missing same-source ${path}`, (t) => {
    const f = internalFixture(t);
    rmSync(join(f.root, path));
    const before = frozenInputs(f.root);
    const result = internalRun(f, ["--consumer", consumer]);
    assert.notEqual(result.status, 0, result.stdout + result.stderr);
    assert.equal(JSON.parse(readFileSync(join(f.root, "result.json"))).status, "failed");
    assert.deepEqual(frozenInputs(f.root), before);
  });
}
test("internal Skills rejects a redirected component directory", (t) => {
  const f = internalFixture(t);
  const other = mkdtempSync(join(tmpdir(), "redirected-skills-unit-"));
  t.after(() => rmSync(other, { recursive: true, force: true }));
  rmSync(join(f.root, "skills"), { recursive: true });
  symlinkSync(other, join(f.root, "skills"), "dir");
  const result = internalRun(f, ["--consumer", "soha-skills"]);
  assert.notEqual(result.status, 0, result.stdout + result.stderr);
  assert.equal(JSON.parse(readFileSync(join(f.root, "result.json"))).status, "failed");
});
test("internal mode rejects declared, external selection, source root redirection and stale SHA", (t) => {
  const f = internalFixture(t); const before = frozenInputs(f.root);
  const listed = internalRun(f, ["--list"]);
  assert.equal(listed.status, 0);
  assert.equal(listed.stdout.trim(), "soha\nsoha-web\nsoha-cli\nsoha-agent\nsoha-skills");
  assert.notEqual(internalRun(f, ["--declared"]).status, 0);
  assert.notEqual(internalRun(f, ["--consumer", "soha-app"]).status, 0);
  const redirected = run(f.root, ["--internal-source"], { SOHA_CONSUMER_ROOT: join(f.root, "web") }, f.checker);
  assert.notEqual(redirected.status, 0);
  assert.notEqual(internalRun(f, ["--contracts-sha", "f".repeat(40)]).status, 0);
  assert.deepEqual(frozenInputs(f.root), before);
});

test("external declared and candidate never install inside the producer npm workspace", (t) => {
  const f = internalFixture(t);
  f.writeJSON("soha-web/package.json", { name: "soha-web", scripts: { build: "exit 99" } });
  const before = frozenInputs(f.root);
  for (const mode of [[], ["--declared"]]) {
    const result = run(f.root, ["--consumer", "soha-web", ...mode], {}, f.checker);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /outside the source npm workspace/);
    assert.deepEqual(frozenInputs(f.root), before);
  }
});

function nativeSuite(f, { partial = false, helper = true, core = f.root } = {}) {
  const write = (name, body) => {
    const path = join(core, name);
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, body);
  };
  for (const name of partial ? ["real_engine_integration_test.go"] : ["real_engine_integration_test.go", "real_udp_integration_test.go"]) {
    write(`internal/networkproxyruntime/${name}`, "//go:build soha_native_integration\n\npackage networkproxyruntime\n");
  }
  if (helper) write("scripts/run-proxy-native-tests.py", "raise RuntimeError('fixture native helper must only run through the explicit subprocess mock')\n");
}

// Fixture-only subprocess boundary: canonical checker/workspace checks and ordinary
// Go/npm fixture commands remain real. This never executes or proves native kernels.
function fakeNative(f, variant = "pass", extra = {}) {
  const preload = join(f.root, "fixture-native-preload.mjs");
  const nativeRoot = mkdtempSync(join(tmpdir(), "native-caller-artifacts-unit-"));
  const untouched = join(nativeRoot, "unrelated.txt");
  writeFileSync(untouched, "preserve unrelated output");
  f.test.after(() => rmSync(nativeRoot, { recursive: true, force: true }));
  writeFileSync(preload, `
import childProcess from "node:child_process";
import { syncBuiltinESMExports } from "node:module";
import { existsSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
const original = childProcess.spawnSync;
childProcess.spawnSync = (command, args, options) => {
  if (command !== "python3" || !args[0].endsWith("/scripts/run-proxy-native-tests.py")) return original(command, args, options);
  const receipt = args[args.indexOf("--out") + 1];
  if (existsSync(receipt)) throw Error("native receipt was reused");
  const mode = args[args.indexOf("--sdk-mode") + 1];
  const profile = (name, count, scopes) => {
    const log = join(dirname(receipt), "receipt." + name + ".go.json");
    const stderr = join(dirname(receipt), "receipt." + name + ".stderr.log");
    writeFileSync(log, "explicit fixture-only mock, no native runtime proof\\n");
    writeFileSync(stderr, "");
    return { log, stderr, exitCode: 0, attachExitCode: 0, evidence: { result: "PASS", terminalTests: count,
      scopes: scopes.map(([parent, children]) => ({ result: "PASS", parent, children })) } };
  };
  const head = original("git", ["-C", options.cwd, "rev-parse", "HEAD"], { encoding: "utf8" }).stdout.trim();
  const value = { schemaVersion: "proxy-native-gate/v1", result: "PASS", runId: "a".repeat(32), sdk: { mode },
    head, sourceArchiveSHA256: "b".repeat(64), workingTreeSHA256: "c".repeat(64), terminalTests: 26,
    profiles: { dataplane: profile("dataplane", 23, [["TestRealEngineIntegration", 22], ["TestRealUDPIntegration", 6]]),
      parser: profile("parser", 3, [["TestRealKernelPreflightIntegration", 3]]) }, fixtureInvocation: args };
  value.resources = [{ kind: "container", name: "fixture-owned-container", id: "d".repeat(64), image: "sha256:" + "e".repeat(64), network: "none" }];
  value.cleanup = value.resources.map((resource) => ({ ...resource, result: "PASS" }));
  const variant = ${JSON.stringify(variant)};
  if (variant === "missing-receipt") return { status: 0 };
  if (variant === "failed-result") value.result = "FAIL";
  if (variant === "wrong-mode") value.sdk.mode = "declared";
  if (variant === "wrong-count") value.terminalTests = 25;
  if (variant === "wrong-head") value.head = "f".repeat(40);
  if (variant === "missing-parser") delete value.profiles.parser;
  if (variant === "missing-tcp-branch") value.profiles.dataplane.evidence.scopes[0].children = 21;
  if (variant === "wrong-profile-count") value.profiles.dataplane.evidence.terminalTests = 22;
  if (variant === "failed-profile") value.profiles.parser.exitCode = 1;
  if (variant === "foreign-log") value.profiles.parser.log = "/unrelated/old-go.json";
  if (variant === "cleanup-fail") value.cleanup[0].result = "FAIL";
  if (variant === "unaccounted-intent") value.cleanup = [];
  if (variant === "exception-with-intent") {
    value.result = "FAIL";
    value.resources[0].id = null;
    value.cleanup = [];
  }
  writeFileSync(receipt, variant === "malformed-receipt" ? "invalid JSON" : JSON.stringify(value));
  return { status: ["exit-fail", "exception-with-intent"].includes(variant) ? 17 : 0 };
};
syncBuiltinESMExports();
`);
  // Preserve the canonical wrapper directory/token so real nested Go/npm checks share its lock.
  return { sentinelDirectory: nativeRoot, environment: { ...extra,
    NODE_OPTIONS: [extra.NODE_OPTIONS, "--import=" + pathToFileURL(preload).href].filter(Boolean).join(" ") } };
}

for (const invalid of ["partial-suite", "missing-helper"]) test(`native caller ${invalid} fails Core while Web still runs`, (t) => {
  const f = internalFixture(t);
  nativeSuite(f, { partial: invalid === "partial-suite", helper: invalid !== "missing-helper" });
  const result = internalRun(f);
  assert.equal(result.status, 1, result.stdout + result.stderr);
  const report = JSON.parse(readFileSync(join(f.root, "result.json")));
  assert.deepEqual(report.results.map(({ status }) => status), ["failed", "passed", "passed", "passed", "passed"]);
  assert.equal(report.results[0].nativeQualification.status, "failed");
  assert.match(report.results[0].reason, invalid === "partial-suite" ? /partial/ : /owning.*helper/);
});

for (const remote of [false, true]) test(`native caller mock records mandatory source command and explicit remote=${remote}`, (t) => {
  const f = internalFixture(t);
  nativeSuite(f);
  const mock = fakeNative(f, "pass", { OPENSOHA_KEEP_VERIFY_ARTIFACTS: "1" });
  const env = mock.environment;
  const result = run(f.root, ["--internal-source", ...(remote ? ["--allow-remote-native-docker"] : [])],
    { ...env, GOPROXY: "off", GOSUMDB: "off", GOTOOLCHAIN: "local", npm_config_offline: "true" }, f.checker);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  const entry = JSON.parse(readFileSync(join(f.root, "result.json"))).results[0];
  assert.equal(entry.nativeQualification.status, "passed");
  assert.equal(entry.nativeQualification.receipt.sdk.mode, "source");
  assert.equal(entry.nativeQualification.receipt.terminalTests, 26);
  const command = entry.commands.at(-1);
  assert.equal(command[0], "python3");
  assert.deepEqual(command[1], [join(realpathSync(f.root), "scripts/run-proxy-native-tests.py"), "--sdk-mode", "source", "--out", entry.nativeQualification.receiptPath,
    ...(remote ? ["--allow-remote-docker"] : [])]);
  const base = nativeArtifactRoot();
  assert.ok(entry.nativeQualification.artifactDirectory.startsWith(base + "/opensoha-consumer-native-"));
  assert.equal(existsSync(entry.nativeQualification.receiptPath), true);
  assert.equal(existsSync(join(mock.sentinelDirectory, "unrelated.txt")), true);
  t.after(() => rmSync(entry.nativeQualification.artifactDirectory, { recursive: true, force: true }));
});

for (const variant of ["exit-fail", "missing-receipt", "malformed-receipt", "failed-result", "wrong-mode", "wrong-count", "wrong-head",
  "missing-parser", "missing-tcp-branch", "wrong-profile-count", "failed-profile", "foreign-log", "cleanup-fail", "unaccounted-intent", "exception-with-intent"]) {
  test(`native caller mock rejects ${variant}, retains exact recovery output and preserves Web result`, (t) => {
    const f = internalFixture(t);
    nativeSuite(f);
    const mock = fakeNative(f, variant);
    const env = mock.environment;
    const result = run(f.root, ["--internal-source"], { ...env, GOPROXY: "off", GOSUMDB: "off", GOTOOLCHAIN: "local", npm_config_offline: "true" }, f.checker);
    assert.equal(result.status, 1, result.stdout + result.stderr);
    const report = JSON.parse(readFileSync(join(f.root, "result.json")));
    assert.deepEqual(report.results.map(({ status }) => status), ["failed", "passed", "passed", "passed", "passed"]);
    const qualification = report.results[0].nativeQualification;
    t.after(() => rmSync(qualification.artifactDirectory, { recursive: true, force: true }));
    assert.equal(qualification.status, "failed");
    assert.equal(existsSync(qualification.artifactDirectory), true);
    assert.equal(qualification.artifactsRetained, true);
    assert.match(result.stderr, /retained qualification artifacts:/);
    assert.equal(existsSync(join(mock.sentinelDirectory, "unrelated.txt")), true);
    if (variant === "exit-fail") assert.match(qualification.invocationError, /status 17/);
    if (["cleanup-fail", "unaccounted-intent", "exception-with-intent", "missing-receipt", "malformed-receipt"].includes(variant)) {
      assert.equal(qualification.recoveryRequired, true);
    }
  });
}

test("native caller reports and preserves durable unknown intent even without a consumer report output", (t) => {
  const f = internalFixture(t);
  nativeSuite(f);
  const { environment } = fakeNative(f, "exception-with-intent");
  const result = spawnSync(process.execPath, [f.checker, "--internal-source", "--consumer", "soha"], {
    encoding: "utf8", env: { ...process.env, SOHA_CONSUMER_ROOT: f.root, ...environment,
      GOPROXY: "off", GOSUMDB: "off", GOTOOLCHAIN: "local" },
  });
  assert.equal(result.status, 1, result.stdout + result.stderr);
  const directory = result.stderr.match(/\[native\] retained qualification artifacts: (.+)/)?.[1];
  assert.ok(directory);
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const receipt = JSON.parse(readFileSync(join(directory, "receipt.json")));
  assert.equal(receipt.resources[0].id, null);
  assert.equal(receipt.resources[0].name, "fixture-owned-container");
  assert.deepEqual(receipt.cleanup, []);
});

test("native dry-run prints the planned command without creating native output or running the helper", (t) => {
  const f = internalFixture(t);
  nativeSuite(f);
  const mock = fakeNative(f, "exit-fail");
  const env = mock.environment;
  const before = readdirSync(nativeArtifactRoot()).filter((name) => name.startsWith("opensoha-consumer-native-"));
  const result = run(f.root, ["--internal-source", "--dry-run", "--allow-remote-native-docker"], env, f.checker);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stdout, /--sdk-mode source --out <native-output-dir>\/receipt.json --allow-remote-docker/);
  const entry = JSON.parse(readFileSync(join(f.root, "result.json"))).results[0];
  assert.equal(entry.nativeQualification.status, "dry-run");
  assert.equal(entry.nativeQualification.receiptPath, undefined);
  assert.deepEqual(readdirSync(nativeArtifactRoot()).filter((name) => name.startsWith("opensoha-consumer-native-")), before);
});

test("external candidate Core with native source fails clearly instead of substituting its embedded SDK", (t) => {
  const root = fixture(t);
  nativeSuite({ root }, { core: join(root, "soha") });
  const result = run(root, ["--consumer", "soha", "--dry-run"]);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /external candidate Core native qualification is unsupported/);
});

test("external declared Core native caller mock uses only its own helper and declared SDK mode", (t) => {
  const root = fixture(t);
  const core = join(root, "soha");
  nativeSuite({ root }, { core });
  for (const args of [["init"], ["add", "."], ["-c", "user.name=Unit", "-c", "user.email=unit@example.invalid", "commit", "-m", "native fixture"]]) {
    const result = spawnSync("git", ["-C", core, ...args], { encoding: "utf8" });
    assert.equal(result.status, 0, result.stderr);
  }
  const { environment: env } = fakeNative({ root, test: t }, "pass", fakeGo(root, { Version: "v0.1.0", Sum: "h1:unit-only" }));
  const result = run(root, ["--consumer", "soha", "--declared"], env);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  const entry = JSON.parse(readFileSync(join(root, "result.json"))).results[0];
  assert.equal(entry.nativeQualification.receipt.sdk.mode, "declared");
  assert.equal(entry.nativeQualification.artifactsRetained, false);
  assert.equal(existsSync(entry.nativeQualification.artifactDirectory), false);
  assert.equal(entry.commands.at(-1)[1][0], join(core, "scripts/run-proxy-native-tests.py"));
  assert.equal(entry.commands.at(-1)[1].includes("source"), false);
});

for (const rootMode of ["runner-outside", "runner-equal", "runner-nested", "tmp-nested"]) {
  test(`native unknown recovery survives actual outer TemporaryDirectory exit with ${rootMode}`, (t) => {
    const root = fixture(t);
    const core = join(root, "soha");
    nativeSuite({ root }, { core });
    for (const args of [["init"], ["add", "."], ["-c", "user.name=Unit", "-c", "user.email=unit@example.invalid", "commit", "-m", "native fixture"]]) {
      const result = spawnSync("git", ["-C", core, ...args], { encoding: "utf8" });
      assert.equal(result.status, 0, result.stderr);
    }
    const canonical = { directory: process.env.OPENSOHA_VERIFY_DIR, active: process.env.OPENSOHA_VERIFY_ACTIVE };
    const mock = fakeNative({ root, test: t }, "exception-with-intent", fakeGo(root, { Version: "v0.1.0", Sum: "h1:unit-only" }));
    // This one non-nested child simulates a different external automatic wrapper.
    // Every Go/native command is stubbed; ordinary fixtures retain the real lease.
    const wrapper = spawnSync("python3", ["-c", `
import json, os, pathlib, subprocess, sys, tempfile
root, node, checker, mode = sys.argv[1:]
env = dict(os.environ)
env.pop("OPENSOHA_VERIFY_ACTIVE", None)
with tempfile.TemporaryDirectory(prefix="automatic-native-unit-", dir=root) as automatic:
    nested = pathlib.Path(automatic) / "runner" / "nested"
    nested.mkdir(parents=True)
    env["OPENSOHA_VERIFY_DIR"] = automatic
    if mode == "tmp-nested":
        env.pop("RUNNER_TEMP", None)
        env["TMPDIR"] = str(nested)
    else:
        env["RUNNER_TEMP"] = root if mode == "runner-outside" else automatic if mode == "runner-equal" else str(nested)
    child = subprocess.run([node, checker, "--declared", "--consumer", "soha", "--require-all", "--out", str(pathlib.Path(root) / "result.json")], env=env, text=True, capture_output=True)
automatic_removed = not pathlib.Path(automatic).exists()
print(json.dumps({"exitCode": child.returncode, "stdout": child.stdout, "stderr": child.stderr, "automaticRoot": automatic, "automaticRemoved": automatic_removed}))
`, root, process.execPath, script, rootMode], {
      encoding: "utf8", env: { ...process.env, ...mock.environment, SOHA_CONSUMER_ROOT: root },
    });
    assert.equal(wrapper.status, 0, wrapper.stderr);
    const outer = JSON.parse(wrapper.stdout);
    assert.equal(outer.exitCode, 1, outer.stdout + outer.stderr);
    assert.equal(outer.automaticRemoved, true);
    assert.equal(existsSync(outer.automaticRoot), false);
    const report = JSON.parse(readFileSync(join(root, "result.json")));
    const qualification = report.results[0].nativeQualification;
    t.after(() => rmSync(qualification.artifactDirectory, { recursive: true, force: true }));
    assert.ok(qualification.artifactDirectory.startsWith(realpathSync(root) + sep + "opensoha-consumer-native-"));
    assert.equal(qualification.artifactsRetained, true);
    assert.equal(qualification.recoveryRequired, true);
    const receipt = JSON.parse(readFileSync(qualification.receiptPath));
    assert.equal(receipt.resources[0].id, null);
    assert.deepEqual(receipt.cleanup, []);
    assert.equal(existsSync(receipt.profiles.dataplane.log), true);
    assert.equal(existsSync(receipt.profiles.parser.stderr), true);
    assert.equal(existsSync(join(mock.sentinelDirectory, "unrelated.txt")), true);
    assert.equal(process.env.OPENSOHA_VERIFY_DIR, canonical.directory);
    assert.equal(process.env.OPENSOHA_VERIFY_ACTIVE, canonical.active);
    const calls = readFileSync(join(root, "trace.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    assert.deepEqual(calls.map((args) => args[0]), ["list", "mod", "test"]);
  });
}

for (const modulePath of ["github.com/opensoha/soha-contracts", "github.com/opensoha/soha/contracts"]) {
  test(`declared mode resolves its actual ${modulePath} identity`, (t) => {
    const root = fixture(t);
    writeFileSync(join(root, "soha", "go.mod"), `module fixture\nrequire ${modulePath} v0.1.0\n`);
    const result = run(root, ["--declared", "--consumer", "soha"], fakeGo(root, { Version: "v0.1.0", Sum: "h1:unit-only" }));
    assert.equal(result.status, 0, result.stderr);
    const calls = readFileSync(join(root, "trace.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    assert.equal(calls[0].at(-1), modulePath);
    assert.equal(calls[1].at(-1), modulePath);
  });
}
test("candidate rejects old-module consumer before substituting the new SDK", (t) => {
  const root = fixture(t);
  const result = run(root, ["--consumer", "soha"], fakeGo(root, {}));
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /candidate SDK module identity differs/);
  assert.equal(existsSync(join(root, "trace.jsonl")), false);
});

test("declared module selection ignores exclude blocks", (t) => {
  const root = fixture(t);
  const modulePath = "github.com/opensoha/soha/contracts";
  writeFileSync(join(root, "soha", "go.mod"), `module fixture\nrequire (\n ${modulePath} v0.1.0\n)\nexclude (\n ${modulePath} v0.0.9\n)\n`);
  const result = run(root, ["--declared", "--consumer", "soha"], fakeGo(root, { Version: "v0.1.0", Sum: "h1:unit-only" }));
  assert.equal(result.status, 0, result.stderr);
});
