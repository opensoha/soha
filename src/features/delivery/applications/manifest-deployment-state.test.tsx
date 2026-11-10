import { renderToStaticMarkup } from 'react-dom/server'
import { MemoryRouter } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import { ManifestDeploymentState } from './manifest-deployment-state'

vi.mock('../manifests/resource-inventory', () => ({
  ManifestResourceInventoryTable: ({ items }: { items: unknown[] }) => (
    <div>资源数 {items.length}</div>
  ),
}))

it('separates revisions and desired digest while retaining error and task evidence', () => {
  const digest = 'a'.repeat(64)
  const markup = renderToStaticMarkup(
    <MemoryRouter>
      <ManifestDeploymentState
        deployment={
          {
            spec: { desiredRevision: 7, desiredDigest: digest },
            status: {
              appliedRevision: 6,
              lastExecutionTaskId: 'task-123',
              lastErrorMessage: '执行失败',
            },
          } as Parameters<typeof ManifestDeploymentState>[0]['deployment']
        }
      />
    </MemoryRouter>,
  )
  expect(markup).toContain('期望版本')
  expect(markup).toContain('已应用版本')
  expect(markup).toContain('期望配置摘要')
  expect(markup).toContain('aaaaaaaaaaaa…')
  expect(markup).toContain(digest)
  expect(markup).toContain('<details')
  expect(markup).toContain('/delivery/execution-tasks/task-123')
  expect(markup).toContain('执行失败')
  expect(markup).toContain('资源数 0')
})
