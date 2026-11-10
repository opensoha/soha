import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { validateFixtureDirectory } from "./validate-fixtures.mjs";

const root = new URL("../", import.meta.url);
const schema = JSON.parse(await readFile(new URL("agent/dsh-runtime.schema.json", root), "utf8"));
const ajv = new Ajv2020({ allErrors: true, strict: true });
addFormats(ajv);
const validate = ajv.compile(schema);
const fixtures = new URL("fixtures/json-schema/dsh-runtime/", root);
const count = await validateFixtureDirectory(fixtures, validate);
const event = JSON.parse(await readFile(new URL("valid/event-delta.json", fixtures), "utf8"));
for (const [field, limit] of [["delta", 16384], ["assistantText", 131072]]) {
  const sample = { ...event, type: field === "delta" ? "message_delta" : "completed", data: { [field]: "x".repeat(limit) } };
  assert.equal(validate(sample), true, `${field} accepts its boundary`);
  sample.data[field] += "x";
  assert.equal(validate(sample), false, `${field} rejects overflow`);
}
const page = ajv.compile({ $ref: `${schema.$id}#/$defs/EventPage` });
assert.equal(page({ items: [event], nextSequence: 2, hasMore: false }), true);
assert.equal(page({ items: [], nextSequence: 0, hasMore: false }), true);
assert.equal(page({ items: [event], nextSequence: -1, hasMore: false }), false);
assert.equal(page({ items: Array(257).fill(event), nextSequence: 2, hasMore: false }), false);
assert.equal(page({ items: [event], nextSequence: 2, hasMore: false, toolGrant: "secret" }), false);
console.log(`validated dsh-runtime schema, ${count} fixtures, 4 text boundaries and 5 page boundaries`);
