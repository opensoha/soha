/** @vitest-environment jsdom */

import { ApiError } from '@/services/api-error'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { PluginStorageConnections } from './plugin-storage-connections'
import { parsePluginStorageDriver, pluginStorageConfiguration } from './plugin-storage-schema'

const api = vi.hoisted(() => ({ getEnvelope: vi.fn(), post: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api }))

const driver = {
  id: 'azureblob',
  name: 'Azure Blob Storage',
  configSchema: {
    type: 'object',
    additionalProperties: false,
    'x-soha-test-connection-action': 'storage.testConnection',
    required: ['container', 'account', 'sas_token'],
    properties: {
      container: { type: 'string', pattern: '^[a-z][a-z0-9-]+$' },
      account: { type: 'string' },
      prefix: { type: 'string', default: 'packages' },
      endpoint: { type: 'string' },
      sas_token: { type: 'string', 'x-soha-widget': 'secret-ref', default: 'raw-credential' },
    },
  },
}

const connection = {
  id: 'azure-1',
  category: 'storage',
  providerType: driver.id,
  name: 'Azure connection',
  enabled: true,
  configuration: [],
  credentialKeys: [],
  healthStatus: 'unknown',
  version: 1,
}
const permissions = {
  canView: true,
  canCreate: true,
  canUpdate: true,
  canDelete: true,
  canTest: true,
}
let root: Root | undefined
let client: QueryClient | undefined
let host: HTMLDivElement | undefined

beforeAll(() => {
  const computedStyle = window.getComputedStyle.bind(window)
  vi.stubGlobal('getComputedStyle', (element: Element) => computedStyle(element))
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'matchMedia',
    vi.fn(() => ({
      matches: false,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
})

beforeEach(() => {
  api.getEnvelope.mockImplementation(async (path: string) => {
    if (path === '/system-integrations/storage-drivers') return { items: [driver] }
    if (path === '/system-integrations?category=storage') {
      return { items: [{ ...connection, id: 's3-1', providerType: 's3', name: 'Existing S3' }] }
    }
    if (path === '/secrets') {
      return {
        items: [
          {
            id: 'ref-1',
            name: 'Azure SAS',
            status: 'active',
            scopeType: 'global',
            scopeId: 'main',
          },
        ],
      }
    }
    throw new Error(`Unexpected GET ${path}`)
  })
  api.post.mockImplementation(async (path: string) => {
    if (path === '/system-integrations') return { data: connection }
    if (path === '/system-integrations/azure-1/test') {
      return { data: { status: 'failed', message: 'unsafe-provider-diagnostics' } }
    }
    throw new Error(`Unexpected POST ${path}`)
  })
})

afterEach(async () => {
  await act(async () => root?.unmount())
  client?.clear()
  host?.remove()
  vi.resetAllMocks()
})

afterAll(() => vi.unstubAllGlobals())

async function render(overrides: Partial<typeof permissions> = {}) {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  host = document.body.appendChild(document.createElement('div'))
  root = createRoot(host)
  await act(async () => {
    root!.render(
      <QueryClientProvider client={client!}>
        <App>
          <PluginStorageConnections {...permissions} {...overrides} />
        </App>
      </QueryClientProvider>,
    )
  })
  await flush()
}

async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

function button(text: string) {
  return Array.from(document.querySelectorAll('button')).find((item) => item.textContent === text)
}

async function fill(id: string, value: string) {
  const input = document.querySelector<HTMLInputElement>(`#plugin-storage-connection_${id}`)!
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
  await act(async () => {
    setValue.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

describe('PluginStorageConnections', () => {
  it('builds the advertised form and sends only named values and a secret reference before testing', async () => {
    await render()
    expect(api.getEnvelope).toHaveBeenCalledWith('/system-integrations/storage-drivers')
    expect(document.body.textContent).not.toContain('Existing S3')
    await act(async () => button('创建连接')!.click())
    await flush()
    expect(document.body.textContent).toContain(driver.name)
    expect(document.body.textContent).toContain('服务端点')
    expect(
      document.querySelector<HTMLInputElement>('#plugin-storage-connection_configuration_prefix')
        ?.value,
    ).toBe('packages')
    expect(document.body.textContent).not.toContain('raw-credential')

    await act(async () => button('保存并测试')!.click())
    expect(api.post).not.toHaveBeenCalled()
    await vi.waitFor(() => expect(document.body.textContent).toContain('请填写此字段。'))
    await fill('name', 'Azure connection')
    await fill('configuration_container', 'files')
    await fill('configuration_account', 'azureaccount')
    const secret = document.querySelector<HTMLInputElement>(
      '#plugin-storage-connection_configuration_sas_token[role="combobox"]',
    )!
    await act(async () => secret.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })))
    const secretOption = document.querySelector<HTMLElement>(
      '.ant-select-item-option[title="Azure SAS · global/main"]',
    )!
    await act(async () => secretOption.click())
    await act(async () => button('保存并测试')!.click())
    await flush()
    await flush()

    expect(api.post.mock.calls).toEqual([
      [
        '/system-integrations',
        {
          category: 'storage',
          providerType: 'azureblob',
          name: 'Azure connection',
          enabled: true,
          configuration: [
            { key: 'container', value: 'files' },
            { key: 'account', value: 'azureaccount' },
            { key: 'prefix', value: 'packages' },
            { key: 'sas_token', value: 'soha://secrets/ref-1' },
          ],
        },
      ],
      ['/system-integrations/azure-1/test'],
    ])
    expect(document.body.textContent).toContain('测试插件存储连接')
    expect(document.body.textContent).not.toContain('unsafe-provider-diagnostics')
  })

  it('does not expose create when reading available drivers is forbidden', async () => {
    api.getEnvelope.mockRejectedValue(new ApiError(403, 'Forbidden'))
    await render()
    expect(button('创建连接')?.disabled).toBe(true)
    expect(document.body.textContent).not.toContain('尚未配置插件存储连接')
    expect(api.post).not.toHaveBeenCalled()
  })

  it('keeps typed defaults, rejects raw secrets and serializes only declared fields', () => {
    const configSchema = {
      ...driver.configSchema,
      properties: {
        ...driver.configSchema.properties,
        verify: { type: 'boolean', default: false },
        workers: { type: 'integer', enum: [0, 2], default: 0 },
      },
    }
    const { fields } = parsePluginStorageDriver({ ...driver, configSchema })!
    const defaults = Object.fromEntries(fields.map((field) => [field.key, field.defaultValue]))
    expect(defaults).toMatchObject({ verify: 'false', workers: '0', sas_token: undefined })
    const values = { ...defaults, container: 'files', account: 'test', sas_token: 'raw' }
    expect(() => pluginStorageConfiguration(fields, values)).toThrow(
      'Invalid storage configuration',
    )
    expect(
      pluginStorageConfiguration(fields, {
        ...values,
        sas_token: 'soha://secrets/ref-1',
        unexpected: 'ignored',
      }),
    ).toEqual([
      { key: 'container', value: 'files' },
      { key: 'account', value: 'test' },
      { key: 'prefix', value: 'packages' },
      { key: 'sas_token', value: 'soha://secrets/ref-1' },
      { key: 'verify', value: 'false' },
      { key: 'workers', value: '0' },
    ])
  })
})
