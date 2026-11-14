import assert from "node:assert/strict";
import { test } from "node:test";

import { redactText, redactValue } from "../src/sdk/redaction.js";

test("redactText covers every sensitive assignment key", () => {
  const cases = [
    ["pass=legacy-pass", "pass=[REDACTED]"],
    ['encryptKey="encryption-secret"', "encryptKey=[REDACTED]"],
    ["signature='signature-secret'", "signature=[REDACTED]"],
    ["cookie=session-secret", "cookie=[REDACTED]"]
  ];

  for (const [input, expected] of cases) {
    assert.equal(redactText(input), expected);
  }
});

test("redactValue covers dashed API key fields", () => {
  assert.deepEqual(redactValue({ "api-key": "secret", visible: "value" }), {
    "api-key": "[REDACTED]",
    visible: "value"
  });
});
