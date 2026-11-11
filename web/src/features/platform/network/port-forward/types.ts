import type { ScopeKey } from '@/types'

export interface PortForwardSession {
  readonly sessionId: string
  readonly clusterId: string
  readonly namespace: string
  readonly targetKind: string
  readonly targetName: string
  readonly localPort: number
  readonly remotePort: number
  readonly status: string
  readonly createdBy?: string
  readonly createdAt: string
}

export interface PortForwardDraft {
  readonly scope: ScopeKey
  readonly targetKind: string
  readonly targetName: string
  readonly namespace: string
  readonly localPort: number
  readonly remotePort: number
}

export interface PortForwardTarget {
  readonly scope: ScopeKey
  readonly sessionId: string
}
