import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Radio,
  Select,
  Space,
  Typography,
} from 'antd'
import type { FormInstance } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { deliveryApi } from '../api'
import {
  DockerDeliveryFields,
  dockerDeliveryForm,
  parseDockerDelivery,
  type DockerDeliveryForm,
} from '../docker-delivery-fields'
import { dockerApi } from '@/features/docker'
import { deliveryQueries } from '../queries'
import { TemplateUpdateEntry } from '../template-updates/entry'
import { manifestQueries } from '../manifests/queries'
import type { ManifestPackage } from '../manifests/types'
import type {
  DeploymentTemplateHelmSource,
  HelmDeliveryConfiguration,
} from '@opensoha/contracts/gen/ts/sohaapi'
import {
  HelmEnvironmentFields,
  HelmSourceFields,
  parseHelmConfiguration,
  parseHelmSource,
} from '../helm-fields'
import {
  deploymentReferenceSignature,
  type ServiceDeploymentSetup,
} from '../service-deployment-setup'
import type {
  ApplicationServiceComponent,
  ServiceDeploymentTemplate,
  TemplateParameterSchema,
  TemplateParameterValues,
} from '../types'

const DiffEditor = lazy(() =>
  import('@/components/yaml-draft-diff-editor').then((module) => ({
    default: module.YamlDraftDiffEditor,
  })),
)
const { Text } = Typography

export type DeploymentFormValues = {
  mode: 'existing' | 'template' | 'independent' | 'helm' | 'docker'
  templateId?: string
  version?: number
  parameters?: Record<string, unknown>
  environmentIds?: string[]
  overrides?: Record<string, Record<string, unknown>>
  overrideKeys?: Record<string, string[]>
  acceptedVersion?: string
  helm?: Record<string, Omit<HelmDeliveryConfiguration, 'source'>>
  helmSource?: DeploymentTemplateHelmSource
  docker?: Record<string, DockerDeliveryForm>
}

export function deploymentParameterFields(
  schema: TemplateParameterSchema,
  values: TemplateParameterValues,
) {
  return Object.fromEntries(
    Object.entries(values).map(([key, value]) => [
      key,
      ['array', 'object'].includes(schema.properties?.[key]?.type ?? '')
        ? JSON.stringify(value, null, 2)
        : value,
    ]),
  )
}

export function parseDeploymentParameters(
  schema: TemplateParameterSchema,
  values: Record<string, unknown> = {},
  keys = Object.keys(values),
): TemplateParameterValues {
  return Object.fromEntries(
    keys.flatMap((key) => {
      let value = values[key]
      if (value === undefined || value === null) return []
      const field = schema.properties?.[key]
      if (!field) throw new Error(`未知部署参数：${key}`)
      if (['object', 'array'].includes(field.type) && typeof value === 'string') {
        try {
          value = JSON.parse(value)
        } catch {
          throw new Error(`${key} 需要有效的 JSON`)
        }
      }
      return [[key, value]]
    }),
  ) as TemplateParameterValues
}

export function initialDeploymentValues(
  service?: ApplicationServiceComponent | null,
): DeploymentFormValues {
  const binding = service?.deploymentTemplate
  return {
    mode:
      binding && !binding.detached
        ? 'template'
        : service?.serviceKind === 'helm_release'
          ? 'helm'
          : 'existing',
    templateId: binding?.templateId,
    version: binding?.version,
    environmentIds: [],
    overrides: {},
    overrideKeys: {},
  }
}

export function useServiceDeployment(
  form: FormInstance,
  applicationId: string,
  service?: ApplicationServiceComponent | null,
) {
  const client = useQueryClient()
  const values = Form.useWatch('deployment', { form, preserve: true }) as
    | DeploymentFormValues
    | undefined
  const enabled = values?.mode === 'template'
  const templateId = values?.templateId || ''
  const version = values?.version || 0
  const templatesQuery = useQuery(deliveryQueries.deploymentTemplates.list(enabled))
  const versionsQuery = useQuery(deliveryQueries.deploymentTemplates.versions(templateId, enabled))
  const templateQuery = useQuery(
    deliveryQueries.deploymentTemplates.version(templateId, version, enabled),
  )
  const environmentsQuery = useQuery(
    deliveryQueries.environments.list(
      enabled || values?.mode === 'helm' || values?.mode === 'docker',
    ),
  )
  const original = service?.deploymentTemplate
  const oldTemplateQuery = useQuery(
    deliveryQueries.deploymentTemplates.version(
      original?.templateId || '',
      original?.version || 0,
      enabled && Boolean(original),
    ),
  )
  const packageQuery = useQuery(
    manifestQueries.detail(
      original?.manifestPackageId || '',
      enabled && Boolean(original?.manifestPackageId),
    ),
  )
  const packageSnapshot = useRef<ManifestPackage | undefined>(undefined)
  const environmentSnapshot = useRef<typeof environmentsQuery.data>(undefined)
  const helmEnvironmentSnapshot = useRef<typeof environmentsQuery.data>(undefined)
  const dockerEnvironmentSnapshot = useRef<typeof environmentsQuery.data>(undefined)
  const initialHelmSource = useRef<DeploymentTemplateHelmSource | undefined>(undefined)
  const initializedTemplate = useRef('')
  const previousTemplate = useRef<ServiceDeploymentTemplate | undefined>(undefined)
  const template = templateQuery.data
  const selection = `${templateId}:${version}`
  useEffect(() => {
    if (values?.mode !== 'docker' || !environmentsQuery.data || dockerEnvironmentSnapshot.current)
      return
    dockerEnvironmentSnapshot.current = structuredClone(
      environmentsQuery.data.filter((item) => item.applicationId === applicationId),
    )
    const existing = dockerEnvironmentSnapshot.current.flatMap((environment) =>
      (environment.targets ?? [])
        .filter(
          (target) =>
            service?.id &&
            target.executorKind === 'docker_compose' &&
            target.metadata?.serviceId === service.id &&
            target.docker,
        )
        .map((target) => ({ environment, config: target.docker! })),
    )
    form.setFieldsValue({
      deployment: {
        environmentIds: existing.map((item) => item.environment.id),
        docker: Object.fromEntries(
          existing.map((item) => [item.environment.id, dockerDeliveryForm(item.config)]),
        ),
      },
    })
  }, [applicationId, environmentsQuery.data, form, service?.id, values?.mode])
  useEffect(() => {
    if (environmentsQuery.data && !environmentSnapshot.current)
      environmentSnapshot.current = structuredClone(environmentsQuery.data)
  }, [environmentsQuery.data])
  useEffect(() => {
    if (
      (template?.source.renderer !== 'helm' && values?.mode !== 'helm') ||
      !environmentsQuery.data ||
      helmEnvironmentSnapshot.current
    )
      return
    helmEnvironmentSnapshot.current = structuredClone(
      environmentsQuery.data.filter((item) => item.applicationId === applicationId),
    )
    const existing = helmEnvironmentSnapshot.current.flatMap((environment) =>
      (environment.targets ?? [])
        .filter(
          (target) =>
            service?.id &&
            target.executorKind === 'helm_sdk' &&
            target.metadata?.serviceId === service.id,
        )
        .map((target) => ({ environment, target })),
    )
    if (existing.length) {
      initialHelmSource.current = existing[0].target.helm?.source
      form.setFieldValue(['deployment', 'helmSource'], existing[0].target.helm?.source)
      form.setFieldValue(
        ['deployment', 'environmentIds'],
        [...new Set(existing.map(({ environment }) => environment.id))],
      )
      form.setFieldValue(
        ['deployment', 'helm'],
        Object.fromEntries(
          existing.map(({ environment, target }) => [environment.id, target.helm]),
        ),
      )
      const overrides = Object.fromEntries(
        existing.map(({ environment, target }) => [
          environment.id,
          target.metadata?.templateParameters ?? {},
        ]),
      )
      form.setFieldValue(
        ['deployment', 'overrides'],
        Object.fromEntries(
          Object.entries(overrides).map(([id, parameters]) => [
            id,
            deploymentParameterFields(
              template?.parameterSchema ?? { type: 'object', properties: {} },
              parameters as TemplateParameterValues,
            ),
          ]),
        ),
      )
      form.setFieldValue(
        ['deployment', 'overrideKeys'],
        Object.fromEntries(
          Object.entries(overrides).map(([id, parameters]) => [
            id,
            Object.keys(parameters as object),
          ]),
        ),
      )
    }
  }, [applicationId, environmentsQuery.data, form, service?.id, template, values?.mode])
  useEffect(() => {
    if (!template || initializedTemplate.current === selection) return
    const initial =
      initializedTemplate.current === '' &&
      original?.templateId === templateId &&
      original?.version === version
    const parameters = initial
      ? original.parameters
      : Object.fromEntries(
          Object.entries(form.getFieldValue(['deployment', 'parameters']) ?? {}).filter(
            ([key]) =>
              template.parameterSchema.properties?.[key]?.type ===
              previousTemplate.current?.parameterSchema.properties?.[key]?.type,
          ),
        )
    form.setFieldValue(['deployment', 'parameters'], {
      ...deploymentParameterFields(template.parameterSchema, template.defaults),
      ...(initial
        ? deploymentParameterFields(template.parameterSchema, original?.parameters ?? {})
        : parameters),
    })
    initializedTemplate.current = selection
    previousTemplate.current = template
  }, [form, original, selection, template, templateId, version])
  useEffect(() => {
    if (!packageQuery.data || packageSnapshot.current || !oldTemplateQuery.data) return
    const item = packageQuery.data
    packageSnapshot.current = structuredClone(item)
    form.setFieldValue(
      ['deployment', 'environmentIds'],
      item.bindings.map((binding) => binding.applicationEnvironmentId),
    )
    form.setFieldValue(
      ['deployment', 'overrides'],
      Object.fromEntries(
        item.bindings.map((binding) => [
          binding.applicationEnvironmentId,
          deploymentParameterFields(
            oldTemplateQuery.data.parameterSchema,
            binding.templateParameters ?? {},
          ),
        ]),
      ),
    )
    form.setFieldValue(
      ['deployment', 'overrideKeys'],
      Object.fromEntries(
        item.bindings.map((binding) => [
          binding.applicationEnvironmentId,
          Object.keys(binding.templateParameters ?? {}),
        ]),
      ),
    )
  }, [form, oldTemplateQuery.data, packageQuery.data])

  const prepare = async (
    serviceKey: string,
  ): Promise<{
    reference?: ApplicationServiceComponent['deploymentTemplate']
    setup?: ServiceDeploymentSetup
  }> => {
    const current = form.getFieldValue('deployment') as DeploymentFormValues
    if (current.mode === 'independent') {
      if (!original) throw new Error('请先保存部署配置，再复制为独立配置')
      return { reference: { ...original, detached: true } }
    }
    if (current.mode === 'docker') {
      if (original || service?.serviceKind === 'helm_release')
        throw new Error('已有模板或 Helm 服务不能直接切换为 Docker，请新建服务')
      if (!dockerEnvironmentSnapshot.current) throw new Error('环境配置尚未加载')
      const environments = await Promise.all(
        (current.environmentIds ?? []).map(async (id) => {
          const environment = dockerEnvironmentSnapshot.current!.find(
            (item) => item.id === id && item.applicationId === applicationId,
          )
          if (!environment) throw new Error('所选环境已不存在，请重新加载')
          const docker = parseDockerDelivery(current.docker?.[id])
          const project = await dockerApi.project(docker.projectId)
          if (project.hostId !== docker.hostId)
            throw new Error('Docker 项目已移动到其他主机，请重新选择')
          return { environment, parameters: {}, docker }
        }),
      )
      if (!environments.length) throw new Error('请至少关联一个 Docker 部署环境')
      return {
        setup: {
          docker: true,
          environments,
          originalEnvironments: dockerEnvironmentSnapshot.current.filter((item) =>
            item.targets?.some(
              (target) =>
                target.executorKind === 'docker_compose' &&
                target.metadata?.serviceId === service?.id,
            ),
          ),
        },
      }
    }
    if (current.mode !== 'template' && current.mode !== 'helm') return {}
    if (current.mode === 'template' && (!current.templateId || !current.version))
      throw new Error('请选择部署模板及已发布版本')
    const selected =
      current.mode === 'template'
        ? await client.fetchQuery(
            deliveryQueries.deploymentTemplates.version(current.templateId!, current.version!),
          )
        : undefined
    if (current.mode === 'template' && !selected) throw new Error('部署模板版本不存在，请重新选择')
    const isHelm = current.mode === 'helm' || selected?.source.renderer === 'helm'
    if (!isHelm && (selected?.source.git || !selected?.source.files?.length))
      throw new Error('此部署来源尚未接入服务创建，请先使用清单文件模板。')
    if (isHelm && original?.manifestPackageId)
      throw new Error('已有清单配置不能直接切换为 Helm，请新建 Helm 服务。')
    if (!isHelm && service?.serviceKind === 'helm_release')
      throw new Error('已有 Helm 服务不能直接切换为清单配置，请新建服务。')
    if (
      selected &&
      original &&
      (original.templateId !== selected.id || original.version !== current.version) &&
      current.acceptedVersion !== `${selected.id}:${current.version}`
    )
      throw new Error('请先查看并确认模板版本差异')
    if (original?.manifestPackageId && !packageSnapshot.current)
      throw new Error('原部署配置尚未加载，不能覆盖保存')
    const parameters = selected
      ? parseDeploymentParameters(selected.parameterSchema, current.parameters)
      : {}
    const environments = await client.fetchQuery(deliveryQueries.environments.list())
    const targets: ServiceDeploymentSetup['environments'] = (current.environmentIds ?? []).map(
      (id) => {
        const environment = (
          isHelm ? (helmEnvironmentSnapshot.current ?? environments) : environments
        ).find((item) => item.id === id && item.applicationId === applicationId)
        if (!environment?.clusterId || !environment.namespace)
          throw new Error('所选环境缺少集群或命名空间，请先完成环境设置')
        return {
          environment,
          parameters: selected
            ? parseDeploymentParameters(
                selected.parameterSchema,
                current.overrides?.[id],
                current.overrideKeys?.[id] ?? [],
              )
            : {},
        }
      },
    )
    if (isHelm && !targets.length) throw new Error('请至少关联一个 Helm 部署环境')
    for (const target of targets.length ? targets : [undefined]) {
      const preview = selected
        ? await deliveryApi.deploymentTemplates.preview(applicationId, {
            templateId: selected.id,
            version: current.version!,
            serviceKey,
            parameters,
            applicationEnvironmentId: target?.environment.id,
            overrides: target?.parameters ?? {},
          })
        : undefined
      if (isHelm && target) {
        const existing = service?.id
          ? target.environment.targets?.filter(
              (item) => item.executorKind === 'helm_sdk' && item.metadata?.serviceId === service.id,
            )
          : []
        if ((existing?.length ?? 0) > 1)
          throw new Error('此服务在同一环境关联了多个 Helm Release，请先整理环境关联')
        const retainedSource =
          initialHelmSource.current &&
          current.helmSource &&
          deploymentReferenceSignature(parseHelmSource(current.helmSource)) ===
            deploymentReferenceSignature(parseHelmSource(initialHelmSource.current))
            ? existing?.[0]?.helm?.source
            : undefined
        const source = selected ? preview?.source.helm : (retainedSource ?? current.helmSource)
        if (!source) throw new Error('请填写 Chart 来源')
        target.helm = parseHelmConfiguration({
          releaseName: serviceKey,
          timeoutSeconds: selected?.health.timeoutSeconds || 300,
          ...current.helm?.[target.environment.id],
          source: parseHelmSource(source),
        })
      }
    }
    return {
      reference: selected
        ? {
            templateId: selected.id,
            version: current.version!,
            parameters,
            manifestPackageId: original?.manifestPackageId,
          }
        : original
          ? { ...original, detached: true }
          : undefined,
      setup: {
        template: selected && structuredClone(selected),
        helm: isHelm,
        environments: targets,
        originalPackage: packageSnapshot.current,
        originalEnvironments: (isHelm
          ? helmEnvironmentSnapshot.current
          : environmentSnapshot.current
        )?.filter((item) =>
          item.targets?.some(
            (target) =>
              service?.id &&
              target.executorKind === (isHelm ? 'helm_sdk' : 'manifest_ssa') &&
              target.metadata?.serviceId === service.id,
          ),
        ),
      },
    }
  }

  const applyPreset = async (
    reference: NonNullable<ApplicationServiceComponent['deploymentTemplate']>,
  ) => {
    const selected = await client.fetchQuery(
      deliveryQueries.deploymentTemplates.version(reference.templateId, reference.version),
    )
    initializedTemplate.current = `${reference.templateId}:${reference.version}`
    previousTemplate.current = selected
    form.setFieldValue('deployment', {
      mode: 'template',
      templateId: reference.templateId,
      version: reference.version,
      parameters: deploymentParameterFields(selected.parameterSchema, {
        ...selected.defaults,
        ...reference.parameters,
      }),
      environmentIds: [],
      overrides: {},
      overrideKeys: {},
    })
  }
  return {
    form,
    applicationId,
    serviceId: service?.id,
    values,
    template,
    templatesQuery,
    versionsQuery,
    templateQuery,
    environmentsQuery,
    oldTemplateQuery,
    packageQuery,
    prepare,
    applyPreset,
    packageSnapshot,
    original,
    selection,
    environments: (environmentsQuery.data ?? []).filter(
      (item) => item.applicationId === applicationId,
    ),
  }
}

type DeploymentState = ReturnType<typeof useServiceDeployment>

export function ParameterField({
  name,
  field,
  label,
  required,
}: {
  name: (string | number)[]
  field: TemplateParameterSchema
  label: string
  required?: boolean
}) {
  if (field.type === 'secret_reference')
    return (
      <Card size="small" title={label}>
        <Form.Item name={[...name, 'name']} label="Secret 名称" rules={[{ required }]}>
          <Input />
        </Form.Item>
        <Form.Item name={[...name, 'key']} label="Secret Key" rules={[{ required }]}>
          <Input />
        </Form.Item>
      </Card>
    )
  return (
    <Form.Item
      name={name}
      label={label}
      tooltip={field.description}
      rules={[{ required, message: `请填写 ${label}` }]}
    >
      {field.type === 'boolean' ? (
        <Radio.Group
          options={[
            { value: true, label: '是' },
            { value: false, label: '否' },
          ].filter((option) => !field.enum?.length || field.enum.includes(option.value))}
        />
      ) : field.enum?.length ? (
        <Select options={field.enum.map((value) => ({ value, label: String(value) }))} />
      ) : field.type === 'number' || field.type === 'integer' ? (
        <InputNumber
          min={field.minimum}
          max={field.maximum}
          precision={field.type === 'integer' ? 0 : undefined}
        />
      ) : field.type === 'object' || field.type === 'array' ? (
        <Input.TextArea
          autoSize={{ minRows: 2, maxRows: 8 }}
          placeholder={field.type === 'array' ? '[]' : '{}'}
        />
      ) : (
        <Input maxLength={field.maxLength} />
      )}
    </Form.Item>
  )
}

const templateContent = (item?: ServiceDeploymentTemplate) =>
  JSON.stringify(
    item && {
      source: item.source,
      parameterSchema: item.parameterSchema,
      defaults: item.defaults,
      environmentOverrides: item.environmentOverrides,
      artifacts: item.artifacts,
      health: item.health,
    },
    null,
    2,
  ) || ''

export function DeploymentTemplateFields({ state }: { state: DeploymentState }) {
  const {
    form,
    values,
    template,
    templatesQuery,
    versionsQuery,
    templateQuery,
    oldTemplateQuery,
    original,
    selection,
  } = state
  const [diffOpen, setDiffOpen] = useState(false)
  const changed = Boolean(
    original &&
    (original.templateId !== values?.templateId || original.version !== values?.version),
  )
  const error =
    templatesQuery.error ||
    versionsQuery.error ||
    templateQuery.error ||
    oldTemplateQuery.error ||
    state.packageQuery.error
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Form.Item name={['deployment', 'mode']} label="部署配置">
        <Radio.Group
          options={[
            { value: 'template', label: '使用部署模板' },
            { value: 'helm', label: 'Helm Chart' },
            { value: 'docker', label: 'Docker / Compose' },
            {
              value: 'existing',
              label: original && !original.detached ? '保留当前配置' : '关联已有配置或稍后配置',
            },
            ...(original && !original.detached
              ? [{ value: 'independent', label: '复制已保存配置为独立配置' }]
              : []),
          ]}
        />
      </Form.Item>
      {values?.mode === 'independent' ? (
        <Alert
          type="info"
          showIcon
          title="保存后使用当前已保存的配置、参数和环境关联；本次尚未保存的模板调整不会用于副本。"
        />
      ) : original?.detached && values?.mode === 'existing' ? (
        <Text>已使用独立部署配置；来源为 v{original.version}。</Text>
      ) : null}
      {values?.mode === 'helm' ? (
        <HelmSourceFields
          name={['deployment', 'helmSource']}
          clusterId={
            state.environments.find((item) => values.environmentIds?.includes(item.id))
              ?.clusterId || state.environments[0]?.clusterId
          }
        />
      ) : null}
      {values?.mode === 'template' ? (
        <>
          {error ? <Alert type="error" showIcon title={error.message} /> : null}
          <Form.Item
            name={['deployment', 'templateId']}
            label="部署模板"
            rules={[{ required: true }]}
          >
            <Select
              disabled={Boolean(original && !original.detached)}
              loading={templatesQuery.isLoading}
              showSearch={{ optionFilterProp: 'label' }}
              onChange={(id) => {
                const item = templatesQuery.data?.find((candidate) => candidate.id === id)
                form.setFieldValue(
                  ['deployment', 'version'],
                  id === original?.templateId
                    ? original?.version
                    : item?.publishedVersion || undefined,
                )
                form.setFieldValue(['deployment', 'acceptedVersion'], undefined)
              }}
              options={(templatesQuery.data ?? []).map((item) => ({
                value: item.id,
                label: item.name,
                disabled:
                  (item.publicationState === 'deprecated' ||
                    !item.publishedVersion ||
                    !item.enabled) &&
                  item.id !== original?.templateId,
              }))}
            />
          </Form.Item>
          <Form.Item name={['deployment', 'version']} label="固定版本" rules={[{ required: true }]}>
            <Select
              disabled={Boolean(original && !original.detached)}
              loading={versionsQuery.isLoading}
              options={(versionsQuery.data ?? []).map((item) => ({
                value: item.publishedVersion,
                label: `v${item.publishedVersion}`,
              }))}
            />
          </Form.Item>
          {original && !original.detached ? (
            <Space orientation="vertical">
              <Text type="secondary">
                已关联服务按环境比较并采用新版本，保护其他环境的固定版本。
              </Text>
              <TemplateUpdateEntry
                kind="deployment"
                templateId={original.templateId}
                applicationId={state.applicationId}
                serviceId={state.serviceId}
                label="按环境采用模板版本"
              />
            </Space>
          ) : null}
          {changed ? (
            <Button
              onClick={() => setDiffOpen(true)}
              disabled={!template || !oldTemplateQuery.data}
            >
              查看并确认版本差异
            </Button>
          ) : null}
          {changed && values.acceptedVersion === selection ? (
            <Text type="success">已确认所选版本</Text>
          ) : null}
          {Object.entries(template?.parameterSchema.properties ?? {}).map(([key, field]) => (
            <ParameterField
              key={key}
              name={['deployment', 'parameters', key]}
              label={key}
              field={field}
              required={template?.parameterSchema.required?.includes(key)}
            />
          ))}
          <Modal
            title="部署模板版本差异"
            open={diffOpen}
            width="90vw"
            onCancel={() => setDiffOpen(false)}
            onOk={() => {
              form.setFieldValue(['deployment', 'acceptedVersion'], selection)
              setDiffOpen(false)
            }}
            okText="确认采用此版本"
            destroyOnHidden
          >
            {diffOpen ? (
              <Suspense fallback={<Text>加载差异…</Text>}>
                <DiffEditor
                  title="配置与参数"
                  original={templateContent(oldTemplateQuery.data)}
                  modified={templateContent(template)}
                  editable={false}
                  leftLabel={`原版本 v${original?.version}`}
                  rightLabel={`所选版本 v${values?.version}`}
                />
              </Suspense>
            ) : null}
          </Modal>
        </>
      ) : null}
    </Space>
  )
}

export function DeploymentEnvironmentFields({ state }: { state: DeploymentState }) {
  const { form, values, template, environments, environmentsQuery } = state
  if (values?.mode !== 'template' && values?.mode !== 'helm' && values?.mode !== 'docker')
    return <Text>保留已有部署配置；可在环境设置中继续关联。</Text>
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      {environmentsQuery.error ? (
        <Alert type="error" showIcon title={environmentsQuery.error.message} />
      ) : null}
      <Form.Item name={['deployment', 'environmentIds']} label="关联环境">
        <Select
          mode="multiple"
          loading={environmentsQuery.isLoading || state.packageQuery.isLoading}
          disabled={state.packageQuery.isLoading}
          onChange={(ids: string[]) => {
            if (template?.source.renderer !== 'helm' && values.mode !== 'helm') return
            for (const id of ids) {
              if (!form.getFieldValue(['deployment', 'helm', id]))
                form.setFieldValue(['deployment', 'helm', id], {
                  releaseName: form.getFieldValue('key'),
                  values: {},
                  imageMappings: [],
                  timeoutSeconds: template?.health.timeoutSeconds || 300,
                })
            }
          }}
          options={environments.map((item) => ({
            value: item.id,
            label:
              values.mode === 'docker'
                ? item.environmentKey || item.environmentId
                : `${item.environmentKey || item.environmentId} · ${item.namespace || '未设置命名空间'}`,
            disabled: values.mode !== 'docker' && (!item.clusterId || !item.namespace),
          }))}
        />
      </Form.Item>
      {(values.environmentIds ?? []).map((id) => (
        <Card
          key={id}
          size="small"
          title={environments.find((item) => item.id === id)?.environmentKey || id}
        >
          {values.mode === 'docker' ? (
            <DockerDeliveryFields name={['deployment', 'docker', id]} />
          ) : null}
          {template?.source.renderer === 'helm' || values.mode === 'helm' ? (
            <HelmEnvironmentFields
              key={`${state.selection}:${id}`}
              name={['deployment', 'helm', id]}
              applicationId={state.applicationId}
              environmentId={id}
              source={values.mode === 'helm' ? values.helmSource : template?.source.helm}
            />
          ) : null}
          <Form.Item
            name={['deployment', 'overrideKeys', id]}
            label="覆盖参数"
            hidden={values.mode === 'docker'}
          >
            <Select
              mode="multiple"
              options={(template?.environmentOverrides ?? []).map((key) => ({
                value: key,
                label: key,
              }))}
              onChange={(keys: string[]) => {
                for (const key of keys)
                  if (form.getFieldValue(['deployment', 'overrides', id, key]) === undefined)
                    form.setFieldValue(
                      ['deployment', 'overrides', id, key],
                      form.getFieldValue(['deployment', 'parameters', key]),
                    )
              }}
            />
          </Form.Item>
          {(values.overrideKeys?.[id] ?? []).map((key) =>
            template?.parameterSchema.properties?.[key] ? (
              <ParameterField
                key={key}
                name={['deployment', 'overrides', id, key]}
                field={template.parameterSchema.properties[key]}
                label={key}
                required
              />
            ) : (
              <Alert key={key} type="error" title={`参数 ${key} 已不存在，请移除覆盖`} />
            ),
          )}
        </Card>
      ))}
      <Text type="secondary">保存固定模板版本和环境参数。部署时再预检资源、完成审批并执行。</Text>
    </Space>
  )
}
