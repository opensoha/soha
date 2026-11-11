/** @vitest-environment jsdom */
import { act, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Form, Button } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { deliveryApi } from '../api'
import { manifestApi } from '../manifests/api'
import { dockerApi } from '@/features/docker'
import {
  DeploymentEnvironmentFields,
  DeploymentTemplateFields,
  initialDeploymentValues,
  useServiceDeployment,
  ParameterField,
} from './deployment-template-fields'
import { BuildSourceFields } from './build-source-fields'
import { HelmEnvironmentFields, parseHelmValues } from '../helm-fields'
import type {
  ApplicationServiceComponent,
  BuildTemplate,
  ServiceDeploymentTemplate,
} from '../types'

vi.mock('../api', () => ({
  deliveryApi: {
    applications: { inspectHelmChart: vi.fn() },
    deploymentTemplates: { list: vi.fn(), versions: vi.fn(), version: vi.fn(), preview: vi.fn() },
    environments: { list: vi.fn() },
    buildTemplates: { list: vi.fn(), versions: vi.fn() },
  },
}))
vi.mock('../manifests/api', () => ({ manifestApi: { get: vi.fn() } }))
vi.mock('@/features/auth', () => ({
  hasPermission: () => true,
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))
vi.mock('@/components/yaml-draft-diff-editor', () => ({
  YamlDraftDiffEditor: ({ original, modified }: { original: string; modified: string }) => (
    <div>
      {original}
      {modified}
    </div>
  ),
}))

const now = '2026-09-12T00:00:00Z'
const template: ServiceDeploymentTemplate = {
  id: 'http',
  key: 'http',
  name: 'HTTP 服务',
  enabled: true,
  createdAt: now,
  updatedAt: now,
  publishedVersion: 1,
  publicationState: 'published',
  revision: 1,
  source: {
    renderer: 'raw_yaml',
    files: [{ path: 'deployment.yaml', content: 'kind: Deployment' }],
  },
  parameterSchema: {
    type: 'object',
    properties: {
      replicas: { type: 'integer' },
      enabled: { type: 'boolean' },
      labels: { type: 'object', mapValues: { type: 'string' } },
    },
    required: ['replicas', 'enabled'],
  },
  defaults: { replicas: 1, enabled: true, labels: {} },
  environmentOverrides: ['replicas', 'enabled'],
  artifacts: {},
  health: { mode: 'workload_ready', timeoutSeconds: 300 },
}
const service: ApplicationServiceComponent = {
  id: 'api',
  applicationId: 'app',
  key: 'api',
  name: 'API',
  serviceKind: 'kubernetes_workload',
  enabled: true,
  version: 1,
  createdAt: now,
  updatedAt: now,
  deploymentTemplate: {
    templateId: 'http',
    version: 1,
    parameters: { replicas: 0, enabled: false, labels: { owner: 'team' } },
    manifestPackageId: 'config',
  },
}
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
const prepared = vi.fn()

function Harness() {
  const [form] = Form.useForm()
  const state = useServiceDeployment(form, 'app', service)
  const [error, setError] = useState('')
  return (
    <Form form={form} initialValues={{ deployment: initialDeploymentValues(service) }}>
      <DeploymentTemplateFields state={state} />
      <DeploymentEnvironmentFields state={state} />
      <Button onClick={() => form.setFieldValue(['deployment', 'version'], 2)}>选择 v2</Button>
      <Button onClick={() => form.setFieldValue(['deployment', 'mode'], 'independent')}>
        独立副本
      </Button>
      <Button
        onClick={() =>
          void state
            .prepare('api')
            .then(prepared)
            .catch((error: Error) => setError(error.message))
        }
      >
        校验配置
      </Button>
      <span>{error}</span>
    </Form>
  )
}
async function click(label: string) {
  await act(async () => {
    const button = [...document.querySelectorAll('button')].find(
      (item) => item.textContent?.replace(/\s/g, '') === label.replace(/\s/g, ''),
    )
    expect(button, label).toBeDefined()
    button!.click()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}
beforeEach(async () => {
  vi.resetAllMocks()
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  window.matchMedia = vi.fn().mockImplementation(() => ({
    matches: false,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  vi.mocked(deliveryApi.deploymentTemplates.list).mockResolvedValue([
    { ...template, publishedVersion: 2 },
  ])
  vi.mocked(deliveryApi.deploymentTemplates.versions).mockResolvedValue([
    template,
    { ...template, publishedVersion: 2 },
  ])
  vi.mocked(deliveryApi.deploymentTemplates.version).mockImplementation(async (_id, version) => ({
    ...template,
    publishedVersion: version,
    defaults: { ...template.defaults, replicas: version },
  }))
  vi.mocked(deliveryApi.environments.list).mockResolvedValue([
    {
      id: 'app-test',
      applicationId: 'app',
      environmentId: 'test',
      environmentKey: '测试环境',
      clusterId: 'cluster',
      namespace: 'api-test',
      createdAt: now,
      updatedAt: now,
    },
  ])
  vi.mocked(manifestApi.get).mockResolvedValue({
    id: 'config',
    applicationId: 'app',
    serviceId: 'api',
    name: 'API 配置',
    renderer: 'raw_yaml',
    status: 'draft',
    currentRevision: 0,
    files: template.source.files!,
    createdAt: now,
    updatedAt: now,
    bindings: [
      {
        id: 'binding',
        applicationEnvironmentId: 'app-test',
        environmentKey: 'test',
        clusterId: 'cluster',
        namespace: 'api-test',
        templateParameters: { enabled: false, replicas: 2 },
      },
    ],
  })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  await act(async () => {
    root.render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <Harness />
      </QueryClientProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
})
afterEach(async () => {
  await act(async () => root.unmount())
  // antd delayed validation feedback may settle after unmount.
  await new Promise((resolve) => setTimeout(resolve, 500))
  container.remove()
  vi.unstubAllGlobals()
})

it('prepares an existing Docker project in an environment without a Kubernetes binding', async () => {
  const docker = { hostId: 'host', projectId: 'project', imageMappings: { api: 'main' } }
  vi.mocked(deliveryApi.environments.list).mockResolvedValue([
    {
      id: 'docker-dev',
      applicationId: 'app',
      environmentId: 'dev',
      environmentKey: 'Docker 测试',
      createdAt: now,
      updatedAt: now,
      targets: [
        {
          id: 'docker-target',
          clusterId: '',
          namespace: '',
          workloadKind: 'ComposeProject',
          workloadName: 'project',
          enabled: true,
          executorKind: 'docker_compose',
          targetKind: 'host_service',
          metadata: { serviceId: 'api' },
          docker,
        },
      ],
    },
  ])
  vi.spyOn(dockerApi, 'hosts').mockResolvedValue({ items: [], page: 1, pageSize: 200, total: 0 })
  vi.spyOn(dockerApi, 'projects').mockResolvedValue({ items: [], page: 1, pageSize: 200, total: 0 })
  const project = vi
    .spyOn(dockerApi, 'project')
    .mockResolvedValue({ id: 'project', hostId: 'host' } as Awaited<
      ReturnType<typeof dockerApi.project>
    >)
  function DockerHarness() {
    const [form] = Form.useForm()
    const state = useServiceDeployment(form, 'app', {
      ...service,
      serviceKind: 'external_service',
      deploymentTemplate: undefined,
    })
    const [error, setError] = useState('')
    return (
      <Form form={form} initialValues={{ deployment: { mode: 'docker' } }}>
        <DeploymentEnvironmentFields state={state} />
        <Button
          onClick={() =>
            void state
              .prepare('api')
              .then(prepared)
              .catch((err: Error) => setError(err.message))
          }
        >
          检查 Docker 配置
        </Button>
        <span>{error}</span>
      </Form>
    )
  }
  await act(async () => {
    root.render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <DockerHarness />
      </QueryClientProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(
    document.querySelector<HTMLInputElement>('input[aria-label="Compose 服务名"]')?.value,
  ).toBe('api')
  expect(document.querySelector<HTMLInputElement>('input[aria-label="产物容器名"]')?.value).toBe(
    'main',
  )
  await click('检查 Docker 配置')
  expect(prepared).toHaveBeenCalledWith(
    expect.objectContaining({
      setup: expect.objectContaining({
        docker: true,
        environments: [expect.objectContaining({ docker })],
      }),
    }),
  )
  prepared.mockClear()
  project.mockResolvedValue({ id: 'project', hostId: 'moved-host' } as Awaited<
    ReturnType<typeof dockerApi.project>
  >)
  await click('检查 Docker 配置')
  expect(prepared).not.toHaveBeenCalled()
  expect(document.body.textContent).toContain('项目已移动到其他主机')
  vi.restoreAllMocks()
})

it('preserves typed build overrides and limits scalar enum choices', async () => {
  const buildTemplate: BuildTemplate = {
    id: 'build',
    key: 'build',
    name: '构建参数',
    builderKind: 'kaniko',
    enabled: true,
    revision: 1,
    publishedVersion: 1,
    publicationState: 'published',
    createdAt: now,
    updatedAt: now,
    buildCommands: ['echo ready'],
    variableSchema: { count: { type: 'integer' }, enabled: { type: 'boolean' } },
    defaultVariables: { count: 3, enabled: true },
  }
  vi.mocked(deliveryApi.buildTemplates.list).mockResolvedValue([buildTemplate])
  vi.mocked(deliveryApi.buildTemplates.versions).mockResolvedValue([buildTemplate])
  function BuildHarness() {
    const [form] = Form.useForm()
    return (
      <Form
        form={form}
        initialValues={{
          name: 'source',
          type: 'platform_build_template',
          config: {
            buildTemplateId: 'build',
            buildTemplateVersion: 1,
            variables: { count: 0, enabled: false },
          },
        }}
      >
        <BuildSourceFields form={form} repositories={[]} section="build" />
        <ParameterField
          name={['restricted']}
          label="受限布尔"
          field={{ type: 'boolean', enum: [true] }}
        />
        <ParameterField
          name={['numeric']}
          label="受限数字"
          field={{ type: 'integer', enum: [1, 2] }}
        />
        <Button onClick={() => prepared(form.getFieldsValue(true))}>读取构建参数</Button>
      </Form>
    )
  }
  await act(async () => {
    root.render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <BuildHarness />
      </QueryClientProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  await click('读取构建参数')
  expect(prepared).toHaveBeenCalledWith(
    expect.objectContaining({
      config: expect.objectContaining({ variables: { count: 0, enabled: false } }),
    }),
  )
  const boolGroup = container.querySelector('label[for="restricted"]')!.closest('.ant-form-item')!
  expect(boolGroup.querySelectorAll('input[type="radio"]')).toHaveLength(1)
  const numberGroup = container.querySelector('label[for="numeric"]')!.closest('.ant-form-item')!
  expect(numberGroup.querySelector('[role="combobox"]')).not.toBeNull()
  expect(numberGroup.querySelector('[role="spinbutton"]')).toBeNull()
})

it('keeps the bound version and typed values when the catalog already has a newer version', async () => {
  await click('校验配置')
  expect(prepared).toHaveBeenCalledWith(
    expect.objectContaining({
      reference: expect.objectContaining({
        version: 1,
        parameters: service.deploymentTemplate!.parameters,
      }),
    }),
  )
  expect(deliveryApi.deploymentTemplates.preview).toHaveBeenCalledWith(
    'app',
    expect.objectContaining({
      applicationEnvironmentId: 'app-test',
      version: 1,
      parameters: { replicas: 0, enabled: false, labels: { owner: 'team' } },
      overrides: { enabled: false, replicas: 2 },
    }),
  )
  expect(container.querySelector<HTMLInputElement>('#deployment_parameters_replicas')?.value).toBe(
    '0',
  )
})

it('requires explicit comparison confirmation before adopting another immutable version', async () => {
  await click('选择 v2')
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  await click('校验配置')
  expect(prepared).not.toHaveBeenCalled()
  expect(container.textContent).toContain('请先查看并确认模板版本差异')
  await click('查看并确认版本差异')
  await click('确认采用此版本')
  await click('校验配置')
  expect(prepared).toHaveBeenCalledWith(
    expect.objectContaining({ reference: expect.objectContaining({ version: 2 }) }),
  )
})

it('copies saved inputs without writing or using unsaved template changes', async () => {
  await click('选择 v2')
  await click('独立副本')
  await click('校验配置')
  expect(prepared).toHaveBeenCalledWith({
    reference: { ...service.deploymentTemplate, detached: true },
  })
  expect(deliveryApi.deploymentTemplates.preview).not.toHaveBeenCalled()
  expect(container.textContent).toContain('本次尚未保存的模板调整不会用于副本')
})

it('prepares native Helm YAML configuration without a manifest package or a build', async () => {
  function HelmHarness() {
    const [form] = Form.useForm()
    const state = useServiceDeployment(form, 'app')
    return (
      <Form
        form={form}
        initialValues={{
          key: 'api',
          deployment: {
            mode: 'helm',
            environmentIds: ['app-test'],
            helmSource: {
              repositoryUrl: 'oci://registry.example.com/charts',
              chart: 'api',
              version: '1.2.3',
              values: 'enabled: false\nreplicas: 0\n',
            },
            helm: {
              'app-test': {
                releaseName: 'api-test',
                values: 'worker:\n  enabled: true\n',
                imageMappings: [{ containerName: 'main', path: '/image', value: 'image' }],
              },
            },
          },
        }}
      >
        <DeploymentTemplateFields state={state} />
        <DeploymentEnvironmentFields state={state} />
        <Button onClick={() => void state.prepare('api').then(prepared)}>保存 Helm 配置</Button>
      </Form>
    )
  }
  await act(async () => {
    root.render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <HelmHarness />
      </QueryClientProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 40))
  })
  await click('保存 Helm 配置')
  expect(prepared).toHaveBeenCalledWith(
    expect.objectContaining({
      setup: expect.objectContaining({
        helm: true,
        template: undefined,
        environments: [
          expect.objectContaining({
            environment: expect.objectContaining({ id: 'app-test', namespace: 'api-test' }),
            helm: expect.objectContaining({
              releaseName: 'api-test',
              source: expect.objectContaining({
                chart: 'api',
                version: '1.2.3',
                values: { enabled: false, replicas: 0 },
              }),
              values: { worker: { enabled: true } },
              imageMappings: [{ containerName: 'main', path: '/image', value: 'image' }],
            }),
          }),
        ],
      }),
    }),
  )
  expect(deliveryApi.deploymentTemplates.preview).not.toHaveBeenCalled()
})

it('rejects ambiguous YAML and preserves typed Helm values', () => {
  expect(parseHelmValues('enabled: false\nreplicas: 0\nnested: {values: [1, 2]}')).toEqual({
    enabled: false,
    replicas: 0,
    nested: { values: [1, 2] },
  })
  for (const invalid of ['[1, 2]', 'true', 'a: 1\na: 2', 'a: .inf', 'a: &loop {b: *loop}'])
    expect(() => parseHelmValues(invalid), invalid).toThrow()
})

it('edits Schema fields without replacing nested YAML and hides inspection after Chart changes', async () => {
  vi.mocked(deliveryApi.applications.inspectHelmChart).mockResolvedValue({
    name: 'api',
    version: '1.0.0',
    digest: `sha256:${'a'.repeat(64)}`,
    defaultValues: { count: 1 },
    hasValuesSchema: true,
    valuesSchema: { type: 'object', properties: { count: { type: 'integer', minimum: 0 } } },
  })
  function SchemaHarness() {
    const [form] = Form.useForm()
    const [version, setVersion] = useState('1.0.0')
    return (
      <Form
        form={form}
        initialValues={{
          helm: { releaseName: 'api', values: 'count: 0\nnested: {enabled: false}\n' },
        }}
      >
        <HelmEnvironmentFields
          name={['helm']}
          applicationId="app"
          environmentId="app-test"
          source={{
            repositoryUrl: 'https://charts.example.com',
            chart: 'api',
            version,
            values: {},
          }}
        />
        <Button onClick={() => setVersion('2.0.0')}>切换 Chart</Button>
        <Button onClick={() => prepared(parseHelmValues(form.getFieldValue(['helm', 'values'])))}>
          读取 Values
        </Button>
      </Form>
    )
  }
  await act(async () => {
    root.render(
      <QueryClientProvider client={new QueryClient()}>
        <SchemaHarness />
      </QueryClientProvider>,
    )
  })
  await click('读取 Chart 默认值与 Schema')
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  const fields = [...container.querySelectorAll<HTMLElement>('[role="button"]')].find((item) =>
    item.textContent?.includes('按 Schema 编辑覆盖字段'),
  )!
  expect(fields).toBeDefined()
  await act(async () => fields.click())
  expect(container.querySelector<HTMLInputElement>('[role="spinbutton"]')?.value).toBe('0')
  await click('移除覆盖')
  await click('读取 Values')
  expect(prepared).toHaveBeenLastCalledWith({ nested: { enabled: false } })
  await click('切换 Chart')
  expect(container.textContent).not.toContain('按 Schema 编辑覆盖字段')
  expect(container.textContent).not.toContain('api 1.0.0 · 默认 Values')
})
