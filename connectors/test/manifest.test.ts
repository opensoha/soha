import assert from "node:assert/strict";
import { test } from "node:test";

import { feishuManifest } from "../src/connectors/feishu/manifest.js";
import { validateManifest } from "../src/sdk/manifest.js";

test("Feishu manifest satisfies connector lifecycle contract", () => {
  assert.deepEqual(validateManifest(feishuManifest), []);
  assert.deepEqual(feishuManifest.lifecycle, ["created", "configured", "started", "stopped"]);
  assert.equal(feishuManifest.runtime.entrypoint, "src/connectors/feishu/server.ts");
  assert.ok(feishuManifest.actions.some((action) => action.name === "feishu.message.send_text"));
  assert.ok(feishuManifest.capabilities.some((capability) => capability.name === "runtime.http"));
});
