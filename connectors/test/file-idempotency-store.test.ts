import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rename, rm, rmdir } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import { FileIdempotencyStore } from "../src/sdk/reliability.js";

test("duplicate lookup avoids a rewrite and a new event saves cleanup with the record", async (t) => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "soha-idempotency-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const file = path.join(directory, "idempotency.json");
  const store = new FileIdempotencyStore(file);
  assert.equal(store.checkAndRecord("expired", 10, new Date(100)), true);
  assert.equal(store.checkAndRecord("unexpired", 100, new Date(100)), true);
  const saved = await readFile(file, "utf8");

  assert.equal(store.checkAndRecord("unexpired", 100, new Date(110)), false);
  assert.equal(await readFile(file, "utf8"), saved);
  assert.equal(store.checkAndRecord("new", 25, new Date(110)), true);
  assert.deepEqual(JSON.parse(await readFile(file, "utf8")), { entries: { unexpired: 200, new: 135 } });

  const reloaded = new FileIdempotencyStore(file);
  assert.equal(reloaded.has("expired", new Date(110)), false);
  assert.equal(reloaded.has("new", new Date(134)), true);
  assert.equal(reloaded.checkAndRecord("new", 25, new Date(135)), true);
});

test("failed persistence preserves the previous entries and allows the event to retry", async (t) => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "soha-idempotency-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const file = path.join(directory, "idempotency.json");
  const store = new FileIdempotencyStore(file);
  const now = new Date(100);
  assert.equal(store.checkAndRecord("existing", 100, now), true);

  const backup = path.join(directory, "before-failure.json");
  await rename(file, backup);
  await mkdir(file);
  assert.throws(() => store.checkAndRecord("retry", 100, now));
  assert.equal(store.has("existing", now), true);
  assert.equal(store.has("retry", now), false);
  await rmdir(file);
  await rename(backup, file);

  assert.equal(store.checkAndRecord("retry", 100, now), true);
  assert.equal(new FileIdempotencyStore(file).has("retry", now), true);
});
