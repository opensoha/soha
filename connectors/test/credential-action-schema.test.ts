import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
import { CONNECTOR_ACTION_PROTOCOL } from "../src/sdk/types.js";

test("credential action declaration and envelope use the packaged versioned contract", async () => {
  const schema = JSON.parse(await readFile(new URL("../../schemas/connector-action.schema.json", import.meta.url), "utf8"));
  const manifest = JSON.parse(await readFile(new URL("../../schemas/connector-manifest.schema.json", import.meta.url), "utf8"));
  assert.equal(schema.properties.schemaVersion.const, CONNECTOR_ACTION_PROTOCOL);
  assert.deepEqual(schema.required, ["schemaVersion", "action", "input", "credentials"]);
  assert.equal(schema.additionalProperties, false);
  assert.deepEqual(manifest.$defs.action, schema.$defs.ActionDefinition);
  assert.equal(schema.properties.credentials.properties.entries.maxProperties, 16);
  assert.equal(schema.$defs.Credential.properties.value.maxLength, 65_536);
  assert.deepEqual(schema.$defs.ActionDefinition.if, { required: ["credentialProtocol"] });
  assert.equal(schema.$defs.ActionDefinition.then.properties.name.maxLength, 128);
  assert.equal(schema.$defs.ActionDefinition.properties.name.maxLength, undefined);
});
