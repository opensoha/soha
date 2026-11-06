import { admissionWebhookFromYAML } from '../shared/admission-webhook-model'
export function validatingWebhookFromYAML(content: string, name: string) {
  return admissionWebhookFromYAML(content, name, 'ValidatingWebhookConfiguration')
}
