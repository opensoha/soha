import { useEffect, useState } from 'react'
import { Button, Card, Space, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { isApiError } from '@/services/api-error'
import { deliveryQueries } from './queries'
import type { DeliveryPlan } from './types'

export function usePlanReferenceImpact(plan: DeliveryPlan | null | undefined, open = true) {
  const id = open && plan?.applicationId && plan.applicationEnvironmentId ? plan.id : ''
  const [page, setPage] = useState<{ planId: string; cursors: string[] }>()
  const [changedPlanId, setChangedPlanId] = useState('')
  const cursors = page?.planId === id ? page.cursors : []
  const query = useQuery(deliveryQueries.plans.referenceImpact(id, cursors.at(-1) ?? ''))
  const impact = id && !query.isError ? query.data : undefined
  const changedInResponse =
    impact?.isCurrent === false ||
    Boolean(id && isApiError(query.error) && query.error.status === 409)
  useEffect(() => {
    if (changedInResponse) setChangedPlanId(id)
  }, [changedInResponse, id])

  return {
    enabled: Boolean(id),
    impact,
    changed: changedInResponse || Boolean(id && id === changedPlanId),
    query,
    pageNumber: cursors.length + 1,
    firstPage: () => setPage({ planId: id, cursors: [] }),
    previousPage: () => setPage({ planId: id, cursors: cursors.slice(0, -1) }),
    nextPage: () => {
      if (impact?.nextCursor) setPage({ planId: id, cursors: [...cursors, impact.nextCursor] })
    },
  }
}

export function PlanReferenceImpact({ view }: { view: ReturnType<typeof usePlanReferenceImpact> }) {
  if (!view.enabled) return null
  const { changed, impact, pageNumber, query } = view
  return (
    <Card
      size="small"
      title="下游引用影响"
      aria-busy={query.isFetching}
      extra={
        <Button type="text" loading={query.isFetching} onClick={() => void query.refetch()}>
          刷新引用影响
        </Button>
      }
    >
      <Space orientation="vertical" size={8} style={{ width: '100%' }}>
        <ReadFeedback query={query} label="下游引用影响" />
        {query.isLoading ? <Typography.Text>正在读取下游引用影响…</Typography.Text> : null}
        {changed ? (
          <ContextFeedback
            showIcon
            type="warning"
            title="引用影响已变化或需要重新核对"
            description="请重新预检并生成新的交付计划。"
          />
        ) : null}
        {impact?.items.length ? (
          <ul style={{ marginBlock: 0, paddingInlineStart: 20 }}>
            {impact.items.map((consumer) => (
              <li key={consumer.referenceId} style={{ overflowWrap: 'anywhere' }}>
                <Typography.Text strong>{consumer.environmentKey}</Typography.Text>
                {' · '}
                <Typography.Text type="secondary">服务 {consumer.serviceId}</Typography.Text>
              </li>
            ))}
          </ul>
        ) : null}
        {impact?.hasRestrictedConsumers ? (
          <Typography.Text type="secondary">部分引用环境受权限保护。</Typography.Text>
        ) : null}
        {impact && !impact.items.length && !impact.hasRestrictedConsumers ? (
          <Typography.Text type="secondary">
            {pageNumber === 1 ? '当前没有下游引用。' : '此页没有可见引用。'}
          </Typography.Text>
        ) : null}
        {query.isError && pageNumber > 1 ? (
          <Button onClick={view.firstPage}>返回第一页</Button>
        ) : pageNumber > 1 || impact?.nextCursor ? (
          <Space wrap>
            <Button disabled={pageNumber === 1 || query.isFetching} onClick={view.previousPage}>
              上一页
            </Button>
            <Typography.Text type="secondary">第 {pageNumber} 页</Typography.Text>
            <Button disabled={!impact?.nextCursor || query.isFetching} onClick={view.nextPage}>
              下一页
            </Button>
          </Space>
        ) : null}
      </Space>
    </Card>
  )
}
