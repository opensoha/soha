/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DeliveryBlueprintsPage } from './blueprints/page'

const testState = vi.hoisted(() => ({
  permissionSnapshot: {
    permissionKeys: ['delivery.application.update'],
    visibleMenuIds: [],
    visibleMenus: [],
  },
  apiPut: vi.fn(async () => ({ data: {} })),
  apiGet: vi.fn(async (path: string) => {
    if (path === '/delivery/blueprints') {
      return {
        data: [
          {
            id: 'blueprint-1',
            key: 'node-service',
            name: 'Node Service',
            description: 'Node.js service onboarding',
            applicationDraft: {
              key: 'node-service',
              name: 'Node Service',
              group: 'frontend',
              language: 'node',
              enabled: true,
            },
            buildSources: [
              {
                id: 'source-1',
                name: 'Repo Dockerfile',
                type: 'repo_dockerfile',
                enabled: true,
                isDefault: true,
                config: { contextDir: '.', dockerfilePath: 'Dockerfile' },
              },
            ],
            environmentBindings: [
              {
                environmentKey: 'dev',
                workflowTemplateId: 'wf-dev',
                buildPolicy: { sourceId: 'source-1', refType: 'branch', refValue: 'main' },
                releasePolicy: { actionKind: 'deploy', verificationMode: 'workflow' },
              },
            ],
            files: [
              { path: 'Dockerfile', kind: 'dockerfile', content: 'FROM node:22', required: true },
            ],
            enabled: true,
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-08T12:00:00Z',
          },
        ],
      }
    }
    if (path === '/delivery/blueprints/blueprint-1/usage') {
      return {
        data: {
          templateKind: 'blueprint',
          templateId: 'blueprint-1',
          usageCount: 1,
          applicationCount: 1,
          environmentCount: 1,
          productionEnvironmentCount: 0,
          approvalBindingCount: 0,
          targetCount: 1,
          riskLevel: 'low',
          riskReasons: ['1 spec file templates'],
          recommendedAction: 'save_with_standard_review',
          applications: [{ id: 'app-1', name: 'Node Service', key: 'node-service' }],
          buildSources: [
            {
              applicationId: 'app-1',
              buildSourceId: 'source-1',
              buildSourceName: 'Repo Dockerfile',
              application: { id: 'app-1', name: 'Node Service', key: 'node-service' },
              bindingCount: 1,
              riskLevel: 'low',
            },
          ],
          fileKindCounts: { dockerfile: 1 },
          lastExecutionSummary: {
            source: 'delivery_blueprint_runtime',
            stateCounts: { succeeded: 1, failed: 0, running: 1, pending: 0 },
            statusCounts: { completed: 1, running: 1 },
            latest: {
              kind: 'execution_task',
              id: 'task-blueprint-1',
              applicationId: 'app-1',
              applicationEnvironmentId: 'binding-blueprint-1',
              taskKind: 'onboarding',
              status: 'running',
              observedAt: '2026-05-08T11:20:00Z',
            },
            items: [
              {
                kind: 'build',
                id: 'build-blueprint-1',
                applicationId: 'app-1',
                buildSourceId: 'source-1',
                sourceSystem: 'manual',
                status: 'completed',
                observedAt: '2026-05-08T10:00:00Z',
              },
              {
                kind: 'execution_task',
                id: 'task-blueprint-1',
                applicationId: 'app-1',
                applicationEnvironmentId: 'binding-blueprint-1',
                taskKind: 'onboarding',
                status: 'running',
                observedAt: '2026-05-08T11:20:00Z',
              },
            ],
          },
        },
      }
    }
    if (path === '/application-environments')
      return { data: [{ id: 'binding-1', environmentId: 'env-dev', environmentKey: 'dev' }] }
    if (path === '/build-templates')
      return { data: [{ id: 'build-1', name: 'Node Docker', key: 'node-docker', enabled: true }] }
    if (path === '/workflow-templates')
      return { data: [{ id: 'wf-dev', name: '开发发布流程', key: 'dev-release', enabled: true }] }
    throw new Error(`Unhandled GET ${path}`)
  }),
}))

vi.mock('@/features/auth/permission-snapshot', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, key: string) =>
    snapshot?.permissionKeys?.includes(key) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: testState.permissionSnapshot },
    isLoading: false,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: (path: string) => testState.apiGet(path),
    getEnvelope: async () => ({
      items: [{ id: 'registry-a', name: 'Private', endpoint: 'registry.example' }],
    }),
    post: vi.fn(),
    put: testState.apiPut,
    delete: vi.fn(),
  },
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function renderWithProviders(node: ReactNode, route = '/delivery/blueprints') {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[route]}>{node}</MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })

  await act(async () => {
    for (let index = 0; index < 16; index += 1) {
      await new Promise((resolve) => setTimeout(resolve, 0))
    }
  })

  return container
}

async function waitForText(container: ParentNode, text: string) {
  for (let index = 0; index < 40; index += 1) {
    if (container.textContent?.includes(text)) return
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  }
}

describe('DeliveryBlueprintsPage', () => {
  beforeEach(() => {
    testState.apiGet.mockClear()
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        matches: false,
        media: '',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) root.unmount()
    })
    roots = []
    for (const container of containers) container.remove()
    containers = []
    vi.clearAllMocks()
  })

  it('renders onboarding templates as a left-list and right-designer workspace', async () => {
    const container = await renderWithProviders(<DeliveryBlueprintsPage />)

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/blueprints')
    expect(container.querySelector('.soha-delivery-blueprint-workspace')).not.toBeNull()
    expect(container.querySelector('.soha-delivery-blueprint-list')).not.toBeNull()
    expect(container.querySelector('.soha-delivery-blueprint-designer')).not.toBeNull()
    const list = container.querySelector('.soha-delivery-blueprint-list')!
    const designer = container.querySelector('.soha-delivery-blueprint-designer')!
    expect(list.querySelector('[aria-label="新建模板"]')).not.toBeNull()
    expect(list.querySelector('[aria-label="刷新"]')).not.toBeNull()
    expect(designer.querySelector('.soha-delivery-blueprint-toolbar')).not.toBeNull()
    expect(designer.querySelector('[aria-label="新建模板"]')).toBeNull()
    expect(list.textContent).not.toContain('保存')
    expect(container.querySelector('.soha-template-designer-shell__toolbar')).toBeNull()
    expect(container.textContent).toContain('保存')
    expect(container.textContent).toContain('渲染规范')
    expect(container.textContent).not.toContain('平台接入')
    expect(container.textContent).toContain('Node Service')
    expect(container.textContent).toContain('node-service')
    expect(container.textContent).toContain('构建 1')
    expect(container.textContent).toContain('计划 1')
    expect(container.textContent).toContain('模板 1')
    expect(container.textContent).toContain('基础信息')
    expect(container.textContent).toContain('应用档案')
    expect(container.textContent).toContain('构建源')
    expect(container.textContent).toContain('发布计划')
    expect(container.textContent).toContain('文件模板')
    expect(container.textContent).toContain('高级配置')
    expect(container.textContent).toContain('模板影响面')
    await waitForText(container, '成功 1')
    expect(container.textContent).toContain('成功 1')
    expect(container.textContent).toContain('运行中 1')
    expect(container.textContent).toContain('最近证据：执行任务: onboarding')
    expect(container.textContent).toContain('跳转：')
    expect(container.textContent).not.toContain('Application Draft(JSON)')
    expect(container.textContent).not.toContain('Build Sources(JSON Array)')
    expect(container.textContent).not.toContain('Environment Bindings(JSON Array)')
  })

  it('opens the file-template preset view from a shared URL', async () => {
    const container = await renderWithProviders(
      <DeliveryBlueprintsPage />,
      '/delivery/blueprints?tab=files&preset=helm_values',
    )

    expect(container.textContent).toContain(
      '按预设快速添加 Dockerfile、YAML、Helm 或 Kustomize 模板。',
    )
    expect(container.textContent).toContain('Helm Values 模板')
    expect(container.textContent).toContain('文件模板')
  })

  it('keeps a new draft selected when entering from an existing template URL', async () => {
    const container = await renderWithProviders(
      <DeliveryBlueprintsPage />,
      '/delivery/blueprints?templateId=blueprint-1',
    )
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="新建模板"]')!.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await vi.waitFor(() => {
      expect(container.querySelector('.soha-delivery-blueprint-toolbar')?.textContent).toContain(
        '未保存',
      )
      expect(container.querySelector<HTMLInputElement>('#key')?.value).not.toBe('node-service')
    })
    const cancel = Array.from(
      container.querySelectorAll<HTMLButtonElement>('.soha-delivery-blueprint-toolbar button'),
    ).find((button) => button.textContent === '取消更改')!
    expect(cancel.disabled).toBe(false)
    await act(async () => {
      cancel.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.querySelector<HTMLInputElement>('#key')?.value).toBe('node-service')
    expect(testState.apiPut).not.toHaveBeenCalled()
  })

  it('saves typed external CI configuration after changing a Dockerfile source', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const container = await renderWithProviders(
      <DeliveryBlueprintsPage />,
      '/delivery/blueprints?tab=build',
    )
    const select = async (id: string, label: string) => {
      await act(async () => {
        container
          .querySelector(`#${id}`)!
          .dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
      })
      await act(async () => {
        const option = Array.from(
          document.querySelectorAll('.ant-select-item-option-content'),
        ).find((element) => element.textContent === label)!
        expect(option).toBeDefined()
        option.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      })
    }
    await select('buildSources_0_type', 'GitLab CI')
    await waitForText(container, '产物镜像仓库')
    for (const [name, value] of [
      ['pipelineTag', 'soha-v1'],
      ['artifactJob', 'publish'],
    ]) {
      await act(async () => {
        const input = container.querySelector(`#buildSources_0_externalPipeline_${name}`)!
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
          input,
          value,
        )
        input.dispatchEvent(new Event('input', { bubbles: true }))
      })
    }
    await select('buildSources_0_externalPipeline_registryId', 'Private · registry.example')
    await act(async () => {
      Array.from(container.querySelectorAll('button'))
        .find((button) => button.textContent === '保存')!
        .dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(testState.apiPut).toHaveBeenCalledWith(
      '/delivery/blueprints/blueprint-1',
      expect.objectContaining({
        buildSources: [
          expect.objectContaining({
            type: 'external_pipeline',
            config: {
              externalPipeline: {
                provider: 'gitlab',
                pipelineTag: 'soha-v1',
                artifactJob: 'publish',
                registryId: 'registry-a',
              },
            },
          }),
        ],
      }),
    )
  })
})
