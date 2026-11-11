import { readFile } from "node:fs/promises";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { parse } from "yaml";
import { fixtureSchemaNames, validateFixtureDirectory, formatAjvErrors } from "./validate-fixtures.mjs";

const root = new URL("..", import.meta.url);
const openapiPath = "openapi/soha-api.yaml";

const requiredJsonSchemas = [
  { file: "agent/dsh-runtime.schema.json", fixtureDir: "fixtures/json-schema/dsh-runtime" },
  { file: "runner/docker-project-identity.schema.json", fixtureDir: "fixtures/json-schema/docker-project-identity" },
  { file: "runner/docker-container-identity.schema.json", fixtureDir: "fixtures/json-schema/docker-container-identity" },
  { file: "runner/docker-runtime-read.schema.json", fixtureDir: "fixtures/json-schema/docker-runtime-read" },
  { file: "runner/docker-runtime-read-result.schema.json", fixtureDir: "fixtures/json-schema/docker-runtime-read-result" },
  { file: "endpointcontrol/protocol.schema.json", fixtureDir: "fixtures/json-schema/endpoint-controls" },
  {
    file: "capabilities/cluster-capability-matrix.schema.json",
    fixtureDir: "fixtures/json-schema/cluster-capability-matrix",
  },
  { file: "profiles/agent-profile.schema.json", fixtureDir: "fixtures/json-schema/agent-profile" },
  { file: "presets/mcp-preset.schema.json", fixtureDir: "fixtures/json-schema/mcp-preset" },
  { file: "governance/approval-request.schema.json", fixtureDir: "fixtures/json-schema/approval-request" },
  { file: "governance/audit-log.schema.json", fixtureDir: "fixtures/json-schema/audit-log" },
  { file: "agent/agent-protocol.schema.json", fixtureDir: "fixtures/json-schema/agent-protocol" },
  { file: "agent/agent-provider-definition.schema.json", fixtureDir: "fixtures/json-schema/agent-provider-definition" },
  { file: "agent/agent-provider-registry.schema.json", fixtureDir: "fixtures/json-schema/agent-provider-registry" },
  { file: "agent/agent-run-step.schema.json", fixtureDir: "fixtures/json-schema/agent-run-step" },
  { file: "agent/agent-observation.schema.json", fixtureDir: "fixtures/json-schema/agent-observation" },
  { file: "agent/agent-checkpoint.schema.json", fixtureDir: "fixtures/json-schema/agent-checkpoint" },
  { file: "agent/environment-lease.schema.json", fixtureDir: "fixtures/json-schema/environment-lease" },
  { file: "agent/environment-snapshot.schema.json", fixtureDir: "fixtures/json-schema/environment-snapshot" },
  { file: "agent/personal-environment-request.schema.json", fixtureDir: "fixtures/json-schema/personal-environment-request" },
  { file: "agent/personal-environment-result.schema.json", fixtureDir: "fixtures/json-schema/personal-environment-result" },
  { file: "agent/personal-environment-export.schema.json", fixtureDir: "fixtures/json-schema/personal-environment-export" },
  { file: "agent/personal-environment-preflight-request.schema.json", fixtureDir: "fixtures/json-schema/personal-environment-preflight-request" },
  { file: "agent/personal-environment-preflight-result.schema.json", fixtureDir: "fixtures/json-schema/personal-environment-preflight-result" },
  { file: "agent/personal-guest-binding.schema.json", fixtureDir: "fixtures/json-schema/personal-guest-binding" },
  { file: "agent/personal-guest-enrollment.schema.json", fixtureDir: "fixtures/json-schema/personal-guest-enrollment" },
  { file: "agent/personal-guest-heartbeat.schema.json", fixtureDir: "fixtures/json-schema/personal-guest-heartbeat" },
  { file: "agent/personal-guest-renewal.schema.json", fixtureDir: "fixtures/json-schema/personal-guest-renewal" },
  { file: "agent/personal-guest-data-plan.schema.json", fixtureDir: "fixtures/json-schema/personal-guest-data-plan" },
  { file: "agent/personal-guest-provisioning.schema.json", fixtureDir: "fixtures/json-schema/personal-guest-provisioning" },
  { file: "agent/personal-guest-runtime.schema.json", fixtureDir: "fixtures/json-schema/personal-guest-runtime" },
  { file: "agent/personal-guest-runtime-result.schema.json", fixtureDir: "fixtures/json-schema/personal-guest-runtime-result" },
  { file: "agent/personal-guest-tools.schema.json", fixtureDir: "fixtures/json-schema/personal-guest-tools" },
  { file: "agent/personal-browser-network-response.schema.json", fixtureDir: "fixtures/json-schema/personal-browser-network-response" },
  { file: "agent/personal-browser-network-request.schema.json", fixtureDir: "fixtures/json-schema/personal-browser-network-request" },
  { file: "agent/agent-provider-runtime-event.schema.json", fixtureDir: "fixtures/json-schema/agent-provider-runtime-event" },
  { file: "evaluation/evaluation-dataset.schema.json", fixtureDir: "fixtures/json-schema/evaluation-dataset" },
  { file: "evaluation/evaluation-run.schema.json", fixtureDir: "fixtures/json-schema/evaluation-run" },
  { file: "evaluation/evaluation-result.schema.json", fixtureDir: "fixtures/json-schema/evaluation-result" },
  { file: "knowledge/knowledge-base.schema.json", fixtureDir: "fixtures/json-schema/knowledge-base" },
  { file: "knowledge/knowledge-source.schema.json", fixtureDir: "fixtures/json-schema/knowledge-source" },
  { file: "knowledge/knowledge-search.schema.json", fixtureDir: "fixtures/json-schema/knowledge-search" },
  { file: "knowledge/context-envelope.schema.json", fixtureDir: "fixtures/json-schema/context-envelope" },
  { file: "knowledge/connector-definition.schema.json", fixtureDir: "fixtures/json-schema/connector-definition" },
  { file: "knowledge/connector-checkpoint.schema.json", fixtureDir: "fixtures/json-schema/connector-checkpoint" },
  { file: "knowledge/source-import-policy.schema.json", fixtureDir: "fixtures/json-schema/source-import-policy" },
  { file: "knowledge/ingestion-job.schema.json", fixtureDir: "fixtures/json-schema/ingestion-job" },
  { file: "knowledge/ingestion-stage.schema.json", fixtureDir: "fixtures/json-schema/ingestion-stage" },
  { file: "knowledge/index-publish.schema.json", fixtureDir: "fixtures/json-schema/index-publish" },
  { file: "knowledge/retrieval-trace.schema.json", fixtureDir: "fixtures/json-schema/retrieval-trace" },
  { file: "knowledge/graph-revision.schema.json", fixtureDir: "fixtures/json-schema/graph-revision" },
  { file: "agent/fleet-target.schema.json", fixtureDir: "fixtures/json-schema/fleet-target" },
  { file: "agent/provider-rollout.schema.json", fixtureDir: "fixtures/json-schema/provider-rollout" },
  { file: "agent/conformance-run.schema.json", fixtureDir: "fixtures/json-schema/conformance-run" },
  { file: "agent/environment-template.schema.json", fixtureDir: "fixtures/json-schema/environment-template" },
  { file: "agent/multi-agent-plan.schema.json", fixtureDir: "fixtures/json-schema/multi-agent-plan" },
  { file: "evaluation/executor-profile.schema.json", fixtureDir: "fixtures/json-schema/executor-profile" },
  { file: "evaluation/sample-attempt.schema.json", fixtureDir: "fixtures/json-schema/sample-attempt" },
  { file: "evaluation/replay-plan.schema.json", fixtureDir: "fixtures/json-schema/replay-plan" },
  { file: "evaluation/gate-policy.schema.json", fixtureDir: "fixtures/json-schema/gate-policy" },
  { file: "evaluation/gate-decision.schema.json", fixtureDir: "fixtures/json-schema/gate-decision" },
  { file: "evaluation/feedback-sample.schema.json", fixtureDir: "fixtures/json-schema/feedback-sample" },
  { file: "memory/memory-record.schema.json", fixtureDir: "fixtures/json-schema/memory-record" },
  { file: "memory/memory-policy.schema.json", fixtureDir: "fixtures/json-schema/memory-policy" },
  {
    file: "runner/operation-lifecycle.schema.json",
    fixtureDir: "fixtures/json-schema/runner-operation-lifecycle",
  },
  { file: "projects/project-manifest.schema.json", fixtureDir: "fixtures/json-schema/project-manifest" },
  { file: "mcp/gateway-manifest.schema.json", fixtureDir: "fixtures/json-schema/gateway-manifest" },
  {
    file: "mcp/gateway-capability-baseline.schema.json",
    fixtureDir: "fixtures/json-schema/gateway-capability-baseline",
  },
  { file: "skills/skill-manifest.schema.json", fixtureDir: "fixtures/json-schema/skill-manifest" },
  { file: "plugins/companion-pack.schema.json", fixtureDir: "fixtures/json-schema/companion-pack" },
  { file: "plugins/companion-provenance.schema.json", fixtureDir: "fixtures/json-schema/companion-provenance" },
  { file: "plugins/plugin-manifest.schema.json", fixtureDir: "fixtures/json-schema/plugin-manifest" },
  {
    file: "plugins/observability-log-query-v1.schema.json",
    fixtureDir: "fixtures/json-schema/observability-log-query-v1",
  },
  {
    file: "plugins/marketplace-catalog.schema.json",
    fixtureDir: "fixtures/json-schema/marketplace-catalog",
  },
  { file: "events/event-envelope.schema.json", fixtureDir: "fixtures/json-schema/event-envelope" },
  { file: "auth/token-claims.schema.json", fixtureDir: "fixtures/json-schema/token-claims" },
  { file: "auth/permission-catalog.schema.json", fixtureDir: "fixtures/json-schema/permission-catalog" },
  { file: "connectors/connector-action.schema.json", fixtureDir: "fixtures/json-schema/connector-action" },
  {
    file: "connectors/connector-event-envelope.schema.json",
    fixtureDir: "fixtures/json-schema/connector-event-envelope",
  },
  {
    file: "network/network-runtime-protocol.schema.json",
    fixtureDir: "fixtures/json-schema/network-runtime-protocol",
  },
  {
    file: "network/network-ingest-event.schema.json",
    fixtureDir: "fixtures/json-schema/network-ingest-event",
  },
  {
    file: "network/network-radius-accounting.schema.json",
    fixtureDir: "fixtures/json-schema/network-radius-accounting",
  },
];

const requiredOpenapiFixtureSchemas = [
  "DockerRuntimeContainerBinding",
  "DockerRuntimeVolume",
  "DockerRuntimeFileEntry",
  "DockerRuntimeReadResult",
  "DockerRuntimeReadEnvelope",

  "EnvironmentServiceReferenceInput",
  "EnvironmentServiceReferenceDeleteInput",
  "EnvironmentServiceReference",
  "EnvironmentServiceReferenceEnvelope",
  "EnvironmentServiceReferenceSummary",
  "EnvironmentServiceReferencePage",
  "EnvironmentServiceReferenceResolvedTarget",
  "EnvironmentServiceReferenceConsumer",
  "EnvironmentServiceReferenceConsumerPage",
  "DeliveryPlanServiceReferenceImpact",
  "TemplateUpdatePreviewInput",
  "TemplateUpdatePolicyInput",
  "TemplateMigrationPolicy",
  "TemplateUpdateBatchInput",
  "PersonalDockerEnvironmentPolicy",
  "PersonalVMEnvironmentPolicy",
  "PersonalEnvironmentQuota",
  "PersonalAssistantArtifactListEnvelope",
  "PersonalAssistantUpdateRequest",
  "PersonalEnvironmentActionRequest",
  "PersonalEnvironmentDataDeletionRequest",
  "PersonalEnvironmentDataExportRequest",
  "PersonalGuestToolsInput",
  "PersonalGuestRuntimeInput",
  "PersonalGuestRuntimeEnvelope",
  "PersonalWorkspaceRuntimeEnvelope",
  "PersonalWorkspaceRuntime",
  "PersonalEnvironment",
  "PersonalEnvironmentEnvelope",
  "PersonalEnvironmentUsage",
  "PersonalEnvironmentTemplate",
  "PersonalEnvironmentTemplateListEnvelope",
  "WorkbenchMainSessionRequest",
  "ClusterAgentUpgradeInput",
  "ToolCapability",
  "ToolInvocationRequest",
  "ToolInvocationResult",
  "CapabilityValueSemantic",
  "CapabilityPlan",
  "CapabilityTaskInput",
  "CapabilityTask",
  "CapabilityAssessment",
  "DeliveryBatchAssessmentInput",
  "ObservabilityMetricAssessmentInput",
  "VirtualMachineCreateInput",
  "VirtualizationCapacityPolicyInput",
  "VirtualMachineGuestAccessInput",
  "SecretGenerateSSHKeyRequest",
  "SecretReferenceUsage",
  "VirtualMachineAccessBindingInput",
  "VirtualMachineSSHSessionInput",
  "VirtualMachineSSHCommandInput",
  "AgentSSHSessionRequest",
  "AgentSSHCommandRequest",
  "AgentSSHCommandResult",
  "VMSSHCredentialRedemptionInput",
  "VirtualMachineSSHCommand",
  "VirtualMachineSSHFrame",
  "VirtualizationCapacityInput",
  "VirtualizationCapacityResult",
  "VirtualizationWorkerPoolInput",
  "VirtualizationWorkerCreateInput",
  "VirtualizationWorkerPoolListEnvelope",
  "WorkbenchInspectionTaskInput",
  "WorkbenchInspectionTrigger",
  "DockerQuickCreateHostInput",
  "DeliveryBatchAssessment",
  "DeliveryAccessResult",
  "ApprovalRequest",
  "CapabilityPlanValidation",
  "DeliveryDocument",
  "DeliveryTemplateSourceInput",
  "WorkflowCatalogPage",
  "BuildTriggerRequest",
  "HelmDeliveryConfiguration",
  "HelmDeliverySnapshot",
  "DockerDeliveryConfiguration",
  "DockerDeliverySnapshot",
  "DockerProjectInput",
  "ReleaseTargetInput",
  "DeliveryTargetInput",
  "DeliveryWorkflowDefinition",
  "DeliveryBatchInput",
  "DeliveryWorkflowInput",
  "DeliveryPlanApprovalInput",
  "ManifestRevisionInput",
  "DeliveryBlueprintInput",
  "ServiceDeploymentTemplateInput",
  "DeploymentTemplatePreviewInput",
  "ServiceDeploymentTemplateBinding",

  "AgentChatInput",
  "WorkbenchModelPreferences",
  "WorkbenchModelOption",
  "WorkbenchContextUsage",
  "PasswordLoginRequest",
  "UpdateProfileRequest",
  "ChangePasswordRequest",
  "PasswordChangeResult",
  "AuthResultEnvelope",
  "DesktopAuthAttemptCreateRequest",
  "DesktopAuthAttemptEnvelope",
  "DesktopAuthAttemptExchangeRequest",
  "ApplicationInput",
  "ApplicationWorkflowInput",
  "BuildTemplateInput",
  "WorkflowTemplateInput",
  "TemplatePublishInput",
  "ApplicationRuntimeEnvironment",
  "ExecutionCallbackRequest",
  "DeliveryDraftInput",
  "KubernetesServiceImportRequest",
  "KubernetesImportCandidateListEnvelope",
  "KubernetesServiceImportResultEnvelope",
  "HelmReleaseImportRequest",
  "HelmReleaseImportResultEnvelope",
  "ManifestPackage",
  "ManifestSourceUpdateInput",
  "ManifestDeploymentEnvelope",
  "ManifestExecutionTaskPayload",
  "ManifestServiceTemplateInputs",
  "AnnouncementReceiptPageEnvelope",
  "DockerContainerStartInput",
  "DockerOperationCallbackRequest",
  "DockerHostAgentEnrollmentRequest",
  "DockerHostAgentCredentialsEnvelope",
  "AgentRunCallbackRequest",
  "AIGatewayManifestEnvelope",
  "LLMUpstreamTestResultEnvelope",
  "PluginManifest",
  "CompanionPackManifest",
  "CompanionInteractionRequest",
  "CompanionInteractionReceiptEnvelope",
  "CompanionProfileEnvelope",
  "PluginProvenanceStatement",
  "PersonalAccessTokenInput",
  "ServiceAccountInput",
  "SecretCreateRequest",
  "SecretMetadataEnvelope",
  "SecretRotateRequest",
  "SecretLeaseRedemptionEnvelope",
  "SoftwarePackage",
  "SoftwarePackageDownloadRecord",
  "SoftwarePackageURLImportRequest",
  "SoftwareStorage",
  "GovernanceStatusEnvelope",
  "MCPCapabilityListEnvelope",
  "CloudExtensionPointListEnvelope",
  "WorkbenchStreamEvent",
  "WorkbenchTaskIntent",
  "WorkbenchProfessionalCapabilityInput",
  "AgentRunResumeRequest",
  "AgentRunEventListEnvelope",
  "KnowledgeSourceDocument",
  "AgentProviderRegistryAcknowledgement",
  "KnowledgeIngestionJobInput",
  "EvaluationGateRequest",
  "ComputeOverviewEnvelope",
  "ComputeAccessSourceListEnvelope",
  "ComputeProviderListEnvelope",
  "ComputeResourceRelationListEnvelope",
  "ComputeTaskListEnvelope",
  "ComputeTaskStreamEvent",
  "ObservabilityGrafanaDashboardImportInput",
  "ObservabilityMetricDataSourceListEnvelope",
  "ObservabilityDashboardImportResultEnvelope",
  "ObservabilityDashboardPanelQueryInput",
  "ObservabilityContext",
  "ObservabilityQuerySnapshot",
  "ObservabilityQueryResultMeta",
  "ObservabilityServiceListEnvelope",
  "ObservabilityServiceEnvelope",
  "ObservabilityTopologyEnvelope",
  "ObservabilityMetricCatalogEnvelope",
  "LogQuery",
  "AlertRuleInput",
  "AlertRuleEnvelope",
  "AlertRuleTestResultEnvelope",
  "AlertRuleRunListEnvelope",
  "KubernetesResourceCreateRequest",
  "KubernetesResourceCreateScopeDecisionEnvelope",
  "KubernetesResourcePreflightEnvelope",
  "KubernetesResourceCreateResultEnvelope",
  "KubernetesResourceCreationReceipt",
  "KubernetesWorkloadSnapshotRequest",
  "KubernetesWorkloadSnapshotEnvelope",
  "KubernetesResourceAgentCreateRequest",
  "KubernetesClusterListEnvelope",
  "KubernetesPodListEnvelope",
  "KubernetesNetworkTopologyEnvelope",
  "KubernetesResourceSearchEnvelope",
  "KubernetesResourceStreamEvent",
  "AlertEventStreamSignal",
  "KubernetesResourceGraphEnvelope",
  "OperationalPlanEnvelope",
  "KubernetesSecurityPostureEnvelope",
  "IdentitySettingsEnvelope",
  "UpdateLoginProvidersSettingsRequest",
  "BrandingSettingsEnvelope",
  "UpdateBrandingSettingsRequest",
  "BrandingAssetUploadEnvelope",
  "UploadBrandingAssetRequest",
  "RuntimeConfigSnapshotEnvelope",
  "RuntimeConfigChangeRequest",
  "AgentInstallationEnvelope",
  "ErrorEnvelope",
  "RepositoryInput",
  "RepositoryAnalysisInput",
  "RepositoryAnalysis",
  "RegistryConnectionInput",
  "RegistryConnectionListEnvelope",
  "IdentityRuntimeCapabilityEnvelope",
  "IdentityApplication",
  "IdentityApplicationEnvelope",
  "IdentityApplicationLaunchListEnvelope",
  "PortalSecuritySummaryEnvelope",
  "PortalBootstrapEnvelope",
  "PortalApplicationListEnvelope",
  "PortalLaunchRequest",
  "PortalLaunchDecisionEnvelope",
  "BrowserHandoffEnvelope",
  "BrowserHandoffCompletionEnvelope",
  "OIDCClient",
  "OIDCClientInput",
  "OIDCClientSecretRevealEnvelope",
  "IdentityProvider",
  "SAMLMetadataInput",
  "IdentityErrorEnvelope",
  "IdentityOutpostClaimRequest",
  "IdentityOutpost",
  "IdentityOutpostInput",
  "IdentityProviderSetup",
  "IdentityOutpostDeployment",
  "IdentityApplicationOnboardingInput",
  "MFAWebAuthnRequestOptionsEnvelope",
  "MFAAdminResetRequest",
  "DirectoryRuntimeStatusEnvelope",
  "DirectoryEventListEnvelope",
  "DirectoryPersonLifecycle",
  "PermissionCatalogEnvelope",
  "AccessRoleInput",
  "MenuInput",
  "MenuEnvelope",
  "MenuListEnvelope",
  "ScopeGrantInput",
  "ScopeGrantEnvelope",
  "ScopeGrantListEnvelope",
  "AuthorizationDecision",
  "EndpointDevice",
  "EndpointDeviceRegistrationInput",
  "NetworkGateway",
  "NetworkGatewayInput",
  "NetworkMihomoProfile",
  "NetworkMihomoProfileInput",
  "NetworkConnectionOption",
  "NetworkNASBinding",
  "NetworkNASBindingInput",
  "NetworkLease",
  "NetworkAccessPolicy",
  "NetworkAccessPolicyInput",
  "NetworkConflictAnalysisRequest",
  "NetworkPolicyPreviewRequest",
  "NetworkPolicyPreviewResult",
  "NetworkPolicySnapshot",
  "NetworkResourceInput",
  "NetworkRuntimeEnrollmentInput",
  "NetworkRuntimeEnrollmentSecret",
  "NetworkSession",
  "NetworkSessionActionExecuteInput",
  "NetworkSessionActionInput",
  "NetworkSessionActionPlan",
  "NetworkSessionCommand",
  "NetworkSiteInput",
  "NetworkSiteProfileBinding",
  "NetworkSiteProfileBindingInput",
  "NetworkSpaceInput",
  "ResourceLease",
];

const requiredJsonExamples = [
  {
    file: "examples/cluster-capability-matrix/runtime-parity.json",
    schemaFile: "capabilities/cluster-capability-matrix.schema.json",
  },
  { file: "examples/agent-profile/k8s-sre-readonly.json", schemaFile: "profiles/agent-profile.schema.json" },
  { file: "examples/mcp-preset/k8s-readonly.json", schemaFile: "presets/mcp-preset.schema.json" },
  { file: "examples/governance/approval-request.json", schemaFile: "governance/approval-request.schema.json" },
  { file: "examples/governance/audit-log.json", schemaFile: "governance/audit-log.schema.json" },
  { file: "examples/gateway-manifest/minimal.json", schemaFile: "mcp/gateway-manifest.schema.json" },
  {
    file: "examples/gateway-capability-baseline/minimal.json",
    schemaFile: "mcp/gateway-capability-baseline.schema.json",
  },
  { file: "examples/token-claims/service-account.json", schemaFile: "auth/token-claims.schema.json" },
  { file: "auth/permission-catalog.json", schemaFile: "auth/permission-catalog.schema.json" },
  { file: "examples/plugin-manifest/skill-pack.json", schemaFile: "plugins/plugin-manifest.schema.json" },
  { file: "examples/plugin-manifest/connector.json", schemaFile: "plugins/plugin-manifest.schema.json" },
  { file: "examples/plugin-manifest/compute-provider.json", schemaFile: "plugins/plugin-manifest.schema.json" },
  { file: "examples/marketplace-catalog/default.json", schemaFile: "plugins/marketplace-catalog.schema.json" },
  { file: "examples/skill-manifest/k8s-sre.json", schemaFile: "skills/skill-manifest.schema.json" },
  {
    file: "examples/connector-event-envelope/feishu-message.json",
    schemaFile: "connectors/connector-event-envelope.schema.json",
  },
  {
    file: "examples/runner-operation-lifecycle/late-callback.json",
    schemaFile: "runner/operation-lifecycle.schema.json",
  },
];

const requiredOpenapiExamples = [
  { file: "examples/openapi/gateway-manifest-envelope.json", schemaName: "AIGatewayManifestEnvelope" },
  { file: "examples/openapi/tool-invocation-request.json", schemaName: "ToolInvocationRequest" },
  { file: "examples/openapi/tool-invocation-result-envelope.json", schemaName: "ToolInvocationResultEnvelope" },
  { file: "examples/openapi/agent-run-tool-call-request.json", schemaName: "AgentRunToolCallRequest" },
  { file: "examples/openapi/agent-tool-call-result-envelope.json", schemaName: "AgentToolCallResultEnvelope" },
  { file: "examples/openapi/cloud-extension-point-list-envelope.json", schemaName: "CloudExtensionPointListEnvelope" },
  { file: "examples/openapi/directory-runtime-status-envelope.json", schemaName: "DirectoryRuntimeStatusEnvelope" },
];

const requiredSdkEntrypoints = ["gen/go/sohaapi/types.go", "gen/ts/sohaapi/index.ts"];

const ajv = new Ajv2020({
  allErrors: true,
  strict: false,
  validateFormats: true,
});
addFormats(ajv);
ajv.addFormat("int64", true);

const jsonSchemaValidators = new Map();
let jsonSchemaFixtureCount = 0;
for (const contract of requiredJsonSchemas) {
  const text = await readFile(new URL(contract.file, root), "utf8");
  const parsed = JSON.parse(text);
  if (!parsed.$schema || !parsed.$id) {
    throw new Error(`${contract.file} must declare $schema and $id`);
  }

  const validate = ajv.compile(parsed);
  jsonSchemaValidators.set(contract.file, validate);
  jsonSchemaFixtureCount += await validateFixtureDirectory(new URL(contract.fixtureDir + "/", root), validate);
}

for (const file of requiredSdkEntrypoints) {
  const text = await readFile(new URL(file, root), "utf8");
  if (!text.includes("Code generated from OpenSoha contracts")) {
    throw new Error(`${file} must be a generated SDK entrypoint`);
  }
}

const openapiText = await readFile(new URL(openapiPath, root), "utf8");
const openapi = parse(openapiText);
const permissionDefinitions = await validatePermissionCatalog();
validateOpenapiStructure(openapi, permissionDefinitions);
const openapiFixtureCount = await validateOpenapiFixtures(openapi);
const jsonExampleCount = await validateJsonExamples();
const openapiExampleCount = await validateOpenapiExamples(openapi);

console.log(
  `validated ${requiredJsonSchemas.length} JSON schemas, ${jsonSchemaFixtureCount} JSON schema fixtures, ${jsonExampleCount} JSON examples, ${requiredSdkEntrypoints.length} SDK entrypoints, ${Object.keys(openapi.components.schemas).length} OpenAPI schemas, ${openapiFixtureCount} OpenAPI fixtures, ${openapiExampleCount} OpenAPI examples, and ${collectOperationIds(openapi).length} OpenAPI operations`,
);

async function validateOpenapiFixtures(openapi) {
  const openapiAjv = buildOpenapiAjv(openapi);
  let fixtureCount = 0;
  for (const schemaName of await fixtureSchemaNames(new URL("fixtures/openapi/", root), requiredOpenapiFixtureSchemas)) {
    const validate = getOpenapiSchemaValidator(openapiAjv, schemaName);
    fixtureCount += await validateFixtureDirectory(new URL(`fixtures/openapi/${schemaName}/`, root), validate);
  }
  return fixtureCount;
}

async function validateJsonExamples() {
  for (const example of requiredJsonExamples) {
    const validate = jsonSchemaValidators.get(example.schemaFile);
    if (!validate) {
      throw new Error(`${example.file} references unregistered JSON schema ${example.schemaFile}`);
    }
    await validateExampleFile(example.file, validate);
  }
  return requiredJsonExamples.length;
}

async function validatePermissionCatalog() {
  const catalog = JSON.parse(await readFile(new URL("auth/permission-catalog.json", root), "utf8"));
  const keys = catalog.permissions.map((permission) => permission.key);
  if (new Set(keys).size !== keys.length) {
    throw new Error("auth/permission-catalog.json contains duplicate permission keys");
  }
  const definitions = new Map(catalog.permissions.map((permission) => [permission.key, permission]));
  for (const permission of catalog.permissions) {
    for (const replacement of permission.replacedBy ?? []) {
      if (!definitions.has(replacement)) {
        throw new Error(`${permission.key} references unknown replacement ${replacement}`);
      }
    }
  }
  return definitions;
}

async function validateOpenapiExamples(openapi) {
  const openapiAjv = buildOpenapiAjv(openapi);
  for (const example of requiredOpenapiExamples) {
    const validate = getOpenapiSchemaValidator(openapiAjv, example.schemaName);
    await validateExampleFile(example.file, validate);
  }
  return requiredOpenapiExamples.length;
}

function buildOpenapiAjv(openapi) {
  const documentId = "https://contracts.opensoha.dev/openapi/soha-api.yaml";
  const openapiSchemaDocument = {
    $id: documentId,
    $schema: "https://json-schema.org/draft/2020-12/schema",
    components: {
      schemas: openapi.components.schemas,
    },
  };
  const openapiAjv = new Ajv2020({
    allErrors: true,
    strict: false,
    validateFormats: true,
  });
  addFormats(openapiAjv);
  openapiAjv.addFormat("int64", true);
  openapiAjv.addSchema(openapiSchemaDocument);
  openapiAjv.contractDocumentId = documentId;
  return openapiAjv;
}

function getOpenapiSchemaValidator(openapiAjv, schemaName) {
  const validate = openapiAjv.getSchema(`${openapiAjv.contractDocumentId}#/components/schemas/${schemaName}`);
  if (!validate) {
    throw new Error(`${openapiPath} is missing fixture/example-covered schema ${schemaName}`);
  }
  return validate;
}

async function validateExampleFile(file, validate) {
  const payload = JSON.parse(await readFile(new URL(file, root), "utf8"));
  const passed = validate(payload);
  if (!passed) {
    throw new Error(`${file} should be valid:\n${formatAjvErrors(validate.errors)}`);
  }
}

function validateOpenapiStructure(openapi, permissionDefinitions) {
  if (openapi?.openapi !== "3.1.0") {
    throw new Error(`${openapiPath} must use OpenAPI 3.1.0`);
  }
  if (!openapi.info?.title || !openapi.info?.version) {
    throw new Error(`${openapiPath} must declare info.title and info.version`);
  }
  if (!openapi.paths || Object.keys(openapi.paths).length === 0) {
    throw new Error(`${openapiPath} must declare paths`);
  }
  if (!openapi.components?.schemas || Object.keys(openapi.components.schemas).length === 0) {
    throw new Error(`${openapiPath} must declare components.schemas`);
  }

  const operationIds = collectOperationIds(openapi);
  if (operationIds.length === 0) {
    throw new Error(`${openapiPath} must declare operationId values`);
  }
  const duplicateOperationIds = operationIds.filter((id, index) => operationIds.indexOf(id) !== index);
  if (duplicateOperationIds.length > 0) {
    throw new Error(`duplicate operationId values: ${[...new Set(duplicateOperationIds)].join(", ")}`);
  }

  const invalidColonParams = Object.keys(openapi.paths).filter((path) => /\/:[A-Za-z]/.test(path));
  if (invalidColonParams.length > 0) {
    throw new Error(`OpenAPI paths must use {param} syntax: ${invalidColonParams.join(", ")}`);
  }

  validateComputeTaskCenterContract(openapi);
  validateRuntimeConfigContract(openapi);
  validateAuthProfileContract(openapi);
  validateNetworkAccessNACContract(openapi);
  validateReleasedScopeGrantOperations(openapi);

  const capabilityNames = new Set();
  const riskLevels = new Set(openapi.components.schemas.RiskLevel?.enum ?? []);

  for (const [path, pathItem] of Object.entries(openapi.paths)) {
    for (const [method, operation] of Object.entries(pathItem ?? {})) {
      if (!isHttpMethod(method)) {
        continue;
      }
      if (!operation.operationId) {
        throw new Error(`${method.toUpperCase()} ${path} is missing operationId`);
      }
      if (!operation.responses || Object.keys(operation.responses).length === 0) {
        throw new Error(`${method.toUpperCase()} ${path} is missing responses`);
      }
      validateCapabilityExtension(operation, `${method.toUpperCase()} ${path}`, capabilityNames, riskLevels, permissionDefinitions);
      validateKubernetesOperationPermissions(operation, `${method.toUpperCase()} ${path}`, path, permissionDefinitions);
      if (operation.operationId === "claimExecutionTask") {
        for (const status of ["200", "202"]) {
          const schema = operation.responses[status]?.headers?.["X-Soha-Manifest-Admission-Version"]?.schema;
          if (schema?.type !== "string" || JSON.stringify(schema.enum) !== '["1"]') {
            throw new Error(`claimExecutionTask ${status} must describe Manifest admission confirmation`);
          }
        }
      }
      if (["claimExecutionTask", "claimDockerOperation"].includes(operation.operationId)) {
        for (const status of ["202", "204"]) {
          if (!operation.responses[status]) {
            throw new Error(`${operation.operationId} must declare a ${status} response`);
          }
        }
      }
    }
  }
}

function validateNetworkAccessNACContract(openapi) {
  const operations = {
    "/network-access/connection-options": { get: "listCurrentNetworkConnectionOptions" },
    "/network-access/nas-bindings": { get: "listNetworkNASBindings", post: "createNetworkNASBinding" },
    "/network-access/nas-bindings/{bindingID}": { get: "getNetworkNASBinding", put: "updateNetworkNASBinding", delete: "deleteNetworkNASBinding" },
    "/network-access/site-profile-bindings": { get: "listNetworkSiteProfileBindings", post: "createNetworkSiteProfileBinding" },
    "/network-access/site-profile-bindings/{bindingID}": { get: "getNetworkSiteProfileBinding", put: "updateNetworkSiteProfileBinding", delete: "deleteNetworkSiteProfileBinding" },
    "/network-access/sessions": { get: "listNetworkSessions" },
    "/network-access/sessions/{sessionID}": { get: "getNetworkSession" },
    "/network-access/sessions/{sessionID}/actions/plan": { post: "planNetworkSessionAction" },
    "/network-access/sessions/{sessionID}/actions/execute": { post: "executeNetworkSessionAction" },
  };
  for (const [path, methods] of Object.entries(operations)) {
    for (const [method, operationId] of Object.entries(methods)) {
      if (openapi.paths?.[path]?.[method]?.operationId !== operationId) {
        throw new Error(`${method.toUpperCase()} ${path} must declare ${operationId}`);
      }
    }
  }
}

function validateAuthProfileContract(openapi) {
  const operations = [
    {
      path: "/auth/profile",
      method: "patch",
      operationId: "updateCurrentUserProfile",
      requestSchema: "UpdateProfileRequest",
      responseSchema: "UserProfileEnvelope",
      statuses: ["200", "400", "401", "409"],
    },
    {
      path: "/auth/profile/password",
      method: "post",
      operationId: "changeCurrentUserPassword",
      requestSchema: "ChangePasswordRequest",
      responseSchema: "PasswordChangeResult",
      statuses: ["200", "400", "401"],
    },
  ];
  for (const contract of operations) {
    const operation = openapi.paths?.[contract.path]?.[contract.method];
    const label = `${contract.method.toUpperCase()} ${contract.path}`;
    if (operation?.operationId !== contract.operationId) {
      throw new Error(`${label} must declare ${contract.operationId}`);
    }
    if (operation.requestBody?.content?.["application/json"]?.schema?.$ref !== `#/components/schemas/${contract.requestSchema}`) {
      throw new Error(`${label} must use ${contract.requestSchema}`);
    }
    if (operation.responses?.["200"]?.content?.["application/json"]?.schema?.$ref !== `#/components/schemas/${contract.responseSchema}`) {
      throw new Error(`${label} must return ${contract.responseSchema}`);
    }
    for (const status of contract.statuses) {
      if (!operation.responses?.[status]) {
        throw new Error(`${label} must declare ${status}`);
      }
    }
  }
}

function validateReleasedScopeGrantOperations(openapi) {
  const operations = {
    "/access/scope-grants": { get: "listScopeGrants", post: "createScopeGrant" },
    "/access/scope-grants/{scopeGrantID}": { put: "updateScopeGrant", delete: "deleteScopeGrant" },
  };
  for (const [path, methods] of Object.entries(operations)) {
    for (const [method, operationId] of Object.entries(methods)) {
      if (openapi.paths?.[path]?.[method]?.operationId !== operationId) {
        throw new Error(`${method.toUpperCase()} ${path} must preserve released operation ${operationId}`);
      }
    }
  }
}

function validateKubernetesOperationPermissions(operation, label, path, permissionDefinitions) {
  const isKubernetesOperation = path === "/clusters" || path.startsWith("/clusters/") || path.startsWith("/kubernetes/agent-installations/");
  const declaration = operation["x-soha-permissions"];
  if (!isKubernetesOperation) {
    return;
  }
  if (!declaration || typeof declaration !== "object" || Array.isArray(declaration)) {
    throw new Error(`${label} must declare x-soha-permissions`);
  }
  const modes = new Set(["all", "authenticated", "install-ticket", "resource", "subject"]);
  if (!modes.has(declaration.mode)) {
    throw new Error(`${label} x-soha-permissions.mode is invalid`);
  }
  const keys = declaration.keys ?? [];
  if (!Array.isArray(keys) || keys.some((key) => typeof key !== "string" || key.trim() !== key || key === "")) {
    throw new Error(`${label} x-soha-permissions.keys must contain trimmed strings`);
  }
  if (declaration.mode === "all" && keys.length === 0) {
    throw new Error(`${label} x-soha-permissions all mode requires keys`);
  }
  if (new Set(keys).size !== keys.length || [...keys].sort().some((key, index) => key !== keys[index])) {
    throw new Error(`${label} x-soha-permissions.keys must be unique and sorted`);
  }
  for (const key of keys) {
    const definition = permissionDefinitions.get(key);
    if (!definition) {
      throw new Error(`${label} x-soha-permissions references unknown permission ${key}`);
    }
    if (definition.status !== "active" || definition.assignable !== true) {
      throw new Error(`${label} x-soha-permissions references non-assignable permission ${key}`);
    }
  }
}

function validateCapabilityExtension(operation, label, capabilityNames, riskLevels, permissionDefinitions) {
  const capability = operation["x-soha-capability"];
  if (capability === undefined) {
    return;
  }
  if (!capability || typeof capability !== "object" || Array.isArray(capability)) {
    throw new Error(`${label} x-soha-capability must be an object`);
  }
  if (typeof capability.name !== "string" || !/^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+$/.test(capability.name)) {
    throw new Error(`${label} x-soha-capability.name must be a dotted capability name`);
  }
  if (capabilityNames.has(capability.name)) {
    throw new Error(`duplicate x-soha-capability.name: ${capability.name}`);
  }
  capabilityNames.add(capability.name);
  if (!riskLevels.has(capability.riskLevel)) {
    throw new Error(`${label} x-soha-capability.riskLevel must use RiskLevel`);
  }
  if (typeof capability.requiresApproval !== "boolean") {
    throw new Error(`${label} x-soha-capability.requiresApproval must be boolean`);
  }
  if (!Array.isArray(capability.permissionKeys) || capability.permissionKeys.length === 0) {
    throw new Error(`${label} x-soha-capability.permissionKeys must be a non-empty array`);
  }
  if (capability.permissionKeys.some((key) => typeof key !== "string" || key.trim() !== key || key === "")) {
    throw new Error(`${label} x-soha-capability.permissionKeys must contain non-empty trimmed strings`);
  }
  if (new Set(capability.permissionKeys).size !== capability.permissionKeys.length) {
    throw new Error(`${label} x-soha-capability.permissionKeys must not contain duplicates`);
  }
  const sortedPermissionKeys = [...capability.permissionKeys].sort();
  if (sortedPermissionKeys.some((key, index) => key !== capability.permissionKeys[index])) {
    throw new Error(`${label} x-soha-capability.permissionKeys must be sorted`);
  }
  for (const key of capability.permissionKeys) {
    const definition = permissionDefinitions.get(key);
    if (!definition) {
      throw new Error(`${label} x-soha-capability.permissionKeys references unknown permission ${key}`);
    }
    if (definition.status !== "active" || definition.assignable !== true) {
      throw new Error(`${label} x-soha-capability.permissionKeys references non-assignable permission ${key}`);
    }
  }
}

function validateRuntimeConfigContract(openapi) {
  const operations = {
    "/settings/runtime-config": ["get", "getRuntimeConfig"],
    "/settings/runtime-config/validate": ["post", "validateRuntimeConfig"],
    "/settings/runtime-config/apply": ["post", "applyRuntimeConfig"],
    "/settings/runtime-config/history": ["get", "listRuntimeConfigHistory"],
    "/settings/runtime-config/rollback": ["post", "rollbackRuntimeConfig"],
    "/settings/runtime-config/applications/{runtimeConfigApplicationID}": ["get", "getRuntimeConfigApplication"],
  };
  for (const [path, [method, operationId]] of Object.entries(operations)) {
    if (openapi.paths?.[path]?.[method]?.operationId !== operationId) {
      throw new Error(`${method.toUpperCase()} ${path} must declare ${operationId}`);
    }
  }

  const applyModes = openapi.components.schemas.RuntimeConfigApplyMode?.enum ?? [];
  for (const mode of ["hot", "reconfigure", "lifecycle", "restart"]) {
    if (!applyModes.includes(mode)) {
      throw new Error(`RuntimeConfigApplyMode must include ${mode}`);
    }
  }

  const changeRequestRequired = openapi.components.schemas.RuntimeConfigChangeRequest?.required ?? [];
  if (!changeRequestRequired.includes("expectedVersion")) {
    throw new Error("RuntimeConfigChangeRequest must require expectedVersion");
  }
  const changeRequired = openapi.components.schemas.RuntimeConfigChange?.required ?? [];
  if (changeRequired.includes("reset") || openapi.components.schemas.RuntimeConfigChange?.properties?.reset?.default !== undefined) {
    throw new Error("RuntimeConfigChange.reset must remain optional without a schema default");
  }
  const rollbackRequired = openapi.components.schemas.RuntimeConfigRollbackRequest?.required ?? [];
  for (const field of ["expectedVersion", "targetVersion"]) {
    if (!rollbackRequired.includes(field)) {
      throw new Error(`RuntimeConfigRollbackRequest must require ${field}`);
    }
  }
}

function validateComputeTaskCenterContract(openapi) {
  const listParameters = openapi.paths?.["/compute/tasks"]?.get?.parameters ?? [];
  const parameterNames = new Set(listParameters.map((parameter) => parameter?.name).filter(Boolean));
  for (const name of ["resourceKind", "resourceId"]) {
    if (!parameterNames.has(name)) {
      throw new Error(`GET /compute/tasks must declare ${name} query parameter`);
    }
  }

  const logOperation = openapi.paths?.["/compute/tasks/{domain}/{id}/logs"]?.get;
  if (logOperation?.operationId !== "listComputeTaskLogs") {
    throw new Error("GET /compute/tasks/{domain}/{id}/logs must declare listComputeTaskLogs");
  }
  if (!openapi.components.schemas.ComputeTaskLogListEnvelope) {
    throw new Error("OpenAPI schemas must declare ComputeTaskLogListEnvelope");
  }
}

function collectOperationIds(openapi) {
  const operationIds = [];
  for (const pathItem of Object.values(openapi.paths ?? {})) {
    for (const [method, operation] of Object.entries(pathItem ?? {})) {
      if (isHttpMethod(method) && operation?.operationId) {
        operationIds.push(operation.operationId);
      }
    }
  }
  return operationIds;
}

function isHttpMethod(method) {
  return ["get", "post", "put", "patch", "delete"].includes(method);
}
