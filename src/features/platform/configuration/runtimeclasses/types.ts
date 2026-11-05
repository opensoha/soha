import type { ConfigurationDetailBase } from '../shared/types'

export interface RuntimeClassResource extends ConfigurationDetailBase {
  readonly handler: string
}

export const RUNTIME_CLASS_TEMPLATE = `apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: example-runtime
handler: example-handler
`
