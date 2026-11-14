import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Empty, Input, Space, Typography } from 'antd'
import { ArrowRightOutlined } from '@ant-design/icons'
import { useInfiniteQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import type {
  ComputeDomain,
  ComputeResourceKind,
  ComputeResourceRef,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { usePermissionSnapshot } from '@/features/auth'
import { canAccessRoute, getRouteMeta } from '@/routes/meta'
import { localeText, useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { computeQueries } from './queries'

import './compute.css'

const { Text } = Typography
const RELATION_LABELS: Record<string, [string, string]> = {
  connected_by: ['连接于', 'Connected by'],
  contains: ['包含', 'Contains'],
  derived_from: ['来源于', 'Derived from'],
  exposes: ['暴露', 'Exposes'],
  manages: ['管理', 'Manages'],
  provisions: ['供给', 'Provisions'],
  runs_on: ['运行于', 'Runs on'],
}
const RELATION_SOURCE_LABELS: Record<string, [string, string]> = {
  derived: ['推导关系', 'Derived'],
  persisted: ['存储关系', 'Persisted'],
  provider: ['提供方关系', 'Provider'],
}
export function computeResourceIdentity(
  resource: Pick<ComputeResourceRef, 'domain' | 'kind' | 'id'>,
) {
  return JSON.stringify([resource.domain, resource.kind, resource.id])
}
function resourcePath(resource: ComputeResourceRef) {
  if (resource.domain === 'virtualization' && resource.kind === 'vm')
    return `/compute/virtualization/vms/${encodeURIComponent(resource.id)}`
  if (resource.domain === 'container_runtime' && resource.kind === 'runtime_host')
    return `/compute/runtimes/hosts/${encodeURIComponent(resource.id)}`
  if (resource.domain === 'container_runtime' && resource.kind === 'project')
    return `/compute/runtimes/projects/${encodeURIComponent(resource.id)}`
  return undefined
}
function ResourceLink({ resource }: { resource: ComputeResourceRef }) {
  const { data } = usePermissionSnapshot()
  const path = resourcePath(resource)
  const label = resource.displayName || resource.id
  return path && canAccessRoute(getRouteMeta(path), data?.data) ? (
    <Link to={path}>{label}</Link>
  ) : (
    <Text strong>{label}</Text>
  )
}
interface RelationsProps {
  domain: ComputeDomain
  kind: ComputeResourceKind
  resourceId: string
}
export function ComputeRelationsPanel(props: RelationsProps) {
  return (
    <RelationsExplorer
      key={JSON.stringify([props.domain, props.kind, props.resourceId])}
      {...props}
    />
  )
}
function RelationsExplorer({ domain, kind, resourceId }: RelationsProps) {
  const { localeCode } = useI18n()
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<string>()
  const query = useInfiniteQuery({
    ...computeQueries.resourceRelations(domain, kind, resourceId),
    retry: false,
  })
  const data = readableQueryData(query)
  const relations = data?.relations ?? []
  const nodes = useMemo(() => {
    const records = new Map<string, ComputeResourceRef>()
    if (data) {
      records.set(computeResourceIdentity(data.resource), data.resource)
      data.relations.forEach((relation) => {
        records.set(computeResourceIdentity(relation.from), relation.from)
        records.set(computeResourceIdentity(relation.to), relation.to)
      })
    }
    return [...records.values()]
  }, [data])
  useEffect(() => {
    if (data && selected && !nodes.some((node) => computeResourceIdentity(node) === selected))
      setSelected(undefined)
  }, [data, nodes, selected])
  const selectedNode = nodes.find((node) => computeResourceIdentity(node) === selected)
  const keyword = search.trim().toLowerCase()
  const filtered = relations.filter((relation) => {
    if (
      selected &&
      ![relation.from, relation.to].some((node) => computeResourceIdentity(node) === selected)
    )
      return false
    return (
      !keyword ||
      [
        relation.from.displayName,
        relation.from.id,
        relation.to.displayName,
        relation.to.id,
        relation.type,
        RELATION_LABELS[relation.type]?.[localeCode === 'zh_CN' ? 0 : 1],
      ].some((value) => value?.toLowerCase().includes(keyword))
    )
  })
  return (
    <section
      className="soha-compute-relations"
      aria-label={localeText(localeCode, '资源关系探索', 'Resource relationships')}
    >
      <div className="soha-compute-relations-toolbar">
        <Space wrap>
          <Text strong>{data?.resource.displayName || resourceId}</Text>
          {data ? (
            <Text type="secondary">
              {localeText(
                localeCode,
                `本次读取 ${relations.length} 条关系`,
                `${relations.length} relationships in this result`,
              )}
            </Text>
          ) : null}
        </Space>
        <Space wrap>
          <Input
            size="small"
            allowClear
            aria-label={localeText(localeCode, '筛选资源关系', 'Filter relationships')}
            placeholder={localeText(localeCode, '名称、ID 或关系类型', 'Name, ID or relationship')}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            className="soha-compute-relations-search"
          />
          <ManagementRefreshButton
            size="small"
            tooltip={localeText(localeCode, '刷新资源关系', 'Refresh relationships')}
            loading={query.isFetching}
            onClick={() => void query.refetch()}
          />
        </Space>
      </div>
      <Text type="secondary">
        {localeText(
          localeCode,
          '选择关联对象，查看本次记录中的关系与来源；关系不代表实时流量。',
          'Select an object to inspect relationships and sources in this result; these are not live traffic.',
        )}
      </Text>
      <ReadFeedback query={query} label={localeText(localeCode, '资源关系', 'Relationships')} />
      {query.isLoading ? <ManagementState kind="loading" compact bordered={false} /> : null}
      {data ? (
        <>
          {data.nextCursor ? (
            <Alert
              type="info"
              showIcon
              action={
                <Button
                  size="small"
                  loading={query.isFetchingNextPage}
                  disabled={query.isFetching}
                  onClick={() => void query.fetchNextPage()}
                >
                  {localeText(localeCode, '继续加载关系', 'Load more relationships')}
                </Button>
              }
              title={localeText(
                localeCode,
                '仍有后续关系记录，当前结果不代表完整关系链',
                'More relationship records exist; this result is not the complete dependency chain',
              )}
            />
          ) : null}
          {relations.length ? (
            <div
              className="soha-compute-relation-nodes"
              aria-label={localeText(localeCode, '关联对象选择', 'Related object selection')}
            >
              <Button
                size="small"
                type={!selected ? 'primary' : 'default'}
                aria-pressed={!selected}
                onClick={() => setSelected(undefined)}
              >
                {localeText(localeCode, '全部关系', 'All relationships')}
              </Button>
              {nodes.map((node) => (
                <Button
                  key={computeResourceIdentity(node)}
                  size="small"
                  type={selected === computeResourceIdentity(node) ? 'primary' : 'default'}
                  aria-pressed={selected === computeResourceIdentity(node)}
                  aria-label={localeText(
                    localeCode,
                    `选中关联对象 ${node.displayName || node.id}`,
                    `Select related object ${node.displayName || node.id}`,
                  )}
                  onClick={() => setSelected(computeResourceIdentity(node))}
                >
                  {node.displayName || node.id}
                </Button>
              ))}
            </div>
          ) : null}
          {selectedNode ? (
            <div
              className="soha-compute-relation-selected"
              aria-label={localeText(localeCode, '选中关联对象', 'Selected related object')}
            >
              <Space wrap>
                <ResourceLink resource={selectedNode} />
                <MetadataTag label={selectedNode.kind} tone="blue" />
                <Text copyable>{selectedNode.id}</Text>
              </Space>
            </div>
          ) : null}
          {filtered.length ? (
            <div className="soha-compute-relation-list">
              {filtered.map((relation, index) => (
                <div
                  className={`soha-compute-relation-row${selected ? ' is-selected' : ''}`}
                  key={`${computeResourceIdentity(relation.from)}:${relation.type}:${computeResourceIdentity(relation.to)}:${relation.source}:${index}`}
                >
                  <div className="soha-compute-relation-chain">
                    <ResourceLink resource={relation.from} />
                    <Space size={6}>
                      <ArrowRightOutlined aria-hidden />
                      <MetadataTag
                        label={
                          RELATION_LABELS[relation.type]?.[localeCode === 'zh_CN' ? 0 : 1] ??
                          relation.type
                        }
                        tone="blue"
                      />
                      <ArrowRightOutlined aria-hidden />
                    </Space>
                    <ResourceLink resource={relation.to} />
                  </div>
                  <Space size={6} wrap>
                    <MetadataTag
                      label={
                        RELATION_SOURCE_LABELS[relation.source]?.[localeCode === 'zh_CN' ? 0 : 1] ??
                        relation.source
                      }
                      tone="cyan"
                    />
                    {relation.stale ? (
                      <StatusTag
                        value="stale"
                        label={localeText(localeCode, '数据陈旧', 'Stale')}
                      />
                    ) : null}
                    <Text type="secondary">{formatDateTime(relation.observedAt)}</Text>
                  </Space>
                </div>
              ))}
            </div>
          ) : (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={localeText(
                localeCode,
                relations.length
                  ? '当前筛选没有关系'
                  : data.nextCursor
                    ? '当前批次未返回关系，仍有后续记录'
                    : '暂无资源关系',
                relations.length
                  ? 'No relationships match the selection'
                  : data.nextCursor
                    ? 'No relations in this batch; more records exist'
                    : 'No relationships',
              )}
            />
          )}
        </>
      ) : null}
    </section>
  )
}
