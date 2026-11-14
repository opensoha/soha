import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Card, Input, Select, Spin, Typography } from 'antd'
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { platformOverviewQueries } from './queries'
import type { OverviewResourceKind } from './types'

const { Search } = Input
const { Text } = Typography

export interface OverviewResourceKindOption {
  label: string
  value: OverviewResourceKind
}

interface ResourceFinderProps {
  clusterId: string | null
  clusterName?: string
  localeCode: 'zh_CN' | 'en_US'
  options: OverviewResourceKindOption[]
}

export function ResourceFinder({
  clusterId,
  clusterName,
  localeCode,
  options,
}: ResourceFinderProps) {
  const navigate = useNavigate()
  const [kind, setKind] = useState<OverviewResourceKind>(options[0]?.value ?? 'pods')
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const effectiveKind = options.some((item) => item.value === kind) ? kind : options[0]?.value
  const query = useQuery(
    platformOverviewQueries.resourceSearch(
      clusterId,
      effectiveKind ?? 'pods',
      keyword,
      options.length > 0,
    ),
  )
  const visibleQueryData = readableQueryData(query)
  const kindLabel = useMemo(
    () => options.find((item) => item.value === effectiveKind)?.label ?? '',
    [effectiveKind, options],
  )
  const results = visibleQueryData?.items ?? []

  return (
    <Card
      className="soha-overview-runtime-card soha-platform-resource-finder"
      title={localeCode === 'zh_CN' ? '资源快速定位' : 'Resource Finder'}
      extra={
        <Text type="secondary" className="text-xs">
          {clusterName || clusterId || '-'}
        </Text>
      }
    >
      <ReadFeedback canRead={query.isEnabled} query={query} label="资源记录" />

      {!clusterId ? (
        <ManagementState
          bordered={false}
          compact
          kind="select-scope"
          title={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'}
        />
      ) : options.length === 0 ? (
        <ManagementState
          bordered={false}
          compact
          kind="no-permission"
          title={localeCode === 'zh_CN' ? '无权限查询资源' : 'No resource search access'}
        />
      ) : (
        <>
          <div className="soha-platform-resource-searchbar">
            <Select
              aria-label={localeCode === 'zh_CN' ? '资源类型' : 'Resource kind'}
              options={options}
              value={effectiveKind}
              onChange={(value) => {
                setKind(value)
                setKeyword('')
              }}
            />
            <Search
              allowClear
              value={draft}
              placeholder={
                localeCode === 'zh_CN'
                  ? `搜索 ${kindLabel} 名称、命名空间或状态`
                  : `Search ${kindLabel} by name, namespace, or status`
              }
              onChange={(event) => {
                setDraft(event.target.value)
                if (!event.target.value) setKeyword('')
              }}
              onSearch={(value) => setKeyword(value.trim())}
            />
          </div>

          {query.isFetching ? (
            <div className="soha-platform-ops-loading">
              <Spin size="small" />
            </div>
          ) : query.isError ? (
            <ManagementState
              bordered={false}
              compact
              kind="error"
              title={localeCode === 'zh_CN' ? '资源查询失败' : 'Resource search failed'}
            />
          ) : keyword && results.length === 0 ? (
            <ManagementState
              bordered={false}
              compact
              title={localeCode === 'zh_CN' ? '没有匹配的资源' : 'No matching resources'}
            />
          ) : results.length > 0 ? (
            <>
              <div className="soha-platform-resource-results">
                {results.map((item) => (
                  <button
                    key={item.key}
                    type="button"
                    className="soha-platform-resource-result"
                    onClick={() => navigate(item.path)}
                  >
                    <span className="soha-platform-resource-result-copy">
                      <Text strong>{item.name}</Text>
                      <span>
                        {item.namespace || (localeCode === 'zh_CN' ? '集群级' : 'Cluster scoped')}
                      </span>
                    </span>
                    <span className="soha-platform-resource-result-meta">
                      <span>{kindLabel}</span>
                      {item.status ? <StatusTag value={item.status} /> : null}
                    </span>
                  </button>
                ))}
              </div>
              {visibleQueryData?.truncated ? (
                <Text className="soha-platform-resource-search-meta" type="secondary">
                  {localeCode === 'zh_CN'
                    ? `仅展示前 ${results.length} 条匹配结果`
                    : `Showing the first ${results.length} matches`}
                </Text>
              ) : null}
            </>
          ) : null}
        </>
      )}
    </Card>
  )
}
