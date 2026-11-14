import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { chmodSync, copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { normalizeSourcePath, sourceProvenance } from "./source-provenance.mjs";

function fixture(run) {
  const root = mkdtempSync(path.join(process.env.OPENSOHA_VERIFY_DIR || tmpdir(), "connector-source-"));
  const git = (...args) => execFileSync("git", ["-C", root, ...args], { stdio: "pipe" });
  try {
    git("init", "-q");
    git("config", "user.name", "fixture");
    git("config", "user.email", "fixture@example.invalid");
    writeFileSync(path.join(root, "go.mod"), "module github.com/opensoha/soha\n");
    mkdirSync(path.join(root, "connectors"));
    writeFileSync(path.join(root, "connectors/input"), "source");
    writeFileSync(path.join(root, "connectors/package.json"), JSON.stringify({ name: "@opensoha/connectors" }));
    git("add", "."); git("commit", "-qm", "fixture");
    run(root);
  } finally { rmSync(root, { recursive: true, force: true }); }
}

test("real layout binds source repository, commit and component without remotes", () => fixture(root => {
  const value = sourceProvenance(path.join(root, "connectors"));
  assert.equal(value.sourceRepository, "github.com/opensoha/soha");
  assert.equal(value.sourceDirectory, "connectors");
  assert.match(value.sourceCommit, /^[a-f0-9]{40}$/);
  assert.equal(value.sourceModified, false);
  rmSync(path.join(root, "go.mod"));
  writeFileSync(path.join(root, "package.json"), JSON.stringify({ name: "@opensoha/connectors" }));
  assert.equal(sourceProvenance(root).sourceRepository, "github.com/opensoha/soha-connectors");
}));

test("bytes, executable mode, deletion and symlink text change source identity", () => fixture(root => {
  const component = path.join(root, "connectors");
  const file = path.join(component, "input");
  const initial = sourceProvenance(component);
  chmodSync(file, 0o755);
  assert.notEqual(sourceProvenance(component).sourceSha256, initial.sourceSha256);
  chmodSync(file, 0o644); writeFileSync(file, "changed");
  assert.notEqual(sourceProvenance(component).sourceSha256, initial.sourceSha256);
  assert.equal(sourceProvenance(component).sourceModified, true);
  rmSync(file); const deleted = sourceProvenance(component);
  assert.notEqual(deleted.sourceSha256, initial.sourceSha256);
  symlinkSync("missing-target-a", file); const linked = sourceProvenance(component);
  rmSync(file); symlinkSync("missing-target-b", file);
  assert.notEqual(sourceProvenance(component).sourceSha256, linked.sourceSha256);
}));

test("nested unknown layout and missing parent module cannot claim a source repo", () => fixture(root => {
  writeFileSync(path.join(root, "go.mod"), "module example.invalid/foreign\n");
  assert.throws(() => sourceProvenance(path.join(root, "connectors")), /unrecognized/);
  rmSync(path.join(root, "go.mod"));
  assert.throws(() => sourceProvenance(path.join(root, "connectors")), /ENOENT/);
}));


test("exact output exclusions preserve source files sharing the output directory", () => fixture(root => {
  const component = path.join(root, "connectors");
  const out = path.join(component, "release-artifacts");
  mkdirSync(out);
  const files = ["artifact.tgz", "artifact.tgz.sha256", "artifact.release-manifest.json"].map(name => path.join(out, name));
  const initial = sourceProvenance(component, files);
  for (const file of files) writeFileSync(file, "output");
  assert.deepEqual(sourceProvenance(component, files), initial);
  writeFileSync(path.join(out, "real-source.txt"), "source");
  assert.notEqual(sourceProvenance(component, files).sourceSha256, initial.sourceSha256);
}));

test("custom unignored output prepare/verify succeeds and source or manifest tampering fails", () => fixture(root => {
  const component = path.join(root, "connectors");
  mkdirSync(path.join(component, "scripts"));
  mkdirSync(path.join(component, "connectors"));
  writeFileSync(path.join(component, "connectors/index.json"), JSON.stringify({ connectors: [] }));
  writeFileSync(path.join(component, "package.json"), JSON.stringify({ name: "@opensoha/connectors", version: "0.1.0", private: true, files: ["input", "connectors"] }));
  for (const name of ["source-provenance.mjs", "prepare-release-artifacts.mjs", "verify-release-artifacts.mjs"]) {
    copyFileSync(new URL(name, import.meta.url), path.join(component, "scripts", name));
  }
  const invoke = (script, args) => spawnSync(process.execPath, [path.join(component, "scripts", script), ...args], {
    cwd: component, encoding: "utf8", env: { ...process.env, npm_config_workspaces: "false" }
  });
  const prepared = invoke("prepare-release-artifacts.mjs", ["--out-dir", "release-artifacts"]);
  assert.equal(prepared.status, 0, prepared.stdout + prepared.stderr);
  const artifact = path.join(component, "release-artifacts/opensoha-connectors-0.1.0.tgz");
  const manifestFile = path.join(component, "release-artifacts/opensoha-connectors-0.1.0.release-manifest.json");
  const original = readFileSync(manifestFile);
  const verify = () => invoke("verify-release-artifacts.mjs", [artifact]);
  assert.equal(verify().status, 0);
  writeFileSync(path.join(component, "input"), "tampered source");
  const changedSource = verify();
  assert.notEqual(changedSource.status, 0);
  assert.match(changedSource.stderr, /source provenance mismatch: sourceSha256/);
  writeFileSync(path.join(component, "input"), "source");
  const manifest = JSON.parse(original);
  manifest.provenance.sourceCommit = "0".repeat(40);
  writeFileSync(manifestFile, JSON.stringify(manifest));
  const changedManifest = verify();
  assert.notEqual(changedManifest.status, 0);
  assert.match(changedManifest.stderr, /source provenance mismatch: sourceCommit/);
  writeFileSync(manifestFile, original);
  assert.equal(verify().status, 0);
}));


test("Windows relative output paths normalize to Git's slash-separated identity", () => {
  const relative = path.win32.relative("C:\\source\\connectors", "C:\\source\\connectors\\release-artifacts\\artifact.tgz");
  const excluded = new Set([normalizeSourcePath(relative, path.win32.sep)]);
  assert.equal(excluded.has("release-artifacts/artifact.tgz"), true);
  assert.equal(excluded.has("release-artifacts/real-source.ts"), false);
});
