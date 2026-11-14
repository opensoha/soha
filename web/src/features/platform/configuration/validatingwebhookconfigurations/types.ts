import type { AdmissionWebhook, ConfigurationDetailBase } from '../shared/types'

export interface ValidatingWebhookConfigurationResource extends ConfigurationDetailBase {
  readonly webhooks: number | AdmissionWebhook[]
}

export const VALIDATING_WEBHOOK_TEMPLATE = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: example-validating-webhooks
webhooks: []
`
