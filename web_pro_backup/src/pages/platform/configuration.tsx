import { Button, Space } from 'antd'
import { useLocation, useNavigate } from '@umijs/max'
import {
  ConfigMapDetailPage,
  SecretDetailPage,
} from '@/features/platform/configuration-detail-pages'
import {
  ConfigurationConfigMapsPage,
  ConfigurationHPAPage,
  ConfigurationLeasesPage,
  ConfigurationLimitRangesPage,
  ConfigurationMutatingWebhooksPage,
  ConfigurationPodDisruptionBudgetsPage,
  ConfigurationPriorityClassesPage,
  ConfigurationResourceQuotasPage,
  ConfigurationRuntimeClassesPage,
  ConfigurationSecretsPage,
  ConfigurationValidatingWebhooksPage,
} from '@/features/platform/platform-management-pages'
import { PlatformWorkspaceShell } from './workspace-shell'

const tabs = [
  { key: 'configmaps', label: 'ConfigMaps', path: '/configuration/configmaps' },
  { key: 'secrets', label: 'Secrets', path: '/configuration/secrets' },
  { key: 'quotas', label: 'ResourceQuotas', path: '/configuration/resourcequotas' },
  { key: 'hpas', label: 'HPA', path: '/configuration/hpas' },
  { key: 'webhooks', label: 'Webhooks', path: '/configuration/mutatingwebhookconfigurations' },
]

function renderContent(pathname: string) {
  if (pathname.startsWith('/configuration/configmaps/')) return <ConfigMapDetailPage embedded />
  if (pathname.startsWith('/configuration/secrets/')) return <SecretDetailPage embedded />
  if (pathname.startsWith('/configuration/secrets')) return <ConfigurationSecretsPage />
  if (pathname.startsWith('/configuration/resourcequotas')) return <ConfigurationResourceQuotasPage />
  if (pathname.startsWith('/configuration/limitranges')) return <ConfigurationLimitRangesPage />
  if (pathname.startsWith('/configuration/hpas')) return <ConfigurationHPAPage />
  if (pathname.startsWith('/configuration/poddisruptionbudgets')) return <ConfigurationPodDisruptionBudgetsPage />
  if (pathname.startsWith('/configuration/priorityclasses')) return <ConfigurationPriorityClassesPage />
  if (pathname.startsWith('/configuration/runtimeclasses')) return <ConfigurationRuntimeClassesPage />
  if (pathname.startsWith('/configuration/leases')) return <ConfigurationLeasesPage />
  if (pathname.startsWith('/configuration/mutatingwebhookconfigurations')) return <ConfigurationMutatingWebhooksPage />
  if (pathname.startsWith('/configuration/validatingwebhookconfigurations')) return <ConfigurationValidatingWebhooksPage />
  return <ConfigurationConfigMapsPage />
}

export default function PlatformConfigurationPage() {
  const { pathname } = useLocation()
  const navigate = useNavigate()

  return (
    <PlatformWorkspaceShell
      title="Configuration"
      description="Inspect mutable runtime config, quotas, and admission-related resources from a consolidated configuration workspace."
      currentPath={pathname}
      tabs={tabs}
      badge="Operational config"
      metrics={[
        { label: 'Mutable surfaces', value: 'ConfigMaps, Secrets, YAML editors' },
        { label: 'Guardrails', value: 'Quotas, limits, disruption budgets' },
        { label: 'Cluster surfaces', value: 'Priority, runtime, webhook config' },
      ]}
      actions={(
        <Space>
          <Button onClick={() => navigate('/configuration/configmaps')}>ConfigMaps</Button>
          <Button type="primary" onClick={() => navigate('/configuration/secrets')}>Secrets</Button>
        </Space>
      )}
    >
      {renderContent(pathname)}
    </PlatformWorkspaceShell>
  )
}
