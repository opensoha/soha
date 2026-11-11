import { useMemo, useState } from 'react'
import { Select, Space, Typography, type TableColumnsType } from 'antd'
import { AdminTable } from '@/components/admin-table'
import { StatusTag } from '@/components/status-tag'
import { localeText, useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { coverageLabels } from './model'
import type { TopologyCoverage } from './types'

const { Text } = Typography
const reasons: Record<string, [string, string]> = {
  timeout: ['读取超时，可重试或缩小范围', 'Read timed out. Retry or narrow the scope.'],
  source_unavailable: ['来源暂不可用', 'Source unavailable'],
  soha_permission: ['缺少 Soha 读取权限', 'Soha read permission is missing'],
  agent_action_denied: ['Agent 未授权此读取动作', 'Agent read action is not allowed'],
  kubernetes_rbac: ['Kubernetes RBAC 拒绝读取', 'Kubernetes RBAC denied this read'],
}
export function TopologyCoveragePanel({
  coverage,
  generatedAt,
  observedAt,
}: {
  coverage: TopologyCoverage[]
  generatedAt?: string
  observedAt?: string
}) {
  const { localeCode, t } = useI18n()
  const [provider, setProvider] = useState('')
  const [attentionOnly, setAttentionOnly] = useState(false)
  const attention = coverage.filter((item) => item.state !== 'available' || item.truncated)
  const groups = useMemo(() => [...new Set(coverage.map((item) => item.provider))], [coverage])
  const rows = coverage.filter(
    (item) =>
      (!provider || provider === item.provider) &&
      (!attentionOnly || item.state !== 'available' || item.truncated),
  )
  const columns: TableColumnsType<TopologyCoverage> = [
    {
      key: 'source',
      title: localeText(localeCode, '来源 / 范围', 'Source / Scope'),
      width: 210,
      render: (_, item) => (
        <div className="soha-topology-coverage-cell">
          <Text strong>{item.provider}</Text>
          <span className="soha-topology-wrap">
            {item.sourceId.replace(`${item.provider}.`, '')}
          </span>
          <Text type="secondary">
            {item.namespace ||
              localeText(localeCode, '查询范围全部命名空间', 'All namespaces in scope')}
          </Text>
        </div>
      ),
    },
    {
      key: 'state',
      title: localeText(localeCode, '读取结果', 'Read result'),
      width: 230,
      render: (_, item) => {
        const reason = item.reasonCode && reasons[item.reasonCode]
        return (
          <div className="soha-topology-coverage-cell">
            <StatusTag
              value={
                item.state === 'available' && !item.truncated
                  ? 'healthy'
                  : item.state === 'error'
                    ? 'error'
                    : 'warning'
              }
              label={t(
                `network.topology.coverage.${item.state}`,
                coverageLabels[item.state] || item.state,
              )}
            />
            {reason && <span>{localeText(localeCode, ...reason)}</span>}
            {item.reasonCode && (
              <Text type="secondary" className="soha-topology-wrap">
                {item.reasonCode}
              </Text>
            )}
            {item.truncated && (
              <Text type="warning">
                {localeText(localeCode, '已达到数据预算', 'Data budget reached')}
              </Text>
            )}
          </div>
        )
      },
    },
    {
      key: 'time',
      title: localeText(localeCode, '来源时间', 'Source time'),
      width: 155,
      render: (_, item) => (
        <div className="soha-topology-coverage-cell">
          <Text type="secondary">
            {item.observedAt
              ? localeText(localeCode, '采样', 'Observed')
              : localeText(localeCode, '检查', 'Checked')}
          </Text>
          <span>
            {item.observedAt || item.checkedAt
              ? formatDateTime(item.observedAt || item.checkedAt)
              : '—'}
          </span>
        </div>
      ),
    },
  ]
  return (
    <div className="soha-topology-coverage-panel">
      <div className="soha-topology-coverage-summary">
        <span>
          <strong>{coverage.length - attention.length}</strong>
          {localeText(localeCode, '项可完整读取', 'fully readable')}
        </span>
        <span>
          <strong>{attention.length}</strong>
          {localeText(localeCode, '项需关注', 'need attention')}
        </span>
      </div>
      <Text type="secondary">
        {localeText(
          localeCode,
          '计数只覆盖已加载内容；未读取的引用不等于资源不存在。',
          'Counts cover loaded content only. Unread references do not establish that a resource is absent.',
        )}
      </Text>
      <Space wrap>
        <Select
          aria-label={localeText(localeCode, '覆盖来源', 'Coverage provider')}
          value={provider}
          onChange={setProvider}
          options={[
            { value: '', label: localeText(localeCode, '全部 Provider', 'All providers') },
            ...groups.map((value) => ({ value, label: value })),
          ]}
        />
        <Select
          aria-label={localeText(localeCode, '覆盖结果', 'Coverage result')}
          value={attentionOnly ? 'attention' : 'all'}
          onChange={(value) => setAttentionOnly(value === 'attention')}
          options={[
            { value: 'all', label: localeText(localeCode, '全部结果', 'All results') },
            { value: 'attention', label: localeText(localeCode, '仅需关注', 'Needs attention') },
          ]}
        />
      </Space>
      <AdminTable
        rowKey={(item) => `${item.sourceId}:${item.namespace || ''}`}
        columns={columns}
        dataSource={rows}
        pagination={false}
        enableColumnSelection={false}
        scroll={{ x: 595, y: 430 }}
      />
      <Text type="secondary">
        {localeText(localeCode, '生成于', 'Generated ')}
        {formatDateTime(generatedAt)} · {localeText(localeCode, '来源采样', 'Source observed ')}
        {observedAt ? formatDateTime(observedAt) : localeText(localeCode, '未提供', 'Unavailable')}
      </Text>
      <details className="soha-topology-metadata">
        <summary>
          {localeText(localeCode, '读取权限与插件配置', 'Read permissions and provider setup')}
        </summary>
        <p>
          {localeText(
            localeCode,
            'API 安装、读取权限和数据预算分别影响覆盖范围。插件需要 Agent 动作、已保存的自定义资源授权及 Kubernetes RBAC。',
            'Installed APIs, read permissions and data budgets independently affect coverage. Providers require the Agent action, saved custom-resource grants and Kubernetes RBAC.',
          )}
        </p>
        <p>
          Traefik · <code>traefik.io</code>: ingressroutes, ingressroutetcps, ingressrouteudps,
          traefikservices, middlewares, middlewaretcps.
          <br />
          APISIX · <code>apisix.apache.org</code>: apisixroutes, apisixupstreams, apisixtls.
        </p>
        <p>
          {localeText(
            localeCode,
            '资源至少需要 list，Agent 还需 platform.custom_resources.list。在集群保存指定 namespace 的授权后，审查并应用生成的 RBAC；保存配置不会自动扩大运行中 Agent 权限。',
            'Resources require list and the Agent requires platform.custom_resources.list. Save grants for the required namespaces, then review and apply generated RBAC. Saving settings does not expand running Agent permissions.',
          )}
        </p>
      </details>
    </div>
  )
}
