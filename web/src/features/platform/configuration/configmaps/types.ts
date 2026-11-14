import type { ConfigurationDetailBase, ConfigurationResourceRecord } from '../shared/types'

export interface ConfigMapResource extends ConfigurationResourceRecord {
  readonly namespace: string
  readonly dataEntries: number
}

export interface ConfigMapDetail extends ConfigurationDetailBase {
  readonly namespace: string
  readonly immutable: boolean
  readonly data?: Record<string, string>
  readonly binaryData?: Record<string, string>
}

export interface UpdateConfigMapDataPayload {
  readonly data: Record<string, string>
  readonly binaryData: Record<string, string>
}

export const CONFIGMAP_DEFAULT_TEMPLATE = `apiVersion: v1
kind: ConfigMap
metadata:
  name: example-config
data:
  key: value
`
