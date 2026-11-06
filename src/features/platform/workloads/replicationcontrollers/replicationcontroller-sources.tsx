import { Card, Typography } from 'antd'
import { Link } from 'react-router-dom'
import type { UseQueryResult } from '@tanstack/react-query'
import { ReadFeedback } from '@/components/request-feedback'
import { targetMatchesWorkload } from '@/features/platform/workloads-model'
import { useI18n } from '@/i18n'
import type { ScopeKey, ApplicationEnvironment } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import type { ReplicationControllerDetail } from './types'
export function ReplicationControllerSources({
  detail,
  scope,
  bindings,
  canRead,
}: {
  detail: ReplicationControllerDetail
  scope: ScopeKey
  bindings: UseQueryResult<ApplicationEnvironment[], Error>
  canRead: boolean
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const matches = canRead
    ? readableQueryData(bindings)?.filter((binding) =>
        binding.targets?.some((target) =>
          targetMatchesWorkload(
            target,
            scope.clusterId || '',
            detail.namespace,
            detail.name,
            'ReplicationController',
          ),
        ),
      )
    : undefined
  const owners =
    detail.relatedResources?.filter(
      (r) => r.relation === 'owner' && r.namespace === detail.namespace && Boolean(r.name.trim()),
    ) || []
  return (
    <>
      <Card className="soha-detail-card" title={zh ? '资源归属' : 'Ownership'}>
        {owners.length ? (
          owners.map((owner) => (
            <Typography.Paragraph key={`${owner.kind}/${owner.name}`}>
              {owner.kind} / {owner.name}
            </Typography.Paragraph>
          ))
        ) : (
          <Typography.Text type="secondary">
            {zh ? '未返回上级控制器' : 'No owning controller returned'}
          </Typography.Text>
        )}
      </Card>
      <Card className="soha-detail-card" title={zh ? '交付来源' : 'Delivery source'}>
        {!canRead ? (
          <Typography.Text type="secondary">
            {zh ? '需要应用查看权限' : 'Application view permission required'}
          </Typography.Text>
        ) : (
          <>
            <ReadFeedback canRead query={bindings} label={zh ? '交付绑定' : 'Delivery bindings'} />
            {bindings.isLoading && (
              <Typography.Text type="secondary">
                {zh ? '正在读取绑定' : 'Loading bindings'}
              </Typography.Text>
            )}
            {matches?.map((binding) => (
              <Typography.Paragraph key={binding.id}>
                <Link to={`/applications/${encodeURIComponent(binding.applicationId)}`}>
                  {binding.applicationId}
                </Link>
                <br />
                {binding.environmentKey || binding.environmentId}
              </Typography.Paragraph>
            ))}
            {matches && !matches.length && (
              <Typography.Text type="secondary">
                {zh
                  ? '当前返回的应用环境没有绑定此 RC'
                  : 'No returned application environment is bound to this RC'}
              </Typography.Text>
            )}
          </>
        )}
      </Card>
    </>
  )
}
