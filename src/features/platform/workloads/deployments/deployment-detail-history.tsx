import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import type { RolloutHistory, ScopeKey } from '@/types'
import { formatDateTime } from '@/utils/time'
import { Button, Card, Drawer, Input, Spin, Typography } from 'antd'
import { lazy, Suspense, useState } from 'react'

const Revision = lazy(async () => ({
  default: (await import('./deployment-detail-revision')).DeploymentDetailRevision,
}))

export function DeploymentDetailHistory({
  scope,
  records,
  currentRevision,
  loading,
  error,
  canRollback,
  blocked,
  onRollback,
  onRefresh,
}: {
  scope: ScopeKey
  records: RolloutHistory[]
  currentRevision?: string
  loading: boolean
  error?: string
  canRollback: boolean
  blocked: boolean
  onRollback: (record: RolloutHistory) => void
  onRefresh: () => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const [selected, setSelected] = useState<RolloutHistory | null>(null)
  const [search, setSearch] = useState('')
  const [onlyActive, setOnlyActive] = useState(false)
  const sorted = [...records]
    .sort((a, b) => b.revision.localeCompare(a.revision, undefined, { numeric: true }))
    .filter(
      (row) =>
        (!onlyActive || row.replicas > 0) &&
        `${row.revision} ${row.name} ${row.images?.join(' ') || ''}`
          .toLowerCase()
          .includes(search.trim().toLowerCase()),
    )
  return (
    <Card
      className="soha-detail-card soha-deployment-history"
      title={zh ? '版本历史' : 'Revision history'}
      extra={
        <Typography.Text type="secondary">
          {sorted.length} / {records.length}
        </Typography.Text>
      }
      loading={loading}
    >
      <div className="soha-deployment-toolbar">
        <Input
          aria-label={zh ? '搜索版本' : 'Search revisions'}
          allowClear
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={zh ? '搜索版本 / ReplicaSet / 镜像' : 'Search revision / ReplicaSet / image'}
        />
        <Button aria-pressed={onlyActive} onClick={() => setOnlyActive(!onlyActive)}>
          {zh ? '有副本的版本' : 'Active replicas'}
        </Button>
      </div>
      {error && (
        <ManagementState
          compact
          kind="error"
          title={error}
          description={
            records.length
              ? zh
                ? '以下保留上次成功读取的版本记录'
                : 'Showing revision records from the previous successful read'
              : undefined
          }
          actions={<Button onClick={onRefresh}>{zh ? '重试' : 'Retry'}</Button>}
        />
      )}
      {!error && !sorted.length && (
        <ManagementState
          compact
          title={
            records.length
              ? zh
                ? '没有匹配的版本'
                : 'No matching revisions'
              : zh
                ? '暂无版本历史'
                : 'No revision history'
          }
        />
      )}
      <div
        className="soha-deployment-history-list"
        role="list"
        tabIndex={0}
        aria-label={zh ? '连续版本历史' : 'Continuous revision history'}
      >
        {sorted.map((record) => (
          <article
            key={`${record.namespace}/${record.name}/${record.revision}`}
            className="soha-deployment-history-row"
            role="listitem"
          >
            <div>
              <strong className="soha-rollout-history-revision">
                {zh ? '版本' : 'Revision'} {record.revision}
              </strong>
              {record.revision === currentRevision && (
                <MetadataTag label={zh ? '当前模板' : 'Current template'} tone="blue" />
              )}
              <Typography.Text type="secondary">
                {record.createdAt ? formatDateTime(record.createdAt) : '—'}
              </Typography.Text>
              <Button
                onClick={() => setSelected(record)}
                aria-label={`${zh ? '查看版本' : 'View revision'} ${record.revision} ${zh ? '模板与差异' : 'template and differences'}`}
              >
                {zh ? '模板与差异' : 'Template and differences'}
              </Button>
              {canRollback && (
                <Button
                  disabled={blocked || record.revision === currentRevision}
                  onClick={() => onRollback(record)}
                >
                  {zh ? '回滚到此版本' : 'Roll back to this revision'}
                </Button>
              )}
            </div>
            <p>
              {record.name} · {zh ? '副本' : 'Replicas'} {record.replicas} · {zh ? '就绪' : 'Ready'}{' '}
              {record.readyReplicas}
            </p>
            {record.images?.length ? (
              <details>
                <summary>
                  {zh ? '镜像' : 'Images'} {record.images.length} · {record.images[0]}
                </summary>
                <ul>
                  {record.images.map((image, index) => (
                    <li key={`${index}:${image}`}>{image}</li>
                  ))}
                </ul>
              </details>
            ) : (
              <Typography.Text type="secondary">
                {zh ? '未记录镜像' : 'No image recorded'}
              </Typography.Text>
            )}
          </article>
        ))}
      </div>
      <Drawer
        title={`${zh ? '版本' : 'Revision'} ${selected?.revision || ''} · ${zh ? '模板与差异' : 'Template and differences'}`}
        size="min(1100px, 100vw)"
        open={Boolean(selected)}
        onClose={() => setSelected(null)}
        destroyOnHidden
      >
        {selected && (
          <Suspense fallback={<Spin />}>
            <Revision
              key={`${selected.namespace}/${selected.name}/${selected.revision}`}
              record={selected}
              records={records}
              currentRevision={currentRevision}
              scope={scope}
            />
          </Suspense>
        )}
      </Drawer>
    </Card>
  )
}
