import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import { verifyNpmRelease } from "./verify-npm-release.mjs";

const version = process.env.RELEASE_VERSION;
const sha = process.env.RELEASE_SHA;
assert.match(version ?? "", /^0\.2\.\d+$/);
assert.match(sha ?? "", /^[0-9a-f]{40}$/);
const manifest = JSON.parse(readFileSync("dist/release/release-manifest.json", "utf8"));
const local = manifest.artifacts.find((item) => item.kind === "npm-tarball");
assert.equal(manifest.releaseSource?.revision, sha);
assert.equal(manifest.version, version);
const npm = await verifyNpmRelease({
  name: "@opensoha/contracts", version, tag: process.env.RELEASE_TAG, sha,
  workflow: ".github/workflows/contracts-release.yml", integrity: local.npmIntegrity, sha256: local.sha256,
});

const directory = mkdtempSync(join(tmpdir(), "contracts-public-release-"));
try {
  writeFileSync(join(directory, "go.mod"), "module contracts-public-verification\n\ngo 1.27.4\n");
  const modulePath = "github.com/opensoha/soha/contracts";
  const module = JSON.parse(run("go", ["mod", "download", "-json", `${modulePath}@v${version}`], {
    GOWORK: "off", GOPROXY: "https://proxy.golang.org", GOSUMDB: "sum.golang.org", GOPRIVATE: "", GONOPROXY: "", GONOSUMDB: "",
  }));
  assert.equal(module.Path, modulePath);
  assert.equal(module.Version, `v${version}`);
  assert(!module.Error && !module.Replace);
  assert.match(module.Sum ?? "", /^h1:/);
  assert.match(module.GoModSum ?? "", /^h1:/);
  const goMod = readFileSync(module.GoMod, "utf8");
  assert(goMod.startsWith(`module ${modulePath}\n`));
  assert(!/^\s*replace\b/m.test(goMod), "published SDK must not contain local replacements");
  if (module.Origin?.Hash) assert.equal(module.Origin.Hash, sha);
  writeFileSync("dist/release/public-verification.json", `${JSON.stringify({
    version, tag: process.env.RELEASE_TAG, sha,
    npm,
    go: { module: modulePath, version: module.Version, sum: module.Sum, goModSum: module.GoModSum, proxy: "https://proxy.golang.org", sumDB: "sum.golang.org" },
  }, null, 2)}\n`);
} finally {
  rmSync(directory, { recursive: true, force: true });
}

function run(command, args, extraEnv = {}) {
  const result = spawnSync(command, args, { cwd: directory, encoding: "utf8", env: { ...process.env, ...extraEnv } });
  if (result.status !== 0) throw new Error(`${command} ${args.join(" ")} failed: ${result.stdout}\n${result.stderr}`);
  return result.stdout;
}
