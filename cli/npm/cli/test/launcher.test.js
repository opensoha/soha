import assert from "node:assert/strict";
import { createServer } from "node:http";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
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
    if (request.url === `/v${version}/checksums.txt`) {
      checksumDownloads++;
      response.end(checksums);
      return;
    }
    if (request.url === `/v${version}/${assetName}`) {
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
