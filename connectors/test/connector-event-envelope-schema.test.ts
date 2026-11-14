import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";

test("connector event envelope schema describes Core batch wrapper", async () => {
  const schemaUrl = new URL("../../schemas/connector-event-envelope.schema.json", import.meta.url);
  const schema = JSON.parse(await readFile(schemaUrl, "utf8")) as Record<string, any>;

  assert.equal(schema.$id, "https://contracts.opensoha.dev/connectors/connector-event-envelope.schema.json");
  assert.deepEqual(schema.required, ["connectorId", "events"]);
  assert.equal(schema.additionalProperties, false);
  assert.equal(schema.properties?.connectorId?.type, "string");
  assert.equal(schema.properties?.connectorId?.pattern, "^[a-z][a-z0-9-]*$");
  assert.equal(schema.properties?.events?.type, "array");
  assert.equal(schema.properties?.events?.minItems, 1);
  assert.equal(schema.properties?.events?.items?.$ref, "#/$defs/ConnectorEvent");

  const connectorEvent = schema.$defs?.ConnectorEvent;
  assert.deepEqual(connectorEvent?.required, ["id", "type", "source", "occurredAt", "payload"]);
  assert.equal(connectorEvent?.additionalProperties, false);
  assert.equal(connectorEvent?.properties?.payload?.type, "object");
  assert.equal(connectorEvent?.properties?.payload?.additionalProperties, true);
});
