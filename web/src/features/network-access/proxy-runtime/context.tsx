import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import type { NetworkProxyInstance } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import { Select, Tag, Typography } from 'antd'
import { useSearchParams } from 'react-router-dom'
import { proxyQueries } from './queries'
const statusColor: Record<NetworkProxyInstance['status'], string> = {
  online: 'success',
  degraded: 'warning',
  offline: 'error',
  disabled: 'default',
  unregistered: 'default',
}
export function useProxyContext(permission: string) {
  const permissions = usePermissionSnapshot()
  const allowed = hasPermission(permissions.data?.data, permission)
  const instances = useQuery(proxyQueries.instances({}, allowed))
  const visibleInstancesData =
    instances.isEnabled === false ? undefined : readableQueryData(instances)
  const [params, setParams] = useSearchParams()
  const requested = params.get('instanceId')
  const selected =
    visibleInstancesData?.find((item) => item.id === requested) ?? visibleInstancesData?.[0]
  const select = (id: string) => {
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.set('instanceId', id)
      return next
    })
  }
  return { permissions, allowed, instances, selected, select }
}
export function ProxyInstanceHeader({
  instances,
  selected,
  onSelect,
}: {
  instances: NetworkProxyInstance[]
  selected: NetworkProxyInstance
  onSelect: (id: string) => void
}) {
  const { t } = useI18n()
  return (
    <div className="soha-server-proxy-head">
      <div className="soha-server-proxy-identity">
        <span className="soha-server-proxy-eyebrow">
          {t('networkAccess.proxyRuntime.instance', '代理实例')}
        </span>
        <h1>{selected.name}</h1>
        <div className="soha-server-proxy-meta">
          <span>
            {selected.engine}
            {selected.engineVersion ? ` · ${selected.engineVersion}` : ''}
          </span>
          <Tag color={statusColor[selected.status]}>
            {t(
              `networkAccess.proxyRuntime.status.${selected.status}`,
              {
                online: '在线',
                degraded: '降级',
                offline: '失联',
                disabled: '已停用',
                unregistered: '未登记',
              }[selected.status],
            )}
          </Tag>
          <Typography.Text type="secondary">
            {t('networkAccess.proxyRuntime.lastSeen', '最近联系')}：
            {selected.lastSeenAt ? formatDateTime(selected.lastSeenAt) : '—'}
          </Typography.Text>
        </div>
      </div>
      <label className="soha-server-proxy-picker">
        <span>{t('networkAccess.proxyRuntime.switchInstance', '切换实例')}</span>
        <Select
          aria-label={t('networkAccess.proxyRuntime.switchInstance', '切换实例')}
          value={selected.id}
          onChange={onSelect}
          options={instances.map((item) => ({
            value: item.id,
            label: `${item.name} · ${item.engine}`,
          }))}
        />
      </label>
    </div>
  )
}
