import type { ResourceYAMLView, ScopeKey } from '@/types'

export type NetworkKind =
  | 'services'
  | 'ingresses'
  | 'gatewayclasses'
  | 'gateways'
  | 'httproutes'
  | 'backendtlspolicies'
  | 'grpcroutes'
  | 'referencegrants'
  | 'endpointslices'
  | 'ingressclasses'
  | 'networkpolicies'

export interface NetworkTarget {
  readonly scope: ScopeKey
  readonly name: string
}

export interface NetworkResourceRecord {
  readonly name: string
  readonly namespace?: string
  readonly ageSeconds: number
  readonly allowedActions?: string[]
  readonly labels?: Record<string, string>
  readonly annotations?: Record<string, string>
}

export interface NetworkEvent {
  readonly name: string
  readonly namespace?: string
  readonly type: string
  readonly reason: string
  readonly involvedKind?: string
  readonly involvedName?: string
  readonly message: string
  readonly count: number
  readonly ageSeconds: number
}

export interface UpdateNetworkYAMLVariables extends NetworkTarget {
  readonly content: string
  readonly requireObservedIdentity?: boolean
}

export type NetworkYAML = ResourceYAMLView
