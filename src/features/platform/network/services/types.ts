import type { NetworkEvent, NetworkResourceRecord } from '../shared/types'

export interface Service extends NetworkResourceRecord {
  readonly namespace: string
  readonly type: string
  readonly clusterIp: string
  readonly ports: string[]
  readonly portMappings?: ServicePortMapping[]
  readonly selector?: Record<string, string>
  readonly endpoints?: ServiceEndpoint[]
  readonly backendPods?: ServiceBackendPod[]
}

export interface ServicePortMapping {
  readonly name?: string
  readonly protocol: 'TCP' | 'UDP' | 'SCTP'
  readonly targetPort: string
  readonly port: number
  readonly nodePort?: number
}

export interface ServiceEndpoint {
  readonly address: string
  readonly ready?: boolean
  readonly serving?: boolean
  readonly terminating?: boolean
  readonly targetRef?: string
  readonly nodeName?: string
  readonly zone?: string
}

export interface ServiceBackendPod {
  readonly name: string
  readonly namespace: string
  readonly phase: string
  readonly readyContainers: string
  readonly restarts: number
  readonly nodeName?: string
  readonly podIp?: string
  readonly labels?: Record<string, string>
  readonly ageSeconds: number
}

export interface ServiceEvent extends NetworkEvent {
  readonly lastTimestamp?: string
}
