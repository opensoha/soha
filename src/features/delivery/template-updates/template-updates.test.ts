import { describe, expect, it, vi } from 'vitest'
import type { TemplateUpdateCandidate } from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'
import { adoptionSelection } from './drawer'
import { templateUpdateApi } from './api'
import { templateUpdateKeys } from './queries'

vi.mock('@/services/api-client', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))

const candidate: TemplateUpdateCandidate = {
  id: 'candidate',
  target: {
    id: 'target',
    kind: 'deployment',
    templateId: 'template',
    applicationId: 'app',
    applicationName: 'App',
    serviceId: 'svc',
    serviceName: 'API',
    applicationEnvironmentId: 'test',
    environmentName: 'Test',
    releaseTargetId: 'runtime',
    fromVersion: 1,
    mode: 'fixed',
    requiresApproval: false,
    eligible: true,
    reason: '',
    independentOverrides: [],
    bindingRevision: 'binding',
    scope: 'environment',
  },
  toVersion: 4,
  digest: 'digest',
  parameters: { replicas: 1, labels: { team: 'retail' } },
  cumulative: {
    fromVersion: 1,
    toVersion: 4,
    changes: [],
    renderStatus: 'available',
    diagnostics: [],
    historyGap: false,
  },
  versions: [],
  eligible: true,
  reason: '',
  requiresApproval: false,
  wave: 2,
}

describe('template adoption inputs', () => {
  it('retains saved parameters until explicitly edited, and removes only selected defaults', () => {
    expect(adoptionSelection(candidate)).toEqual({ targetId: 'target', wave: 2 })
    expect(
      adoptionSelection(candidate, {
        text: JSON.stringify(candidate.parameters),
        defaults: ['replicas'],
      }),
    ).toEqual({
      targetId: 'target',
      wave: 2,
      useNewDefaults: ['replicas'],
      parameters: { labels: { team: 'retail' } },
    })
    expect(candidate.parameters.replicas).toBe(1)
    for (const text of ['[]', 'null', 'true', '{broken'])
      expect(() => adoptionSelection(candidate, { text, defaults: [] })).toThrow()
  })
  it('isolates query identity across application, service, environment and page', () => {
    const scope = {
      kind: 'deployment' as const,
      templateId: 'template',
      applicationId: 'app',
      serviceId: 'svc',
      applicationEnvironmentId: 'test',
    }
    expect(templateUpdateKeys.targets(scope, 1, 20)).not.toEqual(
      templateUpdateKeys.targets({ ...scope, applicationEnvironmentId: 'prod' }, 1, 20),
    )
    expect(templateUpdateKeys.targets(scope, 1, 20)).not.toEqual(
      templateUpdateKeys.targets(scope, 2, 20),
    )
    expect(templateUpdateKeys.targets(scope, 1, 20)).not.toEqual(
      templateUpdateKeys.targets({ ...scope, applicationId: 'other' }, 1, 20),
    )
  })
  it('sends frozen candidate IDs and preview digest without runtime overrides', async () => {
    vi.mocked(api.post).mockResolvedValue({ data: { id: 'batch' } })
    const input = {
      previewDigest: 'frozen',
      candidateIds: ['candidate'],
      waveSize: 2,
      stopOnFailure: true,
      acknowledgeHistoryGaps: false,
      idempotencyKey: 'request',
    }
    await expect(templateUpdateApi.batch({ previewId: 'preview/1', input })).resolves.toEqual({
      id: 'batch',
    })
    expect(api.post).toHaveBeenCalledWith(
      '/delivery/template-updates/previews/preview%2F1/batches',
      input,
    )
  })
  it('uses server pagination and explicit service/environment filters', async () => {
    vi.mocked(api.get).mockResolvedValue({ data: { items: [], total: 41 } })
    await templateUpdateApi.targets(
      {
        kind: 'deployment',
        templateId: ' template ',
        applicationId: 'app',
        serviceId: 'api',
        applicationEnvironmentId: 'test',
      },
      2,
      20,
    )
    expect(api.get).toHaveBeenCalledWith(
      '/delivery/template-updates/targets?kind=deployment&templateId=template&applicationId=app&serviceId=api&applicationEnvironmentId=test&page=2&pageSize=20',
    )
  })
})
