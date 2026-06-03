/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from 'antd'
import { DockerHostsPage, DockerProjectsPage, buildContainerStartPayload, buildQuickHostPayload } from './docker-pages'

const testState = vi.hoisted(() => ({
  permissionSnapshot: {
    permissionKeys: ['docker.hosts.manage'],
    visibleMenuIds: [],
    visibleMenus: [],
  },
  apiGet: vi.fn(async (path: string) => {
    if (path === '/docker/hosts?page=1&pageSize=10') {
      return { data: { items: [], total: 0, page: 1, pageSize: 10 } }
    }
    if (path === '/docker/hosts?page=1&pageSize=200') {
      return { data: { items: [{ id: 'host-1', name: 'local-orbstack', architecture: 'arm64' }], total: 1, page: 1, pageSize: 200 } }
    }
    if (path === '/docker/projects?page=1&pageSize=200') {
      return { data: { items: [{ id: 'project-1', hostId: 'host-1', name: 'soha-orbstack-smoke', sourceKind: 'single_container' }], total: 1, page: 1, pageSize: 200 } }
    }
    if (path === '/docker/services?page=1&pageSize=300') {
      return { data: { items: [], total: 0, page: 1, pageSize: 300 } }
    }
    if (path === '/docker/projects?page=1&pageSize=10&sourceKind=compose') {
      return { data: { items: [], total: 0, page: 1, pageSize: 10 } }
    }
    if (path === '/docker/projects?page=1&pageSize=10&sourceKind=single_container') {
      return {
        data: {
          items: [{
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
              ports: [{ hostIp: '127.0.0.1', hostPort: 18083, containerPort: 80, protocol: 'tcp' }],
            },
          }],
          total: 1,
          page: 1,
          pageSize: 10,
        },
      }
    }
    if (path === '/virtualization/clusters') {
      return { data: [
        { id: 'conn-pve', name: 'pve-a', provider: 'pve', enabled: true, config: { defaultBridge: 'vmbr0' } },
        { id: 'conn-kv', name: 'kubevirt-a', provider: 'kubevirt', enabled: true, config: { backendUrl: 'https://kube.example:6443' } },
      ] }
    }
    if (path === '/virtualization/images?page=1&pageSize=500') {
      return { data: { items: [
        { id: 'image-pve-template', name: 'ubuntu-template', provider: 'pve', connectionId: 'conn-pve', sourceKind: 'template', sourceRef: '9000' },
        { id: 'image-kv', name: 'ubuntu-ds', provider: 'kubevirt', connectionId: 'conn-kv', sourceKind: 'datasource', sourceRef: 'default/ubuntu' },
      ], total: 2, page: 1, pageSize: 500 } }
    }
    if (path === '/virtualization/flavors') {
      return { data: [{ id: 'flavor-1', name: 'standard-2c4g', cpu: 2, memoryMiB: 4096, diskGiB: 40, enabled: true }] }
    }
    throw new Error(`Unhandled GET ${path}`)
  }),
  apiPost: vi.fn(async (_path: string, _body?: unknown) => ({ data: { id: 'operation-1' } })),
  apiPut: vi.fn(async (_path: string, _body?: unknown) => ({ data: { id: 'updated' } })),
  apiDelete: vi.fn(async (_path: string) => ({ data: undefined })),
}))

vi.mock('@/features/auth/permission-snapshot', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, key: string) => snapshot?.permissionKeys?.includes(key) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: testState.permissionSnapshot },
    isLoading: false,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: (path: string) => testState.apiGet(path),
    post: (path: string, body?: unknown) => testState.apiPost(path, body),
    put: (path: string, body?: unknown) => testState.apiPut(path, body),
    delete: (path: string) => testState.apiDelete(path),
  },
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function renderWithProviders(node: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <App>{node}</App>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await flush()
  return container
}

async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function clickButtonByText(text: string) {
  const button = Array.from(document.querySelectorAll('button')).find((node) => node.textContent?.includes(text))
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error(`button not found by text: ${text}`)
  }
  await act(async () => {
    button.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await Promise.resolve()
  })
}

describe('docker pages', () => {
  beforeEach(() => {
    testState.permissionSnapshot = {
      permissionKeys: ['docker.hosts.manage'],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    testState.apiGet.mockClear()
    testState.apiPost.mockClear()
    testState.apiPut.mockClear()
    testState.apiDelete.mockClear()
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
    vi.stubGlobal('matchMedia', vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })))
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

  it('builds quick host payload with virtualization resource ids and GiB sizing', () => {
    expect(buildQuickHostPayload({
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
    })).toMatchObject({
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
  })

  it('builds structured container start payload for quick Docker app launch', () => {
    expect(buildContainerStartPayload({
      hostId: 'host-1',
      name: 'preview-api',
      image: 'nginx:alpine',
      architecture: 'arm64',
      restartPolicy: 'unless-stopped',
      ports: [
        { name: 'http', hostIp: '0.0.0.0', hostPort: 18080, containerPort: 80, protocol: 'tcp', exposureScope: 'internal', domainName: 'preview.internal.example.com', domainScheme: 'https', domainTlsEnabled: true },
        { name: 'admin', hostIp: '127.0.0.1', hostPort: 18081, containerPort: 8080, protocol: 'tcp', exposureScope: 'vpn' },
      ],
      volumes: [{ type: 'bind', source: '/data/preview', target: '/usr/share/nginx/html', readOnly: true }],
      environmentVariables: [{ name: 'APP_ENV', value: 'test' }],
      resources: { cpus: 0.5, memoryMiB: 512, memoryReservationMiB: 256 },
    })).toMatchObject({
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
      resources: { cpus: 0.5, memoryBytes: 512 * 1024 ** 2, memoryReservationBytes: 256 * 1024 ** 2 },
    })
  })

  it('loads virtualization resources for the Docker host quick-create drawer', async () => {
    await renderWithProviders(<DockerHostsPage />)
    await clickButtonByText('虚拟化快速构建')
    await flush()

    expect(document.body.textContent).toContain('虚拟化快速构建 Docker 主机')
    expect(document.body.textContent).toContain('虚拟化连接')
    expect(document.body.textContent).toContain('镜像 / 模板')
    expect(document.body.textContent).toContain('规格')
    expect(document.body.textContent).toContain('Cloud-init 用户数据')
    expect(document.body.textContent).not.toContain('PVE 连接 ID')
    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/clusters')
    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/images?page=1&pageSize=500')
    expect(testState.apiGet).toHaveBeenCalledWith('/virtualization/flavors')
  })

  it('keeps port mappings in container detail context and summarizes them in the single-container table', async () => {
    testState.permissionSnapshot = {
      permissionKeys: ['docker.projects.view', 'docker.projects.manage', 'docker.projects.deploy', 'docker.ports.manage'],
      visibleMenuIds: [],
      visibleMenus: [],
    }
    await renderWithProviders(<DockerProjectsPage />)

    let tabTexts = Array.from(document.querySelectorAll('.ant-tabs-tab-btn')).map((node) => node.textContent?.trim()).filter(Boolean)
    expect(tabTexts).toEqual(['Compose', '单容器服务'])

    const singleContainerTab = Array.from(document.querySelectorAll('.ant-tabs-tab-btn'))
      .find((node) => node.textContent?.trim() === '单容器服务') as HTMLElement | undefined
    expect(singleContainerTab).not.toBeUndefined()
    await act(async () => {
      singleContainerTab?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })
    await flush()

    tabTexts = Array.from(document.querySelectorAll('.ant-tabs-tab-btn')).map((node) => node.textContent?.trim()).filter(Boolean)
    expect(tabTexts).toEqual(['Compose', '单容器服务'])
    expect(document.body.textContent).toContain('端口映射')
    expect(document.body.textContent).toContain('127.0.0.1:18083 -> 80/tcp')
  })
})
