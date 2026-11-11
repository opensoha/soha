import { QueryClient } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { virtualizationApi } from './virtualization-api'
import { computeKeys } from '@/features/compute'
import { virtualizationKeys, virtualizationMutationKeys } from './keys'
import { invalidateVirtualizationQueries, virtualizationMutations } from './mutations'

async function executeMutation<TVariables>(mutationFn: unknown, variables: TVariables) {
  if (typeof mutationFn !== 'function') throw new Error('Expected a mutation function')
  return mutationFn(variables, {} as never)
}

async function executeSuccess(handler: unknown, data: unknown, variables: unknown) {
  if (typeof handler !== 'function') throw new Error('Expected an onSuccess handler')
  await handler(data, variables, undefined, {} as never)
}

function createQueryClient() {
  const queryClient = new QueryClient()
  const invalidateQueries = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
  return { invalidateQueries, queryClient }
}

describe('virtualizationMutations', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('deduplicates explicit invalidation prefixes', async () => {
    const { invalidateQueries, queryClient } = createQueryClient()

    await invalidateVirtualizationQueries(queryClient, [
      virtualizationKeys.images(),
      virtualizationKeys.images(),
      computeKeys.overview(),
    ])

    expect(invalidateQueries).toHaveBeenCalledTimes(2)
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: virtualizationKeys.images() })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: computeKeys.overview() })
  })

  it('wraps action variables and uses stable mutation keys', async () => {
    const { queryClient } = createQueryClient()
    const powerVm = vi.spyOn(virtualizationApi, 'powerVm').mockResolvedValue(undefined as never)
    const deleteCluster = vi
      .spyOn(virtualizationApi, 'deleteCluster')
      .mockResolvedValue(undefined as never)

    const powerOptions = virtualizationMutations.powerVm(queryClient)
    const deleteOptions = virtualizationMutations.deleteCluster(queryClient)

    expect(powerOptions.mutationKey).toEqual(virtualizationMutationKeys.vm('power'))
    expect(deleteOptions.mutationKey).toEqual(virtualizationMutationKeys.cluster('delete'))

    await executeMutation(powerOptions.mutationFn, { id: 'vm-1', action: 'restart' })
    await executeMutation(deleteOptions.mutationFn, { id: 'cluster-1', force: true })

    expect(powerVm).toHaveBeenCalledWith('vm-1', 'restart')
    expect(deleteCluster).toHaveBeenCalledWith('cluster-1', { force: true })
  })

  it('invalidates only image caches after image CRUD', async () => {
    const { invalidateQueries, queryClient } = createQueryClient()
    const options = virtualizationMutations.createImage(queryClient)

    await executeSuccess(options.onSuccess, undefined, { name: 'ubuntu' })

    expect(invalidateQueries).toHaveBeenCalledOnce()
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: virtualizationKeys.images() })
  })

  it('invalidates VM lists, detail, Compute overview and operations after a power action', async () => {
    const { invalidateQueries, queryClient } = createQueryClient()
    const options = virtualizationMutations.powerVm(queryClient)

    await executeSuccess(options.onSuccess, undefined, { id: 'vm-1', action: 'start' })

    expect(invalidateQueries).toHaveBeenCalledTimes(4)
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: virtualizationKeys.vmLists() })
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: virtualizationKeys.vmDetail('vm-1'),
    })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: computeKeys.overview() })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: virtualizationKeys.operations() })
  })

  it('uses unwrapped mutation operations for entity-specific invalidation', async () => {
    const { invalidateQueries, queryClient } = createQueryClient()
    const createOptions = virtualizationMutations.createVm(queryClient)
    const cancelOptions = virtualizationMutations.cancelOperation(queryClient)

    await executeSuccess(createOptions.onSuccess, { id: 'op-create', vmId: 'vm-created' }, {})
    await executeSuccess(
      cancelOptions.onSuccess,
      { id: 'op-cancel', vmId: 'vm-canceled' },
      'op-cancel',
    )

    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: virtualizationKeys.vmDetail('vm-created'),
    })
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: virtualizationKeys.vmDetail('vm-canceled'),
    })
  })

  it('maps VM plan and idempotent create variables to the API', async () => {
    const { queryClient } = createQueryClient()
    const payload = { connectionId: 'conn-1', name: 'demo-vm' }
    const planVmCreate = vi.spyOn(virtualizationApi, 'planVmCreate').mockResolvedValue({
      capability: 'virtualization.vms.create.trigger',
      target: 'demo-vm',
      ready: true,
      riskLevel: 'execute',
      requiresApproval: true,
      changes: [],
      warnings: [],
    })
    const createVm = vi.spyOn(virtualizationApi, 'createVm').mockResolvedValue({ id: 'op-create' })

    await executeMutation(virtualizationMutations.planCreateVm().mutationFn, payload)
    await executeMutation(virtualizationMutations.createVm(queryClient).mutationFn, {
      idempotencyKey: 'idem-vm-1234',
      payload,
    })

    expect(planVmCreate).toHaveBeenCalledWith(payload)
    expect(createVm).toHaveBeenCalledWith(payload, 'idem-vm-1234')
  })

  it('invalidates the full domain after synchronization changes asset inventories', async () => {
    const { invalidateQueries, queryClient } = createQueryClient()
    const options = virtualizationMutations.syncCluster(queryClient)

    await executeSuccess(options.onSuccess, undefined, 'cluster-1')

    expect(invalidateQueries).toHaveBeenCalledOnce()
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: virtualizationKeys.all })
  })
})
