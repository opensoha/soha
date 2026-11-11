import { gatewayApi } from './api'
import type { ApiResponse } from '@/types'
import type { ToolInvocationRequest } from '@opensoha/contracts/gen/ts/sohaapi'
import { isSecretReference } from '@/features/secrets'
import type {
  AccessPolicy,
  AccessPolicyUpsertPayload,
  AIClient,
  ApprovalRequest,
  CreatedPersonalAccessToken,
  CreatedServiceAccountToken,
  DrawerState,
  GatewayDrawerFormValues,
  LLMModelRoute,
  LLMUpstream,
} from './types'
import {
  accessPolicyApprovalPolicyFromValues,
  accessPolicyConditionsFromValues,
  firstNumber,
  gatewayTokenMetadataFromValues,
  gatewayTokenScopesFromValues,
  valuesToResourceScopes,
} from './types'

export type GatewayDeleteKind = 'grant' | 'policy' | 'binding' | 'personal-token' | 'model-route'

export type GatewayTokenKind = 'personal-token' | 'service-token'

function recordId(drawer: DrawerState) {
  return (drawer.record as { id?: string } | undefined)?.id
}

function parseJsonObjectField(value: unknown, label: string): Record<string, unknown> {
  const text = String(value ?? '').trim()
  if (!text) return {}
  const parsed = JSON.parse(text) as unknown
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error(`${label} 必须是 JSON object`)
  }
  return parsed as Record<string, unknown>
}

export interface GatewayToolInvocationValues {
  inputJson?: string
  secretRefs?: Array<{ alias?: string; ref?: string }>
}

function relayUpstreamPayload(values: GatewayDrawerFormValues) {
  return {
    name: values.name,
    providerKind: values.providerKind,
    baseUrl: values.baseUrl,
    apiKey: values.apiKey || undefined,
    status: values.status,
    priority: firstNumber(values, 'priority') ?? 100,
    weight: firstNumber(values, 'weight') ?? 100,
    timeoutSeconds: firstNumber(values, 'timeoutSeconds') ?? 120,
    streamTimeoutSeconds: firstNumber(values, 'streamTimeoutSeconds') ?? 300,
    maxConcurrency: firstNumber(values, 'maxConcurrency') ?? 0,
    supportedModels: values.supportedModels ?? [],
    defaultHeaders: parseJsonObjectField(values.defaultHeadersJson, 'Default headers'),
    proxyUrl: values.proxyUrl || undefined,
    metadata: parseJsonObjectField(values.metadataJson, 'Metadata'),
  }
}

export function buildGatewayToolInvocation(
  toolName: string,
  values: GatewayToolInvocationValues,
  context: { aiClientId?: string; skillId?: string; capabilityVersion?: string } = {},
): ToolInvocationRequest {
  const secretRefs: Record<string, string> = {}
  for (const item of values.secretRefs ?? []) {
    const alias = item.alias?.trim() ?? ''
    const ref = item.ref?.trim() ?? ''
    if (!alias && !ref) continue
    if (!/^[A-Z_][A-Z0-9_]*$/.test(alias)) throw new Error(`无效 Secret alias: ${alias}`)
    if (!isSecretReference(ref)) throw new Error(`无效 Secret reference: ${ref}`)
    if (secretRefs[alias]) throw new Error(`重复 Secret alias: ${alias}`)
    secretRefs[alias] = ref
  }
  return {
    toolName,
    capabilityVersion: context.capabilityVersion || undefined,
    input: parseJsonObjectField(values.inputJson, 'Input'),
    aiClientId: context.aiClientId || undefined,
    skillId: context.skillId || undefined,
    secretRefs: Object.keys(secretRefs).length ? secretRefs : undefined,
  }
}

export function invokeGatewayTool(input: {
  toolName: string
  capabilityVersion?: string
  values: GatewayToolInvocationValues
  aiClientId?: string
  skillId?: string
}) {
  const payload = buildGatewayToolInvocation(input.toolName, input.values, input)
  return gatewayApi.tools.invoke(input.toolName, payload)
}

export async function upsertGatewayResource(drawer: DrawerState, values: GatewayDrawerFormValues) {
  const id = recordId(drawer)
  switch (drawer.kind) {
    case 'ai-client': {
      const payload = {
        id: values.id,
        name: values.name,
        kind: values.kind,
        status: values.status,
        redirectUris: values.redirectUris ?? [],
        allowedOrigins: values.allowedOrigins ?? [],
      }
      return id ? gatewayApi.clients.update(id, payload) : gatewayApi.clients.create(payload)
    }
    case 'relay-upstream': {
      const payload = relayUpstreamPayload(values)
      return id
        ? gatewayApi.relay.updateUpstream(id, payload)
        : gatewayApi.relay.createUpstream(payload)
    }
    case 'relay-route': {
      const payload = {
        id: values.id,
        publicModel: values.publicModel,
        providerKind: values.providerKind || undefined,
        upstreamId: values.upstreamId || undefined,
        upstreamModel: values.upstreamModel,
        routeGroup: values.routeGroup || undefined,
        priority: firstNumber(values, 'priority') ?? 100,
        weight: firstNumber(values, 'weight') ?? 100,
        enabled: values.enabled !== 'false',
        transformPolicy: parseJsonObjectField(values.transformPolicyJson, 'Transform policy'),
        fallbackPolicy: parseJsonObjectField(values.fallbackPolicyJson, 'Fallback policy'),
        cachePolicy: parseJsonObjectField(values.cachePolicyJson, 'Cache policy'),
        rateLimitProfileId: values.rateLimitProfileId || undefined,
        metadata: parseJsonObjectField(values.metadataJson, 'Metadata'),
      }
      return id
        ? gatewayApi.relay.updateModelRoute(id, payload)
        : gatewayApi.relay.createModelRoute(payload)
    }
    case 'personal-token':
      return gatewayApi.personalTokens.create({
        name: values.name,
        scopes: gatewayTokenScopesFromValues(values),
        permissionKeys: values.permissionKeys ?? [],
        metadata: gatewayTokenMetadataFromValues(values),
        expiresAt: values.expiresAt || undefined,
      })
    case 'service-account':
      return gatewayApi.serviceAccounts.create({
        id: values.id,
        name: values.name,
        description: values.description,
        status: values.status,
        ownerUserId: values.ownerUserId,
        roleIds: values.roleIds ?? [],
        teamIds: values.teamIds ?? [],
        scopeGrantIds: values.scopeGrantIds ?? [],
      })
    case 'service-token':
      return gatewayApi.serviceAccounts.createToken(id ?? '', {
        name: values.name,
        scopes: gatewayTokenScopesFromValues(values),
        permissionKeys: values.permissionKeys ?? [],
        metadata: gatewayTokenMetadataFromValues(values),
        expiresAt: values.expiresAt || undefined,
      })
    case 'service-token-revoke':
      return gatewayApi.serviceTokens.revoke(values.tokenId ?? '')
    case 'tool-grant':
      return gatewayApi.grants.create({
        subjectType: values.subjectType,
        subjectId: values.subjectId,
        aiClientId: values.aiClientId,
        toolName: values.toolName,
        effect: values.effect,
        riskLevel: values.riskLevel,
        permissionKeys: values.permissionKeys ?? [],
        resourceScopes: valuesToResourceScopes(values),
        requiresApproval: values.requiresApproval === 'true',
        expiresAt: values.expiresAt || undefined,
      })
    case 'access-policy': {
      const payload: AccessPolicyUpsertPayload = {
        name: values.name,
        description: values.description,
        enabled: values.enabled === 'true',
        subjectType: values.subjectType,
        subjectId: values.subjectId,
        aiClientId: values.aiClientId,
        effect: values.effect,
        toolPatterns: values.toolPatterns ?? [],
        skillIds: values.skillIds ?? [],
        riskLevels: values.riskLevels ?? [],
        resourceScopes: valuesToResourceScopes(values),
        approvalPolicy: accessPolicyApprovalPolicyFromValues(values),
        conditions: accessPolicyConditionsFromValues(values),
      }
      return id ? gatewayApi.policies.update(id, payload) : gatewayApi.policies.create(payload)
    }
    case 'skill-binding': {
      const payload = {
        subjectType: values.subjectType,
        subjectId: values.subjectId,
        aiClientId: values.aiClientId,
        skillId: values.skillId,
        capabilityRefs: values.capabilityRefs ?? [],
        enabled: values.enabled === 'true',
      }
      return id ? gatewayApi.bindings.update(id, payload) : gatewayApi.bindings.create(payload)
    }
  }
}

export function deleteGatewayResource(kind: GatewayDeleteKind, id: string) {
  if (kind === 'grant') return gatewayApi.grants.delete(id)
  if (kind === 'policy') return gatewayApi.policies.delete(id)
  if (kind === 'binding') return gatewayApi.bindings.delete(id)
  if (kind === 'model-route') return gatewayApi.relay.deleteModelRoute(id)
  return gatewayApi.personalTokens.revoke(id)
}

export async function rotateGatewayToken(
  kind: GatewayTokenKind,
  id: string,
): Promise<ApiResponse<CreatedPersonalAccessToken | CreatedServiceAccountToken>> {
  return kind === 'personal-token'
    ? await gatewayApi.personalTokens.rotate(id)
    : await gatewayApi.serviceTokens.rotate(id)
}

export function disableGatewayClient(record: AIClient) {
  return gatewayApi.clients.update(record.id, {
    id: record.id,
    name: record.name,
    kind: record.kind,
    status: 'disabled',
    redirectUris: record.redirectUris ?? [],
    allowedOrigins: record.allowedOrigins ?? [],
  })
}

export function disableGatewayUpstream(record: LLMUpstream) {
  return gatewayApi.relay.updateUpstream(record.id, {
    id: record.id,
    name: record.name,
    providerKind: record.providerKind,
    baseUrl: record.baseUrl,
    status: 'disabled',
    priority: record.priority,
    weight: record.weight,
    timeoutSeconds: record.timeoutSeconds,
    streamTimeoutSeconds: record.streamTimeoutSeconds,
    maxConcurrency: record.maxConcurrency,
    supportedModels: record.supportedModels ?? [],
    defaultHeaders: record.defaultHeaders ?? {},
    proxyUrl: record.proxyUrl,
    metadata: record.metadata ?? {},
  })
}

export const testGatewayUpstream = (record: LLMUpstream) => gatewayApi.relay.testUpstream(record.id)

export const testGatewayUpstreamDraft = (values: GatewayDrawerFormValues) =>
  gatewayApi.relay.testUpstreamDraft(relayUpstreamPayload(values))

export function decideGatewayApproval(input: {
  action: 'approve' | 'reject' | 'cancel'
  id: string
  comment?: string
}) {
  return gatewayApi.approvals.decide(input.id, input.action, input.comment)
}

export type GatewayDecisionTarget = {
  action: 'approve' | 'reject' | 'cancel'
  record: ApprovalRequest
}

export type GatewayEditableRecord = AccessPolicy | LLMModelRoute | LLMUpstream | AIClient
