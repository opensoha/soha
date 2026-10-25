import { lazy, Suspense, useState } from 'react'
import { Button, Drawer, Input, Modal, Select, Space, Typography } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { podQueries, useRealtimeSessionDock, RelatedResourceYaml } from '@/features/platform'
import { toScopeKey } from '@/types'
import { deliveryQueries } from '../queries'
import { deliveryMutations } from '../mutations'
import type {
  ApplicationEnvironment,
  ApplicationRuntimeWorkload,
  ResourceRelationConfig,
  ResourceRelationNode,
} from '../types'
import { relationRulesPayload } from './relation-rules'
import { PodMetricsDetail } from './service-pod-workspace'
import {
  ResourceRelationWorkbench,
  relationDisplayNodes,
  type RelationDisplayNode,
} from './resource-relation-workbench'

const PodInspection = lazy(async () => ({
  default: (await import('./pod-inspection')).PodInspection,
}))
const isPod = (node: ResourceRelationNode) =>
  node.resource.kind === 'Pod' &&
  node.resource.apiVersion === 'v1' &&
  Boolean(node.resource.uid) &&
  node.role !== 'candidate'

export function ResourceRelationsBrowser({
  workload,
  binding,
  view,
  onOpenRootYaml,
}: {
  onOpenRootYaml?: () => void
  workload: ApplicationRuntimeWorkload
  binding?: ApplicationEnvironment
  view: 'resources' | 'metrics'
}) {
  const permissions = usePermissionSnapshot().data?.data
  const canRead = hasPermission(permissions, 'delivery.application-environments.view')
  const canPods = hasPermission(permissions, 'platform.pods.view')
  const canEdit = hasPermission(permissions, 'delivery.application-environments.update')
  const rootUid = workload.source!.resource.uid!
  const query = useQuery(
    deliveryQueries.environments.targetRelations(
      workload.applicationEnvironmentId,
      workload.targetId!,
      rootUid,
      canRead,
    ),
  )
  const { openSession } = useRealtimeSessionDock()
  const [params, setParams] = useSearchParams()
  const [inspected, setInspected] = useState<ResourceRelationNode>()
  const [yamlNode, setYamlNode] = useState<RelationDisplayNode>()
  const referencesQuery = useQuery(
    deliveryQueries.workloads.runtime(
      {
        applicationId: binding?.applicationId || '',
        applicationEnvironmentId: workload.applicationEnvironmentId,
        workloadName: workload.workloadName,
      },
      Boolean(binding?.applicationId) &&
        binding?.id === workload.applicationEnvironmentId &&
        Boolean(rootUid) &&
        canRead &&
        hasPermission(permissions, 'delivery.applications.view') &&
        workload.workloadKind === 'Deployment' &&
        view === 'resources',
    ),
  )
  const [editBase, setEditBase] = useState<ApplicationEnvironment>()
  const [draft, setDraft] = useState('')
  const [inputError, setInputError] = useState('')
  const client = useQueryClient()
  const save = useOperationMutation(deliveryMutations.environments.update(client), {
    label: ['保存关系规则', 'Save relation rules'],
    target: () => workload.workloadName,
    receipt: () => ({
      message: '关系规则已保存',
      evidence: '仅更新 Soha 观察配置，未修改或发布 Kubernetes 资源。',
    }),
  })
  const response = query.isError || !canRead ? undefined : query.data
  const identityKeys = ['clusterId', 'namespace', 'uid', 'apiVersion', 'kind', 'name'] as const
  const rootChanged =
    response && identityKeys.some((key) => response.root[key] !== workload.source!.resource[key])
  const data = rootChanged ? undefined : response
  const visibleYaml = Boolean(
    data &&
    yamlNode &&
    relationDisplayNodes(
      data,
      referencesQuery.isError ? [] : referencesQuery.data?.deployment?.relatedResources,
    ).some((node) => node.key === yamlNode.key && node.resource.uid === yamlNode.resource.uid),
  )
  const visibleInspection = Boolean(
    data &&
    canPods &&
    inspected &&
    data.nodes.some((node) => isPod(node) && node.resource.uid === inspected.resource.uid),
  )
  const pods = data?.nodes.filter(isPod) ?? []
  const metricUid = params.get('metricUid')
  const metricPod = pods.find((node) => node.resource.uid === metricUid)
  const selectMetrics = (node: ResourceRelationNode) => {
    const next = new URLSearchParams(params)
    next.set('serviceTab', 'metrics')
    next.set('metricUid', node.resource.uid!)
    next.set('metricPod', node.resource.name)
    next.set('metricCluster', node.resource.clusterId)
    next.set('metricNamespace', node.resource.namespace!)
    setParams(next)
  }
  const openEditor = () => {
    if (!binding) return
    setEditBase(binding)
    setInputError('')
    save.reset()
    setDraft(
      JSON.stringify(data?.config ?? { version: 'v1', rootUid, sources: [], rules: [] }, null, 2),
    )
  }
  const submit = () => {
    if (!editBase) return
    try {
      const config = JSON.parse(draft) as ResourceRelationConfig
      if (!config || config.rootUid !== rootUid) throw new Error('规则必须保留当前根资源 UID。')
      const payload = relationRulesPayload(editBase, workload.targetId!, config)
      setInputError('')
      save.mutate({ id: editBase.id, payload })
    } catch (error) {
      setInputError(error instanceof Error ? error.message : 'JSON 配置无效')
    }
  }
  if (!rootUid) return <ManagementState compact kind="not-configured" title="根资源 UID 尚未绑定" />
  if (rootChanged)
    return (
      <ManagementState
        compact
        kind="error"
        title="根资源关联已变化"
        description="刷新应用后重新选择目标；不会把新根资源的关系展示在旧身份下。"
      />
    )
  if (!canRead) return <ManagementState compact kind="no-permission" title="无环境读取权限" />
  return (
    <section aria-label={view === 'metrics' ? '关联 Pod 指标' : '运行资源关系'}>
      <Space wrap>
        <Typography.Text strong>
          {view === 'metrics' ? '关联 Pod 指标' : '运行资源关系'}
        </Typography.Text>
        <Button onClick={() => void query.refetch()} loading={query.isFetching}>
          刷新关系
        </Button>
        {view === 'resources' && canEdit ? (
          <Button
            disabled={
              !data ||
              !binding?.updatedAt ||
              !binding.targets?.some((t) => t.id === workload.targetId)
            }
            onClick={openEditor}
          >
            编辑关系规则
          </Button>
        ) : null}
        {data ? (
          <StatusTag
            value={data.partial ? 'warning' : 'info'}
            label={data.partial ? '关系不完整' : '已完成本次范围读取'}
          />
        ) : null}
      </Space>
      <p>
        <Typography.Text type="secondary">
          按资源 UID 核验归属；共享引用不认领运行时，标签匹配仅为候选。
          {data?.observedAt ? ` 观测时间 ${data.observedAt}` : ''}
        </Typography.Text>
      </p>
      {query.isPending ? (
        <ManagementState compact kind="loading" />
      ) : query.isError ? (
        <ReadFeedback query={query} label="运行资源关系" />
      ) : data ? (
        <>
          {data.coverage.some((c) => c.state !== 'available') ? (
            <ul aria-label="关系读取覆盖">
              {data.coverage
                .filter((c) => c.state !== 'available')
                .map((c, i) => (
                  <li key={`${c.queryId}/${i}`}>
                    {c.queryId} · {c.state} · {c.reasonCode || '来源未完整读取'}
                  </li>
                ))}
            </ul>
          ) : null}
          {view === 'metrics' ? (
            <>
              {!canPods ? (
                <ManagementState compact kind="no-permission" title="无 Pod 指标读取权限" />
              ) : pods.length === 0 ? (
                <ManagementState
                  compact
                  kind="empty"
                  title="暂无已确认的关联 Pod"
                  description="检查 GVR 来源、owner UID 和权限，或补充关联规则；共享引用与标签候选不会自动展开。"
                />
              ) : (
                <>
                  <Select
                    aria-label="关联 Pod 指标范围"
                    placeholder="选择已确认的 Pod"
                    value={metricPod?.resource.uid}
                    options={pods.map((node) => ({
                      value: node.resource.uid,
                      label: `${node.resource.namespace} / ${node.resource.name}`,
                    }))}
                    onChange={(value) => {
                      const node = pods.find((n) => n.resource.uid === value)
                      if (node) selectMetrics(node)
                    }}
                  />
                  {metricPod ? (
                    <VerifiedRelationPod
                      key={metricPod.resource.uid}
                      node={metricPod}
                      workload={workload}
                      mode="metrics"
                    />
                  ) : (
                    <ManagementState
                      compact
                      kind={metricUid ? 'not-found' : 'empty'}
                      title={metricUid ? '所选 Pod 已变化或不在当前关系范围' : '选择 Pod 查看指标'}
                    />
                  )}
                </>
              )}
            </>
          ) : (
            <>
              {data.nodes.length === 1 ? (
                <ManagementState
                  compact
                  kind="empty"
                  title="尚未发现可确认的子资源"
                  description="这不代表应用未运行。可检查来源和权限，或补充声明式关联规则。"
                />
              ) : null}
              {workload.workloadKind === 'Deployment' && referencesQuery.isError ? (
                <ReadFeedback query={referencesQuery} label="入口与配置引用" />
              ) : null}
              <ResourceRelationWorkbench
                data={data}
                references={
                  referencesQuery.isError ? [] : referencesQuery.data?.deployment?.relatedResources
                }
                renderActions={(node) =>
                  isPod(node) && canPods ? (
                    <Space wrap>
                      <Button
                        size="small"
                        aria-label={`查看 ${node.resource.name} 详情`}
                        onClick={() => setInspected(node)}
                      >
                        详情
                      </Button>
                      <Button
                        size="small"
                        aria-label={`查看 ${node.resource.name} 指标`}
                        onClick={() => selectMetrics(node)}
                      >
                        指标
                      </Button>
                      {(['logs', 'terminal'] as const)
                        .filter((kind) =>
                          hasPermission(
                            permissions,
                            kind === 'logs' ? 'platform.pods.logs' : 'platform.pods.exec',
                          ),
                        )
                        .map((kind) => (
                          <Button
                            size="small"
                            key={kind}
                            aria-label={`查看 ${node.resource.name} ${kind === 'logs' ? '日志' : '终端'}`}
                            onClick={() =>
                              openSession({
                                kind,
                                clusterId: node.resource.clusterId,
                                namespace: node.resource.namespace!,
                                podName: node.resource.name,
                                podUid: node.resource.uid,
                                ...(kind === 'terminal' ? { shell: '/bin/sh' } : {}),
                              })
                            }
                          >
                            {kind === 'logs' ? '日志' : '终端'}
                          </Button>
                        ))}
                    </Space>
                  ) : node.role === 'candidate' ? (
                    <Typography.Text type="secondary">候选需确认，不开放运行操作</Typography.Text>
                  ) : node.resource.uid === data.root.uid ? (
                    onOpenRootYaml ? (
                      <Button onClick={onOpenRootYaml}>查看源 YAML</Button>
                    ) : (
                      <Typography.Text type="secondary">源配置入口见配置与模板</Typography.Text>
                    )
                  ) : node.resource.kind === 'Secret' ? (
                    <Typography.Text type="secondary">
                      仅展示 Secret 引用身份，不回显数据
                    </Typography.Text>
                  ) : (
                    <Button onClick={() => setYamlNode(node)}>
                      {node.resource.uid ? '查看资源 YAML' : '按名称查看当前 YAML'}
                    </Button>
                  )
                }
              />
            </>
          )}
        </>
      ) : null}
      <Drawer
        open={visibleYaml}
        onClose={() => setYamlNode(undefined)}
        title={yamlNode ? `${yamlNode.resource.kind} / ${yamlNode.resource.name}` : '资源 YAML'}
        destroyOnHidden
        size="min(880px, 100vw)"
      >
        {visibleYaml && yamlNode ? (
          <>
            {!yamlNode.resource.uid ? (
              <p>
                此引用未固定
                UID；按名称重新读取当前同名对象，不代表原关联对象的配置，也不会在此处执行变更。
              </p>
            ) : null}
            <RelatedResourceYaml
              fresh
              target={{
                ...yamlNode.resource,
                namespace: yamlNode.resource.namespace || workload.namespace,
              }}
              expectedUid={yamlNode.resource.uid}
            />
          </>
        ) : null}
      </Drawer>
      <Drawer
        open={visibleInspection}
        onClose={() => setInspected(undefined)}
        title={inspected?.resource.name}
        destroyOnHidden
        size="min(880px, 100vw)"
      >
        {visibleInspection && inspected ? (
          <VerifiedRelationPod
            key={inspected.resource.uid}
            node={inspected}
            workload={workload}
            mode="details"
          />
        ) : null}
      </Drawer>
      <Modal
        title="当前目标的声明式关系规则"
        open={Boolean(editBase)}
        onCancel={() => setEditBase(undefined)}
        onOk={submit}
        okText="保存观察规则"
        cancelText="关闭"
        confirmLoading={save.isPending}
        okButtonProps={{ disabled: save.blocked }}
        destroyOnHidden
      >
        <p>
          根 UID：{rootUid}。sources 添加自定义 GVR；reference 仅引用，manual 固定 UID，selector
          仅生成候选。规则不授予权限，也不修改集群资源。
        </p>
        <details>
          <summary>查看规则格式示例</summary>
          <p>
            按实际资源替换 GVR、名称与 UID。人工关联必须固定 UID；跨 Namespace 来源仍逐项校验权限。
          </p>
          <pre className="soha-resource-yaml-content" tabIndex={0}>
            {JSON.stringify(
              {
                version: 'v1',
                rootUid,
                sources: [
                  {
                    apiVersion: 'example.com/v1',
                    resource: 'workers',
                    kind: 'Worker',
                    namespace: workload.namespace,
                  },
                ],
                rules: [
                  {
                    id: 'shared',
                    type: 'reference',
                    target: {
                      apiVersion: 'v1',
                      resource: 'services',
                      kind: 'Service',
                      namespace: workload.namespace,
                    },
                    name: 'shared-service',
                  },
                ],
              },
              null,
              2,
            )}
          </pre>
        </details>
        <Input.TextArea
          aria-label="关系规则 JSON"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          autoSize={{ minRows: 10, maxRows: 22 }}
          maxLength={16384}
        />
        {inputError ? <div role="alert">{inputError}</div> : null}
        {save.feedback}
      </Modal>
    </section>
  )
}

function VerifiedRelationPod({
  node,
  workload,
  mode,
}: {
  node: ResourceRelationNode
  workload: ApplicationRuntimeWorkload
  mode: 'details' | 'metrics'
}) {
  const ref = node.resource
  const options = podQueries.detail(toScopeKey(ref.clusterId, ref.namespace!), ref.name)
  const query = useQuery({ ...options, staleTime: 0, gcTime: 0, refetchOnMount: 'always' })
  const [params, setParams] = useSearchParams()
  const requested = Number(params.get('metricRange') || 60)
  const range = [15, 60, 360, 1440].includes(requested) ? requested : 60
  if (query.isPending || query.isFetching) return <ManagementState compact kind="loading" />
  if (query.isError) return <ReadFeedback query={query} label="Pod 身份" />
  if (query.data?.uid !== ref.uid)
    return <ManagementState compact kind="error" title="Pod 已重建，请刷新关系后重新选择" />
  if (mode === 'metrics')
    return (
      <PodMetricsDetail
        row={{ pod: query.data, workload: { ...workload, clusterId: ref.clusterId } }}
        range={range}
        onRangeChange={(value) => {
          const next = new URLSearchParams(params)
          next.set('metricRange', String(value))
          setParams(next)
        }}
      />
    )
  return (
    <Suspense fallback={<ManagementState compact kind="loading" />}>
      <PodInspection
        clusterId={ref.clusterId}
        namespace={ref.namespace!}
        podName={ref.name}
        mode="details"
        expectedUid={ref.uid}
        containerMetadata={query.data?.containers}
      />
    </Suspense>
  )
}
