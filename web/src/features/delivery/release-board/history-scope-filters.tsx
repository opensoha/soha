import { Button, Select } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { ManagementQueryField } from '@/components/management-list'
import { deliveryQueries } from '../queries'

export function HistoryScopeFilters() {
  const [search, setSearch] = useSearchParams()
  const { localeCode } = useI18n()
  const english = localeCode === 'en_US'
  const permissions = usePermissionSnapshot()
  const canBrowse = hasPermission(permissions.data?.data, 'delivery.applications.view')
  const applicationId = search.get('applicationId') || undefined
  const serviceId = search.get('serviceId') || undefined
  const applications = useQuery(deliveryQueries.applications.list(canBrowse))
  const services = useQuery(deliveryQueries.applications.services(applicationId ?? '', canBrowse))
  const update = (key: string, value?: string) =>
    setSearch(
      (current) => {
        const next = new URLSearchParams(current)
        if (value) next.set(key, value)
        else next.delete(key)
        if (key === 'applicationId') next.delete('serviceId')
        next.delete('cursor')
        next.delete('workflowId')
        next.delete('buildSourceId')
        next.delete('applicationEnvironmentId')
        return next
      },
      { replace: true, state: null },
    )
  const definitionScoped = ['workflowId', 'buildSourceId', 'applicationEnvironmentId'].some((key) =>
    search.has(key),
  )
  if (!canBrowse && !definitionScoped) return null
  return (
    <>
      {definitionScoped ? (
        <ManagementQueryField>
          <Button onClick={() => update('definitionName')}>
            {english ? 'Definition filter · Clear' : '已按定义筛选 · 清除'}
          </Button>
        </ManagementQueryField>
      ) : null}
      {canBrowse ? (
        <ManagementQueryField label={english ? 'Application' : '应用'} width={220}>
          <Select
            aria-label={english ? 'Filter history by application' : '按应用筛选记录'}
            allowClear
            placeholder={english ? 'All applications' : '全部应用'}
            value={applicationId}
            loading={applications.isLoading}
            status={applications.isError ? 'error' : undefined}
            showSearch={{ optionFilterProp: 'label' }}
            options={applications.data?.map((app) => ({ value: app.id, label: app.name }))}
            onChange={(value) => update('applicationId', value)}
          />
        </ManagementQueryField>
      ) : null}
      {applicationId && canBrowse ? (
        <ManagementQueryField label={english ? 'Service' : '服务'} width={220}>
          <Select
            aria-label={english ? 'Filter history by service' : '按服务筛选记录'}
            allowClear
            placeholder={english ? 'All services' : '全部服务'}
            value={serviceId}
            loading={services.isLoading}
            status={services.isError ? 'error' : undefined}
            showSearch={{ optionFilterProp: 'label' }}
            options={services.data?.map((service) => ({ value: service.id, label: service.name }))}
            onChange={(value) => update('serviceId', value)}
          />
        </ManagementQueryField>
      ) : null}
    </>
  )
}
