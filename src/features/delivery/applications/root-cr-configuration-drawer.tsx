import { lazy, Suspense, useState } from 'react'
import { Alert, Button, Drawer, Select, Space, Typography } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { deliveryQueries } from '../queries'
import { deliveryMutations } from '../mutations'
import { manifestQueries } from '../manifests/queries'
import { manifestMutations } from '../manifests/mutations'
import type { ManifestPackage } from '../manifests/types'
import type { ApplicationEnvironment, ApplicationRuntimeWorkload } from '../types'
import {
  rootCRConfiguration,
  rootCRControlPayload,
  rootCRPackageInput,
} from './root-cr-configuration'

const YamlDraftDiffEditor = lazy(async () => ({
  default: (await import('@/components/yaml-draft-diff-editor')).YamlDraftDiffEditor,
}))

type Props = {
  workload: ApplicationRuntimeWorkload
  binding: ApplicationEnvironment
  onClose: () => void
  onOpenPlan?: (targetId: string, revision: number) => void
}

export function RootCRConfigurationDrawer(props: Props) {
  const permissions = usePermissionSnapshot().data?.data
  const canRead =
    hasPermission(permissions, 'delivery.application-environments.view') &&
    hasPermission(permissions, 'platform.configuration.secret-data.view')
  return (
    <Drawer title="根 CR 配置与发布控制" open size="large" destroyOnHidden onClose={props.onClose}>
      {canRead ? (
        <RootCRConfigurationLoader {...props} />
      ) : (
        <ManagementState compact kind="no-permission" title="读取根 CR 配置需要敏感数据权限" />
      )}
    </Drawer>
  )
}

function RootCRConfigurationLoader(props: Props) {
  const { workload, binding } = props
  const source = useQuery(
    deliveryQueries.environments.targetSource(
      binding.id,
      workload.targetId!,
      workload.source!.resource.uid!,
      true,
    ),
  )
  const packages = useQuery(manifestQueries.applicationPackages(binding.applicationId))
  const [selection, setSelection] = useState('')
  if (source.isPending || packages.isPending) return <ManagementState compact kind="loading" />
  if (source.isError) return <ReadFeedback query={source} label="根 CR 当前配置" />
  if (packages.isError) return <ReadFeedback query={packages} label="清单配置" />
  const target = binding.targets?.find((item) => item.id === workload.targetId)
  const candidates = packages.data.filter(
    (item) =>
      item.renderer === 'raw_yaml' &&
      item.files.length === 1 &&
      item.bindings.length === 1 &&
      item.bindings[0].applicationEnvironmentId === binding.id &&
      item.bindings[0].clusterId === workload.clusterId &&
      item.bindings[0].namespace === workload.namespace &&
      (item.bindings[0].id === target?.configRef ||
        item.files[0].path === `root-cr-${workload.source!.resource.uid}.yaml`),
  )
  const linked = candidates.find((item) =>
    item.bindings.some((entry) => entry.id === target?.configRef),
  )
  const recovery = candidates.filter(
    (item) => item.files[0].path === `root-cr-${workload.source!.resource.uid}.yaml`,
  )
  const item =
    candidates.find((entry) => entry.id === selection) ??
    (!selection ? (linked ?? (recovery.length === 1 ? recovery[0] : undefined)) : undefined)
  let configuration: ReturnType<typeof rootCRConfiguration>
  try {
    configuration = rootCRConfiguration(source.data.content, workload)
  } catch (error) {
    return <ManagementState compact kind="error" title={String(error)} />
  }
  return (
    <>
      <Typography.Paragraph>
        {workload.source!.resource.kind} / {workload.workloadName} ·{' '}
        {binding.alias || binding.environmentKey} · {workload.clusterId} / {workload.namespace}
        <br />
        UID {workload.source!.resource.uid}
      </Typography.Paragraph>
      <Typography.Paragraph type="secondary">
        保存配置不改变集群。发布通过现有交付计划、预检与审批；Operator 继续协调子资源。
      </Typography.Paragraph>
      <Select
        aria-label="根 CR 清单配置"
        style={{ width: '100%' }}
        value={item?.id ?? 'new'}
        onChange={setSelection}
        options={[
          { value: 'new', label: '创建新的根 CR 配置' },
          ...candidates.map((entry) => ({
            value: entry.id,
            label: `${entry.name} · v${entry.currentRevision}`,
          })),
        ]}
      />
      <RootCREditor
        key={`${workload.source!.resource.uid}/${item?.id ?? 'new'}`}
        {...props}
        initialPackage={item}
        configuration={configuration}
        onCreated={setSelection}
      />
    </>
  )
}

function RootCREditor({
  workload,
  binding,
  initialPackage,
  configuration,
  onCreated,
  onOpenPlan,
  onClose,
}: Props & {
  initialPackage?: ManifestPackage
  configuration: ReturnType<typeof rootCRConfiguration>
  onCreated: (id: string) => void
}) {
  const client = useQueryClient()
  const permissions = usePermissionSnapshot().data?.data
  const canEdit = hasPermission(permissions, 'delivery.application.update')
  const canControl = hasPermission(permissions, 'delivery.application-environments.update')
  const [item, setItem] = useState(initialPackage)
  const [baseline] = useState(configuration)
  const [draft, setDraft] = useState(initialPackage?.files[0].content ?? configuration.content)
  const [inputError, setInputError] = useState('')
  const [comparison, setComparison] = useState(0)
  const revisions = useQuery(manifestQueries.revisions(item?.id ?? '', Boolean(item)))
  const create = useOperationMutation(manifestMutations.create(client), {
    label: ['保存根 CR 草稿', 'Save root CR draft'],
    target: () => workload.workloadName,
    receipt: (data) => ({ message: '配置草稿已保存', evidence: `清单 ${data.id}，尚未变更集群。` }),
  })
  const update = useOperationMutation(manifestMutations.update(client), {
    label: ['保存根 CR 草稿', 'Save root CR draft'],
    target: () => workload.workloadName,
    receipt: () => ({
      message: '配置草稿已保存',
      evidence: '发布前需保存配置版本并生成交付计划。',
    }),
  })
  const revision = useOperationMutation(manifestMutations.saveRevision(client), {
    label: ['保存配置版本', 'Save configuration version'],
    target: () => workload.workloadName,
    receipt: (data) => ({
      message: `配置版本 v${data.currentRevision} 已保存`,
      evidence: '尚未发布到集群。',
    }),
  })
  const control = useOperationMutation(deliveryMutations.environments.update(client), {
    label: ['确认发布控制权', 'Confirm publication control'],
    target: () => workload.workloadName,
    receipt: () => ({
      message: '发布控制配置已保存',
      evidence: '仅变更 Soha 记录；执行时再次校验源身份、权限和外部写入方。',
    }),
  })
  const busy = [create, update, revision, control].some((operation) => operation.blocked)
  const manifestBinding = item?.bindings.find(
    (entry) =>
      entry.applicationEnvironmentId === binding.id &&
      entry.clusterId === workload.clusterId &&
      entry.namespace === workload.namespace,
  )
  const target = binding.targets?.find((entry) => entry.id === workload.targetId)
  const managed =
    target?.executorKind === 'manifest_ssa' &&
    target.configRef === manifestBinding?.id &&
    target.metadata?.ownershipMode === 'managed'
  async function save() {
    try {
      const input = rootCRPackageInput(binding, workload, draft, item)
      const saved = item
        ? await update.mutateAsync({ id: item.id, input })
        : await create.mutateAsync(input)
      setItem(saved)
      onCreated(saved.id)
      setInputError('')
    } catch (error) {
      setInputError(error instanceof Error ? error.message : String(error))
    }
  }
  function setControl(managed: boolean) {
    try {
      control.mutate({
        id: binding.id,
        payload: rootCRControlPayload(
          binding,
          workload,
          manifestBinding?.id ?? '',
          baseline.generation,
          managed,
        ),
      })
      setInputError('')
    } catch (error) {
      setInputError(error instanceof Error ? error.message : String(error))
    }
  }
  const changed = !item || draft !== item.files[0].content
  const original = comparison
    ? (revisions.data?.find((entry) => entry.version === comparison)?.files[0]?.content ?? '')
    : baseline.content
  return (
    <Space orientation="vertical" size="middle" style={{ width: '100%', marginTop: 16 }}>
      {baseline.blocked ? (
        <Alert type="warning" title={baseline.blocked} />
      ) : (
        <Alert
          type="info"
          title={
            managed
              ? 'Soha 发布根配置 · Operator 协调子资源'
              : '当前仅观察，尚未允许 Soha 发布根配置'
          }
        />
      )}
      <Typography.Text type="secondary">
        比较基线：当前集群 generation {baseline.generation}
        。状态、finalizers、拥有者和子资源不进入配置。
      </Typography.Text>
      {item ? (
        <Select
          aria-label="配置差异基线"
          value={comparison}
          onChange={setComparison}
          options={[
            { value: 0, label: '与当前集群配置比较' },
            ...(revisions.data ?? []).map((entry) => ({
              value: entry.version,
              label: `与配置版本 v${entry.version} 比较`,
            })),
          ]}
        />
      ) : null}
      {revisions.isError ? <ReadFeedback query={revisions} label="配置版本历史" /> : null}
      <Suspense fallback={<ManagementState compact kind="loading" />}>
        <YamlDraftDiffEditor
          title="根 CR spec 差异"
          description="左侧编辑草稿，右侧核对差异。"
          leftLabel="配置草稿"
          rightLabel={comparison ? `配置版本 v${comparison}` : '当前集群配置'}
          original={original}
          modified={draft}
          onChange={setDraft}
          editable={canEdit && !busy}
        />
      </Suspense>
      {inputError ? <Alert type="error" title={inputError} /> : null}
      {create.feedback}
      {update.feedback}
      {revision.feedback}
      {control.feedback}
      <Space wrap>
        <Button disabled={!canEdit || busy || !changed} onClick={() => void save()}>
          保存草稿
        </Button>
        <Button
          disabled={!canEdit || busy || !item || changed || item.status === 'published'}
          onClick={() => {
            if (item)
              revision.mutate(
                { id: item.id, expectedUpdatedAt: item.updatedAt },
                { onSuccess: setItem },
              )
          }}
        >
          保存配置版本
        </Button>
        <Button
          disabled={
            !canControl ||
            busy ||
            !manifestBinding?.id ||
            !item?.currentRevision ||
            changed ||
            Boolean(baseline.blocked)
          }
          onClick={() => setControl(true)}
        >
          {managed ? '确认本次发布基线' : '允许 Soha 发布根配置'}
        </Button>
        {managed ? (
          <Button disabled={!canControl || busy} onClick={() => setControl(false)}>
            恢复仅观察
          </Button>
        ) : null}
        {managed && item?.currentRevision && onOpenPlan ? (
          <Button
            type="primary"
            disabled={busy || changed || Boolean(baseline.blocked)}
            onClick={() => {
              onClose()
              onOpenPlan(workload.targetId!, item.currentRevision)
            }}
          >
            生成发布计划
          </Button>
        ) : null}
      </Space>
      <Typography.Text type="secondary">
        执行完成、Operator 协调与业务健康分别显示。通用配置更新不提供中间件数据回滚保证。
      </Typography.Text>
    </Space>
  )
}
