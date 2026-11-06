/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { SoftwareLibraryPage } from './page'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  getEnvelope: vi.fn(),
  patch: vi.fn(),
  post: vi.fn(),
  upload: vi.fn(),
}))

vi.mock('@/features/auth', () => ({
  hasPermission: () => true,
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: [
          'software.package.view',
          'software.package.create',
          'software.package.delete',
          'settings.system-integrations.view',
          'settings.system-integrations.create',
          'settings.system-integrations.update',
          'settings.system-integrations.test',
        ],
      },
    },
  }),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns = [],
    dataSource = [],
    empty,
    headerExtra,
  }: {
    columns?: Array<{
      dataIndex?: string
      render?: (value: unknown, record: Record<string, unknown>, index: number) => unknown
    }>
    dataSource?: Array<Record<string, unknown>>
    empty?: React.ReactNode
    headerExtra?: React.ReactNode
  }) => {
    const firstColumn = columns[0]
    return (
      <div data-testid="admin-table">
        {headerExtra}
        {dataSource.map((record, index) => (
          <div key={String(record.id ?? index)}>
            {
              (firstColumn?.render?.(
                firstColumn.dataIndex ? record[firstColumn.dataIndex] : undefined,
                record,
                index,
              ) ?? null) as ReactNode
            }
          </div>
        ))}
        {dataSource.length === 0 ? empty : null}
      </div>
    )
  },
}))

const roots: Root[] = []
const defaultIntegration = {
  id: 'storage-1',
  category: 'storage',
  providerType: 's3',
  name: 'Local MinIO',
  enabled: true,
  configuration: [
    { key: 'endpoint', value: 'http://127.0.0.1:9000' },
    { key: 'bucket', value: 'soha-software' },
    { key: 'region', value: 'us-east-1' },
  ],
  credentialKeys: ['access_key_id', 'secret_access_key'],
  healthStatus: 'healthy',
  version: 1,
  createdAt: '2026-08-25T00:00:00Z',
  updatedAt: '2026-08-25T00:00:00Z',
}

beforeAll(() => {
  const getComputedStyle = window.getComputedStyle.bind(window)
  Object.defineProperty(window, 'getComputedStyle', {
    configurable: true,
    value: (element: Element) => getComputedStyle(element),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
})

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.getEnvelope.mockImplementation((path: string) =>
    path.startsWith('/software/storage')
      ? Promise.resolve({
          data: {
            backend: 's3',
            bucket: 'soha-software',
            objectCount: 0,
            totalBytes: 0,
            items: [],
          },
        })
      : path.startsWith('/system-integrations')
        ? Promise.resolve({ items: [defaultIntegration] })
        : Promise.resolve({ items: [] }),
  )
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function renderPage() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={client}>
          <SoftwareLibraryPage />
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await flush()
  return container
}

async function openObjectStorageDrawer(container: HTMLElement) {
  const button = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
    (item) => item.textContent?.trim() === '对象存储',
  )
  await act(async () => button?.click())
  await flush()
}

describe('SoftwareLibraryPage', () => {
  it('defaults to one package and gives every batch package its own metadata', async () => {
    const container = await renderPage()
    const publish = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.includes('上传软件'),
    )
    await act(async () => publish?.click())
    await flush()

    const modal = Array.from(
      document.body.querySelectorAll<HTMLElement>('.ant-modal-container'),
    ).find((item) => item.textContent?.includes('发布软件'))
    let fileInput = modal?.querySelector<HTMLInputElement>('input[type="file"]')
    expect(modal?.textContent).not.toContain('软件 ID')
    expect(fileInput?.multiple).toBe(false)
    expect(modal?.textContent).toContain('单个发布')
    expect(modal?.textContent).toContain('选择安装包')

    const multiMode = Array.from(
      modal?.querySelectorAll<HTMLElement>('.ant-segmented-item') ?? [],
    ).find((item) => item.textContent?.includes('批量发布'))
    await act(async () => multiMode?.click())
    await flush()

    fileInput = modal?.querySelector<HTMLInputElement>('input[type="file"]')
    expect(fileInput?.multiple).toBe(true)
    expect(modal?.textContent).toContain('选择多个安装包')

    const files = [
      new File(['mac'], 'soha-desktop-arm64.dmg'),
      new File(['win'], 'soha-desktop-amd64.exe'),
    ]
    Object.defineProperty(fileInput, 'files', { configurable: true, value: files })
    await act(async () => fileInput?.dispatchEvent(new Event('change', { bubbles: true })))
    await flush()
    const packages = modal?.querySelectorAll<HTMLElement>('.soha-software-batch-package') ?? []
    expect(packages).toHaveLength(2)
    expect(packages[0]?.textContent).toContain('soha-desktop-arm64.dmg')
    expect(packages[0]?.querySelector<HTMLInputElement>('input')?.value).toBe('soha-desktop-arm64')
    expect(packages[1]?.textContent).toContain('soha-desktop-amd64.exe')
    expect(packages[1]?.querySelector<HTMLInputElement>('input')?.value).toBe('soha-desktop-amd64')
    expect(modal?.textContent).toContain('批量上传并发布（2）')
  }, 15_000)

  it('switches the publish source to a server-side URL import', async () => {
    const container = await renderPage()
    const publish = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.includes('上传软件'),
    )
    await act(async () => publish?.click())
    await flush()

    const urlSource = Array.from(
      document.body.querySelectorAll<HTMLElement>('.ant-segmented-item'),
    ).find((item) => item.textContent?.includes('下载地址'))
    await act(async () => urlSource?.click())
    await flush()

    expect(
      document.body.querySelector('input[placeholder="https://downloads.example.com/app.dmg"]'),
    ).not.toBeNull()
    expect(document.body.textContent).toContain('下载并发布')
  })

  it('shows managed storage totals in the software library', async () => {
    apiMocks.getEnvelope.mockImplementation((path: string) =>
      path.startsWith('/software/storage')
        ? Promise.resolve({
            data: {
              backend: 's3',
              objectCount: path.includes('storageIntegrationId=storage-1') ? 2 : 3,
              totalBytes: path.includes('storageIntegrationId=storage-1') ? 2048 : 3072,
              items: [],
            },
          })
        : path.startsWith('/system-integrations')
          ? Promise.resolve({ items: [defaultIntegration] })
          : Promise.resolve({ items: [] }),
    )
    const container = await renderPage()
    expect(container.textContent).not.toContain('Local MinIO')
    expect(container.textContent).not.toContain('新增存储')
    expect(container.textContent).toContain('对象存储')
    expect(container.querySelector('.ant-tabs')).toBeNull()
    expect(apiMocks.getEnvelope).toHaveBeenCalledWith('/software/storage?limit=200')

    await openObjectStorageDrawer(container)

    expect(document.body.textContent).toContain('Local MinIO')
    expect(document.body.textContent).toContain('soha-software')
    expect(document.body.textContent).toContain('新增存储')
    expect(document.body.querySelector('[aria-label="连接正常"]')).not.toBeNull()
    expect(document.body.querySelector('.soha-status-tag')?.textContent).toBe('已启用')
    const drawer = document.body.querySelector('.ant-drawer-open')
    expect(drawer?.textContent).toContain('文件总数3')
    expect(drawer?.textContent).toContain('总已用空间3.0 KiB')
    const storageRow = Array.from(drawer?.querySelectorAll('tr') ?? []).find((row) =>
      row.textContent?.includes('Local MinIO'),
    )
    expect(storageRow?.textContent).toContain('2')
    expect(storageRow?.textContent).toContain('2.0 KiB')
    expect(apiMocks.getEnvelope).toHaveBeenCalledWith(
      '/software/storage?limit=200&storageIntegrationId=storage-1',
    )
  })

  it('opens object storage configuration in a modal', async () => {
    const container = await renderPage()
    await openObjectStorageDrawer(container)
    const configureButton = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>('button'),
    ).find((button) => button.textContent?.includes('新增存储'))
    await act(async () => configureButton?.click())
    await flush()

    expect(document.body.querySelector('.ant-modal-container')).not.toBeNull()
    expect(document.body.querySelector<HTMLElement>('.ant-modal-wrap')?.style.zIndex).toBe('1100')
    expect(document.body.textContent).toContain('连接信息')
    expect(document.body.textContent).toContain('访问凭据')
    expect(apiMocks.getEnvelope).toHaveBeenCalledWith(
      '/system-integrations?category=storage&providerType=s3',
    )
  })

  it('opens stored file metadata in a detail drawer', async () => {
    apiMocks.getEnvelope.mockImplementation((path: string) =>
      path.startsWith('/software/storage')
        ? Promise.resolve({
            data: {
              backend: 's3',
              bucket: 'soha-software',
              objectCount: 1,
              totalBytes: 2048,
              items: [],
            },
          })
        : path.includes('/download-records')
          ? Promise.resolve({
              items: [
                {
                  id: 'audit-1',
                  actorId: 'user-1',
                  actorName: 'OpenSoha',
                  downloadedAt: '2026-08-27T08:00:00Z',
                  durationMs: 245,
                  sourceIp: '127.0.0.1',
                },
              ],
            })
          : path.startsWith('/system-integrations')
            ? Promise.resolve({ items: [] })
            : Promise.resolve({
                items: [
                  {
                    id: 'package-1',
                    softwareId: 'soha-desktop',
                    name: 'Soha Desktop',
                    publisher: 'OpenSoha',
                    version: '1.0.0',
                    platform: 'darwin',
                    arch: 'arm64',
                    fileName: 'soha-desktop.dmg',
                    sizeBytes: 2048,
                    sha256: 'abcdef0123456789',
                    downloadPath: '/software/packages/package-1/download',
                    downloadCount: 3,
                    createdAt: '2026-08-25T00:00:00Z',
                    updatedAt: '2026-08-25T01:00:00Z',
                  },
                ],
              }),
    )

    const container = await renderPage()
    const fileButton = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.includes('soha-desktop.dmg'),
    )
    await act(async () => fileButton?.click())
    await flush()

    const drawer = document.body.querySelector('.ant-drawer-open')
    expect(drawer?.textContent).toContain('文件详情')
    expect(drawer?.textContent).toContain('abcdef0123456789')
    expect(drawer?.textContent).toContain('下载地址')
    expect(drawer?.textContent).toContain('/software/packages/package-1/download')
    expect(drawer?.textContent).toContain('下载次数')
    expect(drawer?.textContent).toContain('OpenSoha')
    expect(drawer?.textContent).toContain('127.0.0.1')
  })

  it('tests the configured S3-compatible connection without exposing credentials', async () => {
    const integration = {
      id: 'storage-1',
      category: 'storage',
      providerType: 's3',
      name: 'Software object storage',
      enabled: true,
      configuration: [
        { key: 'endpoint', value: 'https://minio.example.com' },
        { key: 'bucket', value: 'soha-software' },
        { key: 'region', value: 'us-east-1' },
      ],
      credentialKeys: ['access_key_id', 'secret_access_key'],
      healthStatus: 'unknown',
      version: 1,
      createdAt: '2026-08-25T00:00:00Z',
      updatedAt: '2026-08-25T00:00:00Z',
    }
    apiMocks.getEnvelope.mockImplementation((path: string) => {
      if (path.startsWith('/software/storage')) {
        return Promise.resolve({
          data: {
            backend: 's3',
            bucket: 'soha-software',
            objectCount: 0,
            totalBytes: 0,
            items: [],
          },
        })
      }
      if (path.startsWith('/system-integrations')) {
        return Promise.resolve({
          items: [integration],
        })
      }
      return Promise.resolve({ items: [] })
    })
    apiMocks.patch.mockResolvedValue({ data: integration })
    apiMocks.post.mockResolvedValue({
      data: {
        integrationId: 'storage-1',
        status: 'succeeded',
        checkedAt: '2026-08-25T00:00:00Z',
        latencyMs: 5,
        capabilities: ['object.get', 'object.put', 'object.delete'],
      },
    })

    const container = await renderPage()
    await openObjectStorageDrawer(container)
    const configureButton =
      container.querySelector<HTMLButtonElement>(
        'button[aria-label="编辑 Software object storage"]',
      ) ??
      document.body.querySelector<HTMLButtonElement>(
        'button[aria-label="编辑 Software object storage"]',
      )
    await act(async () => configureButton?.click())
    await flush()

    const testButton = Array.from(document.body.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.includes('保存并测试'),
    )
    await act(async () => testButton?.click())
    await flush()
    await flush()

    expect(apiMocks.patch).toHaveBeenCalledWith(
      '/system-integrations/storage-1',
      expect.any(Object),
    )
    expect(apiMocks.post).toHaveBeenCalledWith('/system-integrations/storage-1/test')
    expect(apiMocks.patch.mock.invocationCallOrder[0]).toBeLessThan(
      apiMocks.post.mock.invocationCallOrder[0],
    )
    expect(JSON.stringify([apiMocks.patch.mock.calls, apiMocks.post.mock.calls])).not.toContain(
      'secret_access_key',
    )
  })

  it('fails closed when object storage configuration cannot be loaded', async () => {
    apiMocks.getEnvelope.mockImplementation((path: string) =>
      path.startsWith('/system-integrations')
        ? Promise.reject(new Error('unavailable'))
        : Promise.resolve({
            data: { backend: 's3', objectCount: 0, totalBytes: 0, items: [] },
          }),
    )

    const container = await renderPage()
    await openObjectStorageDrawer(container)
    expect(document.body.textContent).toContain('对象存储读取失败')
    expect(document.body.textContent).not.toContain('配置对象存储')
  })

  it('supports multiple active storage integrations', async () => {
    const integration = {
      id: 'storage-1',
      category: 'storage',
      providerType: 's3',
      name: 'Storage',
      enabled: true,
      configuration: [],
      credentialKeys: ['access_key_id', 'secret_access_key'],
      healthStatus: 'unknown',
      version: 1,
      createdAt: '2026-08-25T00:00:00Z',
      updatedAt: '2026-08-25T00:00:00Z',
    }
    apiMocks.getEnvelope.mockImplementation((path: string) =>
      path.startsWith('/system-integrations')
        ? Promise.resolve({ items: [integration, { ...integration, id: 'storage-2' }] })
        : Promise.resolve({
            data: { backend: 's3', objectCount: 0, totalBytes: 0, items: [] },
          }),
    )

    const container = await renderPage()
    await openObjectStorageDrawer(container)
    expect(document.body.textContent).toContain('存储实例2')
    expect(container.textContent).toContain('全部存储')
    expect(document.body.querySelector('button[aria-label="编辑 Storage"]')).not.toBeNull()
    expect(document.body.textContent).not.toContain('对象存储配置冲突')
  })
})
