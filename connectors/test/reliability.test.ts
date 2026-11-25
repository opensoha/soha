import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import { FileDeadLetterQueue, FileIdempotencyStore, FileRetryQueue, MemoryDeadLetterQueue } from "../src/sdk/reliability.js";

test("file idempotency store rejects duplicates across instances until ttl expires", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "soha-connectors-idempotency-"));
  const file = path.join(dir, "idempotency.json");
  const now = new Date("2026-06-12T00:00:00.000Z");

  const first = new FileIdempotencyStore(file);
  assert.equal(first.checkAndRecord("event:event-1", 1000, now), true);

  const second = new FileIdempotencyStore(file);
  assert.equal(second.checkAndRecord("event:event-1", 1000, now), false);
  assert.equal(second.checkAndRecord("event:event-1", 1000, new Date(now.getTime() + 1001)), true);
});

test("file dead letter queue persists redacted entries and honors max size", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "soha-connectors-dead-letter-"));
  const file = path.join(dir, "dead-letter.json");

  const queue = new FileDeadLetterQueue(file, 1);
  queue.record({
    connectorId: "feishu",
    action: "feishu.message.send_text",
    requestId: "request-1",
    error: {
      code: "rate_limited",
      message: "retry later Authorization=Bearer dead-letter-secret",
      retryable: true
    },
    input: {
      text: "hello",
      token: "secret-token-value"
    },
    now: new Date("2026-06-12T00:00:00.000Z")
  });
  assert.deepEqual(queue.list()[0]?.input, {
    text: "hello",
    token: "[REDACTED]"
  });
  assert.equal(queue.list()[0]?.error.message, "retry later Authorization=[REDACTED]");

  queue.record({
    connectorId: "feishu",
    action: "feishu.message.send_text",
    requestId: "request-2",
    error: {
      code: "feishu_api_error",
      message: "temporary token=retry-secret",
      retryable: true
    },
    input: {
      text: "hello"
    },
    now: new Date("2026-06-12T00:00:01.000Z")
  });

  const reloaded = new FileDeadLetterQueue(file, 10);
  const entries = reloaded.list();
  assert.equal(entries.length, 1);
  assert.equal(entries[0]?.requestId, "request-2");
  assert.deepEqual(entries[0]?.input, { text: "hello" });
  assert.equal(entries[0]?.error.message, "temporary token=[REDACTED]");
});

test("file retry queue persists due entries and supports completion", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "soha-connectors-retry-queue-"));
  const file = path.join(dir, "retry-queue.json");
  const queue = new FileRetryQueue(file, 10);

  const entry = queue.enqueue({
    connectorId: "feishu",
    action: "feishu.message.send_text",
    requestId: "request-1",
    attempts: 2,
    nextAttemptAt: "2026-06-12T00:00:05.000Z",
    error: {
      code: "feishu_api_error",
      message: "temporary token=retry-secret",
      retryable: true
    },
    input: {
      text: "hello",
      token: "secret-token-value"
    },
    now: new Date("2026-06-12T00:00:00.000Z")
  });

  const reloaded = new FileRetryQueue(file, 10);
  assert.equal(reloaded.claimDue(new Date("2026-06-12T00:00:04.000Z")).length, 0);
  const due = reloaded.claimDue(new Date("2026-06-12T00:00:05.000Z"));
  assert.equal(due.length, 1);
  assert.equal(due[0]?.requestId, "request-1");
  assert.deepEqual(due[0]?.input, {
    text: "hello",
    token: "[REDACTED]"
  });
  assert.equal(due[0]?.error.message, "temporary token=[REDACTED]");
  assert.equal(reloaded.complete(entry.id), true);
  assert.equal(new FileRetryQueue(file, 10).list().length, 0);
});

test("file queues sanitize legacy entries when loading", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "soha-connectors-legacy-queue-"));
  const retryFile = path.join(dir, "retry.json");
  const deadLetterFile = path.join(dir, "dead-letter.json");
  const error = {
    code: "connector_error",
    message: "Authorization=Bearer legacy-bearer",
    retryable: true
  };

  await writeFile(
    retryFile,
    JSON.stringify({
      entries: [
        {
          id: "feishu:send:retry:1",
          connectorId: "feishu",
          action: "send",
          createdAt: "2026-06-12T00:00:00.000Z",
          updatedAt: "2026-06-12T00:00:00.000Z",
          nextAttemptAt: "2026-06-12T00:00:05.000Z",
          attempts: 1,
          error,
          input: { passwd: "legacy-password" }
        }
      ]
    })
  );
  await writeFile(
    deadLetterFile,
    JSON.stringify({
      entries: [
        {
          id: "feishu:send:1",
          connectorId: "feishu",
          action: "send",
          recordedAt: "2026-06-12T00:00:00.000Z",
          error,
          input: { pass: "legacy-pass" }
        }
      ]
    })
  );

  assert.equal(new FileRetryQueue(retryFile).list()[0]?.error.message, "Authorization=[REDACTED]");
  assert.deepEqual(new FileRetryQueue(retryFile).list()[0]?.input, { passwd: "[REDACTED]" });
  assert.deepEqual(new FileDeadLetterQueue(deadLetterFile).list()[0]?.input, { pass: "[REDACTED]" });

  const persisted = `${await readFile(retryFile, "utf8")} ${await readFile(deadLetterFile, "utf8")}`;
  assert.equal(persisted.includes("legacy-bearer"), false);
  assert.equal(persisted.includes("legacy-password"), false);
  assert.equal(persisted.includes("legacy-pass"), false);
});

test("retry queue IDs remain unique after completion and reload", async (t) => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "soha-connectors-retry-ids-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const file = path.join(dir, "retry.json");
  const queue = new FileRetryQueue(file, 10);
  const input = {
    connectorId: "feishu",
    action: "send",
    attempts: 1,
    nextAttemptAt: "2026-06-12T00:00:00.000Z",
    error: { code: "temporary", message: "try again", retryable: true },
    input: {}
  };
  const first = queue.enqueue(input);
  const second = queue.enqueue(input);
  assert.equal(queue.complete(first.id), true);
  const reloaded = new FileRetryQueue(file, 10);
  const third = reloaded.enqueue(input);
  assert.notEqual(third.id, second.id);
  assert.equal(reloaded.complete(third.id), true);
  assert.deepEqual(reloaded.list().map((entry) => entry.id), [second.id]);
});

test("dead letter IDs remain unique when old entries are evicted", async (t) => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "soha-connectors-dead-letter-ids-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  for (const queue of [new MemoryDeadLetterQueue(1), new FileDeadLetterQueue(path.join(dir, "dead-letter.json"), 1)]) {
    const ids = Array.from({ length: 4 }, () => queue.record({
      connectorId: "feishu",
      action: "send",
      error: { code: "failed", message: "failed", retryable: false },
      input: {}
    }).id);
    assert.equal(new Set(ids).size, ids.length);
  }
});

test("queues reject unsafe capacities before loading or recording entries", async (t) => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "soha-connectors-queue-capacity-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const factories = [
    (capacity: number) => new MemoryDeadLetterQueue(capacity),
    (capacity: number) => new FileDeadLetterQueue(path.join(dir, "dead-letter.json"), capacity),
    (capacity: number) => new FileRetryQueue(path.join(dir, "retry.json"), capacity)
  ];
  for (const create of factories) {
    assert.doesNotThrow(() => create(1));
    for (const capacity of [-1, 0, 1.5, NaN, Infinity, -Infinity, Number.MAX_SAFE_INTEGER + 1]) {
      assert.throws(() => create(capacity), /queue capacity must be a positive safe integer/);
    }
  }
});
