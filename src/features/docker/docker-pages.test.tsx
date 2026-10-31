/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act, StrictMode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from 'antd'
import { RuntimeHostModal, buildRuntimeHostPayload } from './hosts/create-page'
import { DockerHostsPage, buildQuickHostPayload } from './hosts/page'
import { DockerProjectDetailPage } from './projects/detail-page'
import {
  DockerProjectsPage,
  buildContainerStartPayload,
  buildProjectPayload,
} from './projects/list-page'
import { DockerTemplatesPage, buildTemplatePayload } from './templates/page'
import { PortsTable } from './ports/table'
import { ServicesTable } from './services/table'
import { OperationsTable } from './operations/table'
import type {
  DockerContainerStartInput,
  DockerProjectInput,
  DockerQuickCreateHostInput,
  DockerTemplateInput,
} from './docker-types'

const testState = vi.hoisted(() => ({
  modules: {
    docker: true,
    virtualization: true,
  },
  permissionSnapshot: {
    permissionKeys: ['docker.hosts.create', 'docker.hosts.update', 'docker.hosts.delete'],
    visibleMenuIds: [],
    visibleMenus: [],
  },
  apiGet: vi.fn(async (path: string): Promise<{ data: unknown }> => {
    if (path === '/modules') {
      return {
        data: [
          {
            descriptor: { id: 'docker', name: 'Docker', defaultPath: '/docker' },
            enabled: testState.modules.docker,
          },
          {
            descriptor: {
              id: 'virtualization',
              name: 'Virtualization',
              defaultPath: '/virtualization',
            },
            enabled: testState.modules.virtualization,
          },
        ],
      }
    }
    if (path === '/docker/hosts?page=1&pageSize=15') {
      return { data: { items: [], total: 0, page: 1, pageSize: 15 } }
    }
    if (
      path === '/docker/templates?page=1&pageSize=15' ||
      path === '/docker/ports?page=1&pageSize=15'
    ) {
      return { data: { items: [], total: 0, page: 1, pageSize: 15 } }
    }
    if (path === '/docker/hosts?page=1&pageSize=200') {
      return {
        data: {
          items: [{ id: 'host-1', name: 'local-orbstack', architecture: 'arm64' }],
          total: 1,
          page: 1,
          pageSize: 200,
        },
      }
    }
    if (path === '/docker/projects?page=1&pageSize=200') {
      return {
        data: {
          items: [
            {
              id: 'project-1',
              hostId: 'host-1',
              name: 'soha-orbstack-smoke',
              sourceKind: 'single_container',
            },
          ],
          total: 1,
          page: 1,
          pageSize: 200,
        },
      }
    }
    if (path === '/docker/projects?page=1&pageSize=15') {
      return {
        data: {
          items: [
            {
              id: 'project-compose',
              hostId: 'host-1',
              name: 'soha-compose-stack',
              slug: 'soha-compose-stack',
              sourceKind: 'inline_compose',
              status: 'running',
              desiredState: 'running',
              environment: 'local',
            },
            {
              id: 'project-1',
              hostId: 'host-1',
              name: 'soha-orbstack-smoke',
              slug: 'soha-orbstack-smoke',
              sourceKind: 'single_container',
              status: 'running',
              desiredState: 'running',
              environment: 'local',
              owner: 'admin',
              config: {
                image: 'nginx:alpine',
                architecture: 'arm64',
                ports: [
                  { hostIp: '127.0.0.1', hostPort: 18083, containerPort: 80, protocol: 'tcp' },
                ],
              },
            },
          ],
          total: 2,
          page: 1,
          pageSize: 15,
        },
      }
    }
    if (path === '/docker/projects/project-1') {
      return {
        data: {
          id: 'project-1',
          hostId: 'host-1',
          name: 'soha-orbstack-smoke',
          slug: 'soha-orbstack-smoke',
          sourceKind: 'single_container',
          status: 'running',
          config: {
            image: 'nginx:alpine',
            serviceName: 'web',
            volumes: [{ target: '/usr/share/nginx/html' }],
          },
        },
      }
    }
    if (path === '/docker/projects/project-1/runtime/volumes?serviceName=web') {
      return {
        data: [
          {
            name: 'fixture',
            type: 'volume',
            target: '/data',
            readOnly: false,
            browseSupported: true,
          },
        ],
      }
    }
    if (
      path ===
      '/docker/projects/project-1/runtime/volume-files?serviceName=web&target=%2Fdata&path=%2F&limit=300'
    ) {
      return {
        data: {
          projectId: 'project-1',
          serviceName: 'web',
          target: '/data',
          path: '/',
          items: [{ name: 'readme.txt', path: '/readme.txt', kind: 'file', sizeBytes: 14 }],
        },
      }
    }
    if (
      path ===
      '/docker/projects/project-1/runtime/volume-file?serviceName=web&target=%2Fdata&path=%2Freadme.txt&limitBytes=262144'
    ) {
      return {
        data: {
          projectId: 'project-1',
          serviceName: 'web',
          target: '/data',
          path: '/readme.txt',
          content: 'synthetic file',
          sizeBytes: 14,
          truncated: false,
        },
      }
    }
    if (path === '/docker/services?projectId=project-1&page=1&pageSize=100') {
      return {
        data: {
          items: [{ id: 'service-1', name: 'web', projectId: 'project-1' }],
          total: 1,
          page: 1,
          pageSize: 100,
        },
      }
    }
    if (path === '/docker/services?projectId=project-compose&page=1&pageSize=100') {
      return {
        data: {
          items: [
            {
              id: 'service-compose-web',
              projectId: 'project-compose',
              hostId: 'host-1',
              name: 'web',
              image: 'nginx:alpine',
              status: 'running',
              containerId: 'compose-web-1',
            },
          ],
          total: 1,
          page: 1,
          pageSize: 100,
        },
      }
    }
    if (path === '/docker/projects?page=1&pageSize=15&sourceKind=compose') {
      return { data: { items: [], total: 0, page: 1, pageSize: 15 } }
    }
    if (path === '/docker/projects?page=1&pageSize=15&sourceKind=single_container') {
      return {
        data: {
          items: [
            {
              id: 'project-1',
              hostId: 'host-1',
              name: 'soha-orbstack-smoke',
              slug: 'soha-orbstack-smoke',
              sourceKind: 'single_container',
              status: 'running',
              desiredState: 'running',
              environment: 'local',
              owner: 'admin',
              config: {
                image: 'nginx:alpine',
                architecture: 'arm64',
                ports: [
                  { hostIp: '127.0.0.1', hostPort: 18083, containerPort: 80, protocol: 'tcp' },
                ],
              },
            },
          ],
          total: 1,
          page: 1,
          pageSize: 15,
        },
      }
    }
    if (path === '/virtualization/clusters') {
      return {
        data: [
          {
            id: 'conn-pve',
            name: 'pve-a',
            provider: 'pve',
            enabled: true,
            config: { defaultBridge: 'vmbr0', defaultStorage: 'local-lvm' },
          },
          {
            id: 'conn-kv',
            name: 'kubevirt-a',
            provider: 'kubevirt',
            enabled: true,
            config: { backendUrl: 'https://kube.example:6443' },
          },
        ],
      }
    }
    if (path === '/virtualization/vms?page=1&pageSize=500') {
      return {
        data: {
          items: [
            {
              id: 'vm-1',
              name: 'docker-vm',
              provider: 'pve',
              connectionId: 'conn-pve',
              connectionName: 'pve-a',
              status: 'running',
            },
          ],
          total: 1,
          page: 1,
          pageSize: 500,
        },
      }
    }
    if (path === '/virtualization/images?page=1&pageSize=500') {
      return {
        data: {
          items: [
            {
              id: 'image-pve-template',
              name: 'ubuntu-template',
              provider: 'pve',
              connectionId: 'conn-pve',
              sourceKind: 'template',
              sourceRef: '9000',
            },
            {
              id: 'image-pve-iso',
              name: 'rocky.iso',
              provider: 'pve',
              connectionId: 'conn-pve',
              sourceKind: 'iso',
              sourceRef: 'local:iso/rocky.iso',
              node: 'pve-a',
            },
            {
              id: 'pve-network-vmbr0',
              name: 'vmbr0',
              provider: 'pve',
              connectionId: 'conn-pve',
              sourceKind: 'network',
              node: 'pve-a',
              config: { bridge: true, network: 'vmbr0' },
            },
            {
              id: 'pve-storage-local-lvm',
              name: 'local-lvm',
              provider: 'pve',
              connectionId: 'conn-pve',
              sourceKind: 'storage',
              node: 'pve-a',
              config: { supportsImages: true },
            },
            {
              id: 'image-kv',
              name: 'ubuntu-ds',
              provider: 'kubevirt',
              connectionId: 'conn-kv',
              sourceKind: 'datasource',
              sourceRef: 'default/ubuntu',
            },
            {
              id: 'nad-tenant',
              name: 'tenant-net',
              provider: 'kubevirt',
              connectionId: 'conn-kv',
              sourceKind: 'networkattachmentdefinition',
              namespace: 'default',
              config: { networkAttachmentDefinition: 'default/tenant-net' },
            },
          ],
          total: 6,
          page: 1,
          pageSize: 500,
        },
      }
    }
    if (path === '/virtualization/flavors') {
      return {
        data: [
          {
            id: 'flavor-1',
            name: 'standard-2c4g',
            cpu: 2,
            memoryMiB: 4096,
            diskGiB: 40,
            enabled: true,
          },
        ],
      }
    }
    throw new Error(`Unhandled GET ${path}`)
  }),
  apiPost: vi.fn(async (_path: string, _body?: unknown) => ({ data: { id: 'operation-1' } })),
  apiPostWithHeaders: vi.fn(async (_path: string, _body?: unknown, _headers?: HeadersInit) => ({
    data: { id: 'operation-1' },
  })),
  apiPostWithSignal: vi.fn(async (_path: string, _body: unknown, _signal: AbortSignal) => ({
    data: {
      entries: [
        {
          id: 'line-1',
          timestamp: '2026-09-21T12:00:00Z',
          message: 'service is ready',
          source: { kind: 'docker', dockerProjectId: 'project-1', dockerService: 'web' },
        },
      ],
      truncated: false,
    },
  })),
  apiPut: vi.fn(async (_path: string, _body?: unknown) => ({ data: { id: 'updated' } })),
  apiDelete: vi.fn(async (_path: string) => ({ data: undefined })),
}))

vi.mock('@/features/auth', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, key: string) =>
    snapshot?.permissionKeys?.includes(key) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: testState.permissionSnapshot },
    isLoading: false,
  }),
}))

vi.mock('@/services/api-client', async () => ({
  ApiError: (await import('@/services/api-error')).ApiError,
  api: {
    get: (path: string) => testState.apiGet(path),
    post: (path: string, body?: unknown) => testState.apiPost(path, body),
    postWithSignal: (path: string, body: unknown, signal: AbortSignal) =>
      testState.apiPostWithSignal(path, body, signal),
    postWithHeaders: (path: string, body: unknown, headers: HeadersInit) =>
      testState.apiPostWithHeaders(path, body, headers),
    put: (path: string, body?: unknown) => testState.apiPut(path, body),
    delete: (path: string) => testState.apiDelete(path),
  },
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function renderWithProviders(node: ReactNode, route = '/') {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[route]}>
          <App>{node}</App>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await settleQueries(queryClient)
  return container
}

async function settleQueries(queryClient: QueryClient) {
  let idleTicks = 0
  for (let index = 0; index < 50; index += 1) {
    await act(async () => {
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (queryClient.isFetching() === 0 && queryClient.isMutating() === 0) {
      idleTicks += 1
      if (idleTicks >= 3) {
        return
      }
    } else {
      idleTicks = 0
    }
  }
}

async function changeInput(input: HTMLInputElement | null, value: string) {
  expect(input).not.toBeNull()
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  await act(async () => {
    setter?.call(input, value)
    input?.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

async function clickButton(label: string) {
  const button = Array.from(document.querySelectorAll('button')).find(
    (item) => item.textContent === label,
  )
  expect(button).toBeDefined()
  await act(async () => button?.click())
}

describe('docker pages', () => {
  beforeEach(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    testState.modules = {
      docker: true,
      virtualization: true,
    }
    testState.permissionSnapshot = {
      permissionKeys: ['docker.hosts.create', 'docker.hosts.update', 'docker.hosts.delete'],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    testState.apiGet.mockClear()
    testState.apiPost.mockClear()
    testState.apiPostWithSignal.mockClear()
    testState.apiPostWithHeaders.mockClear()
    testState.apiPut.mockClear()
    testState.apiDelete.mockClear()
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    )
    Object.defineProperty(window, 'getComputedStyle', {
      configurable: true,
      writable: true,
      value: vi.fn().mockReturnValue({
        width: '0px',
        height: '0px',
        overflow: 'auto',
        getPropertyValue: () => '',
      }),
    })
  })

  afterEach(async () => {
    for (const root of roots) {
      await act(async () => root.unmount())
    }
    roots = []
    containers.forEach((container) => container.remove())
    containers = []
    document.body.innerHTML = ''
    vi.unstubAllGlobals()
  })

  it('uses server retryability for historical direct operations', async () => {
    testState.permissionSnapshot.permissionKeys = [
      'docker.operations.view',
      'docker.operations.retry',
    ]
    const items = [
      {
        id: 'old-log-read',
        operationKind: 'service_action',
        status: 'failed',
        operationState: { retryable: false },
      },
      {
        id: 'old-port-reserve',
        operationKind: 'port_reserve',
        status: 'failed',
        operationState: { retryable: false },
      },
      {
        id: 'service-restart',
        operationKind: 'service_action',
        status: 'failed',
        operationState: { retryable: true },
      },
      { id: 'older-server-deploy', operationKind: 'project_deploy', status: 'failed' },
    ]
    const fallback = testState.apiGet.getMockImplementation()!
    await testState.apiGet.withImplementation(
      async (path: string) => {
        if (path.startsWith('/docker/operations?')) {
          return { data: { items, total: items.length, page: 1, pageSize: 15 } }
        }
        return fallback(path)
      },
      async () => {
        const container = await renderWithProviders(<OperationsTable />)
        for (const item of items) {
          const row = container.querySelector(`tr[data-row-key="${item.id}"]`)
          expect(row).not.toBeNull()
          expect(row?.querySelector('button[aria-label="查看日志"]')).not.toBeNull()
          expect(row?.querySelector('button[aria-label="重试任务"]') !== null).toBe(
            item.operationState?.retryable ?? true,
          )
        }
        expect(testState.apiPost).not.toHaveBeenCalled()
      },
    )
  })

  it('opens selected service logs directly without enqueueing an action', async () => {
    testState.permissionSnapshot.permissionKeys = ['docker.services.view', 'docker.services.logs']
    const originalGet = testState.apiGet.getMockImplementation()!
    testState.apiGet.mockImplementation(async (path: string) => {
      if (
        path === '/docker/services?page=1&pageSize=5&projectId=project-1' ||
        path === '/docker/services?projectId=project-1&page=1&pageSize=5'
      ) {
        return originalGet('/docker/services?projectId=project-1&page=1&pageSize=100')
      }
      return originalGet(path)
    })
    try {
      const container = await renderWithProviders(
        <ServicesTable embedded fixedProjectId="project-1" />,
      )
      await act(async () =>
        container.querySelector<HTMLButtonElement>('button[aria-label="查看日志"]')?.click(),
      )
      for (let i = 0; i < 30 && !testState.apiPostWithSignal.mock.calls.length; i++) {
        await act(async () => {
          await new Promise((resolve) => setTimeout(resolve, 0))
        })
      }
      expect(testState.apiPostWithSignal).toHaveBeenCalledWith(
        '/docker/projects/project-1/logs/query',
        expect.objectContaining({ selector: expect.objectContaining({ dockerService: 'web' }) }),
        expect.any(AbortSignal),
      )
      expect(document.body.textContent).toContain('服务日志 · web')
      expect(testState.apiPost.mock.calls.some(([path]) => path.includes('/actions/'))).toBe(false)
      await act(async () => document.querySelector<HTMLButtonElement>('.ant-drawer-close')?.click())
    } finally {
      testState.apiGet.mockImplementation(originalGet)
    }
  })

  it('builds quick host payload with virtualization resource ids and GiB sizing', () => {
    const payload = buildQuickHostPayload({
      name: 'docker-dev',
      architecture: 'arm64',
      virtualizationConnectionId: 'conn-pve',
      imageId: 'image-pve-template',
      flavorId: 'flavor-1',
      network: 'vmbr0',
      cloudInit: '#cloud-config\npackages:\n  - docker.io',
      config: { providerParams: { snippetStorage: 'local' } },
      cpuCoreCount: 4,
      memoryGiB: 8,
      diskGiB: 80,
      availablePortStart: 20000,
      availablePortEnd: 39999,
    }) satisfies DockerQuickCreateHostInput

    expect(payload).toMatchObject({
      name: 'docker-dev',
      architecture: 'arm64',
      virtualizationConnectionId: 'conn-pve',
      imageId: 'image-pve-template',
      flavorId: 'flavor-1',
      network: 'vmbr0',
      cloudInit: '#cloud-config\npackages:\n  - docker.io',
      config: { providerParams: { snippetStorage: 'local' } },
      memoryBytes: 8 * 1024 ** 3,
      diskBytes: 80 * 1024 ** 3,
    })
    expect(payload).not.toHaveProperty('memoryGiB')
    expect(payload).not.toHaveProperty('diskGiB')
  })

  it('builds quick host payload with KubeVirt network provider params', () => {
    const payload = buildQuickHostPayload({
      name: 'docker-kubevirt',
      architecture: 'amd64',
      virtualizationConnectionId: 'conn-kv',
      imageId: 'image-kv',
      flavorId: 'flavor-1',
      network: 'apps/docker-build-net',
      config: {
        providerParams: {
          networkType: 'multus',
          networkAttachmentDefinition: 'apps/docker-build-net',
          interfaceModel: 'virtio',
          interfaceBinding: 'bridge',
          interfaceName: 'net1',
        },
      },
      cpuCoreCount: 4,
      memoryGiB: 8,
      diskGiB: 80,
    }) satisfies DockerQuickCreateHostInput

    expect(payload).toMatchObject({
      virtualizationConnectionId: 'conn-kv',
      imageId: 'image-kv',
      network: 'apps/docker-build-net',
      config: {
        providerParams: {
          networkType: 'multus',
          networkAttachmentDefinition: 'apps/docker-build-net',
          interfaceModel: 'virtio',
          interfaceBinding: 'bridge',
          interfaceName: 'net1',
        },
      },
      memoryBytes: 8 * 1024 ** 3,
      diskBytes: 80 * 1024 ** 3,
    })
  })

  it('builds compose project payload with typed labels and config', () => {
    const payload = buildProjectPayload({
      hostId: 'host-1',
      name: 'preview-stack',
      sourceKind: '',
      status: '',
      composeContent: '',
      labels: { app: 'preview', managed: true },
      config: {
        serviceName: 'web',
        image: 'nginx:alpine',
        ports: [{ hostPort: 18080, containerPort: 80 }],
      },
    }) satisfies DockerProjectInput

    expect(payload).toMatchObject({
      hostId: 'host-1',
      name: 'preview-stack',
      sourceKind: 'inline_compose',
      status: 'draft',
      composeContent: expect.stringContaining('nginx:alpine'),
      labels: { app: 'preview', managed: true },
      config: {
        serviceName: 'web',
        image: 'nginx:alpine',
        ports: [{ hostPort: 18080, containerPort: 80 }],
      },
    })

    expect(
      buildProjectPayload({
        hostId: 'host-1',
        name: 'remote-stack',
        sourceKind: 'url',
        sourceRef: 'https://example.com/compose.yaml',
        composeContent: 'services:\n  web:\n    image: nginx:alpine\n',
      }).composeContent,
    ).toBeUndefined()
  })

  it('builds structured container start payload for quick Docker app launch', () => {
    const payload = buildContainerStartPayload({
      hostId: 'host-1',
      name: 'preview-api',
      image: 'nginx:alpine',
      architecture: 'arm64',
      restartPolicy: 'unless-stopped',
      ports: [
        {
          name: 'http',
          hostIp: '0.0.0.0',
          hostPort: 18080,
          containerPort: 80,
          protocol: 'tcp',
          exposureScope: 'internal',
          domainName: 'preview.internal.example.com',
          domainScheme: 'https',
          domainTlsEnabled: true,
        },
        {
          name: 'admin',
          hostIp: '127.0.0.1',
          hostPort: 18081,
          containerPort: 8080,
          protocol: 'tcp',
          exposureScope: 'vpn',
        },
      ],
      volumes: [
        { type: 'bind', source: '/data/preview', target: '/usr/share/nginx/html', readOnly: true },
      ],
      environmentVariables: [{ name: 'APP_ENV', value: 'test' }],
      resources: { cpus: 0.5, memoryMiB: 512, memoryReservationMiB: 256 },
      labels: { app: 'preview-api' },
      config: { command: 'nginx -g daemon off;' },
    }) satisfies DockerContainerStartInput

    expect(payload).toMatchObject({
      hostId: 'host-1',
      architecture: 'arm64',
      containerPort: 80,
      hostPort: 18080,
      domainScheme: 'https',
      ports: [
        { hostPort: 18080, containerPort: 80, protocol: 'tcp' },
        { hostPort: 18081, containerPort: 8080, exposureScope: 'vpn' },
      ],
      volumes: [{ source: '/data/preview', target: '/usr/share/nginx/html', readOnly: true }],
      environmentVariables: [{ name: 'APP_ENV', value: 'test' }],
      resources: {
        cpus: 0.5,
        memoryBytes: 512 * 1024 ** 2,
        memoryReservationBytes: 256 * 1024 ** 2,
      },
      labels: { app: 'preview-api' },
      config: { command: 'nginx -g daemon off;' },
    })
    expect(payload).not.toHaveProperty('resources.memoryMiB')
    expect(payload).not.toHaveProperty('resources.memoryReservationMiB')
  })

  it('builds Git Dockerfile source payload and pins local image pull policy', () => {
    const payload = buildContainerStartPayload({
      hostId: 'host-1',
      name: 'preview-api',
      image: 'preview-api:git-main',
      sourceKind: 'git_dockerfile',
      gitBuild: {
        repositoryUrl: 'https://github.com/opensoha/example.git',
        ref: 'feature/runtime',
        dockerfilePath: 'deploy/Dockerfile',
        contextDir: '.',
        pull: true,
      },
      ports: [{ hostPort: 18080, containerPort: 8080 }],
    }) satisfies DockerContainerStartInput

    expect(payload).toMatchObject({
      sourceKind: 'git_dockerfile',
      image: 'preview-api:git-main',
      imagePullPolicy: 'never',
      gitBuild: {
        repositoryUrl: 'https://github.com/opensoha/example.git',
        ref: 'feature/runtime',
        dockerfilePath: 'deploy/Dockerfile',
        contextDir: '.',
        pull: true,
        noCache: false,
      },
    })
  })

  it('builds template payload with typed variables', () => {
    const payload = buildTemplatePayload({
      name: 'nginx-compose',
      templateKind: '',
      enabled: undefined,
      variables: { image: 'nginx:alpine', replicas: 1, tls: false },
    }) satisfies DockerTemplateInput

    expect(payload).toMatchObject({
      name: 'nginx-compose',
      templateKind: 'compose',
      enabled: true,
      variables: { image: 'nginx:alpine', replicas: 1, tls: false },
    })
  })

  it('defaults runtime-host onboarding to quick Agent installation', async () => {
    await renderWithProviders(
      <RuntimeHostModal onClose={() => undefined} open />,
      '/compute/runtimes/hosts',
    )

    expect(document.body.textContent).toContain('接入运行时主机')
    expect(document.body.textContent).toContain('快速安装 Agent')
    expect(document.body.textContent).toContain('已有 Agent')
    expect(document.body.textContent).not.toContain('Agent Endpoint')
    const labels = Array.from(document.querySelectorAll('label')).map((item) => item.textContent)
    expect(labels).not.toContain('Agent ID')
    expect(labels).not.toContain('IP 地址')
    expect(labels).not.toContain('Docker 版本')
    expect(labels).not.toContain('Compose 版本')
    expect(labels).not.toContain('架构')
    expect(document.body.textContent).not.toContain('虚拟化连接')
    expect(document.body.textContent).not.toContain('启动源')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/virtualization/clusters')

    const manualOption = Array.from(document.querySelectorAll('.ant-segmented-item')).find((item) =>
      item.textContent?.includes('已有 Agent'),
    ) as HTMLElement | undefined
    await act(async () => manualOption?.click())
    expect(document.body.textContent).toContain('Agent Endpoint')

    expect(document.querySelector('.soha-step-form__steps')).toBeNull()
    expect(document.body.textContent).toContain('关联虚拟机')
    expect(document.querySelector('#availablePortStart')).not.toBeNull()
    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/vms?page=1&pageSize=500')
  })

  it.each([
    ['templates', '新增模板', () => <DockerTemplatesPage />, 'composeContent', 'services:'],
    ['ports', '新增映射', () => <PortsTable />, 'hostPort', ''],
  ] as const)(
    'opens %s with defaults and validates the single form',
    async (resource, label, page, field, value) => {
      testState.permissionSnapshot.permissionKeys = [
        `docker.${resource}.view`,
        `docker.${resource}.create`,
      ]
      await renderWithProviders(<StrictMode>{page()}</StrictMode>, `/compute/runtimes/${resource}`)
      await clickButton(label)
      expect(document.querySelector('.soha-step-form__steps')).toBeNull()
      const input = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(`#${field}`)
      expect(input).not.toBeNull()
      expect(input?.value).toContain(value)
      if (resource === 'templates') {
        expect(document.querySelector('.ant-modal #enabled')?.getAttribute('aria-checked')).toBe(
          'true',
        )
      } else {
        expect(document.querySelector('.ant-modal')?.textContent).toContain('tcp')
        expect(document.querySelector('.ant-modal')?.textContent).toContain('内部')
      }
      await act(async () => {
        document
          .querySelector('.ant-modal form')
          ?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(testState.apiPost).not.toHaveBeenCalled()
      expect(document.querySelector('#name')?.getAttribute('aria-invalid')).toBe('true')
      if (resource === 'ports') {
        expect(document.querySelector('#hostPort')?.getAttribute('aria-invalid')).toBe('true')
        expect(document.querySelector('#containerPort')?.getAttribute('aria-invalid')).toBe('true')
      }
    },
  )

  it('does not carry a rejected template save into a fresh editor', async () => {
    testState.permissionSnapshot.permissionKeys = [
      'docker.templates.view',
      'docker.templates.create',
    ]
    testState.apiPost.mockRejectedValueOnce(new Error('template save rejected'))
    await renderWithProviders(<DockerTemplatesPage />, '/compute/runtimes/templates')
    await clickButton('新增模板')
    await changeInput(
      document.querySelector<HTMLInputElement>('.ant-modal #name'),
      'template-first',
    )
    await act(async () => {
      document.querySelector<HTMLButtonElement>('.ant-modal-footer .ant-btn-primary')?.click()
      await new Promise((resolve) => setTimeout(resolve, 30))
    })
    expect(testState.apiPost).toHaveBeenCalledWith(
      '/docker/templates',
      expect.objectContaining({ name: 'template-first' }),
    )
    expect(document.querySelector('.ant-modal')?.textContent).toContain('template save rejected')
    await act(async () => document.querySelector<HTMLButtonElement>('.ant-modal-close')?.click())
    await clickButton('新增模板')
    expect(document.querySelector('.ant-modal')?.textContent).not.toContain(
      'template save rejected',
    )
    expect(document.querySelector<HTMLInputElement>('.ant-modal #name')?.value).toBe('')
  })

  it('removes the new host when Agent installation command generation fails', async () => {
    testState.apiPost.mockImplementation(async (path: string) => {
      if (path === '/docker/hosts') return { data: { id: 'host-created', name: 'runtime-a' } }
      if (path === '/docker/hosts/host-created/agent-installation') {
        throw new Error(
          'Soha 对外访问地址尚未配置，请前往“设置中心 > 运行时配置”设置“访问地址”后重试',
        )
      }
      throw new Error(`Unhandled POST ${path}`)
    })

    await renderWithProviders(
      <RuntimeHostModal onClose={() => undefined} open />,
      '/compute/runtimes/hosts',
    )
    await changeInput(document.querySelector<HTMLInputElement>('#name'), 'runtime-a')
    await clickButton('生成安装命令')
    await act(async () => {
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(testState.apiDelete).toHaveBeenCalledWith('/docker/hosts/host-created')
    expect(document.body.textContent).toContain('Soha 对外访问地址尚未配置')
    expect(document.body.textContent).not.toContain('Agent 安装命令生成失败')
    expect(document.body.textContent).toContain('临时主机记录已自动清理')
  })

  it('maps existing-host resource sizes to bytes', () => {
    const payload = buildRuntimeHostPayload({
      connectionMode: 'quick',
      name: 'existing-host',
      cpuCoreCount: 6,
      memoryGiB: 12,
      diskGiB: 96,
      virtualizationConnectionId: 'conn-pve',
      vmId: 'vm-1',
      vmName: 'docker-vm',
    })

    expect(payload).toMatchObject({
      name: 'existing-host',
      cpuCoreCount: 6,
      memoryBytes: 12 * 1024 ** 3,
      diskBytes: 96 * 1024 ** 3,
      virtualizationConnectionId: 'conn-pve',
      vmId: 'vm-1',
      vmName: 'docker-vm',
    })
    expect(payload).not.toHaveProperty('connectionMode')
  })

  it('opens one existing-host onboarding action', async () => {
    await renderWithProviders(<DockerHostsPage />, '/compute/runtimes/hosts')

    const createButton = Array.from(document.querySelectorAll('button')).find(
      (button) => button.textContent === '新增主机',
    )
    expect(createButton).toBeTruthy()
    await act(async () => createButton?.click())

    expect(document.body.textContent).toContain('接入运行时主机')
    expect(document.body.textContent).not.toContain('从虚拟化资源构建')
  })

  it('aggregates compose and single-container projects in one tree table', async () => {
    testState.permissionSnapshot = {
      permissionKeys: [
        'docker.projects.view',
        'docker.projects.create',
        'docker.projects.update',
        'docker.projects.delete',
        'docker.projects.deploy',
        'docker.ports.create',
        'docker.ports.update',
        'docker.ports.delete',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    await renderWithProviders(<DockerProjectsPage />)

    expect(document.querySelector('.soha-docker-management-tabs')).toBeNull()
    expect(document.body.textContent).toContain('soha-compose-stack')
    expect(document.body.textContent).toContain('soha-orbstack-smoke')
    expect(document.body.textContent).toContain('Compose')
    expect(document.body.textContent).toContain('单容器')
    const tableHeaders = Array.from(document.querySelectorAll('th')).map((item) =>
      item.textContent?.trim(),
    )
    expect(tableHeaders).toContain('项目')
    expect(tableHeaders).toContain('服务')
    expect(tableHeaders).not.toContain('服务 / 项目')
    expect(tableHeaders).toContain('镜像')
    expect(tableHeaders).toContain('端口')
    expect(tableHeaders).not.toContain('镜像 / 端口')
    expect(document.body.textContent).toContain('nginx:alpine')
    expect(document.body.textContent).toContain('127.0.0.1:18083 -> 80/tcp')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/docker/hosts?page=1&pageSize=200')
  })

  it('reviews a redeploy plan before idempotent execution', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['docker.projects.view', 'docker.projects.deploy'],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    testState.apiPost.mockResolvedValueOnce({
      data: {
        capability: 'docker.projects.deploy.trigger',
        target: 'project-1',
        ready: true,
        riskLevel: 'execute',
        requiresApproval: true,
        changes: [
          {
            action: 'redeploy',
            resource: 'docker-project',
            summary: '销毁并重建 soha-orbstack-smoke，保留数据卷。',
            sensitiveValuesRedacted: false,
          },
        ],
        warnings: ['将重新拉取镜像。'],
      },
    } as never)
    await renderWithProviders(<DockerProjectsPage />)

    const redeployButton = document.querySelector(
      'button[aria-label="销毁重建应用"]',
    ) as HTMLButtonElement | null
    expect(redeployButton).not.toBeNull()
    expect(document.querySelectorAll('button[aria-label="销毁重建应用"]')).toHaveLength(1)
    await act(async () => {
      redeployButton?.click()
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(testState.apiPost).toHaveBeenCalledWith('/docker/projects/project-1/deploy/plan', {
      action: 'redeploy',
    })
    expect(document.body.textContent).toContain('销毁并重建 soha-orbstack-smoke')
    expect(document.body.textContent).toContain('将重新拉取镜像')
    const approvalCheckbox = document.querySelector(
      '.ant-modal input[type="checkbox"]',
    ) as HTMLInputElement | null
    expect(approvalCheckbox).not.toBeNull()
    await act(async () => approvalCheckbox?.click())
    const confirmButton = Array.from(document.querySelectorAll('.ant-modal-footer button')).find(
      (button) => button.textContent?.includes('销毁重建'),
    ) as HTMLButtonElement | undefined
    expect(confirmButton).toBeDefined()
    await act(async () => {
      confirmButton?.click()
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(testState.apiPostWithHeaders).toHaveBeenCalledWith(
      '/docker/projects/project-1/deploy',
      { action: 'redeploy' },
      expect.objectContaining({ 'Idempotency-Key': expect.any(String) }),
    )
  })

  it('opens quick start as a four-step flow and reveals Git Dockerfile fields', async () => {
    testState.permissionSnapshot = {
      permissionKeys: [
        'docker.projects.view',
        'docker.projects.create',
        'docker.projects.update',
        'docker.projects.delete',
        'docker.projects.deploy',
        'docker.ports.create',
        'docker.ports.update',
        'docker.ports.delete',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    await renderWithProviders(<DockerProjectsPage />)

    const quickStartButton = Array.from(document.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('快速启动'),
    )
    expect(quickStartButton).toBeDefined()
    await act(async () => {
      quickStartButton?.click()
      await Promise.resolve()
    })

    expect(document.body.textContent).toContain('来源')
    expect(document.body.textContent).toContain('运行配置')
    expect(document.body.textContent).toContain('网络与存储')
    expect(document.body.textContent).toContain('确认启动')
    expect(document.querySelector('.ant-segmented-item-selected')?.textContent).toContain(
      '已有镜像',
    )
    const activeStep = document.querySelector('.soha-step-form__content > div:not([hidden])')
    expect(
      Array.from(activeStep?.querySelectorAll('.soha-docker-quick-form-section h2') ?? []).map(
        (item) => item.textContent,
      ),
    ).toEqual(['来源方式', '应用与目标', '镜像与构建'])
    const gitSegment = Array.from(document.querySelectorAll('.ant-segmented-item')).find((item) =>
      item.textContent?.includes('Git 构建'),
    ) as HTMLElement | undefined
    expect(gitSegment).toBeDefined()
    await act(async () => {
      gitSegment?.click()
      await Promise.resolve()
    })

    expect(document.body.textContent).toContain('Git 仓库')
    expect(document.body.textContent).toContain('分支 / Tag / Commit')
    expect(document.body.textContent).toContain('Dockerfile')
    expect(document.body.textContent).toContain('构建目录')
  })

  it('keeps Compose source configuration in three focused steps', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['docker.projects.view', 'docker.projects.create'],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    await renderWithProviders(<DockerProjectsPage />)

    const createButton = Array.from(document.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('创建 Compose'),
    )
    expect(createButton).toBeDefined()
    await act(async () => {
      createButton?.click()
      await Promise.resolve()
    })

    const dialog = Array.from(document.querySelectorAll('[role="dialog"]')).find((item) =>
      item.querySelector('.ant-modal-title')?.textContent?.includes('创建 Compose 项目'),
    )
    const modalTitle = dialog?.querySelector('.ant-modal-title')
    expect(modalTitle?.textContent).toBe('创建 Compose 项目')
    expect(modalTitle?.id).toBeTruthy()
    expect(dialog?.getAttribute('aria-labelledby')?.split(/\s+/)).toContain(modalTitle?.id)
    expect(
      (dialog?.querySelector('.ant-modal-header') as HTMLElement | null)?.style.minHeight,
    ).toBe('32px')
    expect((modalTitle as HTMLElement | null)?.style.position).not.toBe('absolute')
    expect(dialog?.closest('.ant-modal-wrap')?.classList.contains('ant-modal-centered')).toBe(false)
    expect((dialog as HTMLElement | undefined)?.style.top).toBe('32px')

    const stepTitles = Array.from(
      document.querySelectorAll('.soha-step-form__steps .ant-steps-item-title'),
    )
      .map((item) => item.textContent?.trim())
      .filter(Boolean)
    expect(stepTitles).toEqual(['基础信息', '项目设置', '部署来源'])

    const stepPanels = Array.from(
      document.querySelectorAll('.soha-step-form__content > div'),
    ) as HTMLElement[]
    expect(stepPanels).toHaveLength(3)
    expect(stepPanels[0]?.textContent).toContain('Docker 主机')
    expect(stepPanels[0]?.textContent).toContain('描述')
    expect(stepPanels[0]?.textContent).not.toContain('Slug')
    expect(stepPanels[1]?.textContent).toContain('负责人')
    expect(stepPanels[1]?.textContent).toContain('TTL 秒数')
    expect(stepPanels[1]?.textContent).not.toContain('目标态')
    expect(stepPanels[1]?.textContent).not.toContain('状态')
    expect(stepPanels[2]?.textContent).toContain('在线编辑')
    expect(stepPanels[2]?.textContent).toContain('在线获取')
    expect(stepPanels[2]?.textContent).toContain('Git 仓库')
    expect(stepPanels[2]?.textContent).toContain('项目模板')
    expect(stepPanels[2]?.textContent).toContain('.env')
    expect(stepPanels[2]?.textContent).not.toContain('来源引用')
    expect(stepPanels[2]?.textContent).not.toContain('模板 ID')

    const ttlInput = dialog?.querySelector('#ttlSeconds') as HTMLInputElement | null
    expect(ttlInput?.value).toBe('600')

    const urlSegment = Array.from(dialog?.querySelectorAll('.ant-segmented-item') ?? []).find(
      (item) => item.textContent?.includes('在线获取'),
    ) as HTMLElement | undefined
    await act(async () => {
      urlSegment?.click()
      await Promise.resolve()
    })
    expect(dialog?.textContent).toContain('Compose URL')

    const templateSegment = Array.from(dialog?.querySelectorAll('.ant-segmented-item') ?? []).find(
      (item) => item.textContent?.includes('项目模板'),
    ) as HTMLElement | undefined
    await act(async () => {
      templateSegment?.click()
      await Promise.resolve()
    })
    const templateStepTitles = Array.from(
      dialog?.querySelectorAll('.soha-step-form__steps .ant-steps-item-title') ?? [],
    ).map((item) => item.textContent?.trim())
    expect(templateStepTitles).toEqual(['基础信息', '项目设置', '部署来源'])
  })

  it('expands compose projects with their services when service access is available', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['docker.projects.view', 'docker.services.view'],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    const container = await renderWithProviders(<DockerProjectsPage />)

    expect(container.textContent).toContain('soha-compose-stack')
    expect(container.textContent).toContain('web')
    expect(container.textContent).toContain('compose-web-1')
    expect(testState.apiGet).toHaveBeenCalledWith(
      '/docker/services?projectId=project-compose&page=1&pageSize=100',
    )
    expect(testState.apiGet).not.toHaveBeenCalledWith('/docker/services?page=1&pageSize=300')
    expect(
      container.querySelector('[data-row-key="project-compose:service:service-compose-web"]'),
    ).not.toBeNull()
  })

  it('fails closed when the Docker module is disabled', async () => {
    testState.modules = {
      docker: false,
      virtualization: true,
    }
    testState.permissionSnapshot = {
      permissionKeys: [
        'docker.projects.view',
        'docker.projects.create',
        'docker.projects.update',
        'docker.projects.delete',
        'docker.projects.deploy',
        'docker.services.view',
        'docker.services.create',
        'docker.services.update',
        'docker.services.delete',
        'docker.ports.create',
        'docker.ports.update',
        'docker.ports.delete',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    await renderWithProviders(<DockerProjectsPage />)

    expect(testState.apiGet).toHaveBeenCalledWith('/modules')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/docker/projects?page=1&pageSize=15')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/docker/hosts?page=1&pageSize=200')
    expect(document.body.textContent).not.toContain('创建 Compose')
    expect(document.body.textContent).not.toContain('快速启动')
  })

  it('hides Docker runtime tabs and does not fetch runtime data without service permissions', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['docker.projects.view'],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    const container = await renderWithProviders(
      <Routes>
        <Route path="/docker/projects/:projectId" element={<DockerProjectDetailPage />} />
      </Routes>,
      '/docker/projects/project-1',
    )

    const tabTexts = Array.from(container.querySelectorAll('.ant-tabs-tab-btn'))
      .map((node) => node.textContent?.trim())
      .filter(Boolean)
    expect(container.querySelector('.soha-management-detail-header')).not.toBeNull()
    expect(tabTexts).toContain('资源关系')
    expect(container.querySelector('.soha-resource-tabs')).not.toBeNull()
    expect(container.querySelector('.soha-workload-detail-tabs')).not.toBeNull()
    expect(tabTexts).toContain('概览')
    expect(tabTexts).toContain('配置')
    expect(tabTexts).not.toContain('日志')
    expect(tabTexts).not.toContain('Shell')
    expect(tabTexts).not.toContain('卷文件')
    expect(container.textContent).toContain('Docker 主机')
    expect(container.textContent).not.toContain('项目概览')
    expect(container.textContent).not.toContain('容器状态')
    expect(testState.apiGet).toHaveBeenCalledWith('/docker/projects/project-1')
    expect(testState.apiGet).not.toHaveBeenCalledWith(
      '/docker/services?projectId=project-1&page=1&pageSize=100',
    )
    expect(testState.apiGet.mock.calls.some(([path]) => String(path).includes('/runtime/'))).toBe(
      false,
    )
  })

  it.each([
    ['view only', ['docker.projects.view', 'docker.services.view']],
    [
      'view and legacy manage',
      ['docker.projects.view', 'docker.services.view', 'docker.services.manage'],
    ],
    ['files without inventory', ['docker.projects.view', 'docker.services.files']],
  ])('does not load volume files with %s permissions', async (_name, permissionKeys) => {
    testState.permissionSnapshot = {
      permissionKeys: permissionKeys as string[],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    const container = await renderWithProviders(
      <Routes>
        <Route path="/docker/projects/:projectId" element={<DockerProjectDetailPage />} />
      </Routes>,
      '/docker/projects/project-1',
    )
    expect(
      Array.from(container.querySelectorAll('.ant-tabs-tab-btn')).map((node) =>
        node.textContent?.trim(),
      ),
    ).not.toContain('卷文件')
    expect(
      testState.apiGet.mock.calls.some(([path]) => String(path).includes('/runtime/volume')),
    ).toBe(false)
  })

  it('opens volume files with explicit inventory and file permissions', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['docker.projects.view', 'docker.services.view', 'docker.services.files'],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    const container = await renderWithProviders(
      <Routes>
        <Route path="/docker/projects/:projectId" element={<DockerProjectDetailPage />} />
      </Routes>,
      '/docker/projects/project-1',
    )
    const volumesTab = Array.from(
      container.querySelectorAll<HTMLElement>('.ant-tabs-tab-btn'),
    ).find((node) => node.textContent?.trim() === '卷文件')
    expect(volumesTab).toBeDefined()
    expect(
      testState.apiGet.mock.calls.some(([path]) => String(path).includes('/runtime/volume')),
    ).toBe(false)
    await act(async () => volumesTab?.click())
    await vi.waitFor(async () => {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(testState.apiGet).toHaveBeenCalledWith(
        '/docker/projects/project-1/runtime/volumes?serviceName=web',
      )
      expect(testState.apiGet).toHaveBeenCalledWith(
        '/docker/projects/project-1/runtime/volume-files?serviceName=web&target=%2Fdata&path=%2F&limit=300',
      )
      expect(container.querySelector('.soha-docker-volume-file-row')?.textContent).toContain(
        'readme.txt',
      )
    })
    await act(async () =>
      container.querySelector<HTMLButtonElement>('.soha-docker-volume-file-row')?.click(),
    )
    await vi.waitFor(async () => {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(testState.apiGet).toHaveBeenCalledWith(
        '/docker/projects/project-1/runtime/volume-file?serviceName=web&target=%2Fdata&path=%2Freadme.txt&limitBytes=262144',
      )
      expect(
        container.querySelector<HTMLTextAreaElement>('.soha-docker-volume-preview textarea')?.value,
      ).toBe('synthetic file')
    })
  })

  it('gates project overview services and log tabs with their matching permissions', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['docker.projects.view', 'docker.services.view'],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    let container = await renderWithProviders(
      <Routes>
        <Route path="/docker/projects/:projectId" element={<DockerProjectDetailPage />} />
      </Routes>,
      '/docker/projects/project-1',
    )
    let tabTexts = Array.from(container.querySelectorAll('.ant-tabs-tab-btn')).map((node) =>
      node.textContent?.trim(),
    )
    expect(tabTexts).not.toContain('服务')
    expect(tabTexts).not.toContain('日志')
    expect(container.textContent).toContain('容器状态')
    expect(container.textContent).not.toContain('项目概览')
    expect(container.textContent).toContain('Docker 主机')
    expect(container.textContent).toContain('容器 1')
    expect(
      container.querySelector('[role="tabpanel"][aria-hidden="false"] .soha-admin-table-shell'),
    ).not.toBeNull()
    expect(testState.apiGet).toHaveBeenCalledWith(
      '/docker/services?page=1&pageSize=5&projectId=project-1',
    )

    container.remove()
    testState.permissionSnapshot = {
      permissionKeys: ['docker.projects.view', 'docker.services.logs'],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    container = await renderWithProviders(
      <Routes>
        <Route path="/docker/projects/:projectId" element={<DockerProjectDetailPage />} />
      </Routes>,
      '/docker/projects/project-1',
    )
    tabTexts = Array.from(container.querySelectorAll('.ant-tabs-tab-btn')).map((node) =>
      node.textContent?.trim(),
    )
    expect(tabTexts).not.toContain('服务')
    expect(tabTexts).toContain('日志')
  })

  it('omits list filters from project overview services and port tabs', async () => {
    testState.permissionSnapshot = {
      permissionKeys: [
        'docker.projects.view',
        'docker.services.view',
        'docker.services.logs',
        'docker.ports.view',
        'docker.ports.create',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    const container = await renderWithProviders(
      <Routes>
        <Route path="/docker/projects/:projectId" element={<DockerProjectDetailPage />} />
      </Routes>,
      '/docker/projects/project-1',
    )

    expect(
      container.querySelector('[role="tabpanel"][aria-hidden="false"] .soha-vrt-query'),
    ).toBeNull()
    expect(
      container.querySelector('[role="tabpanel"][aria-hidden="false"] .soha-admin-table-shell'),
    ).not.toBeNull()

    const portTab = Array.from(container.querySelectorAll<HTMLElement>('.ant-tabs-tab-btn')).find(
      (item) => item.textContent?.trim() === '端口映射',
    )
    expect(portTab).toBeDefined()
    await act(async () => portTab?.click())
    expect(
      container.querySelector('[role="tabpanel"][aria-hidden="false"] .soha-vrt-query'),
    ).toBeNull()
    expect(
      container.querySelector('[role="tabpanel"][aria-hidden="false"] .soha-admin-table-shell'),
    ).not.toBeNull()

    expect(container.textContent).toContain('新增映射')
  })
})
