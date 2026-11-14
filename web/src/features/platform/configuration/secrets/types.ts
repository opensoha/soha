import type { ConfigurationDetailBase, ConfigurationResourceRecord } from '../shared/types'
import type { components } from '@opensoha/contracts/gen/ts/sohaapi'

export type SecretCertificate = components['schemas']['KubernetesSecretCertificate']

export interface SecretResource extends ConfigurationResourceRecord {
  readonly namespace: string
  readonly immutable?: boolean | null
  readonly type: string
  readonly dataEntries: number
}

export interface SecretDetail extends ConfigurationDetailBase {
  readonly namespace: string
  readonly immutable: boolean
  readonly type: string
  readonly data?: Record<string, string>
}

export interface UpdateSecretDataPayload {
  readonly data: Record<string, string>
}

export const SECRET_DEFAULT_TEMPLATE = `apiVersion: v1
kind: Secret
metadata:
  name: example-secret
type: Opaque
stringData:
  key: value
`
