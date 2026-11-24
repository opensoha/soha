export type RealtimeSessionKind = 'logs' | 'terminal'
export type RealtimeSessionWorkbench = 'platform' | 'delivery'

export interface RealtimeSessionReference {
  applicationId: string
  viewEnvironmentId: string
  sourceEnvironmentId: string
  serviceId: string
  referenceId: string
  referenceRevision: number
  targetId: string
  rootUid: string
}

export interface RealtimeSessionInput {
  clusterId: string
  container?: string
  kind: RealtimeSessionKind
  namespace: string
  podName: string
  podUid?: string
  reference?: RealtimeSessionReference
  shell?: string
  streamingDisabledReason?: string
}

export interface RealtimeSession extends RealtimeSessionInput {
  id: string
}

const referenceFields = [
  'applicationId',
  'viewEnvironmentId',
  'sourceEnvironmentId',
  'serviceId',
  'referenceId',
  'referenceRevision',
  'targetId',
  'rootUid',
] as const

function normalizeSessionReference(
  reference: RealtimeSessionReference | undefined,
): RealtimeSessionReference | null | undefined {
  if (!reference) return undefined
  const applicationId = reference.applicationId.trim()
  const viewEnvironmentId = reference.viewEnvironmentId.trim()
  const sourceEnvironmentId = reference.sourceEnvironmentId.trim()
  const serviceId = reference.serviceId.trim()
  const referenceId = reference.referenceId.trim()
  const targetId = reference.targetId.trim()
  const rootUid = reference.rootUid.trim()
  const referenceRevision = reference.referenceRevision
  if (
    !applicationId ||
    !viewEnvironmentId ||
    !sourceEnvironmentId ||
    !serviceId ||
    !referenceId ||
    !targetId ||
    !rootUid ||
    viewEnvironmentId === sourceEnvironmentId ||
    !Number.isSafeInteger(referenceRevision) ||
    referenceRevision <= 0
  )
    return null
  return {
    applicationId,
    rootUid,
    referenceId,
    referenceRevision,
    serviceId,
    sourceEnvironmentId,
    targetId,
    viewEnvironmentId,
  }
}

export function sessionReferenceMatchesScope(
  reference: RealtimeSessionReference | undefined,
  workbench: RealtimeSessionWorkbench,
  scopeKey?: string,
): boolean {
  if (!reference) return true
  return (
    workbench === 'delivery' &&
    scopeKey === JSON.stringify([reference.applicationId, reference.viewEnvironmentId])
  )
}

export function normalizeRealtimeSession(input: RealtimeSessionInput): RealtimeSession | null {
  const clusterId = input.clusterId.trim()
  const namespace = input.namespace.trim()
  const podName = input.podName.trim()
  if (!clusterId || !namespace || !podName) return null

  const container = input.container?.trim() || undefined
  const podUid = input.podUid?.trim() || undefined
  const reference = normalizeSessionReference(input.reference)
  if (reference === null || (reference && !podUid)) return null
  const shell = input.kind === 'terminal' ? input.shell?.trim() || '/bin/sh' : undefined
  const id = JSON.stringify([
    input.kind,
    clusterId,
    namespace,
    podName,
    container ?? '',
    shell ?? '',
    ...(podUid ? [podUid] : []),
    ...(reference ? referenceFields.map((field) => reference[field]) : []),
  ]).replace(/[^a-zA-Z0-9]/gu, (character) => `_${character.codePointAt(0)!.toString(16)}_`)

  return {
    ...input,
    clusterId,
    container,
    id,
    namespace,
    podName,
    podUid,
    reference,
    shell,
  }
}
