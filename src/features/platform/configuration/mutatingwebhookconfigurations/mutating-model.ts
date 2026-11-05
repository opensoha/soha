import { admissionWebhookFromYAML } from '../shared/admission-webhook-model'
export type {
  AdmissionWebhookDefinition as MutatingWebhook,
  AdmissionWebhookDetail as MutatingWebhookDetail,
  WebhookSelector,
} from '../shared/admission-webhook-model'
export function mutatingWebhookFromYAML(content: string, name: string) {
  return admissionWebhookFromYAML(content, name, 'MutatingWebhookConfiguration')
}
