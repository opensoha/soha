import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { parse, stringify } from "yaml";
import { canonicalJson } from "./openapi-contract-utils.mjs";

const root = new URL("..", import.meta.url);
const checkOnly = process.argv.includes("--check");
const oapiCodegenVersion = "v2.7.1";

const outputs = [
  "gen/go/sohaapi/types.go",
  "gen/ts/sohaapi/index.ts",
  "openapi/soha-api.json",
  "openapi/spec_generated.go",
  "delivery/document.schema.json",
];

const rootPath = fileURLToPath(root);
const outputRootPath = checkOnly ? await mkdtemp(join(tmpdir(), "opensoha-contracts-sdk-")) : rootPath;

try {
  await generateSdk(outputRootPath);
  if (checkOnly) {
    await checkGeneratedOutput(outputRootPath);
  }
} finally {
  if (checkOnly) {
    if (process.env.OPENSOHA_KEEP_VERIFY_ARTIFACTS !== "1") await rm(outputRootPath, { recursive: true, force: true });
  }
}

async function generateSdk(outputRoot) {
  const tsOutput = join(outputRoot, "gen/ts/sohaapi/index.ts");
  const goOutput = join(outputRoot, "gen/go/sohaapi/types.go");

  await generateOpenAPIArtifacts(outputRoot);
  await mkdir(dirname(tsOutput), { recursive: true });
  await mkdir(dirname(goOutput), { recursive: true });

  run("npx", [
    "openapi-typescript",
    "openapi/soha-api.yaml",
    "--root-types",
    "--root-types-no-schema-prefix",
    "--root-types-keep-casing",
    "--default-non-nullable",
    "false",
    "--output",
    tsOutput,
  ]);
  await patchTypeScriptEntrypoint(tsOutput);

  const goSpec = await writeGoCompatibilitySpec();
  try {
  run("go", [
      "run",
      `github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@${oapiCodegenVersion}`,
      "-config",
      "configs/oapi-codegen.yaml",
      "-o",
      goOutput,
      goSpec.path,
    ]);
  } finally {
    if (process.env.OPENSOHA_KEEP_VERIFY_ARTIFACTS !== "1") await rm(goSpec.dir, { recursive: true, force: true });
  }
  await patchGoEntrypoint(goOutput);
  run("gofmt", ["-w", goOutput]);
}

async function generateOpenAPIArtifacts(outputRoot) {
  const yamlText = await readFile(new URL("openapi/soha-api.yaml", root), "utf8");
  const spec = parse(yamlText);
  await generateDeliveryDocumentSchema(spec, outputRoot);
  const jsonText = `${canonicalJson(spec)}\n`;
  const outputDir = join(outputRoot, "openapi");
  const goOutput = join(outputDir, "spec_generated.go");

  await mkdir(outputDir, { recursive: true });
  await writeFile(join(outputDir, "soha-api.json"), jsonText);
  await writeFile(
    goOutput,
    `// Code generated from OpenSoha contracts. DO NOT EDIT.

package openapi

import _ "embed"

const (
	Version = ${JSON.stringify(spec.info.version)}
	YAMLSHA256 = ${JSON.stringify(sha256(yamlText))}
	JSONSHA256 = ${JSON.stringify(sha256(jsonText))}
)

//go:embed soha-api.yaml
var yamlSpec []byte

//go:embed soha-api.json
var jsonSpec []byte

// YAML returns a copy of the canonical OpenAPI YAML document.
func YAML() []byte { return append([]byte(nil), yamlSpec...) }

// JSON returns a copy of the canonical OpenAPI JSON document.
func JSON() []byte { return append([]byte(nil), jsonSpec...) }
`,
  );
  run("gofmt", ["-w", goOutput]);
}

async function generateDeliveryDocumentSchema(spec, outputRoot) {
  const definitions = {};
  const pending = ["DeliveryDocument"];
  while (pending.length) {
    const name = pending.pop();
    if (definitions[name]) continue;
    const definition = spec.components.schemas[name];
    if (!definition) throw new Error(`missing delivery document schema ${name}`);
    const serialized = JSON.stringify(definition).replaceAll(/#\/components\/schemas\/([A-Za-z0-9_]+)/g, (_, referenced) => {
      pending.push(referenced);
      return `#/$defs/${referenced}`;
    });
    definitions[name] = JSON.parse(serialized);
  }
  const schema = {
    $schema: "https://json-schema.org/draft/2020-12/schema",
    $id: "https://contracts.opensoha.dev/delivery/document.schema.json",
    $comment: "Generated from openapi/soha-api.yaml. Do not edit.",
    title: definitions.DeliveryDocument.title,
    $ref: "#/$defs/DeliveryDocument",
    $defs: definitions,
  };
  await mkdir(join(outputRoot, "delivery"), { recursive: true });
  await writeFile(join(outputRoot, "delivery/document.schema.json"), `${canonicalJson(schema)}\n`);
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

async function checkGeneratedOutput(generatedRoot) {
  const staleOutputs = [];
  for (const path of outputs) {
    const expected = await readFile(join(generatedRoot, path), "utf8").catch(() => "");
    const current = await readFile(new URL(path, root), "utf8").catch(() => "");
    if (expected !== current) {
      staleOutputs.push(path);
    }
  }
  if (staleOutputs.length > 0) {
    for (const path of staleOutputs) {
      console.error(`${path} is out of date. Run npm run generate.`);
    }
    throw new Error("generated SDK entrypoints are out of date");
  }
}

async function patchTypeScriptEntrypoint(url) {
  const generated = await readFile(url, "utf8");
  let patched = generated
    .replace(
      "/**\n * This file was auto-generated by openapi-typescript.\n * Do not make direct changes to the file.\n */",
      "// Code generated from OpenSoha contracts by openapi-typescript. DO NOT EDIT.",
    )
    .replace(
      "export type AnyValue = components['schemas']['AnyValue'];",
      [
        "export type AnyObject = Record<string, unknown>;",
        "export type ApiResponse<T = unknown> = { data: T };",
        "export type ApiItemsResponse<T = unknown> = { items: T[] };",
        "export type AuthTokens = TokenSet;",
        "export type AnyValue = components['schemas']['AnyValue'];",
      ].join("\n"),
    );
  // TypeScript cannot recurse through an indexed-access union (TS2502).
  // Lift the generated JSON value union into its existing named export.
  const recursiveValue = patched.match(/        TemplateParameterValue: ([\s\S]*?);\n        TemplateParameterValues:/);
  if (!recursiveValue) throw new Error("generated TemplateParameterValue union is missing");
  const valueType = recursiveValue[1].replaceAll('components["schemas"]["TemplateParameterValue"]', "TemplateParameterValue");
  patched = patched
    .replace(recursiveValue[0], "        TemplateParameterValue: TemplateParameterValue;\n        TemplateParameterValues:")
    .replace("export type TemplateParameterValue = components['schemas']['TemplateParameterValue'];", `export type TemplateParameterValue = ${valueType};`);
  await writeFile(url, patched);
}

async function patchGoEntrypoint(url) {
  let generated = await readFile(url, "utf8");
  generated = generated
    .replace(
      "// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version",
      "// Code generated from OpenSoha contracts by github.com/oapi-codegen/oapi-codegen/v2 version",
    )
    .replace("type AnyValue = interface{}", "type AnyValue = any")
    .replaceAll("map[string]interface{}", "map[string]any");
  // New enums can change collision detection; keep the published reasoning names.
  const reasoningStart = generated.indexOf("// Defines values for WorkbenchModelOptionReasoningEfforts.");
  const reasoningEnd = generated.indexOf("\n// Defines values for ", reasoningStart + 1);
  if (reasoningStart < 0 || reasoningEnd < 0) throw new Error("reasoning effort enum is missing");
  let reasoning = generated.slice(reasoningStart, reasoningEnd);
  for (const suffix of ["High", "Medium", "Low"]) {
    reasoning = reasoning.replaceAll(new RegExp(`\\b${suffix}\\b`, "g"), `WorkbenchModelOptionReasoningEfforts${suffix}`);
  }
  generated = generated.slice(0, reasoningStart) + reasoning + generated.slice(reasoningEnd);
  generated += `

// Deprecated: use the SAMLAttributeMappingTarget-prefixed constants in new code.
const (
	Subject      SAMLAttributeMappingTarget = SAMLAttributeMappingTargetSubject
	Username     SAMLAttributeMappingTarget = SAMLAttributeMappingTargetUsername
	Email        SAMLAttributeMappingTarget = SAMLAttributeMappingTargetEmail
	DisplayName  SAMLAttributeMappingTarget = SAMLAttributeMappingTargetDisplayName
	Role         SAMLAttributeMappingTarget = SAMLAttributeMappingTargetRole
	Organization SAMLAttributeMappingTarget = SAMLAttributeMappingTargetOrganization
	Team         SAMLAttributeMappingTarget = SAMLAttributeMappingTargetTeam
	Project      SAMLAttributeMappingTarget = SAMLAttributeMappingTargetProject
)

// Deprecated: use the DockerContainerPortInputProtocol-prefixed constants in new code.
const (
	TCP DockerContainerPortInputProtocol = DockerContainerPortInputProtocolTCP
	UDP DockerContainerPortInputProtocol = DockerContainerPortInputProtocolUDP
)

// Deprecated: use WorkbenchMessageDeltaEventRoleAssistant in new code.
const Assistant WorkbenchMessageDeltaEventRole = WorkbenchMessageDeltaEventRoleAssistant

// Deprecated: use the ScopeGrantEffect-prefixed constants in new code.
const (
	Allow ScopeGrantEffect = ScopeGrantEffectAllow
	Deny  ScopeGrantEffect = ScopeGrantEffectDeny
)

// Deprecated: use the MarketplaceAdvisorySeverity-prefixed constants in new code.
const (
	Critical MarketplaceAdvisorySeverity = MarketplaceAdvisorySeverityCritical
	High     MarketplaceAdvisorySeverity = MarketplaceAdvisorySeverityHigh
	Medium   MarketplaceAdvisorySeverity = MarketplaceAdvisorySeverityMedium
	Low      MarketplaceAdvisorySeverity = MarketplaceAdvisorySeverityLow
)
`;
  // oapi-codegen changes enum prefixes when another schema introduces the same
  // value. Preserve both published spellings without redeclaring generated ones.
  for (const [typeName, values] of [
    ["IdentityProviderUserMetadataProtocol", [["OIDC", "oidc"], ["Proxy", "proxy"], ["Saml", "saml"]]],
    ["PersonalEnvironmentTemplateMode", [["ObserveOnly", "observe-only"], ["RestrictedWrite", "restricted-write"]]],
    ["PersonalGuestRuntimeInputKind", [["Browser", "browser"], ["Docker", "docker"], ["Execute", "execute"], ["FileRead", "file_read"], ["FileWrite", "file_write"]]],
    ["ExportDeliveryDocumentParamsFormat", [["JSON", "json"], ["Yaml", "yaml"]]],
    ["DeliveryDraftServiceServiceKind", [["ExternalService", "external_service"], ["HelmRelease", "helm_release"], ["Job", "job"], ["KubernetesWorkload", "kubernetes_workload"]]],
    ["PersonalAgentAccessDecisionStatus", [["Approved", "approved"], ["Rejected", "rejected"], ["Revoked", "revoked"]]],
    ["PersonalEnvironmentTemplateBackend", [["Container", "container"], ["VM", "vm"]]],
    ["PersonalWorkspaceRuntimeBrowserControlOwner", [["Agent", "agent"], ["Released", "released"], ["User", "user"]]],
    ["ListAIGatewayRelayUpstreamsParamsStatus", [["Active", "active"], ["Degraded", "degraded"], ["Disabled", "disabled"]]],
    ["DeliveryPlanStatus", [["Draft", "draft"], ["WaitingApproval", "waiting_approval"], ["Confirming", "confirming"], ["Confirmed", "confirmed"]]],
    ["RuntimeNetworkUsageScope", [["NetworkNamespace", "network_namespace"], ["Process", "process"], ["Unavailable", "unavailable"]]],
    // Rejected already belongs to the published PersonalAgentAccessDecisionStatus.
    ["WorkflowRequestReceiptSubmissionStatus", [["Accepted", "accepted"], ["Unknown", "unknown"]]],
    ["WorkbenchAgentStatusEventStatus", [["Cancelled", "cancelled"], ["Failed", "failed"], ["Queued", "queued"], ["Running", "running"], ["Succeeded", "succeeded"]]],
  ]) {
    for (const [suffix, value] of values) {
      for (const name of [suffix, `${typeName}${suffix}`]) {
        if (!new RegExp(`\\b${name}\\s+${typeName}\\s*=`).test(generated)) {
          generated += `\n// Retained for Go SDK source compatibility.\nconst ${name} ${typeName} = "${value}"\n`;
        }
      }
    }
  }
  // Action profiles introduce read-only as another enum. Keep published cache constants stable.
  for (const typeName of [
    "CreateAIGatewayAnthropicMessageParamsXSohaCacheMode",
    "CreateAIGatewayCohereRerankParamsXSohaCacheMode",
    "CreateAIGatewayGeminiGenerateContentParamsXSohaCacheMode",
    "CreateAIGatewayGeminiInteractionsParamsXSohaCacheMode",
    "CreateAIGatewayGeminiStreamGenerateContentParamsXSohaCacheMode",
    "CreateAIGatewayOpenAIAudioSpeechParamsXSohaCacheMode",
    "CreateAIGatewayOpenAIAudioTranscriptionParamsXSohaCacheMode",
    "CreateAIGatewayOpenAIAudioTranslationParamsXSohaCacheMode",
    "CreateAIGatewayOpenAIChatCompletionParamsXSohaCacheMode",
    "CreateAIGatewayOpenAICompatibleProviderAudioSpeechParamsXSohaCacheMode",
    "CreateAIGatewayOpenAICompatibleProviderAudioTranscriptionParamsXSohaCacheMode",
    "CreateAIGatewayOpenAICompatibleProviderAudioTranslationParamsXSohaCacheMode",
    "CreateAIGatewayOpenAICompatibleProviderChatCompletionParamsXSohaCacheMode",
    "CreateAIGatewayOpenAICompatibleProviderEmbeddingParamsXSohaCacheMode",
    "CreateAIGatewayOpenAICompatibleProviderImageEditParamsXSohaCacheMode",
    "CreateAIGatewayOpenAICompatibleProviderImageGenerationParamsXSohaCacheMode",
    "CreateAIGatewayOpenAICompatibleProviderImageVariationParamsXSohaCacheMode",
    "CreateAIGatewayOpenAICompatibleProviderResponseParamsXSohaCacheMode",
    "CreateAIGatewayOpenAIEmbeddingParamsXSohaCacheMode",
    "CreateAIGatewayOpenAIImageEditParamsXSohaCacheMode",
    "CreateAIGatewayOpenAIImageGenerationParamsXSohaCacheMode",
    "CreateAIGatewayOpenAIImageVariationParamsXSohaCacheMode",
    "CreateAIGatewayOpenAIResponseParamsXSohaCacheMode",
    "SohaCacheModeHeader"
]) {
    for (const [suffix, value] of [["Bypass", "bypass"], ["Default", "default"], ["ReadOnly", "read-only"], ["Refresh", "refresh"]]) {
      const name = `${typeName}${suffix}`;
      if (!new RegExp(`\\b${name}\\s+${typeName}\\s*=`).test(generated)) {
        generated += `\n// Retained for Go SDK source compatibility.\nconst ${name} ${typeName} = "${value}"\n`;
      }
    }
  }
  for (const [suffix, value] of [["Bypass", "bypass"], ["Hit", "hit"], ["Miss", "miss"], ["StaleHit", "stale_hit"], ["Write", "write"], ["WriteSkipped", "write_skipped"]]) {
    const name = `LLMCallLogCacheStatus${suffix}`;
    if (!new RegExp(`\\b${name}\\s+LLMCallLogCacheStatus\\s*=`).test(generated)) {
      generated += `\n// Retained for Go SDK source compatibility.\nconst ${name} LLMCallLogCacheStatus = "${value}"\n`;
    }
  }
  await writeFile(url, generated);
}

async function writeGoCompatibilitySpec() {
  const specUrl = new URL("openapi/soha-api.yaml", root);
  const spec = parse(await readFile(specUrl, "utf8"));
  applyGoCompatibility(spec);

  const dir = await mkdtemp(join(tmpdir(), "opensoha-contracts-"));
  const path = join(dir, "soha-api.go.yaml");
  await writeFile(path, stringify(spec));
  return { dir, path };
}

function applyGoCompatibility(spec) {
  const schemas = spec?.components?.schemas ?? {};

  // Keep Rejected available for the published PersonalAgentAccessDecisionStatus.
  schemas.WorkflowRequestReceipt.properties.submissionStatus["x-enum-varnames"] = [
    "WorkflowRequestReceiptSubmissionStatusAccepted",
    "WorkflowRequestReceiptSubmissionStatusUnknown",
    "WorkflowRequestReceiptSubmissionStatusRejected",
  ];

  // Keep the published full-input request-body alias and its string Content.
  // Partial HTTP updates have a separate schema used explicitly by new consumers.
  const proxyBody = spec.paths?.["/network-access/proxy-instances/{instanceID}/configuration"]?.put?.requestBody?.content?.["application/json"];
  if (proxyBody) proxyBody.schema = { $ref: "#/components/schemas/NetworkProxyConfigurationInput" };

  anchorGoSdkSchemas(spec, [
    "KubernetesResourceAgentCreateDocument",
    "KubernetesResourceAgentCreateRequest",
    "KubernetesResourceAgentCreateResult",
    "KubernetesResourceAgentPreflightItem",
    "KubernetesResourceAgentPreflightResult",
    "KubernetesResourceStreamEvent",
    "KubernetesResourceGraphEnvelope",
    "KubernetesSecurityPostureEnvelope",
    "KubernetesResourceUpdateAnalysis",
    "ComputeTaskStreamEvent",
    "ObservabilityMetricDataSource",
    "ObservabilityMetricDataSourceListEnvelope",
    "ManifestPackage",
    "ManifestPackageInput",
    "ManifestPackagePage",
    "ManifestPackageEnvelope",
    "ManifestPackagePageEnvelope",
    "BuildpacksExecutionSpec",
    "ExternalPipelineExecutionSpec",
    "ExternalPipelineArtifactReport",
    "ExternalPipelineRun",
    "DeliveryBatchAssessmentInput",
    "DeliveryBatchAssessment",
    "ObservabilityMetricAssessmentInput",
    "WorkbenchProfessionalCapabilityInput",
    "NetworkProxyInstanceInput",
    "NetworkProxyConfigurationInput",
    "NetworkProxyConfigurationPatchInput",
  ]);

  const riskLevel = schemas.RiskLevel;
  if (isObject(riskLevel)) {
    riskLevel["x-go-type"] = "string";
  }
  const runtimeConfigValue = schemas.RuntimeConfigValue;
  if (isObject(runtimeConfigValue)) {
    runtimeConfigValue["x-go-type"] = "any";
  }
  // Natural JSON maps/lists; constraints remain in the public schema and at the
  // template validation boundary, as for RuntimeConfigValue.
  schemas.TemplateParameterValue["x-go-type"] = "any";
  // These branches only validate field presence. The generated union marshaler
  // adds empty workflow fields to inline batches, violating the public oneOf.
  delete schemas.DeliveryBatchInput.oneOf;
  // Typed patch value branches validate presence only. Keep pointer fields and
  // public oneOf validation; a generated union marshaler injects unused values.
  delete schemas.DockerConfigurationChange.oneOf;
  const runtimeConfigItem = schemas.RuntimeConfigItem;
  if (isObject(runtimeConfigItem)) {
    // oapi-codegen v2.7 cannot model OpenAPI 3.1 conditional validation.
    delete runtimeConfigItem.allOf;
  }
  for (const name of ["NetworkVPNProfileConfig", "NetworkVPNSelectionPolicyConfig", "NetworkVPNConnectionIntentInput"]) {
    // Keep strict 3.1 conditionals in the public spec. oapi-codegen otherwise
    // collapses these structured request DTOs to interface{}.
    if (isObject(schemas[name])) delete schemas[name].allOf;
  }
  const logQuery = schemas.LogQuery;
  if (isObject(logQuery)) {
    // Keep the Go pointer guard while public OpenAPI and TypeScript require a selector.
    delete logQuery.allOf;
  }

  for (const [schemaName, propertyName] of [
    ["ObservabilityDataSource", "credentialKeys"],
    ["ObservabilityDataSourceInput", "clearCredentialKeys"],
  ]) {
    const items = schemas?.[schemaName]?.properties?.[propertyName]?.items;
    if (isObject(items)) {
      // Keep the published Go enum types while the public contract accepts provider-defined keys.
      items.enum = ["bearer_token", "username", "password"];
      delete items.pattern;
      delete items.maxLength;
    }
  }

  setPropertyGoType(schemas, "PluginManifest", "type", "string");
  setPropertyGoType(schemas, "InstalledPlugin", "status", "string");
  for (const propertyName of ["description", "enabled", "configuration"]) {
    keepOptionalPropertyPointer(schemas, "SystemIntegrationUpdateRequest", propertyName);
  }

  for (const schema of Object.values(schemas)) {
    markOptionalPointerFields(schema, schemas);
  }
  // A missing monitoring sample is distinct from zero, including required nullable fields.
  for (const name of ["ResourceMonitoringQuantity", "ResourceMonitoringPoint", "ResourceMonitoringSeries", "ResourceMonitoringNode", "ResourceMonitoringView"]) {
    for (const property of Object.values(schemas[name]?.properties ?? {})) {
      if (property.nullable && property.type === "number") property["x-go-type"] = "*float64";
    }
  }
  for (const property of ["weight", "totalWeight", "mirrorPercent"]) {
    keepOptionalPropertyPointer(schemas, "KubernetesTopologyGraphEdge", property);
  }
  keepOptionalPropertyPointer(schemas, "KubernetesTopologyCondition", "observedGeneration");
  keepOptionalPropertyPointer(schemas, "KubernetesTopologyRule", "priority");
  // Named invocation scopes refine validation without changing the published map DTO.
  schemas.ApprovalRequest.properties.resourceScope["x-go-type-skip-optional-pointer"] = true;
}

function anchorGoSdkSchemas(spec, schemaNames) {
  spec.paths ??= {};
  schemaNames.forEach((schemaName, index) => {
    spec.paths[`/__go-sdk-model-anchors/${index}`] = {
      get: {
        operationId: `anchorGoSdkModel${index}`,
        responses: {
          "200": {
            description: "Go SDK model generation anchor.",
            content: {
              "application/json": {
                schema: { $ref: `#/components/schemas/${schemaName}` },
              },
            },
          },
        },
      },
    };
  });
}

function setPropertyGoType(schemas, schemaName, propertyName, typeName) {
  const property = schemas?.[schemaName]?.properties?.[propertyName];
  if (isObject(property)) {
    property["x-go-type"] = typeName;
  }
}

function keepOptionalPropertyPointer(schemas, schemaName, propertyName) {
  const property = schemas?.[schemaName]?.properties?.[propertyName];
  if (isObject(property)) {
    property["x-go-type-skip-optional-pointer"] = false;
  }
}

function markOptionalPointerFields(schema, schemas) {
  if (!isObject(schema)) {
    return;
  }

  const required = new Set(Array.isArray(schema.required) ? schema.required : []);
  const properties = isObject(schema.properties) ? schema.properties : {};
  for (const [name, property] of Object.entries(properties)) {
    if (!isObject(property)) {
      continue;
    }
    if (!required.has(name) && shouldKeepOptionalPointer(property, schemas)) {
      property["x-go-type-skip-optional-pointer"] = false;
    }
    markOptionalPointerFields(property, schemas);
  }

  if (isObject(schema.items)) {
    markOptionalPointerFields(schema.items, schemas);
  }
  if (isObject(schema.additionalProperties)) {
    markOptionalPointerFields(schema.additionalProperties, schemas);
  }
  for (const composition of ["allOf", "anyOf", "oneOf"]) {
    const entries = schema[composition];
    if (Array.isArray(entries)) {
      for (const entry of entries) {
        markOptionalPointerFields(entry, schemas);
      }
    }
  }
}

function shouldKeepOptionalPointer(property, schemas) {
  if (property["x-go-pointer"] === true) {
    return true;
  }
  if (property.type === "string" && property.format === "date-time") {
    return true;
  }
  if (property.$ref) {
    const refName = getRefName(property.$ref);
    if (refName === "JSONSchema") {
      return false;
    }
    const refSchema = schemas[refName];
    return isSchemaObject(refSchema);
  }
  if (property.type === "object" && (isObject(property.properties) || property.additionalProperties === false)) {
    return true;
  }
  return false;
}

function getRefName(ref) {
  return ref.split("/").at(-1);
}

function isSchemaObject(schema) {
  return isObject(schema) && (schema.type === "object" || isObject(schema.properties));
}

function isObject(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function run(command, args) {
  const result = spawnSync(command, args, {
    cwd: fileURLToPath(root),
    encoding: "utf8",
    stdio: "pipe",
  });
  const output = `${result.stdout ?? ""}${result.stderr ?? ""}`;
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed\n${output}`);
  }
  if (output.trim()) {
    console.error(output.trim());
  }
}
