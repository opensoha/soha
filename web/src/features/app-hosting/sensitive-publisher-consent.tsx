import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import type { HostedApp } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Checkbox, Select, Skeleton, Space, Typography } from 'antd'
import { useState } from 'react'
import { hostedRuntimeMutations, hostedRuntimeQueries } from './runtime-controls-data'

export function HostedAppSensitivePublisherConsent({
  app,
  canEdit,
}: {
  app: HostedApp
  canEdit: boolean
}) {
  const query = useQuery(hostedRuntimeQueries.config(app.id, app.ownerUserId))
  const config = readableQueryData(query)
  const client = useQueryClient()
  const [draft, setDraft] = useState<string[] | null>(null)
  const [acknowledged, setAcknowledged] = useState(false)
  const mutation = useOperationMutation(
    {
      ...hostedRuntimeMutations.updateConfig(client, app.id),
      onSettled: () => setAcknowledged(false),
    },
    {
      label: ['保存协作发布授权', 'Save collaborator publishing consent'],
      target: () => app.name,
      receipt: () => ({ message: '协作发布授权已保存' }),
    },
  )
  const currentIds = app.maintainerUserIds ?? []
  const activeIds = (config?.sensitivePublisherIds ?? []).filter((id) => currentIds.includes(id))
  const labels = new Map(app.maintainers?.map((member) => [member.userId, member.displayName]))
  const selectedIds = (draft ?? activeIds).filter((id) => currentIds.includes(id))
  const changed =
    selectedIds.length !== activeIds.length || selectedIds.some((id) => !activeIds.includes(id))

  return (
    <Card size="small" title="动态应用协作发布授权">
      <ReadFeedback query={query} label="动态应用协作发布授权" />
      {mutation.feedback}
      {query.isLoading ? <Skeleton active paragraph={{ rows: 2 }} /> : null}
      {config ? (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <ContextFeedback
            type="warning"
            showIcon
            title="授权后，所选维护人可发布使用应用数据和运行时凭据的新版本。"
          />
          {activeIds.length ? (
            <Typography.Text>
              当前已授权：{activeIds.map((id) => labels.get(id) || id).join('、')}
            </Typography.Text>
          ) : (
            <Typography.Text type="secondary">当前未授权维护人</Typography.Text>
          )}
          {canEdit && currentIds.length ? (
            <>
              <Select
                aria-label="允许协作发布的维护人"
                placeholder="选择允许协作发布的维护人"
                mode="multiple"
                allowClear
                value={selectedIds}
                disabled={mutation.blocked}
                options={currentIds.map((id) => ({ value: id, label: labels.get(id) || id }))}
                onChange={(next: string[]) => {
                  setDraft(next)
                  setAcknowledged(false)
                }}
              />
              {selectedIds.length ? (
                <Checkbox
                  checked={acknowledged}
                  disabled={mutation.blocked}
                  onChange={(event) => setAcknowledged(event.target.checked)}
                >
                  我确认所选维护人可在发布的版本中访问应用数据和运行时凭据
                </Checkbox>
              ) : null}
              <div>
                <Button
                  type="primary"
                  disabled={
                    !changed || (selectedIds.length > 0 && !acknowledged) || mutation.blocked
                  }
                  loading={mutation.isPending}
                  onClick={() =>
                    mutation.mutate(
                      {
                        sensitivePublisherIds: selectedIds,
                        ...(selectedIds.length ? { acknowledgeSensitiveAccess: true } : {}),
                      },
                      { onSuccess: () => setDraft(null) },
                    )
                  }
                >
                  保存协作发布授权
                </Button>
              </div>
            </>
          ) : null}
        </Space>
      ) : null}
    </Card>
  )
}
