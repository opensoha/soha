import { useEffect, useRef, useState } from 'react'
import type { MutableRefObject } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Collapse,
  Form,
  Input,
  Radio,
  Select,
  Space,
  Switch,
  Typography,
} from 'antd'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ManagementIconButton } from '@/components/management-list'
import { StepForm } from '@/components/step-form'
import { isApiError } from '@/services/api-error'
import { deliveryMutations } from '../mutations'
import { deliveryApi } from '../api'
import { deliveryQueries } from '../queries'
import { useUnsavedDocument } from '../documents/use-unsaved-document'
import { serviceCreationPreset } from './service-presets'
import type {
  ApplicationServiceComponent,
  ApplicationServiceContainer,
  BuildSource,
  DeliveryApplication,
  DeliveryRepository,
} from '../types'
import type { RepositoryDraft, ServiceSetupCheckpoint, ServiceSetupDraft } from '../service-setup'
import { buildSourceRepositoryBindings } from '../service-setup'
import { BuildSourceFields } from './build-source-fields'
import { RepositoryFields } from './repository-fields'
import {
  DeploymentEnvironmentFields,
  DeploymentTemplateFields,
  initialDeploymentValues,
  useServiceDeployment,
} from './deployment-template-fields'
import type { DeploymentFormValues } from './deployment-template-fields'

import './service-editor-layout.css'

const { Text } = Typography
export const SERVICE_KIND_OPTIONS = [
  {
    value: 'kubernetes_workload',
    label: 'Kubernetes 工作负载',
    description: '资源清单与容器镜像驱动',
  },
  { value: 'helm_release', label: 'Helm Release', description: 'Helm Chart 与 values 驱动' },
  { value: 'external_service', label: '外部服务', description: '只纳管依赖、端点与验证' },
  { value: 'job', label: '批处理任务', description: '一次性或周期任务交付' },
]

type ServiceFormValues = Omit<
  ApplicationServiceComponent,
  'applicationId' | 'createdAt' | 'updatedAt' | 'containers'
> & {
  containers?: Array<
    Omit<ApplicationServiceContainer, 'createdAt' | 'updatedAt' | 'runtimePorts'> & {
      runtimePortsText?: string
    }
  >
}

function parsePorts(value?: string) {
  return String(value ?? '')
    .split(',')
    .map((item) => Number(item.trim()))
    .filter((item) => Number.isFinite(item) && Number.isInteger(item) && item > 0 && item <= 65535)
}

function formatPorts(value?: number[]) {
  return value?.join(', ') ?? ''
}

function serviceInitialValues(service?: ApplicationServiceComponent | null): ServiceFormValues {
  if (!service) {
    return {
      serviceKind: 'kubernetes_workload',
      enabled: true,
      containers: [
        {
          name: 'main',
          runtimePortsText: '',
        },
      ],
    } as ServiceFormValues
  }
  return {
    ...service,
    containers: (service.containers ?? []).map((container) => ({
      ...container,
      runtimePortsText: formatPorts(container.runtimePorts),
    })),
  } as ServiceFormValues
}

function mapServicePayload(values: ServiceFormValues) {
  return {
    ...values,
    containers: (values.containers ?? []).map((container) => ({
      ...container,
      runtimePorts: parsePorts(container.runtimePortsText),
      runtimePortsText: undefined,
    })),
  }
}

type EditorValues = ServiceFormValues & {
  deployment: DeploymentFormValues
  sourceMode: 'build' | 'image' | 'configuration' | 'existing'
  buildMode: 'existing' | 'new' | 'shared'
  buildSource: BuildSource
  repositoryDraft: RepositoryDraft['input']
}

export function ServiceEditor({
  application,
  service,
  services,
  repositories,
  canManageBuild,
  closeGuardRef,
  onCancel,
  onSaved,
}: {
  application: DeliveryApplication
  service: ApplicationServiceComponent | null
  services: ApplicationServiceComponent[]
  repositories: DeliveryRepository[]
  canManageBuild: boolean
  closeGuardRef?: MutableRefObject<((proceed: () => void) => void) | null>
  onCancel: () => void
  onSaved: (serviceId: string) => void
}) {
  const { message, modal } = App.useApp()
  const [form] = Form.useForm<EditorValues>()
  const [current, setCurrent] = useState(0)
  const [repositoryTarget, setRepositoryTarget] = useState<number | null>(null)
  const [repositoryDrafts, setRepositoryDrafts] = useState<RepositoryDraft[]>([])
  const [submitted, setSubmitted] = useState<ServiceSetupDraft | null>(null)
  const checkpoint = useRef<ServiceSetupCheckpoint>({ repositories: {} })
  const savedService = useRef({ id: service?.id, version: service?.version })
  const originalBuildSource = useRef<BuildSource | undefined>(undefined)
  const saveInFlight = useRef(false)
  const [draftChanged, setDraftChanged] = useState(false)
  const [initialValues] = useState(() => ({
    ...serviceInitialValues(service),
    deployment: initialDeploymentValues(service),
    sourceMode: service?.buildSourceId
      ? 'build'
      : !service?.metadata?.sourceMode && (service?.repositoryId || service?.repositoryPath)
        ? 'existing'
        : service?.metadata?.sourceMode === 'configuration' ||
            (service && !service.containers?.length)
          ? 'configuration'
          : 'image',
    buildMode: 'existing',
    buildSource: {
      id: crypto.randomUUID(),
      name: '',
      type: 'repo_dockerfile',
      enabled: true,
      isDefault: false,
      buildImage: application.buildImage,
      defaultTag: application.defaultTag,
      config: {
        repositoryBindings: [{ repositoryId: '', checkoutPath: '.', defaultBranch: 'main' }],
        dockerfilePath: 'Dockerfile',
        contextDir: '.',
        builderKind: 'docker',
      },
    },
    repositoryDraft: { provider: 'gitlab', protocol: 'https', defaultBranch: 'main' },
  }))
  const sourceMode =
    Form.useWatch('sourceMode', { form, preserve: true }) ?? initialValues.sourceMode
  const buildMode = Form.useWatch('buildMode', { form, preserve: true }) ?? 'existing'
  const buildSourceId = Form.useWatch('buildSourceId', { form, preserve: true })
  const bindings =
    Form.useWatch(['buildSource', 'config', 'repositoryBindings'], { form, preserve: true }) ?? []
  const selectedSource = application.buildSources?.find((item) => item.id === buildSourceId)
  const sourceDraft = Form.useWatch('buildSource', { form, preserve: true }) as
    | BuildSource
    | undefined
  const containerValues = Form.useWatch('containers', {
    form,
    preserve: true,
  }) as ServiceFormValues['containers']
  const effectiveSource =
    sourceMode === 'build' ? (buildMode === 'existing' ? selectedSource : sourceDraft) : undefined
  const references = services.filter(
    (item) => item.id !== service?.id && item.buildSourceId === buildSourceId,
  )
  const allRepositories = [
    ...repositories,
    ...repositoryDrafts.map((item) => ({ ...item.input, id: item.draftId }) as DeliveryRepository),
  ]
  const queryClient = useQueryClient()
  const deployment = useServiceDeployment(form, application.id, service)
  const [presetsOpen, setPresetsOpen] = useState(false)
  const [applyingPreset, setApplyingPreset] = useState(false)
  const presets = useQuery(deliveryQueries.blueprints.list(presetsOpen && !service))
  const applyPreset = async (value: string) => {
    setDraftChanged(true)
    setApplyingPreset(true)
    try {
      const [blueprintId, key] = JSON.parse(value) as [string, string]
      const preset = serviceCreationPreset(
        await deliveryApi.blueprints.renderSpec(blueprintId),
        key,
      )
      if (preset.service.deploymentTemplate)
        await deployment.applyPreset(preset.service.deploymentTemplate)
      else form.setFieldValue('deployment', initialDeploymentValues())
      form.setFieldsValue({
        ...preset.service,
        sourceMode: preset.buildSource
          ? 'build'
          : preset.service.containers?.length
            ? 'image'
            : 'configuration',
        buildMode: preset.buildSource ? 'new' : 'existing',
        buildSourceId: undefined,
        containers: (preset.service.containers ?? []).map((container) => ({
          ...container,
          runtimePortsText: formatPorts(container.runtimePorts),
        })),
      })
      form.setFieldValue('buildSource', preset.buildSource ?? initialValues.buildSource)
      originalBuildSource.current = undefined
      setRepositoryDrafts([])
    } catch (error) {
      message.error(error instanceof Error ? error.message : '无法读取创建预设')
    } finally {
      setApplyingPreset(false)
    }
  }
  const [preparing, setPreparing] = useState(false)
  const saveMutation = useMutation({
    ...deliveryMutations.applications.saveServiceSetup(queryClient),
    onSuccess: (id) => {
      message.success(service ? '服务组件已更新' : '服务组件已创建')
      onSaved(id)
    },
  })
  useUnsavedDocument(
    draftChanged ||
      preparing ||
      applyingPreset ||
      saveMutation.isPending ||
      Boolean(submitted) ||
      repositoryDrafts.length > 0,
  )
  const submitDraft = (draft: ServiceSetupDraft) => {
    if (saveInFlight.current) return
    saveInFlight.current = true
    saveMutation.mutate(
      { draft, checkpoint: checkpoint.current },
      {
        onSettled: () => {
          saveInFlight.current = false
        },
      },
    )
  }
  const editBuild = (copy: boolean) => {
    if (!selectedSource) return
    setDraftChanged(true)
    originalBuildSource.current = copy ? undefined : structuredClone(selectedSource)
    const config = selectedSource.config ?? {}
    form.setFieldsValue({
      buildMode: copy ? 'new' : 'shared',
      buildSource: {
        ...selectedSource,
        id: copy ? crypto.randomUUID() : selectedSource.id,
        name: copy ? `${selectedSource.name} 副本` : selectedSource.name,
        isDefault: copy ? false : selectedSource.isDefault,
        config: {
          ...config,
          repositoryBindings: config.repositoryBindings?.length
            ? config.repositoryBindings
            : config.repositoryId
              ? [{ repositoryId: config.repositoryId }]
              : [{ repositoryId: '', checkoutPath: '.' }],
        },
      },
    })
  }
  const addRepositoryDraft = async () => {
    await form.validateFields([['repositoryDraft']], { recursive: true })
    setDraftChanged(true)
    const input = form.getFieldValue('repositoryDraft') as RepositoryDraft['input']
    const draftId = `draft:${crypto.randomUUID()}`
    setRepositoryDrafts((items) => [...items, { draftId, input: { ...input } }])
    const index = repositoryTarget ?? 0
    form.setFieldValue(
      ['buildSource', 'config', 'repositoryBindings', index, 'repositoryId'],
      draftId,
    )
    form.setFieldValue(
      ['buildSource', 'config', 'repositoryBindings', index, 'defaultBranch'],
      input.defaultBranch,
    )
    setRepositoryTarget(null)
    form.setFieldValue('repositoryDraft', undefined)
  }
  const save = async () => {
    if (preparing || saveInFlight.current) return
    const values = form.getFieldsValue(true) as EditorValues
    if (repositoryTarget !== null) {
      setCurrent(0)
      message.warning('请先将仓库加入草稿')
      return
    }
    const {
      sourceMode,
      buildMode,
      buildSource,
      repositoryDraft: _repositoryDraft,
      deployment: _deployment,
      ...serviceValues
    } = values
    const source =
      sourceMode === 'build'
        ? buildMode === 'existing'
          ? application.buildSources?.find((item) => item.id === values.buildSourceId)
          : buildSource
        : undefined
    if (sourceMode === 'build' && !source) {
      setCurrent(0)
      message.error('所选构建定义已不存在，请重新选择')
      return
    }
    const binding = buildSourceRepositoryBindings(source)[0]
    const repository = allRepositories.find((item) => item.id === binding?.repositoryId)
    setPreparing(true)
    try {
      const deploymentDraft = await deployment.prepare(String(values.key))
      const draft: ServiceSetupDraft = {
        applicationId: application.id,
        serviceId: savedService.current.id,
        deployment: deploymentDraft.setup,
        service: {
          ...mapServicePayload({ ...serviceInitialValues(service), ...serviceValues }),
          expectedVersion: savedService.current.version,
          ...(deploymentDraft.reference ? { deploymentTemplate: deploymentDraft.reference } : {}),
          ...(deploymentDraft.setup?.helm ? { serviceKind: 'helm_release' } : {}),
          ...(deploymentDraft.setup?.docker ? { serviceKind: 'external_service' } : {}),
          ...(sourceMode === 'existing'
            ? {}
            : {
                buildSourceId: source?.id || '',
                repositoryId: repository?.id || '',
                repositoryProvider: repository?.provider || '',
                repositoryProjectId: repository?.gitlabProjectId || '',
                repositoryPath: repository?.path || '',
                defaultBranch: binding?.defaultBranch || repository?.defaultBranch || '',
                metadata: { ...service?.metadata, sourceMode },
              }),
        },
        buildSource: sourceMode === 'build' && buildMode !== 'existing' ? source : undefined,
        originalBuildSource: buildMode === 'shared' ? originalBuildSource.current : undefined,
        repositories: repositoryDrafts,
      }
      setSubmitted(draft)
      submitDraft(draft)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '部署配置校验失败')
    } finally {
      setPreparing(false)
    }
  }
  const buildFields = (section: 'repositories' | 'build') => (
    <BuildSourceFields
      applicationId={application.id}
      form={form}
      prefix={['buildSource']}
      repositories={allRepositories}
      section={section}
      onConnectRepository={(index) => {
        form.setFieldValue('repositoryDraft', {
          provider: 'gitlab',
          protocol: 'https',
          defaultBranch: 'main',
        })
        setRepositoryTarget(index)
        setCurrent(0)
      }}
    />
  )
  const requestClose = (proceed: () => void) => {
    if (saveInFlight.current || saveMutation.isPending || preparing || applyingPreset) {
      message.warning('正在处理服务配置，请稍候再关闭')
      return
    }
    if (submitted) {
      modal.confirm({
        title: '关闭服务配置？',
        content: '已保存的仓库、构建定义和服务配置将保留，尚未完成的配置不会继续保存。',
        okText: '关闭，保留已保存资源',
        cancelText: '继续配置',
        onOk: proceed,
      })
      return
    }
    if (draftChanged || repositoryDrafts.length) {
      modal.confirm({
        title: '放弃未保存的服务配置？',
        content: '离开后，本次尚未保存的修改将丢失。',
        okText: '放弃修改',
        cancelText: '继续编辑',
        onOk: proceed,
      })
      return
    }
    proceed()
  }
  useEffect(() => {
    if (!closeGuardRef) return
    closeGuardRef.current = requestClose
    return () => {
      closeGuardRef.current = null
    }
  })
  // Keep validation and recovery checkpoints on their original step indices.
  const visibleStepIndices = sourceMode === 'build' ? [0, 1, 2, 3] : [0, 2, 3]
  return (
    <div className="soha-application-service-editor">
      {submitted ? (
        <Space orientation="vertical" className="soha-application-service-editor__receipt">
          <Alert
            type={saveMutation.isError ? 'error' : 'info'}
            showIcon
            title={
              saveMutation.isError
                ? Object.keys(checkpoint.current.repositories).length ||
                  checkpoint.current.buildSourceSaved
                  ? '部分配置已保存'
                  : '保存尚未完成'
                : '正在保存服务'
            }
            description={saveMutation.error?.message || '正在保存仓库、构建定义、服务与部署配置。'}
          />
          <Text>
            服务：{String(submitted.service.name)}。已接入仓库：
            {Object.keys(checkpoint.current.repositories).length}；构建定义：
            {checkpoint.current.buildSourceSaved
              ? '已保存'
              : submitted.buildSource
                ? '待保存'
                : '复用现有配置'}
            。
          </Text>
          {saveMutation.isError ? (
            <Space wrap>
              <Button
                type="primary"
                onClick={() => submitDraft(submitted)}
                disabled={isApiError(saveMutation.error) && saveMutation.error.status === 409}
              >
                继续保存
              </Button>
              {submitted.originalBuildSource &&
              isApiError(saveMutation.error) &&
              saveMutation.error.status === 409 ? (
                <Button
                  onClick={() => {
                    editBuild(false)
                    checkpoint.current.buildSourceSaved = false
                    setSubmitted(null)
                    saveMutation.reset()
                    setCurrent(1)
                  }}
                >
                  重新加载共享构建
                </Button>
              ) : null}
              <Button
                disabled={Boolean(
                  checkpoint.current.pendingRepository ||
                  checkpoint.current.pendingService ||
                  checkpoint.current.pendingServiceUpdate ||
                  checkpoint.current.pendingManifestPackage,
                )}
                onClick={() => {
                  if (checkpoint.current.serviceId)
                    savedService.current = {
                      id: checkpoint.current.serviceId,
                      version: checkpoint.current.serviceVersion,
                    }
                  if (checkpoint.current.manifestPackage)
                    deployment.packageSnapshot.current = checkpoint.current.manifestPackage
                  if (checkpoint.current.buildSourceSaved && submitted.buildSource) {
                    originalBuildSource.current = submitted.buildSource
                    form.setFieldValue('buildMode', 'shared')
                  }
                  checkpoint.current = { repositories: checkpoint.current.repositories }
                  setSubmitted(null)
                  saveMutation.reset()
                }}
              >
                返回编辑
              </Button>
              <Button onClick={() => requestClose(onCancel)}>关闭，保留已保存资源</Button>
            </Space>
          ) : null}
        </Space>
      ) : null}
      <div className="soha-application-service-editor__form" hidden={Boolean(submitted)}>
        <StepForm
          form={form}
          initialValues={initialValues}
          current={Math.max(0, visibleStepIndices.indexOf(current))}
          onValuesChange={() => {
            setDraftChanged(true)
          }}
          onCurrentChange={(visibleIndex) => {
            const next = visibleStepIndices[visibleIndex]
            if (next === 2 && sourceMode === 'build') {
              const source =
                buildMode === 'existing'
                  ? selectedSource
                  : (form.getFieldValue('buildSource') as BuildSource)
              for (const [field, value] of [
                ['imageRepository', source?.buildImage],
                ['defaultTagTemplate', source?.defaultTag],
              ]) {
                const name = ['containers', 0, field as string]
                if (form.getFieldValue(['containers', 0]) && !form.getFieldValue(name))
                  form.setFieldValue(name, value)
              }
            }
            setCurrent(next)
          }}
          onCancel={() => requestClose(onCancel)}
          onFinish={() => void save()}
          contentMaxWidth="100%"
          submitText={service ? '保存服务' : '创建服务'}
          loading={preparing || saveMutation.isPending}
          onFinishFailed={({ errorFields }) => {
            const name = errorFields[0]?.name
            setCurrent(
              name?.[0] === 'deployment'
                ? name?.[1] === 'environmentIds' || name?.[1] === 'overrides'
                  ? 3
                  : 2
                : name?.[0] === 'containers'
                  ? 2
                  : name?.[0] === 'buildSource' && name?.[2] !== 'repositoryBindings'
                    ? 1
                    : 0,
            )
          }}
          steps={[
            {
              title: '服务来源',
              fieldNames: [
                'key',
                'name',
                'buildSourceId',
                ...bindings.map((_: unknown, index: number) => [
                  'buildSource',
                  'config',
                  'repositoryBindings',
                  index,
                  'repositoryId',
                ]),
              ],
              children: (
                <>
                  {!service && !savedService.current.id ? (
                    <Form.Item label="创建预设" extra={presets.error?.message}>
                      <Select<string>
                        aria-label="创建预设"
                        placeholder="可选，组合构建与部署模板"
                        value={undefined}
                        loading={presets.isLoading || applyingPreset}
                        disabled={applyingPreset}
                        onOpenChange={setPresetsOpen}
                        showSearch={{ optionFilterProp: 'label' }}
                        options={(presets.data ?? [])
                          .filter((item) => item.enabled)
                          .flatMap((item) =>
                            (item.services ?? []).map((entry) => ({
                              value: JSON.stringify([item.id, entry.key]),
                              label: `${item.name} / ${entry.name}`,
                            })),
                          )}
                        onSelect={(value) => {
                          if (form.isFieldsTouched())
                            modal.confirm({
                              title: '替换当前草稿为预设配置？',
                              onOk: () => applyPreset(value),
                            })
                          else void applyPreset(value)
                        }}
                      />
                    </Form.Item>
                  ) : null}
                  <Form.Item name="sourceMode" label="服务来源">
                    <Radio.Group
                      options={[
                        ...(initialValues.sourceMode === 'existing'
                          ? [{ value: 'existing', label: '沿用现有来源' }]
                          : []),
                        { value: 'build', label: '从源码构建' },
                        { value: 'image', label: '已有镜像' },
                        { value: 'configuration', label: '仅配置服务' },
                      ]}
                    />
                  </Form.Item>
                  <section className="soha-application-service-editor-section">
                    <div className="soha-application-service-editor-section__head">
                      <Text strong>基础信息</Text>
                      <Text type="secondary">定义服务单元及交付类型</Text>
                    </div>
                    <div className="soha-application-service-form-grid">
                      <Form.Item
                        name="key"
                        label="服务 Key"
                        rules={[{ required: true, message: '请输入服务 Key' }]}
                      >
                        <Input placeholder="api" />
                      </Form.Item>
                      <Form.Item
                        name="name"
                        label="服务名称"
                        rules={[{ required: true, message: '请输入服务名称' }]}
                      >
                        <Input placeholder="API 服务" />
                      </Form.Item>
                      <Form.Item name="serviceKind" label="服务类型">
                        <Select options={SERVICE_KIND_OPTIONS} />
                      </Form.Item>
                    </div>
                    <Collapse
                      ghost
                      size="small"
                      className="soha-application-service-editor__advanced"
                      items={[
                        {
                          key: 'metadata',
                          label: '更多设置（可选）',
                          forceRender: true,
                          children: (
                            <>
                              <div className="soha-application-service-form-grid">
                                <Form.Item name="ownerTeam" label="负责人团队">
                                  <Input />
                                </Form.Item>
                                <Form.Item name="enabled" label="启用" valuePropName="checked">
                                  <Switch />
                                </Form.Item>
                              </div>
                              <Form.Item name="description" label="描述">
                                <Input.TextArea rows={2} />
                              </Form.Item>
                            </>
                          ),
                        },
                      ]}
                    />
                  </section>
                  {sourceMode === 'build' ? (
                    <>
                      <Form.Item name="buildMode" label="构建定义">
                        <Radio.Group
                          onChange={(event) => {
                            if (event.target.value === 'new')
                              form.setFieldValue('buildSource', {
                                ...form.getFieldValue('buildSource'),
                                id: crypto.randomUUID(),
                                isDefault: false,
                                name:
                                  form.getFieldValue(['buildSource', 'name']) ||
                                  `${form.getFieldValue('name') || '服务'} 构建`,
                              })
                          }}
                          options={[
                            { value: 'existing', label: '复用已有构建' },
                            ...(canManageBuild ? [{ value: 'new', label: '新建构建' }] : []),
                            ...(buildMode === 'shared'
                              ? [{ value: 'shared', label: '编辑共享构建' }]
                              : []),
                          ]}
                        />
                      </Form.Item>
                      {buildMode === 'existing' ? (
                        <>
                          <Form.Item
                            name="buildSourceId"
                            label="构建来源"
                            rules={[{ required: true, message: '请选择构建定义' }]}
                          >
                            <Select
                              options={(application.buildSources ?? []).map((item) => ({
                                value: item.id,
                                label: item.name,
                                disabled: item.enabled === false,
                              }))}
                            />
                          </Form.Item>
                          {selectedSource ? (
                            <Space orientation="vertical">
                              <Text>
                                仓库：
                                {buildSourceRepositoryBindings(selectedSource)
                                  .map(
                                    (item) =>
                                      allRepositories.find(
                                        (repository) => repository.id === item.repositoryId,
                                      )?.name || item.repositoryId,
                                  )
                                  .filter(Boolean)
                                  .join('、') || '由外部流水线提供'}
                              </Text>
                              {canManageBuild ? (
                                <Space>
                                  <Button onClick={() => editBuild(true)}>复制为此服务构建</Button>
                                  <Button onClick={() => editBuild(false)}>编辑共享构建</Button>
                                </Space>
                              ) : null}
                            </Space>
                          ) : null}
                        </>
                      ) : (
                        <>
                          {buildMode === 'shared' ? (
                            <Alert
                              type="warning"
                              showIcon
                              title={`保存将修改共享构建，另有 ${references.length} 个服务引用${references.length ? `：${references.map((item) => item.name).join('、')}` : ''}。`}
                            />
                          ) : null}
                          {buildFields('repositories')}
                          {repositoryTarget !== null ? (
                            <Card
                              size="small"
                              title="接入仓库草稿"
                              className="soha-service-repository-draft"
                              classNames={{ body: 'soha-service-repository-draft__body' }}
                            >
                              <RepositoryFields form={form} prefix={['repositoryDraft']} />
                              <Space>
                                <Button
                                  type="primary"
                                  onClick={() => void addRepositoryDraft().catch(() => undefined)}
                                >
                                  加入草稿
                                </Button>
                                <Button onClick={() => setRepositoryTarget(null)}>取消接入</Button>
                              </Space>
                            </Card>
                          ) : null}
                        </>
                      )}
                    </>
                  ) : (
                    <Text type="secondary">
                      {sourceMode === 'image'
                        ? '直接使用容器镜像，保存时清除源码与构建关联。'
                        : sourceMode === 'existing'
                          ? `保留已有来源关联：${service?.repositoryPath || service?.repositoryId}`
                          : '继续选择部署模板与环境，或保留为仅配置服务。'}
                    </Text>
                  )}
                </>
              ),
            },
            {
              title: '构建配置',
              fieldNames: [
                ['buildSource', 'name'],
                ['buildSource', 'type'],
                ['buildSource', 'buildImage'],
                ['buildSource', 'config', 'buildTemplateId'],
                ['buildSource', 'config', 'buildTemplateVersion'],
                ['buildSource', 'config', 'externalPipeline', 'provider'],
                ['buildSource', 'config', 'externalPipeline', 'pipelineTag'],
                ['buildSource', 'config', 'externalPipeline', 'artifactJob'],
                ['buildSource', 'config', 'externalPipeline', 'registryId'],
              ],
              children:
                sourceMode === 'build' ? (
                  buildMode === 'existing' ? (
                    <Text>
                      复用构建：{selectedSource?.name || '尚未选择'}。构建执行参数沿用共享定义。
                    </Text>
                  ) : (
                    buildFields('build')
                  )
                ) : (
                  <Text>此服务无需构建。继续配置容器或保存服务档案。</Text>
                ),
            },
            {
              title: '产物与部署',
              fieldNames: [
                ['deployment', 'templateId'],
                ['deployment', 'version'],
                ['deployment', 'parameters'],
              ],
              children: (
                <>
                  <div className="soha-application-service-editor-summary">
                    <Text strong>
                      {form.getFieldValue('name') || '服务'} · {form.getFieldValue('key')}
                    </Text>
                    <Text>
                      {sourceMode === 'build'
                        ? `构建：${effectiveSource?.name || '未选择'}${buildMode === 'shared' ? '（修改共享定义）' : buildMode === 'existing' ? '（复用）' : '（新建）'}`
                        : sourceMode === 'image'
                          ? '来源：已有镜像'
                          : sourceMode === 'existing'
                            ? '来源：沿用现有配置'
                            : '来源：仅配置服务'}
                    </Text>
                    {effectiveSource ? (
                      <>
                        <Text>
                          仓库：
                          {buildSourceRepositoryBindings(effectiveSource)
                            .map(
                              (binding) =>
                                allRepositories.find((item) => item.id === binding.repositoryId)
                                  ?.name || binding.repositoryId,
                            )
                            .join('、') || '由外部流水线提供'}
                        </Text>
                        <Text>
                          默认产物：
                          {effectiveSource.buildImage || application.buildImage || '未配置'}
                          {effectiveSource.defaultTag ? `:${effectiveSource.defaultTag}` : ''}
                        </Text>
                      </>
                    ) : null}
                  </div>
                  <section className="soha-application-service-editor-section">
                    <Form.List name="containers">
                      {(fields, { add, remove }) => (
                        <div className="soha-application-service-containers-editor">
                          <div className="soha-application-service-containers-editor__head">
                            <span>
                              <Text strong>产物容器</Text>
                              <Text type="secondary"> · 定义镜像、构建文件和运行端口</Text>
                            </span>
                            <Button
                              size="small"
                              icon={<PlusOutlined />}
                              onClick={() => add({ name: 'main' })}
                            >
                              添加容器
                            </Button>
                          </div>
                          {fields.map((field) => (
                            <Card
                              key={field.key}
                              size="small"
                              className="soha-application-service-container-editor"
                            >
                              <div className="soha-application-service-container-editor__grid">
                                <Form.Item
                                  name={[field.name, 'name']}
                                  label="容器名"
                                  rules={[{ required: true, message: '请输入容器名' }]}
                                >
                                  <Input placeholder="main" />
                                </Form.Item>
                                <Form.Item
                                  name={[field.name, 'imageRepository']}
                                  label="产物镜像仓库"
                                  extra={
                                    effectiveSource?.buildImage
                                      ? containerValues?.[field.name]?.imageRepository ===
                                        effectiveSource.buildImage
                                        ? '沿用构建定义输出'
                                        : '覆盖构建默认输出'
                                      : undefined
                                  }
                                  rules={
                                    sourceMode === 'image'
                                      ? [{ required: true, message: '请输入镜像仓库' }]
                                      : []
                                  }
                                >
                                  <Input placeholder="registry.example.com/team/api" />
                                </Form.Item>
                                <Form.Item
                                  name={[field.name, 'defaultTagTemplate']}
                                  label={sourceMode === 'image' ? '镜像 Tag' : 'Tag 模板'}
                                >
                                  <Input
                                    placeholder={
                                      sourceMode === 'image' ? 'stable' : '{{branch}}-{{sha}}'
                                    }
                                  />
                                </Form.Item>
                                <Form.Item
                                  name={[field.name, 'runtimePortsText']}
                                  label="端口"
                                  rules={[
                                    {
                                      validator: (_, value: string) =>
                                        !value ||
                                        value
                                          .split(',')
                                          .every(
                                            (part) =>
                                              /^\d+$/.test(part.trim()) &&
                                              Number(part.trim()) > 0 &&
                                              Number(part.trim()) <= 65535,
                                          )
                                          ? Promise.resolve()
                                          : Promise.reject(
                                              new Error(
                                                '请输入 1–65535 的端口，多个端口用逗号分隔',
                                              ),
                                            ),
                                    },
                                  ]}
                                >
                                  <Input placeholder="8080, 9090" />
                                </Form.Item>
                                {sourceMode === 'build' ? (
                                  <>
                                    <Form.Item
                                      name={[field.name, 'dockerfilePath']}
                                      label="Dockerfile"
                                    >
                                      <Input placeholder="Dockerfile" />
                                    </Form.Item>
                                    <Form.Item
                                      name={[field.name, 'buildContextDir']}
                                      label="构建上下文"
                                    >
                                      <Input placeholder="." />
                                    </Form.Item>
                                  </>
                                ) : null}
                              </div>
                              <ManagementIconButton
                                aria-label="移除容器"
                                danger
                                icon={<MinusCircleOutlined />}
                                size="small"
                                tooltip="移除容器"
                                onClick={() => remove(field.name)}
                              />
                            </Card>
                          ))}
                        </div>
                      )}
                    </Form.List>
                  </section>
                  <DeploymentTemplateFields state={deployment} />
                </>
              ),
            },
            {
              title: '环境关联',
              fieldNames: [
                ['deployment', 'environmentIds'],
                ['deployment', 'overrides'],
              ],
              children: <DeploymentEnvironmentFields state={deployment} />,
            },
          ].filter((_, index) => visibleStepIndices.includes(index))}
        />
      </div>
    </div>
  )
}
