import { useState } from 'react'
import { Button, Card, Tooltip } from 'antd'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { AdminTable } from '@/components/admin-table'
import { ManagementState, ManagementToolbarSearch } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import type { ClusterRoleConfiguration, PolicyRule } from './detail-model'

function Values({ values, core = false }: { values?: string[]; core?: boolean }) {
  return (
    <div className="soha-clusterrole-rule-values">
      {values?.length
        ? values.map((value, index) => (
            <span key={index}>{core && value === '' ? 'core ("")' : value}</span>
          ))
        : '—'}
    </div>
  )
}

export function ClusterRoleRules({ configuration }: { configuration?: ClusterRoleConfiguration }) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const [search, setSearch] = useState('')
  if (!configuration)
    return (
      <ManagementState
        kind="empty"
        title={zh ? '权限规则配置尚不可用' : 'Permission rule configuration unavailable'}
      />
    )
  const query = search.trim().toLowerCase()
  const rows = configuration.rules.map((rule, index) => ({ ...rule, key: String(index) }))
  const filtered = rows.filter((rule) =>
    [
      ...(rule.apiGroups?.map((group) => (group === '' ? 'core ""' : group)) ?? []),
      ...(rule.resources ?? []),
      ...rule.verbs,
      ...(rule.resourceNames ?? []),
      ...(rule.nonResourceURLs ?? []),
    ]
      .join(' ')
      .toLowerCase()
      .includes(query),
  )
  return (
    <Card
      className="soha-detail-card"
      title={
        <span className="soha-clusterrole-section-title">
          {zh ? '权限规则' : 'Permission rules'}
          <Tooltip
            trigger={['hover', 'focus', 'click']}
            title={
              zh
                ? '规则各字段按原值展示；* 保留通配语义。资源中的 / 表示子资源；resourceNames 为空表示未限定名称。聚合角色展示当前 rules，不推断来源。'
                : 'Fields retain their original values; * is a wildcard. / identifies subresources. Empty resourceNames means no name restriction. Aggregated roles show current rules without inferring sources.'
            }
          >
            <Button
              type="text"
              size="small"
              icon={<QuestionCircleOutlined />}
              aria-label={zh ? '权限规则说明' : 'About permission rules'}
            />
          </Tooltip>
        </span>
      }
      extra={
        <span>
          {zh
            ? `当前显示 ${filtered.length} / ${rows.length} 条`
            : `Showing ${filtered.length} / ${rows.length}`}
        </span>
      }
    >
      <div className="soha-clusterrole-rules-toolbar">
        <ManagementToolbarSearch
          value={search}
          onChange={setSearch}
          placeholder={
            zh
              ? '搜索 API 组、资源、操作、名称或 URL'
              : 'Search API groups, resources, verbs, names or URLs'
          }
          aria-label={zh ? '搜索权限规则' : 'Search permission rules'}
        />
      </div>
      <AdminTable
        rowKey="key"
        tableSize="small"
        pagination={false}
        enableColumnSelection={false}
        enableDensity={false}
        columnSettingPlacement="hidden"
        dataSource={filtered}
        empty={
          zh
            ? query
              ? '没有匹配的规则'
              : '当前没有权限规则'
            : query
              ? 'No matching rules'
              : 'No current permission rules'
        }
        columns={[
          {
            key: 'kind',
            title: zh ? '类型' : 'Type',
            width: 140,
            render: (_: unknown, rule: PolicyRule) => (
              <div className="soha-clusterrole-rule-values">
                {rule.resources?.length ? <MetadataTag label={zh ? '资源' : 'Resource'} /> : null}
                {rule.nonResourceURLs?.length ? <MetadataTag label="URL" tone="blue" /> : null}
                {!rule.resources?.length && !rule.nonResourceURLs?.length ? '—' : null}
              </div>
            ),
          },
          {
            key: 'apiGroups',
            title: zh ? 'API 组' : 'API groups',
            width: 160,
            render: (_: unknown, rule: PolicyRule) => <Values values={rule.apiGroups} core />,
          },
          {
            key: 'resources',
            title: zh ? '资源 / 子资源' : 'Resources / subresources',
            width: 230,
            render: (_: unknown, rule: PolicyRule) => <Values values={rule.resources} />,
          },
          {
            key: 'verbs',
            title: zh ? '操作' : 'Verbs',
            width: 180,
            render: (_: unknown, rule: PolicyRule) => <Values values={rule.verbs} />,
          },
          {
            key: 'resourceNames',
            title: 'resourceNames',
            width: 210,
            render: (_: unknown, rule: PolicyRule) =>
              rule.resourceNames?.length ? (
                <Values values={rule.resourceNames} />
              ) : (
                <span>{zh ? '未限定名称' : 'No name restriction'}</span>
              ),
          },
          {
            key: 'nonResourceURLs',
            title: zh ? '非资源 URL' : 'Non-resource URLs',
            width: 220,
            render: (_: unknown, rule: PolicyRule) => <Values values={rule.nonResourceURLs} />,
          },
        ]}
        scroll={{ x: 1140 }}
      />
    </Card>
  )
}
