import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import type { RolloutHistory } from '@/types'
import { formatDateTime } from '@/utils/time'
import { Button, Card, Drawer, Input, Typography } from 'antd'
import { useState, type ReactNode } from 'react'
import './workload-revision-history.css'

export function WorkloadRevisionHistory({
  records,
  currentRevision,
  loading,
  error,
  canRollback = false,
  blocked = false,
  onRollback,
  onRefresh,
  currentName,
  updateName,
  sourceLabel = 'ReplicaSet',
  renderRevision,
  countsKnown = () => true,
  currentLabel,
}: {
  records: RolloutHistory[]
  currentRevision?: string
  loading: boolean
  error?: string
  canRollback?: boolean
  blocked?: boolean
  onRollback?: (record: RolloutHistory) => void
  onRefresh: () => void
  currentName?: string
  updateName?: string
  sourceLabel?: string
  countsKnown?: (record: RolloutHistory) => boolean
  currentLabel?: string
  renderRevision: (record: RolloutHistory) => ReactNode
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
        (!onlyActive || (countsKnown(row) && row.replicas > 0)) &&
        `${row.revision} ${row.name} ${row.images?.join(' ') || ''}`
          .toLowerCase()
          .includes(search.trim().toLowerCase()),
    )
  return (
    <Card
      className="soha-detail-card soha-workload-history"
      title={zh ? '版本历史' : 'Revision history'}
      extra={
        <Typography.Text type="secondary">
          {sorted.length} / {records.length}
        </Typography.Text>
      }
      loading={loading}
    >
      <div className="soha-k8s-detail-toolbar">
        <Input
          aria-label={zh ? '搜索版本' : 'Search revisions'}
          allowClear
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={
            zh ? `搜索版本 / ${sourceLabel} / 镜像` : `Search revision / ${sourceLabel} / image`
          }
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
        className="soha-workload-history-list"
        role="list"
        tabIndex={0}
        aria-label={zh ? '连续版本历史' : 'Continuous revision history'}
      >
        {sorted.map((record) => (
          <article
            key={`${record.namespace}/${record.name}/${record.revision}`}
            className="soha-workload-history-row"
            role="listitem"
          >
            <div>
              <strong className="soha-rollout-history-revision">
                {zh ? '版本' : 'Revision'} {record.revision}
              </strong>
              {(currentName
                ? record.name === currentName
                : record.revision === currentRevision) && (
                <MetadataTag
                  label={
                    currentLabel ||
                    (currentName
                      ? zh
                        ? '当前版本'
                        : 'Current revision'
                      : zh
                        ? '当前模板'
                        : 'Current template')
                  }
                  tone="blue"
                />
              )}
              {updateName === record.name && (
                <MetadataTag label={zh ? '目标版本' : 'Target revision'} tone="blue" />
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
                  onClick={() => onRollback?.(record)}
                >
                  {zh ? '回滚到此版本' : 'Roll back to this revision'}
                </Button>
              )}
            </div>
            <p>
              {record.name} · {zh ? '副本' : 'Replicas'}{' '}
              {countsKnown(record) ? record.replicas : '—'} · {zh ? '就绪' : 'Ready'}{' '}
              {countsKnown(record) ? record.readyReplicas : '—'}
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
        {selected && renderRevision(selected)}
      </Drawer>
    </Card>
  )
}
