import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { secretMutations } from './mutations'
import { configurationKeys } from '../shared/keys'
const updateYAML = vi.hoisted(() => vi.fn())
vi.mock('../shared/api', () => ({ updateConfigurationYAML: updateYAML }))
const variables = {
  scope: { clusterId: 'cluster-a', namespace: 'team-a' },
  name: 'demo',
  content: 'synthetic-secret-draft',
}

describe('Secret YAML mutation lifetime', () => {
  it('does not restore sensitive YAML after permission revocation or owner unmount', async () => {
    const client = new QueryClient()
    let retain = true
    let complete!: (value: { content: string }) => void
    updateYAML.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          complete = resolve
        }),
    )
    const key = configurationKeys.yaml('secrets', variables.scope, variables.name)
    const options = secretMutations.updateContent(client, variables, () => retain)
    expect(options.gcTime).toBe(0)
    const observer = new MutationObserver(client, options)
    const unsubscribe = observer.subscribe(() => undefined)
    const pending = observer.mutate(variables)
    await Promise.resolve()
    retain = false
    client.removeQueries({ queryKey: key, exact: true })
    complete({ content: 'late-sensitive-canary' })
    await pending
    expect(client.getQueryData(key)).toBeUndefined()
    unsubscribe()
    await new Promise((resolve) => setTimeout(resolve, 5))
    expect(
      JSON.stringify(
        client
          .getMutationCache()
          .getAll()
          .map((item) => item.state),
      ),
    ).not.toContain('late-sensitive-canary')
    client.clear()
  })
  it('retains normal authorized success and uses existing invalidation', async () => {
    const client = new QueryClient(),
      invalidate = vi.spyOn(client, 'invalidateQueries').mockResolvedValue()
    updateYAML.mockResolvedValueOnce({ content: 'authorized-yaml' })
    const observer = new MutationObserver(
      client,
      secretMutations.updateContent(client, variables, () => true),
    )
    await observer.mutate(variables)
    expect(
      client.getQueryData(configurationKeys.yaml('secrets', variables.scope, variables.name)),
    ).toEqual({ content: 'authorized-yaml' })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: configurationKeys.lists('secrets') })
    client.clear()
  })
})
