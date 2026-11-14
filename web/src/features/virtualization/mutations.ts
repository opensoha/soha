import {
  mutationOptions,
  type QueryClient,
  type QueryKey,
  type UseMutationOptions,
} from '@tanstack/react-query'
import { computeKeys } from '@/features/compute'
import { virtualizationApi } from './virtualization-api'
import { virtualizationKeys, virtualizationMutationKeys } from './keys'
import type {
  VirtualizationCapacityPolicyInput, VirtualizationCapacityInput, VirtualMachineAccessBindingInput, VirtualMachineSSHSessionInput,
  VirtualizationWorkerPoolInput,
  VirtualizationWorkerCreateInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type {
  CreateVirtualMachineInput,
  VirtualMachinePowerAction,
  VirtualMachineResizeInput,
  VirtualizationClusterInput,
  VirtualizationFlavorInput,
  VirtualizationImageInput,
  VirtualizationOperation,
} from './virtualization-types'

export interface PowerVirtualMachineVariables {
  id: string
  action: VirtualMachinePowerAction
}
export interface CreateVirtualMachineVariables {
  idempotencyKey: string
  payload: CreateVirtualMachineInput
}
export interface ResizeVirtualMachineVariables {
  id: string
  payload: VirtualMachineResizeInput
}

export interface UpdateVirtualizationClusterVariables {
  id: string
  payload: VirtualizationClusterInput
}

export interface DeleteVirtualizationClusterVariables {
  id: string
  force?: boolean
}

export interface UpdateVirtualizationImageVariables {
  id: string
  payload: VirtualizationImageInput
}

export interface UpdateVirtualizationFlavorVariables {
  id: string
  payload: VirtualizationFlavorInput
}

function uniqueQueryKeys(queryKeys: readonly QueryKey[]) {
  return [...new Map(queryKeys.map((queryKey) => [JSON.stringify(queryKey), queryKey])).values()]
}

export async function invalidateVirtualizationQueries(
  queryClient: QueryClient,
  queryKeys: readonly QueryKey[],
) {
  await Promise.all(
    uniqueQueryKeys(queryKeys).map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  )
}

export function withVirtualizationMutationSuccess<TData, TError, TVariables, TOnMutateResult>(
  options: UseMutationOptions<TData, TError, TVariables, TOnMutateResult>,
  afterSuccess: NonNullable<
    UseMutationOptions<TData, TError, TVariables, TOnMutateResult>['onSuccess']
  >,
): UseMutationOptions<TData, TError, TVariables, TOnMutateResult> {
  const invalidate = options.onSuccess
  return {
    ...options,
    onSuccess: async (data, variables, onMutateResult, context) => {
      await invalidate?.(data, variables, onMutateResult, context)
      return afterSuccess(data, variables, onMutateResult, context)
    },
  }
}

const invalidationKeys = {
  vmChanged: (id?: string) => [
    virtualizationKeys.vmLists(),
    computeKeys.overview(),
    virtualizationKeys.operations(),
    ...(id ? [virtualizationKeys.vmDetail(id)] : []),
  ],
  clusterChanged: () => [
    virtualizationKeys.clusters(),
    computeKeys.overview(),
    virtualizationKeys.operations(),
  ],
  imageChanged: () => [virtualizationKeys.images()],
  flavorChanged: () => [virtualizationKeys.flavors()],
  operationChanged: (operation?: VirtualizationOperation) => [
    virtualizationKeys.operations(),
    computeKeys.overview(),
    ...(operation?.vmId ? [virtualizationKeys.vmDetail(operation.vmId)] : []),
  ],
} satisfies Record<string, (...args: never[]) => QueryKey[]>

type OperationResult = Awaited<ReturnType<typeof virtualizationApi.cancelOperation>>

function invalidateOperationCaches(queryClient: QueryClient, operations: OperationResult[]) {
  return invalidateVirtualizationQueries(
    queryClient,
    operations.flatMap((operation) => invalidationKeys.operationChanged(operation)),
  )
}

export const virtualizationMutations = {
  saveCapacityPolicy: (queryClient: QueryClient) => mutationOptions({mutationKey:virtualizationMutationKeys.cluster('capacity-policy'),mutationFn:({id,input}:{id:string;input:VirtualizationCapacityPolicyInput})=>virtualizationApi.saveCapacityPolicy(id,input),onSuccess:(_response,{id})=>invalidateVirtualizationQueries(queryClient,[virtualizationKeys.capacityPolicy(id)])}),
  checkCapacity: () => mutationOptions({mutationKey:virtualizationMutationKeys.vm('capacity-check'),mutationFn:(input:VirtualizationCapacityInput)=>virtualizationApi.checkCapacity(input)}),
  saveAccessBinding: (queryClient: QueryClient) => mutationOptions({mutationKey:virtualizationMutationKeys.vm('access-binding'),mutationFn:({id,input}:{id:string;input:VirtualMachineAccessBindingInput})=>virtualizationApi.saveAccessBinding(id,input),onSuccess:(_response,{id})=>invalidateVirtualizationQueries(queryClient,[virtualizationKeys.accessBinding(id)])}),
  createSSHSession: () => mutationOptions({mutationKey:virtualizationMutationKeys.vm('ssh-session'),mutationFn:({id,input}:{id:string;input:VirtualMachineSSHSessionInput})=>virtualizationApi.createSSHSession(id,input)}),
  closeSSHSession: (queryClient: QueryClient) => mutationOptions({mutationKey:virtualizationMutationKeys.vm('ssh-close'),mutationFn:(id:string)=>virtualizationApi.closeSSHSession(id),onSuccess:(_response,id)=>invalidateVirtualizationQueries(queryClient,[virtualizationKeys.sshSession(id)])}),
  saveWorkerPool: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.cluster('save-worker-pool'),
      mutationFn: ({ id, input }: { id: string; input: VirtualizationWorkerPoolInput }) =>
        virtualizationApi.saveWorkerPool(id, input),
      onSuccess: (_response, { input }) =>
        invalidateVirtualizationQueries(queryClient, [
          virtualizationKeys.workerPools(input.spec.connectionId),
        ]),
    }),
  deleteWorkerPool: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.cluster('delete-worker-pool'),
      mutationFn: ({ id, revision }: { id: string; revision: number; connectionId: string }) =>
        virtualizationApi.deleteWorkerPool(id, revision),
      onSuccess: (_response, { connectionId }) =>
        invalidateVirtualizationQueries(queryClient, [
          virtualizationKeys.workerPools(connectionId),
        ]),
    }),
  createWorker: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.vm('create-worker'),
      mutationFn: ({ id, input }: { id: string; input: VirtualizationWorkerCreateInput }) =>
        virtualizationApi.createWorker(id, input),
      onSuccess: (operation) =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.vmChanged(operation.vmId)),
    }),
  planCreateVm: () =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.vm('create-plan'),
      mutationFn: (payload: CreateVirtualMachineInput) => virtualizationApi.planVmCreate(payload),
    }),
  createVm: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.vm('create'),
      mutationFn: ({ payload, idempotencyKey }: CreateVirtualMachineVariables) =>
        virtualizationApi.createVm(payload, idempotencyKey),
      onSuccess: (operation) =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.vmChanged(operation.vmId)),
    }),
  powerVm: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.vm('power'),
      mutationFn: ({ id, action }: PowerVirtualMachineVariables) =>
        virtualizationApi.powerVm(id, action),
      onSuccess: (_response, variables) =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.vmChanged(variables.id)),
    }),
  resizeVm: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.vm('resize'),
      mutationFn: ({ id, payload }: ResizeVirtualMachineVariables) =>
        virtualizationApi.resizeVm(id, payload),
      onSuccess: (_response, variables) =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.vmChanged(variables.id)),
    }),
  createCluster: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.cluster('create'),
      mutationFn: (payload: VirtualizationClusterInput) => virtualizationApi.createCluster(payload),
      onSuccess: () =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.clusterChanged()),
    }),
  updateCluster: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.cluster('update'),
      mutationFn: ({ id, payload }: UpdateVirtualizationClusterVariables) =>
        virtualizationApi.updateCluster(id, payload),
      onSuccess: () =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.clusterChanged()),
    }),
  clusterDeleteDependencies: () =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.cluster('delete-dependencies'),
      mutationFn: (id: string) => virtualizationApi.clusterDeleteDependencies(id),
    }),
  deleteCluster: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.cluster('delete'),
      mutationFn: ({ id, force }: DeleteVirtualizationClusterVariables) =>
        virtualizationApi.deleteCluster(id, { force }),
      onSuccess: () => invalidateVirtualizationQueries(queryClient, [virtualizationKeys.all]),
    }),
  testCluster: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.cluster('test'),
      mutationFn: (id: string) => virtualizationApi.testCluster(id),
      onSuccess: () =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.clusterChanged()),
    }),
  testClusters: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.cluster('test-many'),
      mutationFn: (ids: string[]) => Promise.allSettled(ids.map(virtualizationApi.testCluster)),
      onSuccess: () =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.clusterChanged()),
    }),
  syncCluster: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.cluster('sync'),
      mutationFn: (id: string) => virtualizationApi.syncCluster(id),
      onSuccess: () => invalidateVirtualizationQueries(queryClient, [virtualizationKeys.all]),
    }),
  syncClusters: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.cluster('sync-many'),
      mutationFn: (ids: string[]) => Promise.all(ids.map(virtualizationApi.syncCluster)),
      onSuccess: () => invalidateVirtualizationQueries(queryClient, [virtualizationKeys.all]),
    }),
  createImage: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.image('create'),
      mutationFn: (payload: VirtualizationImageInput) => virtualizationApi.createImage(payload),
      onSuccess: () =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.imageChanged()),
    }),
  updateImage: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.image('update'),
      mutationFn: ({ id, payload }: UpdateVirtualizationImageVariables) =>
        virtualizationApi.updateImage(id, payload),
      onSuccess: () =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.imageChanged()),
    }),
  deleteImage: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.image('delete'),
      mutationFn: (id: string) => virtualizationApi.deleteImage(id),
      onSuccess: () =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.imageChanged()),
    }),
  createFlavor: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.flavor('create'),
      mutationFn: (payload: VirtualizationFlavorInput) => virtualizationApi.createFlavor(payload),
      onSuccess: () =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.flavorChanged()),
    }),
  updateFlavor: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.flavor('update'),
      mutationFn: ({ id, payload }: UpdateVirtualizationFlavorVariables) =>
        virtualizationApi.updateFlavor(id, payload),
      onSuccess: () =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.flavorChanged()),
    }),
  deleteFlavor: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.flavor('delete'),
      mutationFn: (id: string) => virtualizationApi.deleteFlavor(id),
      onSuccess: () =>
        invalidateVirtualizationQueries(queryClient, invalidationKeys.flavorChanged()),
    }),
  cancelOperation: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.operation('cancel'),
      mutationFn: (id: string) => virtualizationApi.cancelOperation(id),
      onSuccess: (response) => invalidateOperationCaches(queryClient, [response]),
    }),
  cancelOperations: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.operation('cancel-many'),
      mutationFn: (ids: string[]) => Promise.all(ids.map(virtualizationApi.cancelOperation)),
      onSuccess: (responses) => invalidateOperationCaches(queryClient, responses),
    }),
  retryOperation: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.operation('retry'),
      mutationFn: (id: string) => virtualizationApi.retryOperation(id),
      onSuccess: (response) => invalidateOperationCaches(queryClient, [response]),
    }),
  retryOperations: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.operation('retry-many'),
      mutationFn: (ids: string[]) => Promise.all(ids.map(virtualizationApi.retryOperation)),
      onSuccess: (responses) => invalidateOperationCaches(queryClient, responses),
    }),
  syncAll: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: virtualizationMutationKeys.sync(),
      mutationFn: virtualizationApi.syncAll,
      onSuccess: () => invalidateVirtualizationQueries(queryClient, [virtualizationKeys.all]),
    }),
}
