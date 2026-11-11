import type { NetworkResourceRecord } from '../shared/types'
import type { Pod, WorkloadCondition } from '@/types'
import type { ServiceEndpoint } from '../services/types'

export type GatewayAPIKind =
  | 'gatewayclasses'
  | 'gateways'
  | 'httproutes'
  | 'backendtlspolicies'
  | 'grpcroutes'
  | 'referencegrants'

export interface GatewayClass extends NetworkResourceRecord {
  readonly controllerName: string
  readonly accepted?: string
  readonly parametersRef?: string
}

export interface GatewayClassDetail extends GatewayClass {
  readonly conditions?: WorkloadCondition[]
  readonly gateways?: Gateway[]
}

export interface Gateway extends NetworkResourceRecord {
  readonly namespace: string
  readonly gatewayClass?: string
  readonly addresses?: string[]
  readonly listenerCount: number
}

export interface GatewayListener {
  readonly name: string
  readonly protocol: string
  readonly port: number
  readonly hostname?: string
  readonly tlsMode?: string
  readonly certificateRefs?: string[]
  readonly allowedRouteKinds?: string[]
  readonly attachedRoutes: number
  readonly conditions?: WorkloadCondition[]
}

export interface GatewayRouteReference {
  readonly kind: string
  readonly namespace?: string
  readonly name: string
  readonly hostnames?: string[]
  readonly accepted?: string
}

export interface GatewayDetail extends Gateway {
  readonly conditions?: WorkloadCondition[]
  readonly listeners?: GatewayListener[]
  readonly routes?: GatewayRouteReference[]
}

export interface HTTPRoute extends NetworkResourceRecord {
  readonly namespace: string
  readonly hostnames?: string[]
  readonly parentRefs?: string[]
  readonly backendServices?: string[]
}

export interface GatewayRouteBackend {
  readonly kind?: string
  readonly namespace?: string
  readonly name: string
  readonly port?: number
  readonly weight?: number
  readonly endpoints?: ServiceEndpoint[]
  readonly backendPods?: Pod[]
}

export interface GatewayRouteRule {
  readonly matches?: string[]
  readonly filters?: string[]
  readonly backends?: GatewayRouteBackend[]
}

export interface GatewayRouteParentStatus {
  readonly parentRef: string
  readonly controllerName?: string
  readonly conditions?: WorkloadCondition[]
}

export interface HTTPRouteDetail extends HTTPRoute {
  readonly conditions?: WorkloadCondition[]
  readonly parentStatuses?: GatewayRouteParentStatus[]
  readonly rules?: GatewayRouteRule[]
}

export interface BackendTLSPolicy extends NetworkResourceRecord {
  readonly namespace: string
  readonly targetRefs?: string[]
  readonly hostname?: string
  readonly caCertificateRefs?: string[]
  readonly wellKnownCACertificates?: string
}

export interface BackendTLSPolicyDetail extends BackendTLSPolicy {
  readonly conditions?: WorkloadCondition[]
}

export interface GRPCRoute extends NetworkResourceRecord {
  readonly namespace: string
  readonly hostnames?: string[]
  readonly parentRefs?: string[]
  readonly backendServices?: string[]
  readonly ruleCount: number
}

export interface GRPCRouteDetail extends GRPCRoute {
  readonly conditions?: WorkloadCondition[]
  readonly parentStatuses?: GatewayRouteParentStatus[]
  readonly rules?: GatewayRouteRule[]
}

export interface ReferenceGrant extends NetworkResourceRecord {
  readonly namespace: string
  readonly from?: string[]
  readonly to?: string[]
}

export interface ReferenceGrantFrom {
  readonly group: string
  readonly kind: string
  readonly namespace: string
}

export interface ReferenceGrantTo {
  readonly group: string
  readonly kind: string
  readonly name?: string
}

export interface ReferenceGrantDetail extends ReferenceGrant {
  readonly fromRefs?: ReferenceGrantFrom[]
  readonly toRefs?: ReferenceGrantTo[]
}
