import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, unlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { archivedGoProxy } from "./archived-go-sdk.mjs";

const module = "github.com/opensoha/soha-contracts";
const version = "v0.1.30-0.20261110173546-4aef3ea31f6a";
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "archived-sdk-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const directory = join(root, ".github/legacy-goproxy", module, "@v");
  mkdirSync(directory, { recursive: true });
  const metadata = { schemaVersion: 1, module, version, sum: "h1:" + "a".repeat(43) + "=", goModSum: "h1:" + "b".repeat(43) + "=", files: {} };
  for (const extension of ["zip", "mod", "info"]) {
    const name = `${version}.${extension}`;
    const bytes = Buffer.from(extension);
    writeFileSync(join(directory, name), bytes);
    metadata.files[name] = createHash("sha256").update(bytes).digest("hex");
  }
  const save = () => writeFileSync(join(root, ".github/legacy-go-sdk.json"), JSON.stringify(metadata));
  save();
  return { root, directory, metadata, save };
}

test("valid original archive supplies a file proxy without changing module identity", (t) => {
  const f = fixture(t);
  const proxy = archivedGoProxy(f.root, module, version, "https://proxy.golang.org");
  assert.ok(proxy.startsWith("file:"));
  assert.ok(proxy.endsWith(",https://proxy.golang.org"));
});

test("actual archived module files retain their reviewed byte hashes", () => {
  const root = fileURLToPath(new URL("../..", import.meta.url));
  assert.ok(archivedGoProxy(root, module, version, "https://proxy.golang.org").startsWith("file:"));
  const manifest = JSON.parse(readFileSync(join(root, ".github/legacy-go-sdk.json"), "utf8"));
  assert.equal(manifest.sum, "h1:wQuW9V56dc9MReFReBhOxr8BxfTTdQ6ECbVMB7iRYt4=");
  assert.equal(manifest.goModSum, "h1:oK73bgv1NnwMkHQCMglHZaLE50Ax5z/TBbUdP5pwsEE=");
});

test("different module or declared version and output injection are rejected", (t) => {
  const f = fixture(t);
  for (const [path, ref, fallback] of [["example.invalid/module", version, "direct"], [module, "v0.1.0", "direct"], [module, version, "direct\nother=value"]]) {
    assert.throws(() => archivedGoProxy(f.root, path, ref, fallback), /module identity/);
  }
});

test("missing or changed archive bytes fail before Go can fall back to an old repository", (t) => {
  const f = fixture(t);
  const file = join(f.directory, `${version}.zip`);
  writeFileSync(file, "changed");
  assert.throws(() => archivedGoProxy(f.root, module, version), /checksum/);
  unlinkSync(file);
  assert.throws(() => archivedGoProxy(f.root, module, version));
});

test("symlinked manifest and module files cannot redirect the source", (t) => {
  const f = fixture(t);
  const file = join(f.directory, `${version}.mod`);
  unlinkSync(file);
  symlinkSync(join(f.directory, `${version}.info`), file);
  assert.throws(() => archivedGoProxy(f.root, module, version), /checksum/);
  const manifest = join(f.root, ".github/legacy-go-sdk.json");
  unlinkSync(manifest);
  symlinkSync(join(f.directory, `${version}.info`), manifest);
  assert.throws(() => archivedGoProxy(f.root, module, version), /regular source file/);
});

test("malformed metadata cannot authorize a mirror", (t) => {
  const f = fixture(t);
  for (const field of ["schemaVersion", "module", "sum", "goModSum"]) {
    const before = f.metadata[field];
    f.metadata[field] = null;
    f.save();
    assert.throws(() => archivedGoProxy(f.root, module, version), /module identity/);
    f.metadata[field] = before;
  }
});
