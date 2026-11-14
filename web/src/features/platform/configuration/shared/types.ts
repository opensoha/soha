import type { ResourceYAMLView, ScopeKey } from '@/types'

export type ConfigurationKind =
  | 'configmaps'
  | 'secrets'
  | 'resourcequotas'
  | 'limitranges'
  | 'hpas'
  | 'poddisruptionbudgets'
  | 'priorityclasses'
  | 'runtimeclasses'
  | 'leases'
  | 'mutatingwebhookconfigurations'
  | 'validatingwebhookconfigurations'

export type ConfigurationScopeMode = 'cluster' | 'namespace'

export interface ConfigurationTarget {
  readonly scope: ScopeKey
  readonly name: string
}

export interface ConfigurationResourceRecord {
  readonly name: string
  readonly namespace?: string
  readonly ageSeconds: number
  readonly allowedActions?: string[]
}

export interface ConfigurationDetailBase extends ConfigurationResourceRecord {
  readonly createdAt?: string
  readonly labels?: Record<string, string>
  readonly annotations?: Record<string, string>
}

export interface ConfigurationReference {
  readonly kind: string
  readonly name: string
  readonly namespace: string
  readonly path: string
}

export interface AdmissionWebhookRule {
  readonly operations?: string[]
  readonly apiGroups?: string[]
  readonly apiVersions?: string[]
  readonly resources?: string[]
  readonly scope?: string
}

export interface AdmissionWebhook {
  readonly name: string
  readonly clientTarget: string
  readonly url?: string
  readonly serviceName?: string
  readonly serviceNamespace?: string
  readonly servicePath?: string
  readonly servicePort?: number
  readonly caBundleConfigured: boolean
  readonly failurePolicy?: string
  readonly matchPolicy?: string
  readonly sideEffects?: string
  readonly timeoutSeconds?: number
  readonly admissionReviewVersions?: string[]
  readonly namespaceSelector?: string
  readonly objectSelector?: string
  readonly rules?: AdmissionWebhookRule[]
}

export interface CreateConfigurationVariables {
  readonly scope: ScopeKey
  readonly content: string
}

export interface UpdateConfigurationDataVariables<TPayload> {
  readonly target: ConfigurationTarget
  readonly payload: TPayload
}

export interface UpdateConfigurationYAMLVariables extends ConfigurationTarget {
  readonly content: string
  readonly requireObservedIdentity?: boolean
}

export type ConfigurationYAML = ResourceYAMLView
