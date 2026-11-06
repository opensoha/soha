import { AimOutlined, EditOutlined } from '@ant-design/icons'
import { Alert, Button, Descriptions, Space, Typography } from 'antd'
import { Link, useNavigate } from 'react-router-dom'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { localeText, useI18n } from '@/i18n'
import {
  edgeLabel,
  resolutionLabels,
  topologyResourcePath,
  topologyResourceEditPath,
} from './model'
import type { TopologyNode, TopologyEdge } from './types'

const { Text } = Typography

export function Resolution({ value }: { value: string }) {
  const { t } = useI18n()
  return (
    <StatusTag
      value={value === 'resolved' ? 'healthy' : value === 'external' ? 'unknown' : 'warning'}
      label={t(`network.topology.resolution.${value}`, resolutionLabels[value] || value)}
    />
  )
}
export function NodeEvidence({
  node,
  edges,
  nodes,
  clusterId,
  canOpen,
  canBrowseCRD,
  canEdit,
  onFocus,
  onSelect,
}: {
  node: TopologyNode
  edges: TopologyEdge[]
  nodes: TopologyNode[]
  clusterId: string
  canOpen: boolean
  canBrowseCRD: boolean
  canEdit: boolean
  onFocus: () => void
  onSelect: (id: string) => void
}) {
  const { localeCode, t } = useI18n()
  const navigate = useNavigate()
  const editPath = canOpen && canEdit ? topologyResourceEditPath(node, clusterId) : undefined
  const path = canOpen ? topologyResourcePath(node, clusterId) : undefined
  const byId = new Map(nodes.map((item) => [item.id, item]))
  const relations = edges.filter((edge) => edge.source === node.id || edge.target === node.id)
  return (
    <div className="soha-topology-evidence">
      <header className="soha-topology-resource-identity">
        <Space wrap>
          <MetadataTag label={node.resource?.kind || node.type} tone="blue" />
          <Resolution value={node.resolutionState} />
          {node.protocol && <MetadataTag label={node.protocol} />}
        </Space>
        <h3>{node.label}</h3>
        <Text type="secondary">
          {node.resource?.namespace || localeText(localeCode, '集群范围', 'Cluster scope')} ·{' '}
          {node.provider}
        </Text>
        <Space wrap>
          {editPath && (
            <Button
              icon={<EditOutlined aria-hidden />}
              type="primary"
              onClick={() => navigate(editPath)}
            >
              {localeText(localeCode, '编辑 YAML', 'Edit YAML')}
            </Button>
          )}
          {path && <Link to={path}>{localeText(localeCode, '查看资源详情', 'View resource')}</Link>}
          <Button icon={<AimOutlined aria-hidden />} onClick={onFocus}>
            {localeText(localeCode, '聚焦上下游', 'Focus upstream and downstream')}
          </Button>
        </Space>
      </header>
      {canBrowseCRD &&
        node.resource &&
        (['traefik', 'apisix'].includes(node.provider) ||
          ['TCPRoute', 'TLSRoute', 'UDPRoute'].includes(node.resource.kind)) && (
          <Link
            to={`/extensions/apis/${encodeURIComponent(node.resource.apiVersion.split('/')[0])}`}
          >
            {localeText(
              localeCode,
              '进入此 API 组的 CRD 工作区',
              'Open the CRD workspace for this API group',
            )}
          </Link>
        )}
      <details className="soha-topology-metadata">
        <summary>{localeText(localeCode, '资源元数据', 'Resource metadata')}</summary>
        <Descriptions
          column={1}
          size="small"
          items={[
            { key: 'api', label: 'API', children: node.resource?.apiVersion || '—' },
            {
              key: 'uid',
              label: 'UID',
              children: <span className="soha-topology-wrap">{node.resource?.uid || '—'}</span>,
            },
            {
              key: 'version',
              label: localeText(localeCode, '资源版本', 'Resource version'),
              children: node.resource?.resourceVersion || '—',
            },
            ...(node.generation != null
              ? [{ key: 'generation', label: 'Generation', children: node.generation }]
              : []),
          ]}
        />
      </details>
      {node.rule && (
        <>
          <Text strong>{localeText(localeCode, '匹配规则', 'Match rules')}</Text>
          {node.rule.matchSummary && (
            <Text className="soha-topology-wrap">{node.rule.matchSummary}</Text>
          )}
          {(node.rule.matchGroups ?? []).map((group, index) => (
            <div className="soha-topology-match" key={index}>
              <Text type="secondary">
                {index > 0
                  ? localeText(localeCode, '或（OR）', 'OR')
                  : localeText(localeCode, '匹配组', 'Match group')}{' '}
                {index + 1}
              </Text>
              {group.map((match, j) => (
                <div key={j}>
                  {j > 0 ? localeText(localeCode, '且', 'AND ') : ''}
                  {match.type} {match.name} {match.operator} <Text code>{match.value || '*'}</Text>
                </div>
              ))}
            </div>
          ))}
          {node.rule.priority !== undefined && (
            <Text>
              {localeText(localeCode, '优先级：', 'Priority: ')}
              {node.rule.priority}
            </Text>
          )}
          {node.rule.tlsMode && <Text>TLS：{node.rule.tlsMode}</Text>}
        </>
      )}
      {(node.properties ?? []).map((property, index) => (
        <div key={index}>
          <Text type="secondary">{property.name}：</Text>
          <Text className="soha-topology-wrap">
            {property.value || localeText(localeCode, '未配置', 'Not configured')}
          </Text>
        </div>
      ))}
      {(node.conditions ?? []).map((condition, index) => (
        <Alert
          key={index}
          type={
            condition.status === 'True' && condition.observedGeneration === node.generation
              ? 'success'
              : 'warning'
          }
          title={`${condition.type}: ${condition.status}`}
          description={`${condition.reason || ''} · observedGeneration=${condition.observedGeneration ?? localeText(localeCode, '未知', 'Unknown')} · ${condition.lastTransitionTime || localeText(localeCode, '时间未知', 'Time unknown')}`}
        />
      ))}
      {(['up', 'down'] as const).map((direction) => {
        const linked = relations.filter((edge) =>
          direction === 'up' ? edge.target === node.id : edge.source === node.id,
        )
        return (
          <section className="soha-topology-neighbors" key={direction}>
            <h4>
              {localeText(
                localeCode,
                direction === 'up' ? '上游来源' : '下游目标',
                direction === 'up' ? 'Upstream' : 'Downstream',
              )}{' '}
              <MetadataTag label={String(linked.length)} />
            </h4>
            {linked.length ? (
              linked.map((edge) => {
                const neighbor = byId.get(direction === 'up' ? edge.source : edge.target)
                return (
                  <div className="soha-topology-relation" key={edge.id}>
                    {neighbor ? (
                      <Button
                        type="link"
                        onClick={() => onSelect(neighbor.id)}
                        aria-label={`${localeText(localeCode, '查看关联资源', 'Inspect related resource')} ${neighbor.label}`}
                      >
                        <span className="soha-topology-neighbor-name">{neighbor.label}</span>
                      </Button>
                    ) : (
                      <Text>{localeText(localeCode, '不可见', 'Not visible')}</Text>
                    )}
                    {neighbor?.resource && (
                      <Text type="secondary">
                        {neighbor.resource.kind} / {neighbor.resource.name} ·{' '}
                        {neighbor.resource.namespace || localeText(localeCode, '集群', 'Cluster')}
                      </Text>
                    )}
                    <Text type="secondary">
                      {edgeLabel(edge, localeCode, direction === 'down')} ·{' '}
                      {t(
                        `network.topology.resolution.${edge.resolutionState}`,
                        resolutionLabels[edge.resolutionState],
                      )}
                    </Text>
                    {edge.fieldPath && (
                      <Text type="secondary" className="soha-topology-wrap">
                        {localeText(localeCode, '字段依据', 'Field path')}：{edge.fieldPath}
                      </Text>
                    )}
                    {edge.note && (
                      <Text type="secondary" className="soha-topology-wrap">
                        {edge.note}
                      </Text>
                    )}
                  </div>
                )
              })
            ) : (
              <Text type="secondary">
                {localeText(localeCode, '没有已加载的关系', 'No loaded relationships')}
              </Text>
            )}
          </section>
        )
      })}
    </div>
  )
}
