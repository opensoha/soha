/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ResourceRelationWorkbench, relationDisplayNodes } from './resource-relation-workbench'
import type { ResourceRelationsView } from '../domain-types'

const ref = (kind: string, name: string, uid: string) => ({
  clusterId: 'cluster',
  namespace: 'team',
  apiVersion: kind === 'Pod' ? 'v1' : 'apps/v1',
  kind,
  name,
  uid,
  scopeMode: 'namespace' as const,
})
const graph: ResourceRelationsView = {
  root: ref('Deployment', 'api', 'deployment-uid'),
  config: { version: 'v1', rootUid: 'deployment-uid', sources: [], rules: [] },
  nodes: [
    { resource: ref('Deployment', 'api', 'deployment-uid'), role: 'root', depth: 0 },
    { resource: ref('ReplicaSet', 'api-v2', 'rs-uid'), role: 'owned', depth: 1 },
    { resource: ref('Pod', 'api-1', 'pod-uid'), role: 'owned', depth: 2 },
  ],
  edges: [
    { fromUid: 'deployment-uid', toUid: 'rs-uid', type: 'owns' },
    { fromUid: 'rs-uid', toUid: 'pod-uid', type: 'owns' },
  ],
  coverage: [],
  partial: false,
  observedAt: '2026-10-25T00:00:00Z',
}
let root: ReturnType<typeof createRoot>
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.defineProperty(window, 'matchMedia', {
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
    configurable: true,
  })
  const host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})
async function render(data = graph) {
  await act(async () =>
    root.render(
      <ResourceRelationWorkbench
        data={data}
        renderActions={(node) => <span>操作目标 {node.resource.uid}</span>}
      />,
    ),
  )
}
async function select(label: string) {
  await act(async () =>
    (document.querySelector(`[aria-label="${label}"]`) as HTMLButtonElement).click(),
  )
}
it('selects verified nodes and retains actual owner direction and resource identity', async () => {
  await render()
  await select('选择 Pod api-1')
  const detail = document.querySelector('[aria-label="所选资源详情"]')!
  expect(detail.textContent).toContain('pod-uid')
  expect(detail.textContent).toContain('ReplicaSet / api-v2 → 拥有')
  expect(detail.textContent).toContain('操作目标 pod-uid')
})
it('does not transfer an existing selection to a same-name replacement UID', async () => {
  await render()
  await select('选择 Pod api-1')
  await render({
    ...graph,
    nodes: graph.nodes.map((node) =>
      node.resource.kind === 'Pod'
        ? { ...node, resource: { ...node.resource, uid: 'replacement' } }
        : node,
    ),
  })
  expect(document.querySelector('[aria-label="所选资源详情"]')?.textContent).toContain(
    '所选资源不在本次关系范围',
  )
  expect(document.querySelector('[aria-label="所选资源详情"]')?.textContent).not.toContain(
    '操作目标 replacement',
  )
})
it('keeps runtime references separate from ownership without inventing UIDs', () => {
  const nodes = relationDisplayNodes(graph, [
    { kind: 'Service', name: 'api', relation: 'Service selector' },
    { kind: 'ConfigMap', name: 'api-config', relation: 'envFrom' },
    { kind: 'Secret', name: 'credentials', relation: 'secretKeyRef' },
  ])
  expect(nodes.slice(3).every((node) => node.role === 'reference' && !node.resource.uid)).toBe(true)
  expect(nodes.find((node) => node.resource.kind === 'ConfigMap')?.basis).toBe('envFrom')
  expect(graph.edges).toHaveLength(2)
})
