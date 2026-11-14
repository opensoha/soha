/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, it, vi } from 'vitest'
import type { RepositoryAnalysis as AnalysisResult } from '@opensoha/contracts/gen/ts/sohaapi'
import { RepositoryAnalysis } from './repository-analysis'
import { deliveryApi } from '../api'
import type { DeliveryRepository } from '../types'

vi.mock('../api', () => ({ deliveryApi: { applications: { analyzeRepository: vi.fn() } } }))

it('shows metadata suggestions only for the requested directory and preserves manual build settings', async () => {
  ;(globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true
  let finish!: (value: AnalysisResult) => void
  vi.mocked(deliveryApi.applications.analyzeRepository).mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  const repository = { id: 'repo', name: 'API', defaultBranch: 'main' } as DeliveryRepository
  const render = (applicationId: string) =>
    root.render(
      <QueryClientProvider client={client}>
        <input aria-label="已选构建方式" defaultValue="平台构建模板" />
        <RepositoryAnalysis
          key={applicationId}
          applicationId={applicationId}
          repositories={[repository]}
        />
      </QueryClientProvider>,
    )
  try {
    await act(async () => render('app'))
    await act(async () => {
      ;[...container.querySelectorAll('button')]
        .find((button) => button.textContent?.replace(/\s/g, '') === '分析源码')!
        .click()
    })
    expect(deliveryApi.applications.analyzeRepository).toHaveBeenCalledWith('app', {
      repositoryId: 'repo',
      refType: 'branch',
      refName: 'main',
      projectPath: '.',
    })
    await act(async () => {
      finish({
        applicationId: 'app',
        repositoryId: 'repo',
        projectPath: '.',
        ruleVersion: '1',
        status: 'identified',
        resolvedCommit: 'a'.repeat(40),
        candidates: [
          {
            language: 'go',
            projectPath: '.',
            buildMethods: ['repo_dockerfile'],
            evidencePaths: ['go.mod', 'Dockerfile'],
          },
        ],
        evidencePaths: ['go.mod', 'Dockerfile'],
        warnings: [],
      })
      await new Promise((resolve) => setTimeout(resolve, 20))
    })
    expect(container.textContent).toContain('已识别项目')
    expect(container.textContent).toContain('go.mod、Dockerfile')
    expect((container.querySelector('[aria-label="已选构建方式"]') as HTMLInputElement).value).toBe(
      '平台构建模板',
    )
    await act(async () => {
      const directory = container.querySelector('[aria-label="项目目录"]') as HTMLInputElement
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        directory,
        'services/api',
      )
      directory.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(container.textContent).not.toContain('已识别项目')
    await act(async () => render('another-app'))
    expect(container.textContent).not.toContain('已识别项目')
  } finally {
    await act(async () => root.unmount())
    client.clear()
    container.remove()
  }
})
