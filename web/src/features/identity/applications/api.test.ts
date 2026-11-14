import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createIdentityApplication,
  deleteIdentityApplication,
  listIdentityApplications,
  listIdentityProviderCapabilities,
  updateIdentityApplication,
} from './api'
import type { IdentityApplication, IdentityApplicationInput } from './types'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const application: IdentityApplication = {
  id: 'grafana/id',
  slug: 'grafana',
  name: 'Grafana',
  tags: ['observability'],
  providerType: 'link',
  portalVisible: true,
  featured: false,
  sortOrder: 10,
  status: 'enabled',
  createdAt: '2026-07-10T00:00:00Z',
  updatedAt: '2026-07-10T00:00:00Z',
}

const input: IdentityApplicationInput = {
  slug: application.slug,
  name: application.name,
  description: '',
  iconUrl: '',
  tags: application.tags,
  launchUrl: '',
  providerId: '',
  providerType: application.providerType,
  portalVisible: application.portalVisible,
  featured: application.featured,
  sortOrder: application.sortOrder,
  status: application.status,
  metadata: {},
  assignments: [],
}

describe('identity applications api', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('normalizes filters in the request and unwraps list data', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: [application] })

    await expect(
      listIdentityApplications({ query: ' Grafana ', status: ' enabled ' as 'enabled' }),
    ).resolves.toEqual([application])
    expect(apiMocks.get).toHaveBeenCalledWith('/identity/applications?q=Grafana&status=enabled')
  })

  it('unwraps create and update data and encodes application ids', async () => {
    apiMocks.post.mockResolvedValueOnce({ data: application })
    apiMocks.put.mockResolvedValueOnce({ data: application })

    await expect(createIdentityApplication(input)).resolves.toEqual(application)
    await expect(
      updateIdentityApplication({ applicationId: application.id, input }),
    ).resolves.toEqual(application)

    expect(apiMocks.post).toHaveBeenCalledWith('/identity/applications', input)
    expect(apiMocks.put).toHaveBeenCalledWith('/identity/applications/grafana%2Fid', input)
  })

  it('keeps delete transport details out of the domain return type', async () => {
    apiMocks.delete.mockResolvedValueOnce({ data: { status: 'deleted' } })

    await expect(deleteIdentityApplication(application.id)).resolves.toBeUndefined()
    expect(apiMocks.delete).toHaveBeenCalledWith('/identity/applications/grafana%2Fid')
  })

  it('unwraps provider capabilities and tolerates an empty envelope', async () => {
    apiMocks.get.mockResolvedValueOnce({})

    await expect(listIdentityProviderCapabilities()).resolves.toEqual([])
    expect(apiMocks.get).toHaveBeenCalledWith('/identity/provider-capabilities')
  })
})
