import assert from "node:assert/strict";
import { createServer } from "node:http";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { binaryAssetName, ensureBinary, parseChecksum, resolveTarget, sha256 } from "../lib/launcher.js";

test("resolveTarget maps supported Node platforms to Go release targets", () => {
  assert.deepEqual(resolveTarget("darwin", "arm64"), { goos: "darwin", goarch: "arm64", extension: "" });
  assert.deepEqual(resolveTarget("win32", "x64"), { goos: "windows", goarch: "amd64", extension: ".exe" });
  assert.throws(() => resolveTarget("freebsd", "x64"), /unsupported platform/u);
});

test("parseChecksum requires one exact asset match", () => {
  const digest = "a".repeat(64);
  assert.equal(parseChecksum(`${digest}  soha_1.2.3_linux_amd64\n`, "soha_1.2.3_linux_amd64"), digest);
  assert.throws(() => parseChecksum(`${digest}  prefix-soha_1.2.3_linux_amd64\n`, "soha_1.2.3_linux_amd64"), /does not contain/u);
  assert.throws(() => parseChecksum(`${digest}  x\n${digest}  x\n`, "x"), /duplicate/u);
});

test("ensureBinary verifies downloads and replaces a tampered cache entry", async (t) => {
  const version = "9.9.9-test.1";
  const target = { goos: "linux", goarch: "amd64", extension: "" };
  const assetName = binaryAssetName(version, target);
  const binary = Buffer.from("verified native soha fixture\n");
  const checksums = `${sha256(binary)}  ${assetName}\n`;
  let checksumDownloads = 0;
  let binaryDownloads = 0;
  const server = createServer((request, response) => {
    if (request.url === `/cli%2Fv${version}/checksums.txt`) {
      checksumDownloads++;
      response.end(checksums);
      return;
    }
    if (request.url === `/cli%2Fv${version}/${assetName}`) {
      binaryDownloads++;
      response.end(binary);
      return;
    }
    response.statusCode = 404;
    response.end();
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));
  const address = server.address();
  const cacheRoot = await mkdtemp(path.join(tmpdir(), "soha-npx-test-"));
  t.after(() => rm(cacheRoot, { recursive: true, force: true }));
  const options = {
    version,
    target,
    cacheRoot,
    releaseBaseURL: `http://127.0.0.1:${address.port}`,
  };

  const binaryPaths = await Promise.all(Array.from({ length: 8 }, () => ensureBinary(options)));
  const binaryPath = binaryPaths[0];
  assert.ok(binaryPaths.every((candidate) => candidate === binaryPath));
  assert.deepEqual(await readFile(binaryPath), binary);
  assert.equal(checksumDownloads, 1, "concurrent launchers should share one checksum download");
  assert.equal(binaryDownloads, 1);

  await ensureBinary(options);
  assert.equal(binaryDownloads, 1, "valid cached binary should not be downloaded again");

  await writeFile(binaryPath, "tampered\n");
  await ensureBinary(options);
  assert.deepEqual(await readFile(binaryPath), binary);
  assert.equal(binaryDownloads, 2);
});


test("ensureBinary rejects invalid versions, insecure downloads and checksum mismatches", async (t) => {
  const cacheRoot = await mkdtemp(path.join(tmpdir(), "soha-npx-reject-"));
  t.after(() => rm(cacheRoot, { recursive: true, force: true }));
  await assert.rejects(ensureBinary({ version: "../0.2.0", cacheRoot }), /invalid launcher version/u);
  await assert.rejects(ensureBinary({ version: "0.2.0", cacheRoot, releaseBaseURL: "http://example.com" }), /insecure download/u);
  const server = createServer((request, response) => {
    response.end(request.url.endsWith("checksums.txt") ? `${"a".repeat(64)}  soha_0.2.0_linux_amd64\n` : "tampered");
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));
  await assert.rejects(ensureBinary({ version: "0.2.0", cacheRoot,
    target: resolveTarget("linux", "x64"), releaseBaseURL: `http://127.0.0.1:${server.address().port}` }), /sha256 mismatch/u);
});


test("ensureBinary enforces checksum and binary download size bounds", async (t) => {
  const target = resolveTarget("linux", "x64");
  const asset = binaryAssetName("0.2.0", target);
  for (const oversized of ["checksums", "binary"]) {
    await t.test(oversized, async (t) => {
      const cacheRoot = await mkdtemp(path.join(tmpdir(), "soha-npx-size-"));
      t.after(() => rm(cacheRoot, { recursive: true, force: true }));
      const server = createServer((request, response) => {
        const isChecksum = request.url.endsWith("checksums.txt");
        if ((oversized === "checksums") === isChecksum) {
          response.setHeader("content-length", isChecksum ? 1024 * 1024 + 1 : 200 * 1024 * 1024 + 1);
        }
        response.end(isChecksum ? `${"a".repeat(64)}  ${asset}\n` : "binary");
      });
      await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
      t.after(() => new Promise((resolve) => server.close(resolve)));
      await assert.rejects(ensureBinary({ version: "0.2.0", target, cacheRoot,
        releaseBaseURL: `http://127.0.0.1:${server.address().port}` }), /exceeds the .*byte limit/u);
    });
  }
});
