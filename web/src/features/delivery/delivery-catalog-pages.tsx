import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { App, Button, Card, Descriptions, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Tag, Typography } from 'antd'
import { ArrowRightOutlined, DeleteOutlined, EditOutlined, PlayCircleOutlined, PlusOutlined, ReloadOutlined, SendOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import {
  ManagementDetailHeader,
  ManagementIconButton,
  ManagementState,
} from '@/components/management-list'
import { DeliveryTable } from '@/features/delivery/delivery-table'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { useI18n } from '@/i18n'
import {
  createDefaultReleaseDagDefinition,
  normalizeReleaseDagDefinition,
  countReleaseDagNodes,
  type ReleaseDagDefinition,
} from '@/components/release-flow-dag-definition'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import {
  releaseBoardArtifactCount,
  releaseBoardQualitySignal,
  summarizeReleaseBoard,
} from '@/features/delivery/delivery-status'
import type { ApiResponse, ApplicationEnvironment, BuildSource, DeliveryApplication, DeliveryApplicationEnvironmentDetail, DeliveryTargetCandidate, ReleaseBoardEntry, WorkflowRun, WorkflowTemplate } from '@/types'

function parseJSONObject(raw: unknown, field: string) {
  const value = typeof raw === 'string' ? raw.trim() : ''
  if (!value) return {}
  try {
    const parsed = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('invalid')
    }
    return parsed
  } catch {
    throw new Error(`${field} 需要是合法 JSON 对象`)
  }
}

function applicationEnvironmentLabel(binding: Pick<ApplicationEnvironment, 'environmentKey' | 'environmentId'>) {
  return binding.environmentKey || binding.environmentId || '-'
}

function normalizeEnvironmentFormValue(value: unknown) {
  if (Array.isArray(value)) {
    return String(value[0] || '').trim()
  }
  return String(value || '').trim()
}

function initialEnvironmentFormValue(binding: Pick<ApplicationEnvironment, 'environmentKey' | 'environmentId'>) {
  const label = applicationEnvironmentLabel(binding)
  return label === '-' ? [] : [label]
}

interface BuildRecord {
  id: string
  applicationId: string
  status: string
  createdAt: string
}

interface WorkflowRecord {
  id: string
  applicationId: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  updatedAt: string
}

interface ReleaseRecord {
  id: string
  applicationId: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  createdAt: string
}

interface RolloutHistoryRecord {
  name: string
  namespace: string
  revision: string
  images?: string[]
  replicas: number
  readyReplicas: number
  createdAt?: string
}

const RELEASE_TEMPLATE_CATEGORY_OPTIONS = [
  { value: 'release', label: 'Release Flow' },
  { value: 'verification', label: 'Verification' },
  { value: 'promotion', label: 'Promotion' },
]

const { Text } = Typography
type ColumnProps<T> = TableColumnsType<T>[number]

const ReleaseFlowDagEditor = lazy(async () => {
  const mod = await import('@/components/release-flow-dag-editor')
  return { default: mod.ReleaseFlowDagEditor }
})

function matchesBindingTarget(
  target: NonNullable<ApplicationEnvironment['targets']>[number] | undefined,
  clusterId: string,
  namespace: string,
  deploymentName: string,
) {
  if (!target) return false
  return target.clusterId === clusterId
    && target.namespace === namespace
    && target.workloadName === deploymentName
    && target.workloadKind.toLowerCase() === 'deployment'
    && target.enabled !== false
}

function pickLatest<T>(items: T[], matcher: (item: T) => boolean, timeSelector: (item: T) => string) {
  return items
    .filter(matcher)
    .sort((left, right) => new Date(timeSelector(right)).getTime() - new Date(timeSelector(left)).getTime())[0]
}

function findLatestWorkflowForTarget(target: NonNullable<ApplicationEnvironment['targets']>[number], binding: ApplicationEnvironment, workflows: WorkflowRecord[]) {
  return pickLatest(
    workflows,
    (item) => item.applicationId === binding.applicationId && matchesBindingTarget(target, item.clusterId, item.namespace, item.deploymentName),
    (item) => item.updatedAt,
  )
}

function findLatestReleaseForTarget(target: NonNullable<ApplicationEnvironment['targets']>[number], binding: ApplicationEnvironment, releases: ReleaseRecord[]) {
  return pickLatest(
    releases,
    (item) => item.applicationId === binding.applicationId && matchesBindingTarget(target, item.clusterId, item.namespace, item.deploymentName),
    (item) => item.createdAt,
  )
}

function summarizeLatestActivity(localeCode: 'zh_CN' | 'en_US', build?: BuildRecord, workflow?: WorkflowRecord | WorkflowRun, release?: ReleaseRecord) {
  if (release?.createdAt) return localeCode === 'zh_CN' ? `最近发布 ${formatDateTime(release.createdAt)}` : `Latest release ${formatDateTime(release.createdAt)}`
  if (workflow?.updatedAt) return localeCode === 'zh_CN' ? `最近工作流 ${formatDateTime(workflow.updatedAt)}` : `Latest workflow ${formatDateTime(workflow.updatedAt)}`
  if (build?.createdAt) return localeCode === 'zh_CN' ? `最近构建 ${formatDateTime(build.createdAt)}` : `Latest build ${formatDateTime(build.createdAt)}`
  return localeCode === 'zh_CN' ? '暂无执行记录' : 'No execution history'
}

export function ApplicationEnvironmentsPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [form] = Form.useForm<Record<string, unknown>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<ApplicationEnvironment | null>(null)
  const selectedApplicationId = Form.useWatch('applicationId', form) as string | undefined
  const selectedClusterId = Form.useWatch('targetClusterId', form) as string | undefined
  const selectedNamespace = Form.useWatch('targetNamespace', form) as string | undefined
  const selectedTargetKind = Form.useWatch('targetKind', form) as string | undefined
  const selectedExecutorKind = Form.useWatch('executorKind', form) as string | undefined
  const canManageBindings = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.application-environments.manage')

  const bindingsQuery = useQuery({
    queryKey: ['application-environments'],
    queryFn: () => api.get<ApiResponse<ApplicationEnvironment[]>>('/application-environments'),
  })
  const appsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<DeliveryApplication[]>>('/applications'),
  })
  const workflowTemplatesQuery = useQuery({
    queryKey: ['workflow-templates'],
    queryFn: () => api.get<ApiResponse<WorkflowTemplate[]>>('/workflow-templates'),
  })
  const targetCandidatesQuery = useQuery({
    queryKey: ['target-candidates', selectedClusterId, selectedNamespace],
    queryFn: () => api.get<ApiResponse<DeliveryTargetCandidate[]>>(`/application-environments/target-candidates?clusterId=${encodeURIComponent(selectedClusterId || '')}&namespace=${encodeURIComponent(selectedNamespace || '')}`),
    enabled: !!selectedClusterId && !!selectedNamespace && modalVisible && (selectedTargetKind || 'k8s_workload') === 'k8s_workload' && (selectedExecutorKind || 'k8s_job_runner') === 'k8s_job_runner',
  })

  const appNameMap = useMemo(
    () => Object.fromEntries((appsQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [appsQuery.data],
  )
  const environmentOptions = useMemo(() => {
    return Array.from(
      new Set(
        (bindingsQuery.data?.data ?? [])
          .filter((item) => !selectedApplicationId || item.applicationId === selectedApplicationId)
          .map(applicationEnvironmentLabel)
          .filter((item) => item !== '-'),
      ),
    ).map((item) => ({ value: item, label: item }))
  }, [bindingsQuery.data, selectedApplicationId])

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/application-environments', values),
    onSuccess: () => {
      message.success('应用环境绑定创建成功')
      queryClient.invalidateQueries({ queryKey: ['application-environments'] })
      queryClient.invalidateQueries({ queryKey: ['delivery-release-board'] })
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/application-environments/${id}`, values),
    onSuccess: () => {
      message.success('应用环境绑定更新成功')
      queryClient.invalidateQueries({ queryKey: ['application-environments'] })
      queryClient.invalidateQueries({ queryKey: ['delivery-release-board'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/application-environments/${id}`),
    onSuccess: () => {
      message.success('应用环境绑定已删除')
      queryClient.invalidateQueries({ queryKey: ['application-environments'] })
      queryClient.invalidateQueries({ queryKey: ['delivery-release-board'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const columns: ColumnProps<ApplicationEnvironment>[] = [
    { title: '应用', dataIndex: 'applicationId', render: (value: string) => appNameMap[value] || value },
    { title: '环境', dataIndex: 'environmentId', render: (_: string, record: ApplicationEnvironment) => applicationEnvironmentLabel(record) },
    { title: '策略', dataIndex: 'strategyProfileId', render: (value: string) => value || '-' },
    { title: '审批策略', dataIndex: 'approvalPolicyId', render: (value: string) => value || '-' },
    { title: '构建来源', dataIndex: 'buildPolicy', render: (value: ApplicationEnvironment['buildPolicy']) => value?.sourceId || '-' },
    { title: '动作', dataIndex: 'releasePolicy', render: (value: ApplicationEnvironment['releasePolicy']) => value?.actionKind || 'deploy' },
    { title: '发布流程模板', dataIndex: 'workflowTemplate', render: (_: WorkflowTemplate, record: ApplicationEnvironment) => record.workflowTemplate?.name || record.workflowTemplateId || '-' },
    { title: '目标数', dataIndex: 'targets', render: (targets: ApplicationEnvironment['targets']) => targets?.length ?? 0 },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: ApplicationEnvironment) => (
        <Space className="soha-row-action-icons" size={2}>
          {canManageBindings ? (
            <ManagementIconButton
              aria-label="编辑绑定"
              icon={<EditOutlined />}
              size="small"
              tooltip="编辑"
              onClick={() => { setEditing(record); setModalVisible(true) }}
            />
          ) : null}
          {canManageBindings ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)} placement="topRight">
              <ManagementIconButton
                aria-label="删除绑定"
                danger
                icon={<DeleteOutlined />}
                size="small"
                tooltip="删除"
              />
            </Popconfirm>
          ) : null}
          {!canManageBindings ? '-' : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="soha-page">
      <DeliveryTable
        actions={canManageBindings ? (
          <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>
            新建绑定
          </Button>
        ) : null}
        refreshing={bindingsQuery.isFetching}
        onRefresh={() => void bindingsQuery.refetch()}
        columns={columns}
        dataSource={bindingsQuery.data?.data ?? []}
        rowKey="id"
        loading={bindingsQuery.isLoading}
      />
      <Modal title={editing ? '编辑应用环境绑定' : '新建应用环境绑定'} open={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null} width={760} destroyOnHidden>
        <Form
          form={form}
          key={editing?.id ?? 'create-application-environment'}
          layout="vertical"
          onFinish={(values) => {
            const selectedTarget = (targetCandidatesQuery.data?.data ?? []).find((item) => `${item.clusterId}/${item.namespace}/${item.workloadName}` === values.targetWorkload)
            let variables: Record<string, unknown>
            let buildArgs: Record<string, unknown>
            let targetMetadata: Record<string, unknown>
            try {
              variables = parseJSONObject(values.buildVariablesText, '构建变量')
              buildArgs = parseJSONObject(values.buildArgsText, '构建参数')
              targetMetadata = parseJSONObject(values.targetMetadataText, '目标元数据')
            } catch (err) {
              message.error((err as Error).message)
              return
            }
            const resolvedTargetKind = String(values.targetKind || 'k8s_workload')
            const resolvedExecutorKind = String(values.executorKind || 'k8s_job_runner')
            const targetRecord = selectedTarget
              ? {
                  clusterId: selectedTarget.clusterId,
                  namespace: selectedTarget.namespace,
                  workloadKind: selectedTarget.workloadKind,
                  workloadName: selectedTarget.workloadName,
                }
              : {
                  clusterId: String(values.targetClusterId || ''),
                  namespace: String(values.targetNamespace || ''),
                  workloadKind: String(values.targetWorkloadKind || (resolvedTargetKind === 'host_service' ? 'Service' : 'Deployment')),
                  workloadName: String(values.targetWorkload || ''),
                }
            const payload: Record<string, unknown> = {
              applicationId: values.applicationId,
              environmentId: normalizeEnvironmentFormValue(values.environmentId),
              strategyProfileId: values.strategyProfileId || '',
              promotionPolicyId: values.promotionPolicyId || '',
              approvalPolicyId: values.approvalPolicyId || '',
              artifactPolicyId: values.artifactPolicyId || '',
              workflowTemplateId: values.workflowTemplateId,
              buildPolicy: {
                sourceId: values.buildSourceId,
                refType: values.refType || 'branch',
                refValue: values.refValue || '',
                imageTagMode: values.imageTagMode || 'input',
                imageTagTemplate: values.imageTagTemplate || '',
                variables,
                buildArgs,
              },
              releasePolicy: {
                actionKind: values.actionKind || 'deploy',
                requiresApproval: Boolean(values.requiresApproval),
                approverRoles: String(values.approverRoles || '').split(',').map((item) => item.trim()).filter(Boolean),
                autoRollback: Boolean(values.autoRollback),
                rolloutTimeoutSeconds: Number(values.rolloutTimeoutSeconds || 300),
                verificationMode: values.verificationMode || 'workflow',
              },
              targets: targetRecord.workloadName ? [{
                clusterId: targetRecord.clusterId,
                namespace: targetRecord.namespace,
                targetKind: resolvedTargetKind,
                executorKind: resolvedExecutorKind,
                groupKey: values.groupKey || '',
                waveKey: values.waveKey || '',
                regionKey: values.regionKey || '',
                configRef: values.configRef || '',
                workloadKind: targetRecord.workloadKind,
                workloadName: targetRecord.workloadName,
                containerName: values.targetContainer || '',
                metadata: targetMetadata,
                enabled: true,
              }] : [],
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
          initialValues={editing ? {
            applicationId: editing.applicationId,
            environmentId: initialEnvironmentFormValue(editing),
            strategyProfileId: editing.strategyProfileId || '',
            promotionPolicyId: editing.promotionPolicyId || '',
            approvalPolicyId: editing.approvalPolicyId || '',
            artifactPolicyId: editing.artifactPolicyId || '',
            workflowTemplateId: editing.workflowTemplateId,
            buildSourceId: editing.buildPolicy?.sourceId,
            refType: editing.buildPolicy?.refType || 'branch',
            refValue: editing.buildPolicy?.refValue || '',
            imageTagMode: editing.buildPolicy?.imageTagMode || 'input',
            imageTagTemplate: editing.buildPolicy?.imageTagTemplate || '',
            buildVariablesText: JSON.stringify(editing.buildPolicy?.variables ?? {}, null, 2),
            buildArgsText: JSON.stringify(editing.buildPolicy?.buildArgs ?? {}, null, 2),
            actionKind: editing.releasePolicy?.actionKind || 'deploy',
            requiresApproval: editing.releasePolicy?.requiresApproval,
            approverRoles: (editing.releasePolicy?.approverRoles ?? []).join(', '),
            autoRollback: editing.releasePolicy?.autoRollback,
            rolloutTimeoutSeconds: editing.releasePolicy?.rolloutTimeoutSeconds || 300,
            verificationMode: editing.releasePolicy?.verificationMode || 'workflow',
            targetClusterId: editing.targets?.[0]?.clusterId,
            targetNamespace: editing.targets?.[0]?.namespace,
            targetKind: editing.targets?.[0]?.targetKind || 'k8s_workload',
            executorKind: editing.targets?.[0]?.executorKind || 'k8s_job_runner',
            groupKey: editing.targets?.[0]?.groupKey || '',
            waveKey: editing.targets?.[0]?.waveKey || '',
            regionKey: editing.targets?.[0]?.regionKey || '',
            configRef: editing.targets?.[0]?.configRef || '',
            targetWorkload: editing.targets?.[0]?.targetKind === 'k8s_workload' && editing.targets?.[0]?.executorKind === 'k8s_job_runner'
              ? `${editing.targets[0].clusterId}/${editing.targets[0].namespace}/${editing.targets[0].workloadName}`
              : editing.targets?.[0]?.workloadName,
            targetWorkloadKind: editing.targets?.[0]?.workloadKind || 'Deployment',
            targetContainer: editing.targets?.[0]?.containerName,
            targetMetadataText: JSON.stringify(editing.targets?.[0]?.metadata ?? {}, null, 2),
          } : { refType: 'branch', imageTagMode: 'input', buildVariablesText: '{}', buildArgsText: '{}', actionKind: 'deploy', rolloutTimeoutSeconds: 300, verificationMode: 'workflow', targetKind: 'k8s_workload', executorKind: 'k8s_job_runner', targetWorkloadKind: 'Deployment', targetMetadataText: '{}' }}
        >
          <Form.Item name="applicationId" label="应用" rules={[{ required: true, message: '请选择应用' }]}>
            <Select options={(appsQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} />
          </Form.Item>
          <Form.Item name="environmentId" label="环境" rules={[{ required: true, message: '请选择环境' }]}>
            <Select
              showSearch
              mode="tags"
              maxCount={1}
              placeholder="dev / test / prod"
              options={environmentOptions}
            />
          </Form.Item>
          <Form.Item name="buildSourceId" label="构建来源" rules={[{ required: true, message: '请选择构建来源' }]}>
            <Select options={((appsQuery.data?.data ?? []).find((item) => item.id === selectedApplicationId)?.buildSources ?? []).map((item: BuildSource) => ({ value: item.id, label: `${item.name} / ${item.type}` }))} />
          </Form.Item>
          <Form.Item name="strategyProfileId" label="发布策略 Profile"><Input placeholder="rolling-default / canary-prod" /></Form.Item>
          <Form.Item name="promotionPolicyId" label="晋级策略 Policy"><Input placeholder="promote-prod-only" /></Form.Item>
          <Form.Item name="approvalPolicyId" label="审批策略 Policy"><Input placeholder="double-sign-prod" /></Form.Item>
          <Form.Item name="artifactPolicyId" label="制品策略 Policy"><Input placeholder="signed-sbom-required" /></Form.Item>
          <Form.Item name="refType" label="构建引用类型">
            <Select options={[{ value: 'branch', label: 'branch' }, { value: 'tag', label: 'tag' }]} />
          </Form.Item>
          <Form.Item name="refValue" label="构建引用值"><Input placeholder="main / v1.0.0" /></Form.Item>
          <Form.Item name="imageTagMode" label="镜像 Tag 模式">
            <Select options={[{ value: 'input', label: 'input' }, { value: 'template', label: 'template' }, { value: 'build_output', label: 'build_output' }]} />
          </Form.Item>
          <Form.Item name="imageTagTemplate" label="镜像 Tag 模板"><Input placeholder="{{branch}}-{{timestamp}}" /></Form.Item>
          <Form.Item name="buildVariablesText" label="构建变量(JSON)"><Input.TextArea rows={4} /></Form.Item>
          <Form.Item name="buildArgsText" label="构建参数(JSON)"><Input.TextArea rows={4} /></Form.Item>
          <Form.Item name="actionKind" label="交付动作">
            <Select options={[{ value: 'deploy', label: 'deploy' }, { value: 'release', label: 'release' }]} />
          </Form.Item>
          <Form.Item name="requiresApproval" label="需要审批" valuePropName="checked"><Switch /></Form.Item>
          <Form.Item name="approverRoles" label="审批角色"><Input placeholder="release-manager, ops-lead" /></Form.Item>
          <Form.Item name="autoRollback" label="失败自动回滚" valuePropName="checked"><Switch /></Form.Item>
          <Form.Item name="rolloutTimeoutSeconds" label="Rollout 超时秒数"><InputNumber min={30} step={30} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="verificationMode" label="校验模式">
            <Select options={[{ value: 'workflow', label: 'workflow' }, { value: 'none', label: 'none' }]} />
          </Form.Item>
          <Form.Item name="workflowTemplateId" label="发布流程模板">
            <Select options={(workflowTemplatesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} />
          </Form.Item>
          <Form.Item name="targetClusterId" label="目标集群" rules={[{ required: true, message: '请输入目标集群 ID' }]}><Input /></Form.Item>
          <Form.Item name="targetNamespace" label="目标命名空间" rules={[{ required: true, message: '请输入目标命名空间' }]}><Input /></Form.Item>
          <Form.Item name="targetKind" label="目标类型">
            <Select options={[{ value: 'k8s_workload', label: 'k8s_workload' }, { value: 'host_service', label: 'host_service' }, { value: 'helm_release', label: 'helm_release' }, { value: 'kustomize_overlay', label: 'kustomize_overlay' }]} />
          </Form.Item>
          <Form.Item name="executorKind" label="执行器">
            <Select options={[{ value: 'k8s_job_runner', label: 'k8s_job_runner' }, { value: 'ci_agent_runner', label: 'ci_agent_runner' }, { value: 'external_pipeline_adapter', label: 'external_pipeline_adapter' }]} />
          </Form.Item>
          <Form.Item name="groupKey" label="Target Group"><Input placeholder="core-services / edge-cn" /></Form.Item>
          <Form.Item name="waveKey" label="Wave"><Input placeholder="wave-1 / wave-2" /></Form.Item>
          <Form.Item name="regionKey" label="Region"><Input placeholder="cn-shanghai / ap-southeast" /></Form.Item>
          <Form.Item name="configRef" label="配置引用"><Input placeholder="helm-values-prod / kustomize/prod" /></Form.Item>
          <Form.Item name="targetWorkloadKind" label="资源 Kind"><Input placeholder="Deployment / Service / Release" /></Form.Item>
          {(selectedTargetKind || 'k8s_workload') === 'k8s_workload' && (selectedExecutorKind || 'k8s_job_runner') === 'k8s_job_runner' ? (
            <Form.Item name="targetWorkload" label="目标 Deployment" rules={[{ required: true, message: '请选择目标 Deployment' }]}>
              <Select
                showSearch
                options={(targetCandidatesQuery.data?.data ?? []).map((item) => ({
                  value: `${item.clusterId}/${item.namespace}/${item.workloadName}`,
                  label: `${item.clusterId} / ${item.namespace} / ${item.workloadName}`,
                }))}
              />
            </Form.Item>
          ) : (
            <Form.Item name="targetWorkload" label="目标名称" rules={[{ required: true, message: '请输入目标名称' }]}>
              <Input placeholder="billing.service / helm-release / overlay-prod" />
            </Form.Item>
          )}
          <Form.Item name="targetContainer" label="目标容器">
            <Select
              allowClear
              options={(() => {
                const selectedTarget = (targetCandidatesQuery.data?.data ?? []).find((item) => `${item.clusterId}/${item.namespace}/${item.workloadName}` === form.getFieldValue('targetWorkload'))
                return (selectedTarget?.containers ?? []).map((item) => ({ value: item, label: item }))
              })()}
            />
          </Form.Item>
          <Form.Item name="targetMetadataText" label="目标元数据(JSON)">
            <Input.TextArea rows={5} placeholder='{"commands":["systemctl restart billing"],"serviceUnit":"billing.service"}' />
          </Form.Item>
          <div className="soha-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? '更新' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}

export function ReleaseBoardPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canViewApplications = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.applications.view')
  const releaseBoardQuery = useQuery({
    queryKey: ['delivery-release-board'],
    queryFn: () => api.get<ApiResponse<ReleaseBoardEntry[]>>('/delivery/release-board'),
  })

  const rows = releaseBoardQuery.data?.data ?? []
  const summary = useMemo(() => summarizeReleaseBoard(rows), [rows])
  const columns: ColumnProps<ReleaseBoardEntry>[] = [
    {
      title: t('common.application', 'Application'),
      dataIndex: 'applicationName',
      render: (value: string, record) => (
        <Space orientation="vertical" size={0}>
          <Text strong>{value}</Text>
          <Text type="secondary">{record.applicationId}</Text>
        </Space>
      ),
    },
    { title: localeCode === 'zh_CN' ? '环境' : 'Environment', dataIndex: 'environmentName', render: (value: string, record) => value || record.environmentKey || record.environmentId },
    { title: localeCode === 'zh_CN' ? '构建来源' : 'Build Source', dataIndex: 'buildSource', render: (_: unknown, record) => record.buildSource?.name || record.buildSourceId || '-' },
    { title: localeCode === 'zh_CN' ? '目标' : 'Targets', dataIndex: 'targets', render: (targets: ReleaseBoardEntry['targets']) => targets?.length ?? 0 },
    { title: localeCode === 'zh_CN' ? '候选版本' : 'Candidate', dataIndex: 'latestBundle', render: (value: ReleaseBoardEntry['latestBundle']) => value?.version || '-' },
    {
      title: localeCode === 'zh_CN' ? '交付态势' : 'Delivery Signal',
      key: 'quality',
      render: (_: unknown, record) => {
        const signal = releaseBoardQualitySignal(record)
        return <Tag color={signal.color}>{signal.label}</Tag>
      },
    },
    { title: localeCode === 'zh_CN' ? '交付物' : 'Artifacts', key: 'artifacts', render: (_: unknown, record) => releaseBoardArtifactCount(record) },
    { title: localeCode === 'zh_CN' ? '审批' : 'Approval', dataIndex: 'requiresApproval', render: (value: boolean) => <BooleanTag value={value} /> },
    { title: 'Build', dataIndex: 'latestBuild', render: (value: ReleaseBoardEntry['latestBuild']) => <StatusTag value={value?.status || 'unknown'} /> },
    { title: 'Workflow', dataIndex: 'latestWorkflow', render: (value: ReleaseBoardEntry['latestWorkflow']) => <StatusTag value={value?.status || 'unknown'} /> },
    { title: 'Task', dataIndex: 'latestExecutionTask', render: (value: ReleaseBoardEntry['latestExecutionTask']) => <StatusTag value={value?.status || 'unknown'} /> },
    { title: 'Release', dataIndex: 'latestRelease', render: (value: ReleaseBoardEntry['latestRelease']) => <StatusTag value={value?.status || 'unknown'} /> },
    {
      title: localeCode === 'zh_CN' ? '最近活动' : 'Latest Activity',
      key: 'latestActivity',
      render: (_: unknown, record) => summarizeLatestActivity(localeCode, record.latestBuild, record.latestWorkflow, record.latestRelease),
    },
    {
      ...tableColumnPresets.action,
      title: t('common.actions', 'Actions'),
      dataIndex: 'applicationEnvironmentId',
      render: (_: unknown, record: ReleaseBoardEntry) =>
        canViewApplications ? (
          <ManagementIconButton
            aria-label={localeCode === 'zh_CN' ? '进入应用' : 'Open application'}
            icon={<ArrowRightOutlined />}
            size="small"
            tooltip={localeCode === 'zh_CN' ? '进入应用' : 'Open application'}
            onClick={() => navigate(`/applications/${record.applicationId}`)}
          />
        ) : '-',
    },
  ]

  return (
    <div className="soha-page">
      <div className="soha-release-board-summary">
        <Card className="soha-application-signal-card" size="small">
          <span className="soha-application-signal-card__label">环境绑定</span>
          <strong>{summary.total}</strong>
          <Text type="secondary">{summary.targets} 个发布目标</Text>
        </Card>
        <Card className="soha-application-signal-card" size="small">
          <span className="soha-application-signal-card__label">执行中</span>
          <strong>{summary.running}</strong>
          <Text type="secondary">构建 / 工作流 / 发布</Text>
        </Card>
        <Card className="soha-application-signal-card" size="small">
          <span className="soha-application-signal-card__label">待审批</span>
          <strong>{summary.approval}</strong>
          <Text type="secondary">人工审批门禁</Text>
        </Card>
        <Card className="soha-application-signal-card" size="small">
          <span className="soha-application-signal-card__label">可推广</span>
          <strong>{summary.ready}</strong>
          <Text type="secondary">{summary.blocked} 个阻塞</Text>
        </Card>
      </div>
      <DeliveryTable
        refreshing={releaseBoardQuery.isFetching}
        onRefresh={() => void releaseBoardQuery.refetch()}
        columns={columns}
        dataSource={rows}
        rowKey="applicationEnvironmentId"
        loading={releaseBoardQuery.isLoading}
      />
    </div>
  )
}

export function ApplicationEnvironmentDetailPage() {
  const { t, localeCode } = useI18n()
  const { message } = App.useApp()
  const { applicationEnvironmentId } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [selectedTargetId, setSelectedTargetId] = useState<string>('')
  const [imageTag, setImageTag] = useState('')
  const [releaseName, setReleaseName] = useState('')
  const [containerName, setContainerName] = useState('')
  const [rollbackRevision, setRollbackRevision] = useState('')
  const canViewReleaseBoard = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.release-board.view')
  const canTriggerWorkflow = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.workflows.trigger')
  const canTriggerRelease = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.releases.trigger')
  const canRollbackDeployment = hasPermission(permissionSnapshotQuery.data?.data, 'platform.deployment.rollback')
  const backPath = canViewReleaseBoard ? '/release-board' : '/application-environments'
  const backLabel = canViewReleaseBoard
    ? (localeCode === 'zh_CN' ? '返回发布看板' : 'Back to Release Board')
    : (localeCode === 'zh_CN' ? '返回应用环境绑定' : 'Back to App Environment Bindings')

  const bindingQuery = useQuery({
    queryKey: ['application-environment', applicationEnvironmentId],
    queryFn: () => api.get<ApiResponse<DeliveryApplicationEnvironmentDetail>>(`/application-environments/${applicationEnvironmentId}/detail`),
    enabled: !!applicationEnvironmentId,
  })
  const workflowsQuery = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.get<ApiResponse<WorkflowRecord[]>>('/workflows'),
  })
  const releasesQuery = useQuery({
    queryKey: ['releases'],
    queryFn: () => api.get<ApiResponse<ReleaseRecord[]>>('/releases'),
  })

  const detail = bindingQuery.data?.data
  const binding = detail?.binding
  const latestBuild = detail?.latestBuild
  const latestWorkflow = detail?.latestWorkflow
  const latestRelease = detail?.latestRelease

  useEffect(() => {
    if (!binding?.targets?.length) {
      setSelectedTargetId('')
      return
    }
    if (selectedTargetId && binding.targets.some((target) => target.id === selectedTargetId)) {
      return
    }
    setSelectedTargetId(binding.targets[0].id)
  }, [binding, selectedTargetId])

  const selectedTarget = useMemo(
    () => binding?.targets?.find((target) => target.id === selectedTargetId) ?? binding?.targets?.[0],
    [binding, selectedTargetId],
  )

  useEffect(() => {
    if (!selectedTarget) {
      setContainerName('')
      return
    }
    setContainerName(selectedTarget.containerName || '')
  }, [selectedTarget])

  useEffect(() => {
    if (!detail?.buildSource?.defaultTag || imageTag) return
    setImageTag(detail.buildSource.defaultTag)
  }, [detail, imageTag])

  const rolloutHistoryQuery = useQuery({
    queryKey: ['deployment-rollouts', selectedTarget?.clusterId, selectedTarget?.namespace, selectedTarget?.workloadName],
    queryFn: () =>
      api.get<ApiResponse<RolloutHistoryRecord[]>>(
        `/clusters/${selectedTarget!.clusterId}/workloads/deployments/${selectedTarget!.workloadName}/rollouts?namespace=${encodeURIComponent(selectedTarget!.namespace)}`,
      ),
    enabled: !!selectedTarget && selectedTarget.workloadKind.toLowerCase() === 'deployment',
  })

  useEffect(() => {
    const rollouts = rolloutHistoryQuery.data?.data ?? []
    if (rollbackRevision && rollouts.some((item) => item.revision === rollbackRevision)) {
      return
    }
    if (rollouts.length > 1) {
      setRollbackRevision(rollouts[1].revision)
      return
    }
    setRollbackRevision('')
  }, [rollbackRevision, rolloutHistoryQuery.data])

  const targetRows = useMemo(() => {
    if (!binding) return []
    return (binding.targets ?? []).map((target) => ({
      ...target,
      latestWorkflow: findLatestWorkflowForTarget(target, binding, workflowsQuery.data?.data ?? []),
      latestRelease: findLatestReleaseForTarget(target, binding, releasesQuery.data?.data ?? []),
    }))
  }, [binding, workflowsQuery.data, releasesQuery.data])

  const targetColumns: ColumnProps<(typeof targetRows)[number]>[] = [
    { title: t('common.cluster', 'Cluster'), dataIndex: 'clusterId' },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace' },
    { title: localeCode === 'zh_CN' ? '工作负载' : 'Workload', dataIndex: 'workloadName', render: (_: string, record) => `${record.workloadKind} / ${record.workloadName}` },
    { title: t('common.container', 'Container'), dataIndex: 'containerName', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '启用' : 'Enabled', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
    { title: 'Workflow', dataIndex: 'latestWorkflow', render: (_: unknown, record) => <StatusTag value={record.latestWorkflow?.status || 'unknown'} /> },
    { title: 'Release', dataIndex: 'latestRelease', render: (_: unknown, record) => <StatusTag value={record.latestRelease?.status || 'unknown'} /> },
    {
      title: localeCode === 'zh_CN' ? '最近动作' : 'Latest Activity',
      dataIndex: 'latestRelease',
      render: (_: unknown, record) =>
        summarizeLatestActivity(localeCode, undefined, record.latestWorkflow, record.latestRelease),
    },
  ]

  const workflowMutation = useMutation({
    mutationFn: async () => {
      if (!binding || !selectedTarget) throw new Error(t('common.selectTarget', 'Select a release target'))
      return api.post('/workflows/trigger', {
        applicationId: binding.applicationId,
        workflowName: binding.workflowTemplate?.key || binding.workflowTemplate?.name || 'build-release-verify',
        clusterId: selectedTarget.clusterId,
        namespace: selectedTarget.namespace,
        deploymentName: selectedTarget.workloadName,
        triggerBuild: true,
        triggerRelease: false,
      })
    },
    onSuccess: () => {
      message.success(localeCode === 'zh_CN' ? '工作流已触发' : 'Workflow triggered')
      queryClient.invalidateQueries({ queryKey: ['workflows'] })
      queryClient.invalidateQueries({ queryKey: ['application-environment', applicationEnvironmentId] })
      queryClient.invalidateQueries({ queryKey: ['delivery-release-board'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const releaseMutation = useMutation({
    mutationFn: async () => {
      if (!binding || !selectedTarget) throw new Error(t('common.selectTarget', 'Select a release target'))
      const effectiveImageTag = imageTag.trim() || detail?.buildSource?.defaultTag || ''
      if (!effectiveImageTag) {
        throw new Error(localeCode === 'zh_CN' ? '请提供 Image Tag，或先在应用中配置默认 Tag' : 'Provide an image tag, or configure a default tag on the application first')
      }
      return api.post('/releases/trigger', {
        applicationId: binding.applicationId,
        applicationEnvironmentId: binding.id,
        clusterId: selectedTarget.clusterId,
        namespace: selectedTarget.namespace,
        deploymentName: selectedTarget.workloadName,
        containerName: containerName.trim() || selectedTarget.containerName || '',
        image: releaseImagePreview || undefined,
        imageTag: effectiveImageTag,
        releaseName: releaseName.trim(),
        actionKind: detail?.actionKind || 'deploy',
      })
    },
    onSuccess: () => {
      message.success(localeCode === 'zh_CN' ? '发布已触发' : 'Release triggered')
      queryClient.invalidateQueries({ queryKey: ['releases'] })
      queryClient.invalidateQueries({ queryKey: ['application-environment', applicationEnvironmentId] })
      queryClient.invalidateQueries({ queryKey: ['delivery-release-board'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const rollbackMutation = useMutation({
    mutationFn: async () => {
      if (!selectedTarget) throw new Error(t('common.selectTarget', 'Select a release target'))
      if (!rollbackRevision) throw new Error(t('common.selectRevision', 'Select a revision to roll back to'))
      return api.post(
        `/clusters/${selectedTarget.clusterId}/workloads/deployments/rollback`,
        {
          namespace: selectedTarget.namespace,
          name: selectedTarget.workloadName,
          revision: rollbackRevision,
        },
      )
    },
    onSuccess: () => {
      message.success(localeCode === 'zh_CN' ? '回滚已触发' : 'Rollback triggered')
      queryClient.invalidateQueries({ queryKey: ['deployment-rollouts', selectedTarget?.clusterId, selectedTarget?.namespace, selectedTarget?.workloadName] })
      queryClient.invalidateQueries({ queryKey: ['application-environment', applicationEnvironmentId] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  if (bindingQuery.isLoading) {
    return (
      <div className="soha-page">
        <ManagementDetailHeader
          title={localeCode === 'zh_CN' ? '环境详情' : 'Environment Detail'}
          description={localeCode === 'zh_CN' ? '加载应用环境绑定详情。' : 'Loading application-environment binding details.'}
        />
        <ManagementState kind="loading" title={t('common.loading', 'Loading...')} />
      </div>
    )
  }

  if (!binding) {
    return (
      <div className="soha-page">
        <ManagementDetailHeader
          title={localeCode === 'zh_CN' ? '环境详情' : 'Environment Detail'}
          description={localeCode === 'zh_CN' ? '当前绑定不存在或已被删除。' : 'The current binding does not exist or has been removed.'}
          actions={<Button onClick={() => navigate(backPath)}>{backLabel}</Button>}
        />
        <ManagementState kind="not-found" description={localeCode === 'zh_CN' ? '未找到应用环境绑定' : 'Application-environment binding not found'} />
      </div>
    )
  }

  const application = detail?.application
  const environment = detail?.environment
  const selectedTargetIsDeployment = selectedTarget?.workloadKind.toLowerCase() === 'deployment'
  const selectedTargetSupportsDirectRelease = !!selectedTarget && (
    selectedTarget.executorKind !== 'k8s_job_runner'
      || selectedTarget.targetKind !== 'k8s_workload'
      || selectedTargetIsDeployment
  )
  const targetOptions = (binding.targets ?? []).map((target) => ({
    value: target.id,
    label: `${target.clusterId} / ${target.namespace} / ${target.workloadName}`,
  }))
  const rolloutOptions = (rolloutHistoryQuery.data?.data ?? [])
    .filter((item) => item.revision)
    .map((item) => ({
      value: item.revision,
      label: `${item.revision}${item.createdAt ? ` · ${formatDateTime(item.createdAt)}` : ''}`,
    }))
  const releaseImagePreview = detail?.buildSource?.buildImage && (imageTag.trim() || detail.buildSource.defaultTag)
    ? `${detail.buildSource.buildImage}:${imageTag.trim() || detail.buildSource.defaultTag}`
    : ''
  const releaseActionLabel = detail?.actionKind === 'release'
    ? (localeCode === 'zh_CN' ? '触发发布' : 'Trigger Release')
    : (localeCode === 'zh_CN' ? '触发部署' : 'Trigger Deploy')

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title={`${application?.name || binding.applicationId} / ${environment?.name || binding.environmentKey || binding.environmentId}`}
        description={localeCode === 'zh_CN' ? '查看单个应用环境绑定的工作流模板、发布目标和最新执行状态。' : 'Inspect the workflow template, release targets, and latest execution state for a single application-environment binding.'}
        actions={<Button onClick={() => navigate(backPath)}>{backLabel}</Button>}
      />
      <Card className="soha-management-panel-card">
        <Descriptions
          items={[
            { key: 'environmentKey', label: localeCode === 'zh_CN' ? '环境 Key' : 'Environment Key', children: binding.environmentKey || environment?.key || '-' },
            { key: 'workflowTemplate', label: localeCode === 'zh_CN' ? '发布流程模板' : 'Workflow Template', children: binding.workflowTemplate?.name || '-' },
            { key: 'templateCategory', label: localeCode === 'zh_CN' ? '模板分类' : 'Template Category', children: binding.workflowTemplate?.category || '-' },
            { key: 'buildSource', label: localeCode === 'zh_CN' ? '构建来源' : 'Build Source', children: detail?.buildSource?.name || binding.buildPolicy?.sourceId || '-' },
            { key: 'strategyProfile', label: localeCode === 'zh_CN' ? '策略 Profile' : 'Strategy Profile', children: binding.strategyProfileId || '-' },
            { key: 'approvalPolicy', label: localeCode === 'zh_CN' ? '审批策略' : 'Approval Policy', children: binding.approvalPolicyId || '-' },
            { key: 'latestBundle', label: localeCode === 'zh_CN' ? '最新 Bundle' : 'Latest Bundle', children: <StatusTag value={detail?.latestBundle?.status || 'unknown'} /> },
            { key: 'latestTask', label: localeCode === 'zh_CN' ? '最新任务' : 'Latest Task', children: <StatusTag value={detail?.latestExecutionTask?.status || 'unknown'} /> },
            { key: 'latestBuild', label: localeCode === 'zh_CN' ? '最新 Build' : 'Latest Build', children: <StatusTag value={latestBuild?.status || 'unknown'} /> },
            { key: 'latestWorkflow', label: localeCode === 'zh_CN' ? '最新 Workflow' : 'Latest Workflow', children: <StatusTag value={latestWorkflow?.status || 'unknown'} /> },
            { key: 'latestRelease', label: localeCode === 'zh_CN' ? '最新 Release' : 'Latest Release', children: <StatusTag value={latestRelease?.status || 'unknown'} /> },
            { key: 'latestActivity', label: localeCode === 'zh_CN' ? '最近活动' : 'Latest Activity', children: summarizeLatestActivity(localeCode, latestBuild, latestWorkflow, latestRelease) },
          ]}
        />
      </Card>
      <Card className="soha-management-panel-card" title={localeCode === 'zh_CN' ? '交付动作' : 'Delivery Actions'}>
        <div className="soha-delivery-action-grid">
          <div className="soha-delivery-action-block">
            <Text strong>{localeCode === 'zh_CN' ? '发布目标' : 'Release Target'}</Text>
            <Select
              value={selectedTarget?.id}
              options={targetOptions}
              onChange={(value) => setSelectedTargetId(String(value))}
              placeholder={localeCode === 'zh_CN' ? '选择目标 deployment' : 'Select target deployment'}
            />
            <Text type="secondary" style={{ fontSize: 12 }}>
              {binding.workflowTemplate?.name
                ? `${localeCode === 'zh_CN' ? '工作流模板' : 'Workflow Template'}: ${binding.workflowTemplate.name}${binding.workflowTemplate.category ? ` / ${binding.workflowTemplate.category}` : ''}`
                : localeCode === 'zh_CN' ? '当前未绑定工作流模板，将使用默认流程名' : 'No workflow template is bound. The default workflow name will be used.'}
            </Text>
          </div>
          <div className="soha-delivery-action-block">
            <Text strong>{localeCode === 'zh_CN' ? '触发工作流' : 'Trigger Workflow'}</Text>
            <Text type="secondary" style={{ fontSize: 12 }}>{localeCode === 'zh_CN' ? '生成一条 workflow run，只做流程编排，不直接改 deployment 镜像。' : 'Create a workflow run for orchestration only without directly changing the deployment image.'}</Text>
            <Button
              icon={<PlayCircleOutlined />}
              type="primary"
              onClick={() => workflowMutation.mutate()}
              loading={workflowMutation.isPending}
              disabled={!canTriggerWorkflow || !selectedTarget}
            >
              {localeCode === 'zh_CN' ? '触发工作流' : 'Trigger Workflow'}
            </Button>
          </div>
          <div className="soha-delivery-action-block">
            <Text strong>{localeCode === 'zh_CN' ? '触发发布' : 'Trigger Release'}</Text>
            <Input value={imageTag} onChange={(event) => setImageTag(event.target.value)} placeholder={localeCode === 'zh_CN' ? 'Image Tag，默认取应用默认 Tag' : 'Image tag, defaulting to the application default tag'} />
            <Input value={releaseName} onChange={(event) => setReleaseName(event.target.value)} placeholder={localeCode === 'zh_CN' ? 'Release Name，可留空自动生成' : 'Release name, leave empty to auto-generate'} />
            <Input value={containerName} onChange={(event) => setContainerName(event.target.value)} placeholder={localeCode === 'zh_CN' ? 'Container Name，可留空使用绑定值' : 'Container name, leave empty to use the binding value'} />
            {releaseImagePreview ? <Text type="secondary" style={{ fontSize: 12 }}>{`${localeCode === 'zh_CN' ? '目标镜像' : 'Target Image'}: ${releaseImagePreview}`}</Text> : null}
            {!selectedTargetSupportsDirectRelease ? <Text type="warning">{localeCode === 'zh_CN' ? '当前目标暂不支持直接发布。' : 'The current target does not support direct release yet.'}</Text> : null}
            <Button
              icon={<SendOutlined />}
              type="primary"
              onClick={() => releaseMutation.mutate()}
              loading={releaseMutation.isPending}
              disabled={!canTriggerRelease || !selectedTarget || !selectedTargetSupportsDirectRelease}
            >
              {releaseActionLabel}
            </Button>
          </div>
          <div className="soha-delivery-action-block">
            <Text strong>{localeCode === 'zh_CN' ? '回滚' : 'Rollback'}</Text>
            <Select
              value={rollbackRevision || undefined}
              options={rolloutOptions}
              onChange={(value) => setRollbackRevision(String(value))}
              placeholder={t('common.selectRevision', 'Select a revision to roll back to')}
              loading={rolloutHistoryQuery.isLoading}
              disabled={!selectedTarget || rolloutOptions.length === 0}
            />
            <Text type="secondary" style={{ fontSize: 12 }}>
              {!selectedTargetIsDeployment
                ? (localeCode === 'zh_CN' ? '当前目标不是 Deployment，暂不支持回滚。' : 'The current target is not a Deployment, so rollback is not supported yet.')
                : rolloutOptions.length > 0
                  ? (localeCode === 'zh_CN' ? '回滚会直接对目标 deployment 发起 Kubernetes rollback。' : 'Rollback will issue a Kubernetes rollback directly against the target deployment.')
                  : (localeCode === 'zh_CN' ? '当前没有可用的 rollout history。' : 'No rollout history is currently available.')}
            </Text>
            <Button
              icon={<ReloadOutlined />}
              danger
              onClick={() => rollbackMutation.mutate()}
              loading={rollbackMutation.isPending}
              disabled={!canRollbackDeployment || !selectedTarget || !selectedTargetIsDeployment || !rollbackRevision}
            >
              {localeCode === 'zh_CN' ? '回滚到所选版本' : 'Rollback to Selected Revision'}
            </Button>
          </div>
        </div>
      </Card>
      <DeliveryTable
        title={localeCode === 'zh_CN' ? '发布目标' : 'Release Targets'}
        columns={targetColumns}
        dataSource={targetRows}
        rowKey="id"
        pagination={false}
      />
      <Card className="soha-management-panel-card" title={localeCode === 'zh_CN' ? 'Workflow Template 定义' : 'Workflow Template Definition'}>
        {binding.workflowTemplate?.definition ? (
          <pre className="soha-json-block">{JSON.stringify(binding.workflowTemplate.definition, null, 2)}</pre>
        ) : (
          <ManagementState bordered={false} compact kind="not-configured" description={localeCode === 'zh_CN' ? '当前未配置工作流模板定义' : 'No workflow template definition is configured'} />
        )}
      </Card>
      <Card className="soha-management-panel-card" title={localeCode === 'zh_CN' ? '构建与发布策略' : 'Build and Release Policy'}>
        <Descriptions
          items={[
            { key: 'buildPolicy', label: 'Build Policy', children: <pre className="soha-json-block">{JSON.stringify(binding.buildPolicy ?? {}, null, 2)}</pre> },
            { key: 'releasePolicy', label: 'Release Policy', children: <pre className="soha-json-block">{JSON.stringify(binding.releasePolicy ?? {}, null, 2)}</pre> },
          ]}
        />
      </Card>
    </div>
  )
}

export function WorkflowTemplatesPage() {
  const { t, localeCode } = useI18n()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [form] = Form.useForm<Record<string, unknown>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<WorkflowTemplate | null>(null)
  const [editorDefinition, setEditorDefinition] = useState<ReleaseDagDefinition>(createDefaultReleaseDagDefinition())
  const canManageWorkflowTemplates = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.workflow-templates.manage')

  const { data, isFetching, isLoading, refetch } = useQuery({
    queryKey: ['workflow-templates'],
    queryFn: () => api.get<ApiResponse<WorkflowTemplate[]>>('/workflow-templates'),
  })

  useEffect(() => {
    if (!modalVisible) return
    setEditorDefinition(normalizeReleaseDagDefinition(editing?.definition))
  }, [editing, modalVisible])

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/workflow-templates', values),
    onSuccess: () => {
      message.success(localeCode === 'zh_CN' ? 'DAG 发布流程模板创建成功' : 'DAG release flow template created')
      queryClient.invalidateQueries({ queryKey: ['workflow-templates'] })
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/workflow-templates/${id}`, values),
    onSuccess: () => {
      message.success(localeCode === 'zh_CN' ? 'DAG 发布流程模板更新成功' : 'DAG release flow template updated')
      queryClient.invalidateQueries({ queryKey: ['workflow-templates'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/workflow-templates/${id}`),
    onSuccess: () => {
      message.success(localeCode === 'zh_CN' ? 'DAG 发布流程模板已删除' : 'DAG release flow template deleted')
      queryClient.invalidateQueries({ queryKey: ['workflow-templates'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const previewDefinition = useMemo(() => JSON.stringify(editorDefinition, null, 2), [editorDefinition])

  const columns: ColumnProps<WorkflowTemplate>[] = [
    { title: t('common.name', 'Name'), dataIndex: 'name' },
    { title: 'Key', dataIndex: 'key' },
    { title: localeCode === 'zh_CN' ? '分类' : 'Category', dataIndex: 'category', render: (value: string) => value || 'release' },
    {
      title: localeCode === 'zh_CN' ? '模式' : 'Mode',
      dataIndex: 'definition',
      render: (value: WorkflowTemplate['definition']) => normalizeReleaseDagDefinition(value).mode,
    },
    {
      title: localeCode === 'zh_CN' ? '节点数' : 'Nodes',
      dataIndex: 'definition',
      render: (value: WorkflowTemplate['definition']) => countReleaseDagNodes(value),
    },
    { title: localeCode === 'zh_CN' ? '启用' : 'Enabled', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
    { ...tableColumnPresets.datetime, title: t('common.updatedAt', 'Updated At'), dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: t('common.actions', 'Actions'),
      dataIndex: 'id',
      render: (_: unknown, record: WorkflowTemplate) => (
        <Space className="soha-row-action-icons" size={2}>
          {canManageWorkflowTemplates ? (
            <ManagementIconButton
              aria-label="编辑工作流模板"
              icon={<EditOutlined />}
              size="small"
              tooltip="编辑"
              onClick={() => { setEditing(record); setModalVisible(true) }}
            />
          ) : null}
          {canManageWorkflowTemplates ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
              <ManagementIconButton
                aria-label="删除工作流模板"
                danger
                icon={<DeleteOutlined />}
                size="small"
                tooltip="删除"
              />
            </Popconfirm>
          ) : null}
          {!canManageWorkflowTemplates ? '-' : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="soha-page">
      <DeliveryTable
        actions={canManageWorkflowTemplates ? (
          <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>
            {localeCode === 'zh_CN' ? '新建模板' : 'New Template'}
          </Button>
        ) : null}
        refreshing={isFetching}
        onRefresh={() => void refetch()}
        columns={columns}
        dataSource={data?.data ?? []}
        rowKey="id"
        loading={isLoading}
      />
      <Modal
        title={editing ? (localeCode === 'zh_CN' ? '编辑 DAG 发布流程模板' : 'Edit DAG Release Flow Template') : (localeCode === 'zh_CN' ? '新建 DAG 发布流程模板' : 'New DAG Release Flow Template')}
        open={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null) }}
        footer={null}
        width={1440}
        destroyOnHidden
      >
        <Form
          form={form}
          key={editing?.id ?? 'new-workflow-template'}
          layout="vertical"
          onFinish={(values) => {
            const payload = {
              ...values,
              category: values.category || 'release',
              definition: editorDefinition,
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
          initialValues={editing ? { ...editing, category: editing.category || 'release' } : { enabled: true, category: 'release' }}
        >
          <div className="soha-delivery-action-grid">
            <div className="soha-delivery-action-block">
              <Form.Item name="key" label={localeCode === 'zh_CN' ? '模板 Key' : 'Template Key'} rules={[{ required: true, message: localeCode === 'zh_CN' ? '请输入模板 Key' : 'Enter the template key' }]}>
                <Input />
              </Form.Item>
            </div>
            <div className="soha-delivery-action-block">
              <Form.Item name="name" label={localeCode === 'zh_CN' ? '模板名称' : 'Template Name'} rules={[{ required: true, message: localeCode === 'zh_CN' ? '请输入模板名称' : 'Enter the template name' }]}>
                <Input />
              </Form.Item>
            </div>
            <div className="soha-delivery-action-block">
              <Form.Item name="description" label={localeCode === 'zh_CN' ? '描述' : 'Description'}>
                <Input />
              </Form.Item>
            </div>
            <div className="soha-delivery-action-block">
              <Form.Item name="category" label={localeCode === 'zh_CN' ? '分类' : 'Category'}>
                <Select options={RELEASE_TEMPLATE_CATEGORY_OPTIONS} />
              </Form.Item>
              <Form.Item name="enabled" label={localeCode === 'zh_CN' ? '启用' : 'Enabled'} valuePropName="checked">
                <Switch />
              </Form.Item>
            </div>
          </div>

          <Card className="soha-template-editor-card soha-management-panel-card" title={localeCode === 'zh_CN' ? 'DAG 编排画布' : 'DAG Composer'}>
            <Suspense fallback={<ManagementState kind="loading" title={t('common.loading', 'Loading...')} />}>
              <ReleaseFlowDagEditor
                key={editing?.id ?? 'new-release-dag'}
                initialDefinition={modalVisible ? normalizeReleaseDagDefinition(editing?.definition) : createDefaultReleaseDagDefinition()}
                onChange={setEditorDefinition}
              />
            </Suspense>
          </Card>

          <Card className="soha-flow-stage-card soha-management-panel-card" title={localeCode === 'zh_CN' ? 'JSON 预览' : 'JSON Preview'}>
            <pre className="soha-json-block">{previewDefinition}</pre>
          </Card>

          <div className="soha-form-actions">
            <Button onClick={() => setModalVisible(false)}>{t('common.cancel', 'Cancel')}</Button>
            <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? t('common.update', 'Update') : t('common.create', 'Create')}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
