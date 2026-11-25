import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import { Button, Modal, Skeleton, Space, Typography } from 'antd'
import { hostedRuntimeQueries } from './runtime-controls-data'

const { Text } = Typography

export function HostedAppRevisionLogs({
  appId,
  revision,
  onClose,
}: {
  appId: string
  revision: { id: string; revisionNumber: number } | null
  onClose: () => void
}) {
  return (
    <Modal
      title={revision ? `版本 v${revision.revisionNumber} · 脱敏日志` : '脱敏日志'}
      open={Boolean(revision)}
      footer={null}
      onCancel={onClose}
    >
      {revision ? (
        <RevisionEvents key={revision.id} appId={appId} revisionId={revision.id} />
      ) : null}
    </Modal>
  )
}

function RevisionEvents({ appId, revisionId }: { appId: string; revisionId: string }) {
  const query = useQuery(hostedRuntimeQueries.logs(appId, revisionId))
  const logs = readableQueryData(query)

  return (
    <>
      <ReadFeedback query={query} label="版本脱敏日志" />
      <Space orientation="vertical" style={{ width: '100%' }}>
        <Button size="small" loading={query.isFetching} onClick={() => void query.refetch()}>
          刷新日志
        </Button>
        {query.isLoading ? <Skeleton active paragraph={{ rows: 3 }} /> : null}
        {logs &&
          (logs.items.length === 0 ? (
            <Text type="secondary">暂无日志</Text>
          ) : (
            <ul style={{ paddingInlineStart: 20, margin: 0, overflowWrap: 'anywhere' }}>
              {logs.items.map((item, index) => (
                <li key={`${item.at}-${item.source}-${item.code}-${index}`}>
                  <Text type="secondary">
                    {formatDateTime(item.at)} · {item.source === 'build' ? '构建' : '运行'}
                  </Text>
                  <div>
                    <Text code>{item.code}</Text> {item.message}
                  </div>
                </li>
              ))}
            </ul>
          ))}
      </Space>
    </>
  )
}
