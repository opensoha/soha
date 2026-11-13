import { existsSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { join } from "node:path";

const root = new URL("../", import.meta.url);
const errors = [];

const connectorIndex = await readJson("connectors/index.json");
const capabilityMatrix = await readJson("connectors/capability-matrix.json");
const envExample = await readText(".env.example");
const rootReadme = await readText("README.md");
const changelog = await readText("CHANGELOG.md");
const releaseWorkflow = exists(".github/workflows/release.yml")
  ? await readText(".github/workflows/release.yml")
  : "";

if (!Array.isArray(connectorIndex.connectors) || connectorIndex.connectors.length === 0) {
  errors.push("connectors/index.json must contain at least one connector entry");
}

const matrixById = new Map();
if (!Array.isArray(capabilityMatrix.connectors)) {
  errors.push("connectors/capability-matrix.json must contain a connectors array");
} else {
  for (const entry of capabilityMatrix.connectors) {
    if (!entry.id || !entry.status || !Array.isArray(entry.capabilities)) {
      errors.push("connectors/capability-matrix.json contains an incomplete connector entry");
      continue;
    }
    matrixById.set(entry.id, entry);
    validateCapabilityMatrixEntry(entry);
  }
}

for (const envName of [
  "SOHA_CONNECTOR_HTTP_HOST",
  "SOHA_CONNECTOR_HTTP_PORT",
  "SOHA_CONNECTOR_HTTP_TOKEN",
  "SOHA_CONNECTOR_HTTP_MAX_BODY_BYTES",
  "SOHA_CONNECTOR_LOG_LEVEL"
]) {
  if (!envExample.includes(`${envName}=`)) {
    errors.push(`.env.example must include ${envName}`);
  }
}

for (const entry of connectorIndex.connectors) {
  if (!entry.id || !entry.path || !entry.status) {
    errors.push("connectors/index.json contains an incomplete connector entry");
    continue;
  }

  const connectorDir = `connectors/${entry.id}`;
  if (!exists(connectorDir)) {
    errors.push(`${connectorDir} is listed but does not exist`);
  }
  if (!exists(`${connectorDir}/README.md`)) {
    errors.push(`${connectorDir}/README.md is required`);
  }
  if (!rootReadme.includes(`${connectorDir}/`)) {
    errors.push(`README.md layout must mention ${connectorDir}/`);
  }
  if (!matrixById.has(entry.id)) {
    errors.push(`connectors/capability-matrix.json must include ${entry.id}`);
  } else if (matrixById.get(entry.id).status !== entry.status) {
    errors.push(`connectors/capability-matrix.json status for ${entry.id} must match connectors/index.json`);
  }

  if (entry.path.endsWith(".json")) {
    const manifest = await readJson(entry.path);
    validateManifestJson(entry.path, manifest);
    if (manifest.id !== entry.id) {
      errors.push(`${entry.path} id must match connectors/index.json entry`);
    }
    if (manifest.status !== entry.status) {
      errors.push(`${entry.path} status must match connectors/index.json entry`);
    }
    for (const envName of manifestEnvNames(manifest)) {
      if (!envExample.includes(`${envName}=`)) {
        errors.push(`.env.example must include ${envName} from ${entry.path}`);
      }
    }
  }
}

const feishuMatrix = matrixById.get("feishu");
if (feishuMatrix) {
  for (const required of ["runtime.http.webhook_server", "runtime.http.action_endpoint", "reliability.dead_letter"]) {
    if (!feishuMatrix.capabilities.some((capability) => capability.name === required)) {
      errors.push(`Feishu capability matrix must include ${required}`);
    }
  }
}

const schema = await readJson("schemas/connector-manifest.schema.json");
for (const required of ["id", "name", "version", "runtime", "lifecycle", "config", "actions"]) {
  if (!schema.required.includes(required)) {
    errors.push(`connector manifest schema must require ${required}`);
  }
}

const eventEnvelopeSchema = await readJson("schemas/connector-event-envelope.schema.json");
validateEventEnvelopeSchema(eventEnvelopeSchema);

if (!exists("docs/soha-core-integration.md")) {
  errors.push("docs/soha-core-integration.md is required");
}
if (!exists("docs/operations/feishu-runtime-runbook.md")) {
  errors.push("docs/operations/feishu-runtime-runbook.md is required");
}
const coreIntegrationDoc = await readText("docs/soha-core-integration.md");
const feishuRunbook = await readText("docs/operations/feishu-runtime-runbook.md");
if (!rootReadme.includes("docs/soha-core-integration.md")) {
  errors.push("README.md must mention docs/soha-core-integration.md");
}
if (!rootReadme.includes("docs/operations/feishu-runtime-runbook.md")) {
  errors.push("README.md must mention docs/operations/feishu-runtime-runbook.md");
}
if (!rootReadme.includes("CHANGELOG.md")) {
  errors.push("README.md must mention CHANGELOG.md");
}
if (!rootReadme.includes("connectors/capability-matrix.json")) {
  errors.push("README.md must mention connectors/capability-matrix.json");
}
if (!rootReadme.includes("schemas/connector-event-envelope.schema.json")) {
  errors.push("README.md must mention schemas/connector-event-envelope.schema.json");
}
for (const required of ["GitHub build provenance attestation", ".github/workflows/release.yml"]) {
  if (!rootReadme.includes(required)) {
    errors.push(`README.md must mention ${required}`);
  }
}
for (const required of ["actions/attest-build-provenance@v2", "id-token: write", "attestations: write"]) {
  if (!releaseWorkflow.includes(required)) {
    errors.push(`.github/workflows/release.yml must mention ${required}`);
  }
}
if (!coreIntegrationDoc.includes("schemas/connector-event-envelope.schema.json")) {
  errors.push("docs/soha-core-integration.md must mention schemas/connector-event-envelope.schema.json");
}
if (!coreIntegrationDoc.includes("Contracts Promotion Checklist")) {
  errors.push("docs/soha-core-integration.md must include a contracts promotion checklist");
}
for (const required of [
  "GET /healthz",
  "GET /manifest",
  "POST /actions/feishu.message.send_text",
  "GET /retry-queue",
  "GET /dead-letter",
  "GET /metrics",
  "Rollback"
]) {
  if (!feishuRunbook.includes(required)) {
    errors.push(`docs/operations/feishu-runtime-runbook.md must mention ${required}`);
  }
}
for (const planned of connectorIndex.connectors.filter((entry) => entry.status === "planned")) {
  if (!changelog.includes(`${planned.id}\``) || !changelog.includes("v0.1.x deferred")) {
    errors.push(`CHANGELOG.md must mark planned connector ${planned.id} as v0.1.x deferred`);
  }
}

if (errors.length > 0) {
  for (const error of errors) {
    console.error(`validate: ${error}`);
  }
  process.exit(1);
}

console.log(`validated ${connectorIndex.connectors.length} connector entries`);

async function readText(path) {
  return readFile(new URL(path, root), "utf8");
}

async function readJson(path) {
  return JSON.parse(await readText(path));
}

function exists(path) {
  return existsSync(join(root.pathname, path));
}

function validateManifestJson(path, manifest) {
  for (const field of [
    "id",
    "name",
    "version",
    "status",
    "description",
    "runtime",
    "lifecycle",
    "config",
    "secrets",
    "capabilities",
    "permissions",
    "events",
    "actions"
  ]) {
    if (manifest[field] === undefined) {
      errors.push(`${path} missing ${field}`);
    }
  }

  if (!/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(manifest.version ?? "")) {
    errors.push(`${path} version must be semver`);
  }
  if (!["planned", "experimental", "stable"].includes(manifest.status)) {
    errors.push(`${path} status is invalid`);
  }
  if (manifest.runtime?.language !== "typescript") {
    errors.push(`${path} runtime.language must be typescript`);
  }
  if (!manifest.runtime?.entrypoint?.startsWith("src/") || !manifest.runtime.entrypoint.endsWith(".ts")) {
    errors.push(`${path} runtime.entrypoint must point at src/*.ts`);
  }
  for (const state of ["created", "configured", "started", "stopped"]) {
    if (!manifest.lifecycle?.includes(state)) {
      errors.push(`${path} lifecycle must include ${state}`);
    }
  }
  if (manifest.id === "feishu" && !manifest.actions?.some((action) => action.name === "feishu.message.send_text")) {
    errors.push(`${path} must expose feishu.message.send_text`);
  }
  if (manifest.id === "feishu" && !manifest.events?.includes("im.message.receive_v1")) {
    errors.push(`${path} must declare im.message.receive_v1`);
  }
}

function validateCapabilityMatrixEntry(entry) {
  if (!["planned", "experimental", "stable"].includes(entry.status)) {
    errors.push(`capability matrix ${entry.id} status is invalid`);
  }
  for (const capability of entry.capabilities) {
    if (!capability.name || !capability.direction || !capability.status) {
      errors.push(`capability matrix ${entry.id} contains an incomplete capability`);
      continue;
    }
    if (!["inbound", "outbound", "bidirectional"].includes(capability.direction)) {
      errors.push(`capability matrix ${entry.id}.${capability.name} direction is invalid`);
    }
    if (!["implemented", "implemented_memory", "implemented_minimal", "planned", "deferred"].includes(capability.status)) {
      errors.push(`capability matrix ${entry.id}.${capability.name} status is invalid`);
    }
    if (!Array.isArray(capability.evidence) || capability.evidence.length === 0) {
      errors.push(`capability matrix ${entry.id}.${capability.name} must include evidence`);
      continue;
    }
    for (const evidence of capability.evidence) {
      if (!exists(evidence)) {
        errors.push(`capability matrix evidence does not exist: ${evidence}`);
      }
    }
  }
}

function manifestEnvNames(manifest) {
  const names = new Set();
  for (const field of manifest.config ?? []) {
    if (field.env) {
      names.add(field.env);
    }
  }
  for (const secret of manifest.secrets ?? []) {
    if (secret.env) {
      names.add(secret.env);
    }
  }
  return names;
}

function validateEventEnvelopeSchema(schema) {
  if (schema.$id !== "https://contracts.opensoha.dev/connectors/connector-event-envelope.schema.json") {
    errors.push("connector event envelope schema must mirror the soha-contracts canonical $id");
  }

  const required = new Set(schema.required ?? []);
  for (const field of ["connectorId", "events"]) {
    if (!required.has(field)) {
      errors.push(`connector event envelope schema must require ${field}`);
    }
  }
  if (schema.additionalProperties !== false) {
    errors.push("connector event envelope schema must reject unknown top-level fields");
  }

  const properties = schema.properties ?? {};
  if (properties.connectorId?.type !== "string") {
    errors.push("connector event envelope schema connectorId must be type string");
  }
  if (properties.connectorId?.pattern !== "^[a-z][a-z0-9-]*$") {
    errors.push("connector event envelope schema connectorId must match connector id pattern");
  }
  if (properties.events?.type !== "array") {
    errors.push("connector event envelope schema events must be type array");
  }
  if (properties.events?.minItems !== 1) {
    errors.push("connector event envelope schema events must require at least one event");
  }
  if (properties.events?.items?.$ref !== "#/$defs/ConnectorEvent") {
    errors.push("connector event envelope schema events must reference ConnectorEvent");
  }

  const connectorEvent = schema.$defs?.ConnectorEvent ?? {};
  if (connectorEvent.additionalProperties !== false) {
    errors.push("connector event envelope schema ConnectorEvent must reject unknown top-level fields");
  }
  const eventRequired = new Set(connectorEvent.required ?? []);
  for (const field of ["id", "type", "source", "occurredAt", "payload"]) {
    if (!eventRequired.has(field)) {
      errors.push(`connector event envelope schema ConnectorEvent must require ${field}`);
    }
  }
  if (eventRequired.has("subject")) {
    errors.push("connector event envelope schema ConnectorEvent subject must be optional");
  }

  const eventProperties = connectorEvent.properties ?? {};
  const expectedTypes = {
    id: "string",
    type: "string",
    source: "string",
    occurredAt: "string",
    subject: "string",
    payload: "object"
  };
  for (const [field, type] of Object.entries(expectedTypes)) {
    if (eventProperties[field]?.type !== type) {
      errors.push(`connector event envelope schema ConnectorEvent ${field} must be type ${type}`);
    }
  }
  if (eventProperties.occurredAt?.format !== "date-time") {
    errors.push("connector event envelope schema ConnectorEvent occurredAt must use date-time format");
  }
  if (eventProperties.source?.pattern !== "^[a-z][a-z0-9-]*$") {
    errors.push("connector event envelope schema ConnectorEvent source must match connector id pattern");
  }
  if (eventProperties.payload?.additionalProperties !== true) {
    errors.push("connector event envelope schema ConnectorEvent payload must allow provider-specific properties");
  }
}
