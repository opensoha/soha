export type RealtimeSessionKind = 'logs' | 'terminal'
export type RealtimeSessionWorkbench = 'platform' | 'delivery'

export interface RealtimeSessionInput {
  clusterId: string
  container?: string
  kind: RealtimeSessionKind
  namespace: string
  podName: string
  podUid?: string
  shell?: string
  streamingDisabledReason?: string
}

export interface RealtimeSession extends RealtimeSessionInput {
  id: string
}

export function normalizeRealtimeSession(input: RealtimeSessionInput): RealtimeSession | null {
  const clusterId = input.clusterId.trim()
  const namespace = input.namespace.trim()
  const podName = input.podName.trim()
  if (!clusterId || !namespace || !podName) return null

  const container = input.container?.trim() || undefined
  const podUid = input.podUid?.trim() || undefined
  const shell = input.kind === 'terminal' ? input.shell?.trim() || '/bin/sh' : undefined
  const id = JSON.stringify([
    input.kind,
    clusterId,
    namespace,
    podName,
    container ?? '',
    shell ?? '',
    ...(podUid ? [podUid] : []),
  ]).replace(/[^a-zA-Z0-9]/gu, (character) => `_${character.codePointAt(0)!.toString(16)}_`)

  return {
    ...input,
    clusterId,
    container,
    id,
    namespace,
    podName,
    podUid,
    shell,
  }
}
