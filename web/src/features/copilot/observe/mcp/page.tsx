import { useMemo, useState } from 'react'
import { Button, Card, Flex, Typography } from 'antd'
import { ApiOutlined, SettingOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { ManagementState, ManagementToolbarSearch } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { observeQueries } from '../queries'
import './styles.css'

const { Paragraph, Text } = Typography

export function AIMCPPage() {
  const navigate = useNavigate()
  const [filter, setFilter] = useState('')
  const catalogQuery = useQuery(observeQueries.tools.catalog())
  const adapters = useMemo(() => catalogQuery.data?.adapters ?? [], [catalogQuery.data?.adapters])
  const dataSources = useMemo(
    () => catalogQuery.data?.dataSources ?? [],
    [catalogQuery.data?.dataSources],
  )
  const visibleAdapters = useMemo(() => {
    const keyword = filter.trim().toLowerCase()
    if (!keyword) return adapters
    return adapters.filter((item) =>
      [item.id, item.name, item.description, item.sourceKind, ...(item.supportedBackends ?? [])]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword)),
    )
  }, [adapters, filter])
  return (
    <div className="soha-page soha-mcp-page">
      <Flex className="soha-mcp-toolbar" align="center" justify="space-between">
        <ManagementToolbarSearch
          placeholder="搜索 MCP Adapter"
          size={280}
          value={filter}
          onChange={setFilter}
        />
        <Button
          icon={<SettingOutlined />}
          size="small"
          onClick={() => navigate('/ai-workbench/tool-settings')}
        >
          会话装配
        </Button>
      </Flex>

      {catalogQuery.isLoading ? (
        <ManagementState compact kind="loading" title="正在加载 MCP Adapter" />
      ) : catalogQuery.isError ? (
        <ManagementState compact kind="error" title="MCP Adapter 加载失败" />
      ) : visibleAdapters.length === 0 ? (
        <ManagementState
          compact
          title={adapters.length === 0 ? '暂无 MCP Adapter' : '没有匹配的 MCP Adapter'}
        />
      ) : (
        <div className="soha-mcp-card-list" role="list">
          {visibleAdapters.map((adapter) => {
            const linkedSources = dataSources.filter(
              (source) => source.enabled && source.mcpAdapter === adapter.id,
            )
            return (
              <Card className="soha-mcp-card" key={adapter.id} role="listitem" size="small">
                <span className="soha-mcp-card__icon" aria-hidden="true">
                  <ApiOutlined />
                </span>
                <div className="soha-mcp-card__identity">
                  <Text strong>{adapter.name}</Text>
                  <Text type="secondary" copyable={{ text: adapter.id }}>
                    {adapter.id}
                  </Text>
                </div>
                <div className="soha-mcp-card__content">
                  <Paragraph type="secondary" ellipsis={{ rows: 2 }}>
                    {adapter.description || '暂无说明'}
                  </Paragraph>
                  <Flex gap={6} wrap>
                    <MetadataTag label={adapter.sourceKind} />
                    {(adapter.supportedBackends ?? []).map((backend) => (
                      <MetadataTag key={backend} label={backend} />
                    ))}
                    {adapter.supportsSessionOverride ? (
                      <MetadataTag tone="blue" label="session override" />
                    ) : null}
                  </Flex>
                </div>
                <div className="soha-mcp-card__stats">
                  <Text type="secondary">{adapter.tools?.length ?? 0} tools</Text>
                  <Text type="secondary">{linkedSources.length} data sources</Text>
                </div>
              </Card>
            )
          })}
        </div>
      )}
    </div>
  )
}
