/** @vitest-environment jsdom */
import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { parse } from 'yaml'
import { deliveryApi } from '../api'
import type { DeliveryDocument, DeliveryDocumentPreview } from '../types'
import { DeliveryDocumentSourceEditor } from './source-editor'
import { DeliveryDocumentImportDialog, readDeliveryDocumentFiles } from './import-dialog'
import {
  buildTemplateDocument,
  deploymentTemplateDocument,
  workflowDocument,
  workflowTemplateDocument,
} from './model'

vi.mock('../api', () => ({
  deliveryApi: { documents: { preview: vi.fn(), apply: vi.fn(), export: vi.fn() } },
}))

const documentValue: DeliveryDocument = {
  apiVersion: 'delivery.soha.io/v1alpha1',
  kind: 'BuildTemplate',
  metadata: { name: 'image', displayName: 'Image' },
  spec: { buildCommands: ['echo build'], enabled: true },
}
const valid: DeliveryDocumentPreview = {
  id: 'preview-1',
  valid: true,
  candidateDigest: 'sha256:fixed',
  diagnostics: [],
  candidates: [
    {
      path: 'document-1.yaml',
      document: documentValue,
      action: 'create',
      changedPaths: [''],
      sourceDigest: 'source',
      normalizedSpecDigest: 'normalized',
    },
  ],
}
const invalid: DeliveryDocumentPreview = {
  valid: false,
  candidates: [],
  diagnostics: [
    {
      path: 'document-1.yaml',
      document: 1,
      line: 3,
      column: 4,
      pointer: '/spec/unknown',
      code: 'unknown_field',
      message: 'unknown field',
    },
  ],
}
let root: Root
let container: HTMLDivElement
let client: QueryClient

beforeEach(() => {
  ;(globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  window.matchMedia = vi
    .fn()
    .mockReturnValue({ matches: false, addListener: vi.fn(), removeListener: vi.fn() })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  vi.clearAllMocks()
  const getComputedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
})

afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

async function click(label: string) {
  const button = [...document.querySelectorAll('button')].find(
    (item) => item.textContent?.replace(/\s/g, '') === label.replace(/\s/g, ''),
  )
  expect(button, label).toBeDefined()
  await act(async () => button!.click())
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}

async function enter(textarea: HTMLTextAreaElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
      textarea,
      value,
    )
    textarea.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

it('keeps invalid source separate, displays field location, and changes format only after server validation', async () => {
  const synchronized = vi.fn()
  const dirty = vi.fn()
  function Harness() {
    const [value, setValue] = useState(documentValue)
    return (
      <DeliveryDocumentSourceEditor
        kind="BuildTemplate"
        value={value}
        targetId="template-1"
        expectedRevision={7}
        onDirtyChange={dirty}
        onValidated={(next) => {
          synchronized(next)
          setValue(next)
        }}
      />
    )
  }
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <Harness />
      </QueryClientProvider>,
    ),
  )
  const area = container.querySelector('textarea')!
  const initial = area.value
  await enter(area, 'invalid: [')
  expect(dirty).toHaveBeenLastCalledWith(true)
  expect(
    [...container.querySelectorAll<HTMLInputElement>('input[type="radio"]')].every(
      (item) => item.disabled,
    ),
  ).toBe(true)
  vi.mocked(deliveryApi.documents.preview).mockResolvedValue(invalid)
  await click('校验并同步表单')
  expect(synchronized).not.toHaveBeenCalled()
  expect(area.value).toBe('invalid: [')
  expect(container.textContent).toContain('document-1.yaml:3:4')
  expect(container.textContent).toContain('/spec/unknown')
  await click('还原源码')
  expect(area.value).toBe(initial)
  expect(dirty).toHaveBeenLastCalledWith(false)
  const next = { ...documentValue, metadata: { name: 'image', displayName: 'New name' } }
  await enter(area, JSON.stringify(next))
  vi.mocked(deliveryApi.documents.preview).mockResolvedValue({
    ...valid,
    candidates: [{ ...valid.candidates[0], document: next }],
  })
  await click('校验并同步表单')
  expect(synchronized).toHaveBeenCalledExactlyOnceWith(next)
  expect(vi.mocked(deliveryApi.documents.preview).mock.calls.slice(-1)[0]?.[0]).toEqual({
    validateOnly: true,
    files: [
      {
        path: 'document.yaml',
        content: JSON.stringify(next),
        targetId: 'template-1',
        expectedRevision: 7,
      },
    ],
  })
  expect(parse(area.value)).toEqual(next)
  const json = container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[1]
  await act(async () => json.click())
  expect(JSON.parse(area.value)).toEqual(next)
  expect(deliveryApi.documents.apply).not.toHaveBeenCalled()
})

it('previews all import files and retries apply with the same stored candidate and idempotency key', async () => {
  const onClose = vi.fn()
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <App>
          <DeliveryDocumentImportDialog onClose={onClose} />
        </App>
      </QueryClientProvider>,
    ),
  )
  await click('粘贴文档')
  await enter(document.querySelector('textarea')!, JSON.stringify(documentValue))
  vi.mocked(deliveryApi.documents.preview).mockResolvedValue(invalid)
  await click('校验并预览')
  await click('确认导入')
  expect(deliveryApi.documents.apply).not.toHaveBeenCalled()
  vi.mocked(deliveryApi.documents.preview).mockResolvedValue(valid)
  await click('校验并预览')
  vi.mocked(deliveryApi.documents.apply)
    .mockRejectedValueOnce(new Error('connection interrupted'))
    .mockResolvedValueOnce({ previewId: valid.id!, objects: [] })
  await click('确认导入')
  const first = vi.mocked(deliveryApi.documents.apply).mock.calls[0]
  expect(first).toEqual([
    'preview-1',
    { candidateDigest: valid.candidateDigest, idempotencyKey: expect.any(String) },
  ])
  expect(onClose).not.toHaveBeenCalled()
  await click('确认导入')
  expect(vi.mocked(deliveryApi.documents.apply).mock.calls[1]).toEqual(first)
  expect(onClose).toHaveBeenCalledOnce()
})

it('rejects oversized and malformed UTF-8 uploads before preview', async () => {
  await expect(readDeliveryDocumentFiles([])).rejects.toThrow('1–100')
  await expect(
    readDeliveryDocumentFiles([new File([new Uint8Array(1024 * 1024 + 1)], 'large.yaml')]),
  ).rejects.toThrow('1 MiB')
  const invalidFile = new File([], 'invalid.yaml')
  Object.defineProperty(invalidFile, 'arrayBuffer', {
    value: async () => new Uint8Array([0xff]).buffer,
  })
  await expect(readDeliveryDocumentFiles([invalidFile])).rejects.toThrow()
  expect(deliveryApi.documents.preview).not.toHaveBeenCalled()
})

it('exports only document inputs and retains typed template fields', () => {
  const build = buildTemplateDocument({
    key: 'image',
    name: 'Image',
    buildCommands: ['echo a\necho b'],
    variableSchema: { count: { type: 'integer', enum: [1, 2], minimum: 1, maximum: 2 } },
    defaultVariables: { count: 2 },
    enabled: true,
    publish: true,
    expectedRevision: 5,
  })
  expect(build.spec.variableSchema).toEqual({
    count: { type: 'integer', enum: [1, 2], minimum: 1, maximum: 2 },
  })
  expect(JSON.stringify(build)).not.toMatch(/publish|expectedRevision/)
  const workflow = workflowDocument({
    name: 'Deploy',
    targets: [{ id: 'target', applicationId: 'app', serviceId: 'service', action: 'build' }],
  })
  expect(workflow.metadata).toEqual({ name: 'workflow' })
  const recipe = workflowTemplateDocument({
    key: 'recipe',
    name: 'Recipe',
    definition: { mode: 'delivery_batch', maxConcurrency: 4 },
    enabled: true,
  })
  expect(recipe.spec.definition).toEqual({ mode: 'delivery_batch', maxConcurrency: 4 })
  const deployment = deploymentTemplateDocument({
    key: 'http',
    name: 'HTTP',
    source: { renderer: 'raw_yaml', files: [{ path: 'config.yaml', content: 'data: {}' }] },
    parameterSchema: { type: 'object', properties: {} },
    defaults: {},
    environmentOverrides: ['port'],
    health: { mode: 'configuration_only', timeoutSeconds: 30 },
    enabled: true,
  })
  expect(deployment.spec.environmentOverrides).toEqual(['port'])
})
