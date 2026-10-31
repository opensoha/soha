import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { deliveryQueries } from '@/features/delivery'
import { targetMatchesDeployment } from '@/features/platform/workloads-model'
import { useI18n } from '@/i18n'
import type { ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Typography } from 'antd'
import { useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'

export function DeploymentDetailDelivery({
  scope,
  name,
  refreshToken = 0,
}: {
  scope: ScopeKey
  name: string
  refreshToken?: number
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = hasPermission(permissions, 'delivery.applications.view')
  const canWorkflow = canRead && hasPermission(permissions, 'delivery.workflows.view')
  const canRelease = canRead && hasPermission(permissions, 'delivery.releases.view')
  const bindings = useQuery({ ...deliveryQueries.environments.list(), enabled: canRead })
  const applications = useQuery(deliveryQueries.applications.list(canRead))
  const builds = useQuery(deliveryQueries.builds.list({}, canRead))
  const workflows = useQuery(deliveryQueries.workflows.list({}, { enabled: canWorkflow }))
  const releases = useQuery(deliveryQueries.releases.list({}, { enabled: canRelease }))
  const previousRefresh = useRef(refreshToken)
  const refresh = useRef(() => {})
  refresh.current = () => {
    if (canRead) {
      void bindings.refetch()
      void applications.refetch()
      void builds.refetch()
    }
    if (canWorkflow) void workflows.refetch()
    if (canRelease) void releases.refetch()
  }
  useEffect(() => {
    if (previousRefresh.current !== refreshToken) {
      previousRefresh.current = refreshToken
      refresh.current()
    }
  }, [refreshToken])
  const data = canRead ? readableQueryData(bindings) : undefined
  const matches = data?.filter((binding) =>
    binding.targets?.some((target) =>
      targetMatchesDeployment(target, scope.clusterId || '', scope.namespace || '', name),
    ),
  )
  const latest = <T extends { createdAt?: string }>(rows?: T[]) =>
    [...(rows || [])].sort(
      (a, b) => (Date.parse(b.createdAt || '') || 0) - (Date.parse(a.createdAt || '') || 0),
    )[0]
  const apps = canRead ? readableQueryData(applications) : undefined
  const buildRows = canRead ? readableQueryData(builds) : undefined
  const workflowRows = canWorkflow ? readableQueryData(workflows) : undefined
  const releaseRows = canRelease ? readableQueryData(releases) : undefined
  if (!canRead)
    return (
      <ManagementState
        compact
        kind="no-permission"
        title={zh ? '无权查看交付联动' : 'No permission to view delivery integration'}
      />
    )
  return (
    <div className="soha-deployment-stack">
      <ReadFeedback
        canRead={canRead}
        query={bindings}
        label={zh ? '应用环境绑定' : 'Application environment bindings'}
      />
      <ReadFeedback
        canRead={canRead}
        query={applications}
        label={zh ? '应用目录' : 'Applications'}
      />
      <ReadFeedback canRead={canRead} query={builds} label={zh ? '构建记录' : 'Builds'} />
      {canWorkflow && (
        <ReadFeedback canRead query={workflows} label={zh ? '工作流' : 'Workflows'} />
      )}
      {canRelease && (
        <ReadFeedback canRead query={releases} label={zh ? '应用发布' : 'Application releases'} />
      )}
      {bindings.isLoading && (
        <ManagementState compact title={zh ? '正在读取交付绑定' : 'Loading delivery bindings'} />
      )}
      {data && !matches?.length && (
        <ManagementState
          compact
          title={
            zh
              ? '当前返回的应用环境中没有绑定此 Deployment'
              : 'No returned application environment is bound to this Deployment'
          }
        />
      )}
      {matches?.map((binding) => {
        const app = apps?.find((item) => item.id === binding.applicationId)
        const build = latest(
          buildRows?.filter((item) => item.applicationId === binding.applicationId),
        )
        const workflow = latest(
          workflowRows?.filter(
            (item) =>
              item.applicationId === binding.applicationId &&
              item.clusterId === scope.clusterId &&
              item.namespace === scope.namespace &&
              item.deploymentName === name,
          ),
        )
        const release = latest(
          releaseRows?.filter(
            (item) =>
              item.applicationId === binding.applicationId &&
              item.clusterId === scope.clusterId &&
              item.namespace === scope.namespace &&
              item.deploymentName === name,
          ),
        )
        return (
          <Card
            key={binding.id}
            className="soha-detail-card"
            title={
              <Link to={`/applications/${encodeURIComponent(binding.applicationId)}`}>
                {app?.name || binding.applicationId}
              </Link>
            }
            extra={
              <span>
                {binding.environmentKey || binding.environmentId}{' '}
                <Button
                  type="text"
                  aria-label={zh ? '刷新交付记录' : 'Refresh delivery records'}
                  loading={
                    bindings.isFetching ||
                    applications.isFetching ||
                    builds.isFetching ||
                    workflows.isFetching ||
                    releases.isFetching
                  }
                  onClick={() => refresh.current()}
                >
                  {zh ? '刷新' : 'Refresh'}
                </Button>
              </span>
            }
          >
            <div className="soha-deployment-delivery-grid">
              {[
                {
                  label: zh ? '最近应用构建' : 'Latest application build',
                  record: build,
                  path: '/builds/',
                  allowed: canRead,
                  loading: builds.isLoading,
                  error: builds.isError,
                },
                {
                  label: zh ? '最近工作流' : 'Latest workflow',
                  record: workflow,
                  path: '/workflows/',
                  allowed: canWorkflow,
                  loading: workflows.isLoading,
                  error: workflows.isError,
                },
                {
                  label: zh ? '最近应用发布' : 'Latest application release',
                  record: release,
                  path: '/releases/',
                  allowed: canRelease,
                  loading: releases.isLoading,
                  error: releases.isError,
                },
              ].map(({ label, record: item, path, allowed, loading, error }) => {
                return (
                  <article key={label}>
                    <span>{label}</span>
                    {allowed ? (
                      item ? (
                        <>
                          <Link to={`${path}${encodeURIComponent(item.id)}`}>{item.id}</Link>
                          <StatusTag value={item.status} />
                          <Typography.Text type="secondary">
                            {item.createdAt ? formatDateTime(item.createdAt) : '—'}
                          </Typography.Text>
                        </>
                      ) : (
                        <Typography.Text type="secondary">
                          {loading
                            ? zh
                              ? '正在读取'
                              : 'Loading'
                            : error
                              ? zh
                                ? '记录读取失败'
                                : 'Records unavailable'
                              : zh
                                ? '当前返回数据中暂无记录'
                                : 'No record in the returned data'}
                        </Typography.Text>
                      )
                    ) : (
                      <Typography.Text type="secondary">
                        {zh ? '无查看权限' : 'No permission to view'}
                      </Typography.Text>
                    )}
                  </article>
                )
              })}
            </div>
          </Card>
        )
      })}
    </div>
  )
}
