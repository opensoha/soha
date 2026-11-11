import type { NetworkResourceRecord } from '../shared/types'

export type NetworkCoreKind = 'endpointslices' | 'ingressclasses' | 'networkpolicies'

export interface EndpointSlice extends NetworkResourceRecord {
  readonly namespace: string
  readonly addressType: string
  readonly endpoints: number
  readonly ports?: string[]
}

export interface EndpointSliceDetail extends Omit<EndpointSlice, 'endpoints'> {
  readonly serviceName?: string
  readonly endpoints?: EndpointSliceEndpoint[]
}

export interface EndpointSliceEndpoint {
  readonly address: string
  readonly ready?: boolean
  readonly serving?: boolean
  readonly terminating?: boolean
  readonly targetRef?: string
  readonly nodeName?: string
  readonly zone?: string
}

export interface IngressClass extends NetworkResourceRecord {
  readonly controller: string
  readonly isDefault: boolean
  readonly parameters?: string
}

export interface IngressClassIngress {
  readonly name: string
  readonly namespace: string
  readonly className?: string
  readonly hosts?: string[]
  readonly address?: string
  readonly backendServices?: string[]
  readonly ageSeconds: number
}

export interface IngressClassDetail extends IngressClass {
  readonly ingresses?: IngressClassIngress[]
}

export interface NetworkPolicy extends NetworkResourceRecord {
  readonly namespace: string
  readonly policyTypes?: string[]
  readonly ingressRules: number
  readonly egressRules: number
}

export interface NetworkPolicyDetail extends Omit<NetworkPolicy, 'ingressRules' | 'egressRules'> {
  readonly podSelector?: string
  readonly rules?: NetworkPolicyRule[]
  readonly matchingPods?: NetworkPolicyPod[]
}

export interface NetworkPolicyPeer {
  readonly podSelector?: string
  readonly namespaceSelector?: string
  readonly ipBlock?: string
}

export interface NetworkPolicyPort {
  readonly protocol?: string
  readonly port?: string
  readonly endPort?: number
}

export interface NetworkPolicyRule {
  readonly direction: string
  readonly peers?: NetworkPolicyPeer[]
  readonly ports?: NetworkPolicyPort[]
}

export interface NetworkPolicyPod {
  readonly name: string
  readonly namespace: string
  readonly phase: string
  readonly readyContainers: string
  readonly restarts: number
  readonly ageSeconds: number
}
