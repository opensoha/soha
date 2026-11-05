import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { Card, Typography } from 'antd'
import { type ReactNode } from 'react'
import {
  policySelectorTerms,
  type PolicyConfiguration,
  type PolicyObject,
} from './networkpolicy-model'
import type { NetworkPolicyDetail, NetworkPolicyPod } from './types'
import '../gateway-api/gateway.css'
import './networkpolicy.css'

export function PolicySelector({ selector, all }: { selector: PolicyObject; all: string }) {
  const terms = policySelectorTerms(selector)
  return (
    <div className="soha-policy-terms">
      {terms.length ? (
        terms.map((term, i) => (
          <span className="soha-policy-term" key={`${term}/${i}`}>
            {i > 0 && <small>AND</small>}
            <code>{term}</code>
          </span>
        ))
      ) : (
        <span>{all}</span>
      )}
    </div>
  )
}
export function PolicyTypes({ types }: { types?: readonly string[] }) {
  return (
    <div className="soha-policy-types">
      {types?.length ? (
        types.map((t, i) => <MetadataTag key={`${t}/${i}`} label={t} />)
      ) : (
        <span>—</span>
      )}
    </div>
  )
}

export function PolicyRules({
  detail,
  config,
}: {
  detail: NetworkPolicyDetail
  config?: PolicyConfiguration
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  if (!config)
    return (
      <Card className="soha-detail-card" title={zh ? '规则摘要' : 'Rule summary'}>
        <Typography.Paragraph>
          {zh
            ? '完整规则未返回，当前摘要不能区分省略选择器与空选择器；请查看 YAML，不据此推断放行范围。'
            : 'Full rules unavailable. This summary cannot distinguish absent and empty selectors; inspect YAML without inferring allowed traffic.'}
        </Typography.Paragraph>
        <pre className="soha-policy-raw">
          {JSON.stringify({ podSelector: detail.podSelector, rules: detail.rules }, null, 2)}
        </pre>
      </Card>
    )
  const peer = (p: PolicyObject) => {
    const pod = p.podSelector as PolicyObject | undefined,
      ns = p.namespaceSelector as PolicyObject | undefined,
      ip = p.ipBlock as PolicyObject | undefined
    if (ip)
      return (
        <div className="soha-policy-terms">
          <code>{String(ip.cidr || '—')}</code>
          {Array.isArray(ip.except) && ip.except.length > 0 && (
            <span>
              {zh ? '排除' : 'Except'} · {ip.except.join(', ')}
            </span>
          )}
        </div>
      )
    if (!pod && !ns) return <span>{zh ? '全部来源 / 目标' : 'All sources / destinations'}</span>
    return (
      <div className="soha-policy-terms">
        {ns ? (
          <>
            <span>Namespace</span>
            <PolicySelector selector={ns} all={zh ? '所有命名空间' : 'All namespaces'} />
          </>
        ) : (
          <span>
            {zh ? '本命名空间' : 'This namespace'} · {detail.namespace}
          </span>
        )}
        {pod && (
          <>
            <small>AND · Pod</small>
            <PolicySelector
              selector={pod}
              all={zh ? '上述范围内全部 Pods' : 'All Pods within this namespace scope'}
            />
          </>
        )}
        {!pod && <span>{zh ? '上述范围内全部 Pods' : 'All Pods within this namespace scope'}</span>}
      </div>
    )
  }
  return (
    <div className="soha-detail-stack">
      <Typography.Paragraph type="secondary">
        {zh
          ? '规则之间为 OR；同一规则的来源/目标与端口为 AND，多个 peer 或端口为 OR。策略放行范围取并集；实际执行还取决于 CNI。'
          : 'Rules are OR-combined. Peers AND ports within a rule; peer entries and port entries are OR-combined. Policy allowances are additive; enforcement requires CNI support.'}
      </Typography.Paragraph>
      {config.directions.map((d) => (
        <Card
          className="soha-detail-card"
          key={d.name}
          title={d.name}
          extra={
            <MetadataTag
              label={
                d.isolated
                  ? zh
                    ? '纳入隔离'
                    : 'Isolated by this policy'
                  : zh
                    ? '此策略不隔离'
                    : 'Not isolated by this policy'
              }
            />
          }
        >
          {!d.isolated ? (
            <span>
              {zh
                ? '此策略未隔离该方向；其他策略仍可能隔离。'
                : 'This policy does not isolate this direction; other policies may do so.'}
            </span>
          ) : !d.rules.length ? (
            <span>
              {zh
                ? '此策略隔离该方向，但未增加放行规则；其他策略仍可能放行。'
                : 'This policy isolates this direction without adding any allow rule; other policies may allow traffic.'}
            </span>
          ) : (
            d.rules.map((r, i) => (
              <section className="soha-policy-rule" key={r.key}>
                <strong>
                  {zh ? '规则' : 'Rule'} {i + 1}
                </strong>
                <div className="soha-policy-rule-grid">
                  <div>
                    <h4>
                      {d.name === 'Ingress'
                        ? zh
                          ? '允许来源'
                          : 'Allowed sources'
                        : zh
                          ? '允许目标'
                          : 'Allowed destinations'}
                    </h4>
                    {r.peers.length ? (
                      r.peers.map((p, j) => (
                        <div key={j}>
                          {j > 0 && <small>OR</small>}
                          <div className="soha-policy-peer">{peer(p)}</div>
                        </div>
                      ))
                    ) : (
                      <span>{zh ? '全部来源 / 目标' : 'All sources / destinations'}</span>
                    )}
                  </div>
                  <div>
                    <h4>{zh ? '且匹配端口' : 'AND ports'}</h4>
                    <div className="soha-policy-terms">
                      {r.ports.length ? (
                        r.ports.map((p, j) => (
                          <MetadataTag
                            key={j}
                            label={`${String(p.protocol || 'TCP')} · ${p.port === undefined ? (zh ? '全部端口' : 'All ports') : String(p.port)}${p.endPort !== undefined ? `–${p.endPort}` : ''}`}
                          />
                        ))
                      ) : (
                        <span>{zh ? '全部端口与协议' : 'All ports and protocols'}</span>
                      )}
                    </div>
                  </div>
                </div>
              </section>
            ))
          )}
        </Card>
      ))}
    </div>
  )
}
export type PolicyPodLink = (pod: NetworkPolicyPod) => ReactNode
export function PolicyPods({
  detail,
  link,
  canRead,
}: {
  detail: NetworkPolicyDetail
  link: PolicyPodLink
  canRead: boolean
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  return (
    <Card
      className="soha-detail-card"
      title={zh ? '选中 Pods' : 'Selected Pods'}
      extra={canRead && detail.matchingPods ? `${detail.matchingPods.length}` : undefined}
    >
      <AdminTable
        pagination={false}
        enableColumnSelection={false}
        localSorting
        dataSource={canRead ? detail.matchingPods || [] : []}
        rowKey={(p) => `${p.namespace}/${p.name}`}
        scroll={{ x: 820, y: 520 }}
        columns={[
          {
            title: 'Pod',
            key: 'name',
            width: 320,
            render: (_: unknown, p: NetworkPolicyPod) => link(p),
          },
          { title: zh ? '命名空间' : 'Namespace', dataIndex: 'namespace', width: 180 },
          {
            title: zh ? '状态' : 'Status',
            dataIndex: 'phase',
            width: 100,
            render: (v: string) => <StatusTag value={v} />,
          },
          { title: zh ? '就绪' : 'Ready', dataIndex: 'readyContainers', width: 100 },
          { title: zh ? '重启' : 'Restarts', dataIndex: 'restarts', width: 100 },
        ]}
        empty={
          <ManagementState
            compact
            kind={canRead ? 'empty' : 'no-permission'}
            description={
              !canRead
                ? zh
                  ? '无权查看 Pods'
                  : 'No Pod view permission'
                : detail.matchingPods === undefined
                  ? zh
                    ? '本次未返回选中 Pod 清单'
                    : 'Selected Pod inventory not returned'
                  : zh
                    ? '本次返回的匹配 Pod 清单为空'
                    : 'No matching Pods returned'
            }
          />
        }
      />
      <Typography.Paragraph type="secondary" className="soha-policy-note">
        {zh
          ? '仅展示后端本次返回且有权查看的匹配对象，不是流量连通性验证。'
          : 'Only matching objects returned and readable in this response, not a connectivity test.'}
      </Typography.Paragraph>
    </Card>
  )
}
