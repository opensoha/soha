import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { configurationKeys } from './keys'
import { configurationMutations } from './mutations'

const apiMocks = vi.hoisted(() => ({
  createConfigurationResource: vi.fn(),
  deleteConfigurationResource: vi.fn(),
  updateConfigurationData: vi.fn(),
  updateConfigurationYAML: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const target = {
  scope: { clusterId: 'cluster-a', namespace: 'team-a' },
  name: 'app-config',
}

describe('configuration mutations', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('sets detail data and centrally invalidates list and detail after data updates', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    apiMocks.updateConfigurationData.mockResolvedValueOnce({ name: 'app-config', immutable: false })
    const observer = new MutationObserver(
      queryClient,
      configurationMutations.updateData('configmaps', queryClient),
    )

    await observer.mutate({ target, payload: { data: { key: 'next' }, binaryData: {} } })

    expect(
      queryClient.getQueryData(configurationKeys.detail('configmaps', target.scope, target.name)),
    ).toMatchObject({ name: 'app-config' })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: configurationKeys.lists('configmaps') })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: configurationKeys.detail('configmaps', target.scope, target.name),
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: configurationKeys.references('configmaps', target.scope, target.name),
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: configurationKeys.yaml('configmaps', target.scope, target.name),
    })
  })

  it('invalidates only list collections after create', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    apiMocks.createConfigurationResource.mockResolvedValueOnce({ content: 'kind: Secret' })
    const observer = new MutationObserver(
      queryClient,
      configurationMutations.create('secrets', queryClient),
    )

    await observer.mutate({ scope: target.scope, content: 'kind: Secret' })

    expect(invalidate).toHaveBeenCalledTimes(1)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: configurationKeys.lists('secrets') })
  })
})
