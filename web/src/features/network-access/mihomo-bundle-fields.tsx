import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Alert, Select, Typography } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import type { NetworkMihomoProfileInput } from '@opensoha/contracts/gen/ts/sohaapi'
import { proxyQueries } from './proxy-runtime/queries'
import { workspaceQueries } from './proxy-runtime/workspace-queries'
export type ProfileBundle = Required<
  Pick<NetworkMihomoProfileInput, 'bundleId' | 'deliveryMode' | 'orgRevision' | 'sourceRevision'>
>
export function MihomoBundleFields({
  value,
  onChange,
}: {
  value?: ProfileBundle
  onChange: (value: ProfileBundle | undefined) => void
}) {
  const permission = usePermissionSnapshot()
  const canView = hasPermission(permission.data?.data, 'network_access.proxy_instances.view')
  const [instance, setInstance] = useState('')
  const instances = useQuery(proxyQueries.instances({}, canView))
  const bundles = useQuery(workspaceQueries.bundles(instance, canView && !!instance))
  const active = readableQueryData(bundles)?.items.filter((b) => b.state === 'activated') ?? []
  return (
    <div style={{ display: 'grid', gap: 12 }}>
      {!canView ? (
        <Alert
          type="info"
          title="无代理实例查看权限，无法浏览或更换发布包"
          description={
            value
              ? '现有发布绑定保留，可继续修改其它配置。'
              : '请联系拥有代理查看权限的管理员配置发布包。'
          }
        />
      ) : (
        <>
          <ReadFeedback canRead query={instances} label="代理实例" />
          <label>
            代理实例
            <Select
              aria-label="发布包代理实例"
              style={{ width: '100%' }}
              value={instance || undefined}
              options={readableQueryData(instances)?.map((i) => ({ value: i.id, label: i.name }))}
              onChange={(id) => {
                setInstance(id)
                onChange(undefined)
              }}
            />
          </label>
          {instance && (
            <>
              <ReadFeedback canRead query={bundles} label="已激活发布包" />
              <label>
                已激活发布包
                <Select
                  aria-label="已激活发布包"
                  style={{ width: '100%' }}
                  value={value?.bundleId}
                  options={active.map((b) => ({
                    value: b.bundleId,
                    label: `${b.bundleId} · ${b.deliveryMode} · 源版本 ${b.sourceRevision}`,
                  }))}
                  onChange={(id) => {
                    const b = active.find((b) => b.bundleId === id)
                    if (b)
                      onChange({
                        bundleId: b.bundleId,
                        deliveryMode: b.deliveryMode,
                        orgRevision: b.orgRevision,
                        sourceRevision: b.sourceRevision,
                      })
                  }}
                />
              </label>
              {!bundles.isLoading && !active.length && (
                <Alert type="info" title="此实例暂无已激活发布包" />
              )}
            </>
          )}
        </>
      )}
      {value && (
        <Typography.Paragraph>
          发布包 {value.bundleId} · {value.deliveryMode} · 来源版本 {value.sourceRevision} ·
          组织版本 {value.orgRevision}
        </Typography.Paragraph>
      )}
      <Typography.Paragraph type="secondary">
        授权用户和设备范围由发布端与服务端校验。此目录不提供授权名单；配置目标设备必须在发布授权范围内。
      </Typography.Paragraph>
    </div>
  )
}
