import { MutationFeedback } from '@/components/request-feedback'
import { writeOutcomeUnknown } from '@/utils/query-feedback'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, App, Button, Form, Input, Modal, Select, Switch } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { namespaceQueries } from '@/features/platform'
import { isApiError } from '@/services/api-error'
import { deliveryApi } from './api'
import { deliveryMutations } from './mutations'
import { deliveryQueries } from './queries'
import {
  releaseTargetKey,
  releaseTargetsFromCandidates,
  manifestReleaseTargets,
} from './release-targets'
import { manifestQueries } from './manifests/queries'
import type { ApplicationEnvironment, BuildSource, DeliveryApplication } from './types'

export function summarizeBuildSource(source?: BuildSource) {
  if (!source) return '-'
  switch (source.type) {
    case 'repo_dockerfile':
      return 'Repo Dockerfile'
    case 'repo_buildpacks':
      return 'Buildpacks'
    case 'platform_build_template':
      return 'Platform Template'
    case 'external_pipeline':
      return 'External Pipeline'
    default:
      return source.type
  }
}

export function splitApplicationGroups(value?: string | string[] | null) {
  const raw = Array.isArray(value) ? value.join(',') : String(value ?? '')
  return Array.from(
    new Set(
      raw
        .split(/[,，;；/]/)
        .map((item) => item.trim())
        .filter(Boolean),
    ),
  )
}

export function joinApplicationGroups(value?: string | string[] | null) {
  return splitApplicationGroups(value ?? []).join(', ')
}

export function applicationKeyFromName(value: unknown) {
  return String(value ?? '')
    .normalize('NFKC')
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}_-]+/gu, '-')
    .replace(/^-+|-+$/g, '')
}

export function buildApplicationGroupOptions(apps: DeliveryApplication[] = []) {
  return Array.from(new Set(apps.flatMap((app) => splitApplicationGroups(app.group))))
}

export function useApplicationCenterState({
  currentApplication,
  loadApplications = true,
  loadWorkflowTemplates = true,
  loadClusters = true,
}: {
  currentApplication?: DeliveryApplication
  loadApplications?: boolean
  loadWorkflowTemplates?: boolean
  loadClusters?: boolean
} = {}) {
  const { applicationId } = useParams()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const [bindingForm] = Form.useForm<Record<string, unknown>>()
  const [appModalVisible, setAppModalVisible] = useState(false)
  const [bindingModalVisible, setBindingModalVisible] = useState(false)
  const [editingApp, setEditingApp] = useState<DeliveryApplication | null>(null)
  const [editingBinding, setEditingBinding] = useState<ApplicationEnvironment | null>(null)
  const [buildSources, setBuildSources] = useState<BuildSource[]>([])
  const [selectedApplicationId, setSelectedApplicationId] = useState<string>('')

  const applicationsQuery = useQuery(deliveryQueries.applications.list(loadApplications))
  const bindingsQuery = useQuery(deliveryQueries.environments.list())
  const workflowTemplatesQuery = useQuery(
    deliveryQueries.workflowTemplates.list(loadWorkflowTemplates),
  )
  const clustersQuery = useQuery(
    deliveryQueries.dependencies.clusters(loadClusters || bindingModalVisible),
  )
  const environmentCatalogQuery = useQuery(
    deliveryQueries.environmentCatalog.list(bindingModalVisible),
  )
  const registriesQuery = useQuery(deliveryQueries.registries.list(bindingModalVisible))
  const selectedClusterId = Form.useWatch('clusterId', bindingForm) as string | undefined
  const selectedNamespace = Form.useWatch('namespace', bindingForm) as string | undefined
  const namespacesQuery = useQuery(
    namespaceQueries.list({
      clusterId: bindingModalVisible ? selectedClusterId || null : null,
      namespace: null,
    }),
  )
  const targetCandidatesQuery = useQuery(
    deliveryQueries.environments.targetCandidates(
      {
        clusterId: selectedClusterId || '',
        namespace: selectedNamespace || '',
        limit: 200,
      },
      bindingModalVisible,
    ),
  )

  useEffect(() => {
    const appList = applicationsQuery.data ?? (currentApplication ? [currentApplication] : [])
    if (applicationId && appList.some((item) => item.id === applicationId)) {
      if (selectedApplicationId !== applicationId) {
        setSelectedApplicationId(applicationId)
      }
      return
    }
    if (!selectedApplicationId && appList.length > 0) {
      setSelectedApplicationId(appList[0].id)
    }
  }, [applicationId, applicationsQuery.data, currentApplication, selectedApplicationId])

  const selectedApplication = useMemo(
    () =>
      (applicationsQuery.data ?? (currentApplication ? [currentApplication] : [])).find(
        (item) => item.id === selectedApplicationId,
      ) ?? null,
    [applicationsQuery.data, currentApplication, selectedApplicationId],
  )
  const filteredBindings = useMemo(
    () =>
      (bindingsQuery.data ?? []).filter(
        (item) => !selectedApplicationId || item.applicationId === selectedApplicationId,
      ),
    [bindingsQuery.data, selectedApplicationId],
  )
  const workflowTemplateMap = useMemo(
    () => Object.fromEntries((workflowTemplatesQuery.data ?? []).map((item) => [item.id, item])),
    [workflowTemplatesQuery.data],
  )
  const applicationGroupOptions = useMemo(
    () =>
      buildApplicationGroupOptions(
        applicationsQuery.data ?? (currentApplication ? [currentApplication] : []),
      ),
    [applicationsQuery.data, currentApplication],
  )
  const canCreateApplication = hasPermission(permissionSnapshot, 'delivery.application.create')
  const canUpdateApplication = hasPermission(permissionSnapshot, 'delivery.application.update')
  const canDeleteApplication = hasPermission(permissionSnapshot, 'delivery.application.delete')
  const canCreateBinding = hasPermission(
    permissionSnapshot,
    'delivery.application-environments.create',
  )
  const canUpdateBinding = hasPermission(
    permissionSnapshot,
    'delivery.application-environments.update',
  )
  const canDeleteBinding = hasPermission(
    permissionSnapshot,
    'delivery.application-environments.delete',
  )

  const createAppOptions = deliveryMutations.applications.create(queryClient)
  const createAppMutation = useMutation({
    ...createAppOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void createAppOptions.onSuccess?.(result, variables, onMutateResult, context)
      message.success('应用创建成功')
      setSelectedApplicationId(result.id || '')
      setAppModalVisible(false)
    },
  })

  const updateAppOptions = deliveryMutations.applications.update(queryClient)
  const updateAppMutation = useMutation({
    ...updateAppOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void updateAppOptions.onSuccess?.(result, variables, onMutateResult, context)
      message.success('应用更新成功')
      setAppModalVisible(false)
      setEditingApp(null)
    },
  })

  const deleteAppOptions = deliveryMutations.applications.delete(queryClient)
  const deleteAppMutation = useMutation({
    ...deleteAppOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void deleteAppOptions.onSuccess?.(result, variables, onMutateResult, context)
      message.success('应用已删除')
      setSelectedApplicationId('')
    },
    onError: (err: Error) => message.error(err.message),
  })

  const createBindingOptions = deliveryMutations.environments.create(queryClient)
  const createBindingMutation = useMutation({
    ...createBindingOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void createBindingOptions.onSuccess?.(result, variables, onMutateResult, context)
      message.success('环境创建成功')
      setBindingModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const updateBindingOptions = deliveryMutations.environments.update(queryClient)
  const updateBindingMutation = useMutation({
    ...updateBindingOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void updateBindingOptions.onSuccess?.(result, variables, onMutateResult, context)
      message.success('环境更新成功')
      setBindingModalVisible(false)
      setEditingBinding(null)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const deleteBindingOptions = deliveryMutations.environments.delete(queryClient)
  const deleteBindingMutation = useMutation({
    ...deleteBindingOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void deleteBindingOptions.onSuccess?.(result, variables, onMutateResult, context)
      message.success('环境绑定已删除')
    },
    onError: (err: Error) => message.error(err.message),
  })

  return {
    navigate,
    bindingForm,
    appModalVisible,
    setAppModalVisible,
    bindingModalVisible,
    setBindingModalVisible,
    editingApp,
    setEditingApp,
    editingBinding,
    setEditingBinding,
    buildSources,
    setBuildSources,
    applicationsQuery,
    bindingsQuery,
    workflowTemplatesQuery,
    clustersQuery,
    environmentCatalogQuery,
    registriesQuery,
    namespacesQuery,
    targetCandidatesQuery,
    selectedClusterId,
    selectedNamespace,
    selectedApplicationId,
    setSelectedApplicationId,
    selectedApplication,
    filteredBindings,
    workflowTemplateMap,
    applicationGroupOptions,
    canCreateApplication,
    canUpdateApplication,
    canDeleteApplication,
    canCreateBinding,
    canUpdateBinding,
    canDeleteBinding,
    createAppMutation,
    updateAppMutation,
    deleteAppMutation,
    createBindingMutation,
    updateBindingMutation,
    deleteBindingMutation,
  }
}

export type ApplicationCenterState = ReturnType<typeof useApplicationCenterState>

export function ApplicationForm({
  application,
  onCancel,
  onCreated,
  state,
}: {
  application: DeliveryApplication | null
  onCancel: () => void
  onCreated?: (application: DeliveryApplication) => void
  state: ApplicationCenterState
}) {
  const { message } = App.useApp()
  const [form] = Form.useForm<Record<string, unknown>>()
  const keyEdited = useRef(false)
  const submitting = useRef(false)
  const locked =
    state.createAppMutation.isPending ||
    state.updateAppMutation.isPending ||
    writeOutcomeUnknown(state.createAppMutation.error) ||
    writeOutcomeUnknown(state.updateAppMutation.error)
  const reload = useMutation({
    mutationFn: async () => (await deliveryApi.applications.detail(application!.id)).application,
    onSuccess: (latest) => {
      form.resetFields()
      form.setFieldsValue({
        ...latest,
        description: latest.description ?? '',
        group: splitApplicationGroups(latest.group),
      })
      state.setEditingApp(latest)
      state.setBuildSources(latest.buildSources ?? [])
      state.updateAppMutation.reset()
    },
    onError: (error: Error) => message.error(error.message),
  })
  const conflicted =
    isApiError(state.updateAppMutation.error) && state.updateAppMutation.error.status === 409
  return (
    <Form
      form={form}
      layout="vertical"
      initialValues={
        application
          ? {
              ...application,
              description: application.description ?? '',
              group: splitApplicationGroups(application.group),
              enabled: application.enabled,
            }
          : { group: [], description: '' }
      }
      onValuesChange={(changed) => {
        if ('key' in changed) keyEdited.current = true
        if (!application && 'name' in changed && !keyEdited.current) {
          form.setFieldValue('key', applicationKeyFromName(changed.name))
        }
      }}
      onFinish={async (values) => {
        if (submitting.current || locked) return
        submitting.current = true
        try {
          if (application) {
            await state.updateAppMutation.mutateAsync({
              id: application.id,
              payload: {
                ...application,
                ...form.getFieldsValue(true),
                ...values,
                expectedVersion: form.getFieldValue('version'),
                group: joinApplicationGroups(values.group as string[] | string),
                buildSources: state.buildSources,
              },
            })
            return
          }
          await state.createAppMutation
            .mutateAsync({
              name: values.name,
              key: values.key,
              description: values.description,
              group: joinApplicationGroups(values.group as string[] | string),
              enabled: true,
            })
            .then((created) => onCreated?.(created))
            .catch(() => undefined)
        } catch {
          /* Mutation feedback retains the application draft. */
        } finally {
          submitting.current = false
        }
      }}
    >
      <MutationFeedback
        operations={[
          {
            mutation: state.createAppMutation,
            label: '创建应用',
            target: state.createAppMutation.variables?.name
              ? String(state.createAppMutation.variables.name)
              : undefined,
          },
          {
            mutation: state.updateAppMutation,
            label: '保存应用配置',
            target: state.updateAppMutation.variables?.id,
          },
        ]}
      />
      {application && conflicted ? (
        <Alert
          type="warning"
          title="应用配置已被其他人更新"
          description="当前草稿已保留。重新加载会放弃本次修改并载入最新配置。"
          action={
            <Button loading={reload.isPending} onClick={() => reload.mutate()}>
              重新加载配置
            </Button>
          }
        />
      ) : null}
      <Form.Item
        name="name"
        label="应用名称"
        rules={[{ required: true, message: '请输入应用名称' }]}
      >
        <Input />
      </Form.Item>
      <Form.Item
        name="key"
        label="应用 Key"
        rules={[{ required: true, message: '请输入应用 Key' }]}
      >
        <Input />
      </Form.Item>
      <Form.Item name="description" label="备注">
        <Input.TextArea rows={3} placeholder="描述应用的用途" />
      </Form.Item>
      <Form.Item name="group" label="应用分组">
        <Select
          mode="tags"
          tokenSeparators={[',', '，', ';', '；', '/']}
          placeholder="可选，用于筛选应用"
          maxTagCount="responsive"
          options={state.applicationGroupOptions.map((group) => ({
            value: group,
            label: group,
          }))}
        />
      </Form.Item>
      {application ? (
        <Form.Item name="enabled" label="启用" valuePropName="checked">
          <Switch />
        </Form.Item>
      ) : null}
      <div className="soha-form-actions">
        <Button onClick={onCancel}>取消</Button>
        <Button
          htmlType="submit"
          type="primary"
          disabled={locked}
          loading={state.createAppMutation.isPending || state.updateAppMutation.isPending}
        >
          {application ? '保存' : '创建应用'}
        </Button>
      </div>
    </Form>
  )
}

export function ApplicationCenterModals({ state }: { state: ApplicationCenterState }) {
  const editTargets = Form.useWatch('editTargets', state.bindingForm) ?? false
  const packagesQuery = useQuery(
    manifestQueries.applicationPackages(
      state.selectedApplicationId,
      state.bindingModalVisible && Boolean(state.editingBinding),
    ),
  )
  const manifestTargets = manifestReleaseTargets(
    packagesQuery.data ?? [],
    state.editingBinding?.id ?? '',
    state.selectedClusterId,
    state.selectedNamespace,
  )
  const boundEnvironmentIds = new Set(
    state.filteredBindings
      .filter((item) => item.id !== state.editingBinding?.id)
      .map((item) => item.environmentId),
  )
  const environmentOptions = (state.environmentCatalogQuery.data ?? [])
    .filter((item) => item.enabled && !boundEnvironmentIds.has(item.id))
    .map((item) => ({ value: item.id, label: `${item.name} · ${item.key}` }))
  const existingTargets = state.editingBinding?.targets ?? []
  const targetOptions = Array.from(
    new Map(
      [
        ...manifestTargets.map((target) => ({
          value: releaseTargetKey(target),
          label: `${target.metadata?.manifestPackageName} · 资源包`,
        })),
        ...existingTargets.map((target) => ({
          value: releaseTargetKey(target),
          label:
            target.executorKind === 'manifest_ssa'
              ? `${target.metadata?.manifestPackageName || target.workloadName} · 资源包`
              : `${target.workloadKind} / ${target.workloadName}`,
        })),
        ...(state.targetCandidatesQuery.data?.items ?? []).map((candidate) => ({
          value: releaseTargetKey(candidate),
          label: `${candidate.workloadKind} / ${candidate.workloadName} · ${candidate.readyReplicas}/${candidate.desiredReplicas} 就绪`,
        })),
      ].map((option) => [option.value, option]),
    ).values(),
  )

  return (
    <>
      <Modal
        title="编辑应用"
        open={state.appModalVisible && Boolean(state.editingApp)}
        onCancel={() => {
          state.setAppModalVisible(false)
          state.setEditingApp(null)
        }}
        footer={null}
        destroyOnHidden
        width={720}
      >
        <ApplicationForm
          key={state.editingApp?.id ?? 'new'}
          application={state.editingApp}
          state={state}
          onCancel={() => {
            state.setAppModalVisible(false)
            state.setEditingApp(null)
          }}
        />
      </Modal>

      <Modal
        title={state.editingBinding ? '编辑环境' : '新增环境'}
        open={state.bindingModalVisible}
        onCancel={() => {
          state.setBindingModalVisible(false)
          state.setEditingBinding(null)
        }}
        footer={null}
        destroyOnHidden
        width={640}
      >
        <Form
          form={state.bindingForm}
          key={state.editingBinding?.id ?? `binding-${state.selectedApplicationId || 'new'}`}
          layout="vertical"
          initialValues={
            state.editingBinding
              ? {
                  environmentId: state.editingBinding.environmentId,
                  alias: state.editingBinding.alias || state.editingBinding.environmentKey,
                  clusterId:
                    state.editingBinding.clusterId || state.editingBinding.targets?.[0]?.clusterId,
                  namespace:
                    state.editingBinding.namespace || state.editingBinding.targets?.[0]?.namespace,
                  registryId: state.editingBinding.registryId,
                  targetKeys: state.editingBinding.targets?.map(releaseTargetKey),
                }
              : undefined
          }
          onFinish={(values) => {
            if (!state.selectedApplication) return
            const existing = state.editingBinding
            const payload: Record<string, unknown> = {
              expectedUpdatedAt: existing?.updatedAt,
              applicationId: state.selectedApplication.id,
              environmentId: String(values.environmentId || '').trim(),
              alias: String(values.alias || '').trim(),
              clusterId: String(values.clusterId || '').trim(),
              namespace: String(values.namespace || '').trim(),
              registryId: String(values.registryId || '').trim(),
              strategyProfileId: existing?.strategyProfileId,
              promotionPolicyId: existing?.promotionPolicyId,
              artifactPolicyId: existing?.artifactPolicyId,
              workflowTemplateId: existing?.workflowTemplateId,
              workflowTemplateVersion: existing?.workflowTemplateVersion,
              buildPolicy: existing?.buildPolicy,
              releasePolicy: existing?.releasePolicy,
              resourceSelector: existing?.resourceSelector,
              ...(!existing || values.editTargets
                ? {
                    targets: releaseTargetsFromCandidates(
                      state.targetCandidatesQuery.data?.items ?? [],
                      values.targetKeys as string[] | undefined,
                      existing?.targets,
                      manifestTargets,
                    ),
                  }
                : {}),
            }
            if (existing) {
              state.updateBindingMutation.mutate({ id: existing.id, payload })
            } else {
              state.createBindingMutation.mutate(payload)
            }
          }}
        >
          <Form.Item label="应用">
            <Input value={state.selectedApplication?.name || ''} disabled />
          </Form.Item>
          <Form.Item
            name="environmentId"
            label="环境目录"
            rules={[{ required: true, message: '请选择环境' }]}
          >
            <Select
              showSearch={{ optionFilterProp: 'label' }}
              disabled={Boolean(state.editingBinding)}
              loading={state.environmentCatalogQuery.isLoading}
              options={environmentOptions}
              placeholder="选择平台环境"
              onChange={(environmentId) => {
                if (state.bindingForm.isFieldTouched('alias')) return
                const environment = (state.environmentCatalogQuery.data ?? []).find(
                  (item) => item.id === environmentId,
                )
                state.bindingForm.setFieldValue('alias', environment?.name)
              }}
            />
          </Form.Item>
          <Form.Item
            name="alias"
            label="环境别名"
            rules={[{ required: true, message: '请输入环境别名' }]}
          >
            <Input placeholder="例如：集成测试、华东生产" />
          </Form.Item>
          <Form.Item
            name="clusterId"
            label={state.editingBinding ? '默认集群' : '集群'}
            rules={[{ required: true, message: '请选择集群' }]}
          >
            <Select
              showSearch={{ optionFilterProp: 'label' }}
              loading={state.clustersQuery.isLoading}
              placeholder="选择部署集群"
              options={(state.clustersQuery.data ?? []).map((item) => ({
                value: item.id,
                label: item.name,
              }))}
              onChange={() =>
                state.bindingForm.setFieldsValue({
                  namespace: undefined,
                  ...(!state.editingBinding ? { targetKeys: [] } : {}),
                })
              }
            />
          </Form.Item>
          <Form.Item
            name="namespace"
            label={state.editingBinding ? '默认 Namespace' : 'Namespace'}
            rules={[{ required: true, message: '请选择 Namespace' }]}
          >
            <Select
              showSearch={{ optionFilterProp: 'label' }}
              disabled={!state.selectedClusterId}
              loading={state.namespacesQuery.isLoading}
              placeholder="选择 Namespace"
              options={(state.namespacesQuery.data ?? []).map((item) => ({
                value: item.name,
                label: item.name,
              }))}
              onChange={() => {
                if (!state.editingBinding) state.bindingForm.setFieldValue('targetKeys', [])
              }}
            />
          </Form.Item>
          {state.editingBinding ? (
            <>
              <Alert
                type="info"
                showIcon
                title="默认位置仅用于新增目标"
                description="修改默认集群或 Namespace 不会移动、删除或解除已有目标。已有服务保留各自的实际部署位置。"
              />
              <Form.Item
                name="editTargets"
                label="同时调整发布目标"
                valuePropName="checked"
                initialValue={false}
              >
                <Switch />
              </Form.Item>
            </>
          ) : null}
          <Form.Item
            hidden={Boolean(state.editingBinding) && !editTargets}
            name="targetKeys"
            label="发布目标"
            tooltip="可选择已有 Workload 或绑定到此环境的资源包；新环境可先保存，再关联资源包。"
          >
            <Select
              mode="multiple"
              disabled={!state.selectedClusterId || !state.selectedNamespace}
              loading={state.targetCandidatesQuery.isFetching || packagesQuery.isFetching}
              options={targetOptions}
              placeholder="选择 Workload 或资源包"
              showSearch={{ optionFilterProp: 'label' }}
            />
          </Form.Item>
          <Form.Item
            name="registryId"
            label="镜像仓库"
            rules={[{ required: true, message: '请选择镜像仓库' }]}
          >
            <Select
              showSearch={{ optionFilterProp: 'label' }}
              loading={state.registriesQuery.isLoading}
              placeholder="选择该环境默认使用的镜像仓库"
              options={(state.registriesQuery.data ?? []).map((item) => ({
                value: item.id,
                label: `${item.name} · ${item.endpoint}`,
              }))}
            />
          </Form.Item>
          <div className="soha-form-actions">
            <Button
              onClick={() => {
                state.setBindingModalVisible(false)
                state.setEditingBinding(null)
              }}
            >
              取消
            </Button>
            <Button
              htmlType="submit"
              type="primary"
              loading={
                state.createBindingMutation.isPending || state.updateBindingMutation.isPending
              }
            >
              保存
            </Button>
          </div>
        </Form>
      </Modal>
    </>
  )
}
