import type { NetworkResourceRecord } from '../shared/types'

export interface Ingress extends NetworkResourceRecord {
  readonly namespace: string
  readonly className?: string
  readonly hosts?: string[]
  readonly address: string
  readonly backendServices?: string[]
  readonly routes?: IngressRoute[]
  readonly backends?: IngressBackend[]
}

export interface IngressRoute {
  readonly host?: string
  readonly path?: string
  readonly pathType?: string
  readonly tls: boolean
  readonly serviceName: string
  readonly servicePort?: string
}

export interface IngressEndpoint {
  readonly address: string
  readonly ready?: boolean
  readonly targetRef?: string
}

export interface IngressWorkload {
  readonly kind: string
  readonly name: string
  readonly namespace?: string
}

export interface IngressPod {
  readonly name: string
  readonly namespace: string
  readonly phase: string
  readonly readyContainers: string
  readonly restarts: number
  readonly ageSeconds: number
  readonly workloads?: IngressWorkload[]
}

export interface IngressBackend {
  readonly serviceName: string
  readonly endpoints?: IngressEndpoint[]
  readonly pods?: IngressPod[]
}
