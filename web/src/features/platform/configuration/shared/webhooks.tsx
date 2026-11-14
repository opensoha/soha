import { useDeferredValue, useState } from 'react'
import { Card, Descriptions, Tabs } from 'antd'
import { Link } from 'react-router-dom'
import { ManagementSearchableListPane, ManagementState } from '@/components/management-list'
import { BooleanTag } from '@/components/status-tag'
import { buildRelatedResourcePath } from '@/features/platform/workloads-model'
import { useI18n } from '@/i18n'
import type { AdmissionWebhook } from './types'
import './webhooks.css'

const list = (values?: string[]) => values?.map((value) => value || '(core)').join(', ') || '—'

export function AdmissionWebhooks({ webhooks }: { webhooks: AdmissionWebhook[] }) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const [selection, setSelection] = useState<string>()
  const [search, setSearch] = useState('')
  const keyword = useDeferredValue(search).trim().toLowerCase()
  const filtered = webhooks.filter((webhook) =>
    [webhook.name, webhook.clientTarget, webhook.serviceNamespace].some((value) =>
      value?.toLowerCase().includes(keyword),
    ),
  )
  const selected = filtered.find((webhook) => webhook.name === selection) ?? filtered[0]
  const servicePath = selected?.serviceName
    ? buildRelatedResourcePath(
        {
          kind: 'Service',
          name: selected.serviceName,
          namespace: selected.serviceNamespace,
        },
        selected.serviceNamespace ?? null,
      )
    : undefined
  return (
    <div className="soha-webhook-workspace">
      <ManagementSearchableListPane
        activeKey={selected?.name}
        items={filtered}
        getItemKey={(webhook) => webhook.name}
        onItemSelect={(webhook) => setSelection(webhook.name)}
        searchValue={search}
        onSearchChange={setSearch}
        searchPlaceholder={zh ? '搜索 Webhook / Service' : 'Search webhook / service'}
        emptyTitle={zh ? '暂无 Webhook' : 'No webhooks'}
        renderItem={(webhook) => (
          <>
            <strong className="soha-webhook-name">{webhook.name}</strong>
            <span className="soha-webhook-secondary">{webhook.clientTarget || '—'}</span>
            <span className="soha-webhook-secondary">
              {webhook.rules?.length ?? 0} {zh ? '条规则' : 'rules'} ·{' '}
              {webhook.failurePolicy || '—'}
            </span>
          </>
        )}
      />
      <Card className="soha-detail-card soha-webhook-content-card">
        {selected ? (
          <>
            <div className="soha-webhook-heading">
              <strong className="soha-webhook-name">{selected.name}</strong>
              <span className="soha-webhook-secondary">
                {zh ? '通过 YAML 页编辑并对比更改' : 'Edit and compare changes in the YAML tab'}
              </span>
            </div>
            <Tabs
              key={selected.name}
              items={[
                {
                  key: 'connection',
                  label: zh ? '接入配置' : 'Connection',
                  children: (
                    <Descriptions
                      bordered
                      column={1}
                      size="small"
                      styles={{ content: { overflowWrap: 'anywhere', whiteSpace: 'pre-wrap' } }}
                      items={[
                        {
                          key: 'target',
                          label: zh ? '客户端目标' : 'Client target',
                          children: servicePath ? (
                            <Link to={servicePath}>{selected.clientTarget}</Link>
                          ) : (
                            selected.clientTarget || '—'
                          ),
                        },
                        { key: 'url', label: 'URL', children: selected.url || '—' },
                        {
                          key: 'service',
                          label: 'Service',
                          children: selected.serviceName
                            ? `${selected.serviceNamespace || '—'}/${selected.serviceName}`
                            : '—',
                        },
                        {
                          key: 'path',
                          label: zh ? '服务路径' : 'Service path',
                          children: selected.servicePath || '—',
                        },
                        {
                          key: 'port',
                          label: zh ? '服务端口' : 'Service port',
                          children: selected.servicePort ?? '—',
                        },
                        {
                          key: 'ca',
                          label: zh ? 'CA 已配置' : 'CA configured',
                          children: <BooleanTag value={selected.caBundleConfigured} />,
                        },
                        {
                          key: 'failure',
                          label: 'Failure policy',
                          children: selected.failurePolicy || '—',
                        },
                        {
                          key: 'match',
                          label: 'Match policy',
                          children: selected.matchPolicy || '—',
                        },
                        {
                          key: 'effects',
                          label: 'Side effects',
                          children: selected.sideEffects || '—',
                        },
                        {
                          key: 'timeout',
                          label: zh ? '超时' : 'Timeout',
                          children:
                            selected.timeoutSeconds == null ? '—' : `${selected.timeoutSeconds}s`,
                        },
                        {
                          key: 'versions',
                          label: 'Review versions',
                          children: list(selected.admissionReviewVersions),
                        },
                      ]}
                    />
                  ),
                },
                {
                  key: 'rules',
                  label: zh ? '规则' : 'Rules',
                  children: selected.rules?.length ? (
                    <div className="soha-webhook-rules">
                      {selected.rules.map((rule, index) => (
                        <section key={index} aria-label={`${zh ? '规则' : 'Rule'} ${index + 1}`}>
                          <h3>
                            {zh ? '规则' : 'Rule'} {index + 1}
                          </h3>
                          <Descriptions
                            bordered
                            column={1}
                            size="small"
                            styles={{
                              content: { overflowWrap: 'anywhere', whiteSpace: 'pre-wrap' },
                            }}
                            items={[
                              {
                                key: 'operations',
                                label: 'Operations',
                                children: list(rule.operations),
                              },
                              {
                                key: 'groups',
                                label: 'API groups',
                                children: list(rule.apiGroups),
                              },
                              {
                                key: 'versions',
                                label: 'API versions',
                                children: list(rule.apiVersions),
                              },
                              {
                                key: 'resources',
                                label: 'Resources',
                                children: list(rule.resources),
                              },
                              { key: 'scope', label: 'Scope', children: rule.scope || '—' },
                            ]}
                          />
                        </section>
                      ))}
                    </div>
                  ) : (
                    <ManagementState
                      bordered={false}
                      compact
                      description={zh ? '未配置规则' : 'No rules configured'}
                    />
                  ),
                },
                {
                  key: 'selectors',
                  label: zh ? '选择器' : 'Selectors',
                  children: (
                    <Descriptions
                      bordered
                      column={1}
                      size="small"
                      styles={{ content: { overflowWrap: 'anywhere', whiteSpace: 'pre-wrap' } }}
                      items={[
                        {
                          key: 'namespace',
                          label: 'Namespace selector',
                          children: (
                            <code>
                              {selected.namespaceSelector || (zh ? '未设置' : 'Not configured')}
                            </code>
                          ),
                        },
                        {
                          key: 'object',
                          label: 'Object selector',
                          children: (
                            <code>
                              {selected.objectSelector || (zh ? '未设置' : 'Not configured')}
                            </code>
                          ),
                        },
                      ]}
                    />
                  ),
                },
              ]}
            />
          </>
        ) : (
          <ManagementState
            bordered={false}
            description={zh ? '暂无匹配的 Webhook' : 'No matching webhook'}
          />
        )}
      </Card>
    </div>
  )
}
