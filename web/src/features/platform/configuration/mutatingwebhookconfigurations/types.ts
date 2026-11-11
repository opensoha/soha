import type { AdmissionWebhook, ConfigurationDetailBase } from '../shared/types'

export interface MutatingWebhookConfigurationResource extends ConfigurationDetailBase {
  readonly webhooks: number | AdmissionWebhook[]
}

export const MUTATING_WEBHOOK_TEMPLATE = `apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: example-mutating-webhooks
webhooks: []
`
