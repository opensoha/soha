import { ConfigurationYAMLDetailPage } from '../shared/yaml-detail'
import { mutatingWebhookFromYAML } from './mutating-model'
import { AdmissionWebhookOverview } from '../shared/admission-webhook-overview'
import { AdmissionWebhookDetails } from '../shared/admission-webhook-details'
import '../shared/admission-webhook.css'

export function ConfigurationMutatingWebhookConfigurationDetailPage() {
  return (
    <ConfigurationYAMLDetailPage
      kind="mutatingwebhookconfigurations"
      label="MutatingWebhookConfiguration"
      className="soha-mutating-detail soha-admission-webhook-detail"
      fromYAML={mutatingWebhookFromYAML}
      renderFacts={(detail) => <span>{detail.webhooks.length} Webhooks</span>}
      renderOverview={(detail) => <AdmissionWebhookOverview detail={detail} />}
      tabs={[
        {
          key: 'webhooks',
          label: 'Webhooks',
          render: (detail) => <AdmissionWebhookDetails detail={detail} />,
        },
      ]}
      deleteDescription={[
        '删除后 API Server 不再调用此配置中的变更 Webhook，可能改变后续资源准入行为；不会删除回调 Service 或已有资源。',
        'The API server will stop calling these mutating webhooks. This may change admission behavior for subsequent requests; callback Services and existing resources are not deleted.',
      ]}
    />
  )
}
