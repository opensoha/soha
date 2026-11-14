import { mutationOptions, type QueryClient, type QueryKey } from '@tanstack/react-query'
import type { ProgressiveRolloutControlInput } from '@opensoha/contracts/gen/ts/sohaapi'
import { isApiError } from '@/services/api-error'
import { deliveryApi } from './api'
import { deliveryKeys, deliveryMutationKeys } from './keys'
import { saveServiceSetup } from './service-setup'
import { manifestKeys } from './manifests/keys'
import type {
  DeliveryTriggerInput,
  DeliveryBatchInput,
  DeliveryBatchActionInput,
  DeliveryWorkflowInput,
  ApplicationServiceCreateInput,
  ApplicationServiceDeleteInput,
  ApplicationServiceUpdateInput,
  ApplicationWorkflowSaveInput,
  BuildTemplateInput,
  ServiceDeploymentTemplateInput,
  DeploymentTemplatePreviewInput,
  DeliveryDeploymentRollbackInput,
  DeliveryPlanRequest,
  DeliveryRecordInput,
  DeliveryUpdateInput,
  DeliveryWorkloadRestartInput,
  ExecutionCallbackInput,
  ExecutionTaskActionInput,
  KubernetesServiceImportInput,
  HelmReleaseImportInput,
  RegistryInput,
  WorkflowDecisionInput,
} from './types'

function uniqueKeys(keys: QueryKey[]) {
  const seen = new Set<string>()
  return keys.filter((key) => {
    const fingerprint = JSON.stringify(key)
    if (seen.has(fingerprint)) return false
    seen.add(fingerprint)
    return true
  })
}

export function invalidateDeliveryKeys(queryClient: QueryClient, keys: QueryKey[]) {
  return Promise.all(
    uniqueKeys(keys).map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  )
}

export function invalidateApplicationQueries(queryClient: QueryClient) {
  return invalidateDeliveryKeys(queryClient, [
    deliveryKeys.applications.all,
    deliveryKeys.environments.all,
    deliveryKeys.releaseBoard.all,
  ])
}

export function invalidateEnvironmentQueries(queryClient: QueryClient) {
  return invalidateDeliveryKeys(queryClient, [
    deliveryKeys.environments.all,
    deliveryKeys.applications.all,
    deliveryKeys.releaseBoard.all,
  ])
}

export function invalidateRuntimeQueries(queryClient: QueryClient) {
  return invalidateDeliveryKeys(queryClient, [
    deliveryKeys.batches.all,
    deliveryKeys.plans.all,
    deliveryKeys.applications.all,
    deliveryKeys.builds.all,
    deliveryKeys.workflows.all,
    deliveryKeys.releases.all,
    deliveryKeys.releaseBoard.all,
    deliveryKeys.releaseBundles.all,
    deliveryKeys.executionTasks.all,
    deliveryKeys.runtime.all,
  ])
}

export const deliveryMutations = {
  triggers: {
    save: (client: QueryClient) =>
      mutationOptions({
        mutationFn: ({ id, input }: { id?: string; input: DeliveryTriggerInput }) =>
          id ? deliveryApi.triggers.update(id, input) : deliveryApi.triggers.create(input),
        onSuccess: () => client.invalidateQueries({ queryKey: deliveryKeys.triggers.all }),
      }),
  },
  deliveryWorkflows: {
    save: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: [...deliveryMutationKeys.all, 'delivery-workflows', 'save'],
        mutationFn: ({ id, payload }: { id?: string; payload: DeliveryWorkflowInput }) =>
          id
            ? deliveryApi.deliveryWorkflows.update(id, payload)
            : deliveryApi.deliveryWorkflows.create(payload),
        onSuccess: () =>
          queryClient.invalidateQueries({ queryKey: deliveryKeys.deliveryWorkflows.all }),
      }),
  },
  batches: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: [...deliveryMutationKeys.all, 'batches', 'create'],
        mutationFn: (input: DeliveryBatchInput) => deliveryApi.batches.create(input),
        onSuccess: () =>
          invalidateDeliveryKeys(queryClient, [
            deliveryKeys.batches.all,
            deliveryKeys.applications.all,
          ]),
      }),
    cancel: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: [...deliveryMutationKeys.all, 'batches', 'cancel'],
        mutationFn: ({ id, input }: { id: string; input?: DeliveryBatchActionInput }) =>
          deliveryApi.batches.cancel(id, input),
        onSuccess: () =>
          invalidateDeliveryKeys(queryClient, [
            deliveryKeys.batches.all,
            deliveryKeys.executionTasks.all,
          ]),
      }),
  },
  repositories: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.repositories('create'),
        mutationFn: (payload: DeliveryRecordInput) => deliveryApi.repositories.create(payload),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: deliveryKeys.repositories.all }),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.repositories('update'),
        mutationFn: ({ id, payload }: DeliveryUpdateInput<DeliveryRecordInput>) =>
          deliveryApi.repositories.update(id, payload),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: deliveryKeys.repositories.all }),
      }),
    delete: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.repositories('delete'),
        mutationFn: deliveryApi.repositories.delete,
        onSuccess: () => queryClient.invalidateQueries({ queryKey: deliveryKeys.repositories.all }),
      }),
  },
  applications: {
    saveServiceSetup: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.applicationServices('setup'),
        mutationFn: saveServiceSetup,
        onSettled: () =>
          invalidateDeliveryKeys(queryClient, [
            deliveryKeys.applications.all,
            deliveryKeys.repositories.all,
            deliveryKeys.environments.all,
            deliveryKeys.releaseBoard.all,
            manifestKeys.all,
          ]),
      }),
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.applications('create'),
        mutationFn: (payload: DeliveryRecordInput) => deliveryApi.applications.create(payload),
        onSuccess: () => invalidateApplicationQueries(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.applications('update'),
        mutationFn: ({ id, payload }: DeliveryUpdateInput<DeliveryRecordInput>) =>
          deliveryApi.applications.update(id, payload),
        onSuccess: () => invalidateApplicationQueries(queryClient),
        onSettled: (_data, error) => {
          if (isApiError(error) && error.status === 409)
            return invalidateApplicationQueries(queryClient)
        },
      }),
    delete: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.applications('delete'),
        mutationFn: deliveryApi.applications.delete,
        onSuccess: () => invalidateApplicationQueries(queryClient),
      }),
    createService: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.applicationServices('create'),
        mutationFn: ({ applicationId, payload }: ApplicationServiceCreateInput) =>
          deliveryApi.applications.createService(applicationId, payload),
        onSuccess: (_result, variables) =>
          queryClient.invalidateQueries({
            queryKey: deliveryKeys.applications.detail(variables.applicationId),
          }),
      }),
    updateService: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.applicationServices('update'),
        mutationFn: ({ applicationId, serviceId, payload }: ApplicationServiceUpdateInput) =>
          deliveryApi.applications.updateService(applicationId, serviceId, payload),
        onSuccess: (_result, variables) =>
          queryClient.invalidateQueries({
            queryKey: deliveryKeys.applications.detail(variables.applicationId),
          }),
      }),
    deleteService: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.applicationServices('delete'),
        mutationFn: ({ applicationId, serviceId }: ApplicationServiceDeleteInput) =>
          deliveryApi.applications.deleteService(applicationId, serviceId),
        onSuccess: (_result, variables) =>
          queryClient.invalidateQueries({
            queryKey: deliveryKeys.applications.detail(variables.applicationId),
          }),
      }),
  },
  environments: {
    importKubernetesServices: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.environments('import-kubernetes-services'),
        mutationFn: (payload: KubernetesServiceImportInput) =>
          deliveryApi.environments.importKubernetesServices(payload),
        onSuccess: () => invalidateEnvironmentQueries(queryClient),
      }),
    importHelmReleases: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.environments('import-helm-releases'),
        mutationFn: (payload: HelmReleaseImportInput) =>
          deliveryApi.environments.importHelmReleases(payload),
        onSuccess: () => invalidateEnvironmentQueries(queryClient),
      }),
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.environments('create'),
        mutationFn: deliveryApi.environments.create,
        onSuccess: () => invalidateEnvironmentQueries(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.environments('update'),
        mutationFn: ({ id, payload }: DeliveryUpdateInput<DeliveryRecordInput>) =>
          deliveryApi.environments.update(id, payload),
        onSuccess: () => invalidateEnvironmentQueries(queryClient),
      }),
    saveWorkflow: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.environments('save-workflow'),
        mutationFn: ({ applicationId, id, payload }: ApplicationWorkflowSaveInput) =>
          deliveryApi.environments.saveWorkflow(applicationId, id, payload),
        onSuccess: () => invalidateEnvironmentQueries(queryClient),
      }),
    delete: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.environments('delete'),
        mutationFn: deliveryApi.environments.delete,
        onSuccess: () => invalidateEnvironmentQueries(queryClient),
      }),
  },
  deploymentTemplates: {
    publish: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.deploymentTemplates('publish'),
        mutationFn: ({ id, expectedRevision }: { id: string; expectedRevision: number }) =>
          deliveryApi.deploymentTemplates.publish(id, expectedRevision),
        onSuccess: () =>
          queryClient.invalidateQueries({ queryKey: deliveryKeys.deploymentTemplates.all }),
      }),
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.deploymentTemplates('create'),
        mutationFn: deliveryApi.deploymentTemplates.create,
        onSuccess: () =>
          queryClient.invalidateQueries({ queryKey: deliveryKeys.deploymentTemplates.all }),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.deploymentTemplates('update'),
        mutationFn: ({ id, payload }: DeliveryUpdateInput<ServiceDeploymentTemplateInput>) =>
          deliveryApi.deploymentTemplates.update(id, payload),
        onSuccess: () =>
          queryClient.invalidateQueries({ queryKey: deliveryKeys.deploymentTemplates.all }),
      }),
    delete: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.deploymentTemplates('delete'),
        mutationFn: deliveryApi.deploymentTemplates.delete,
        onSuccess: () =>
          queryClient.invalidateQueries({ queryKey: deliveryKeys.deploymentTemplates.all }),
      }),
    preview: () =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.deploymentTemplates('preview'),
        mutationFn: ({
          applicationId,
          payload,
        }: {
          applicationId: string
          payload: DeploymentTemplatePreviewInput
        }) => deliveryApi.deploymentTemplates.preview(applicationId, payload),
      }),
  },
  buildTemplates: {
    publish: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.buildTemplates('publish'),
        mutationFn: ({ id, expectedRevision }: { id: string; expectedRevision: number }) =>
          deliveryApi.buildTemplates.publish(id, expectedRevision),
        onSuccess: () =>
          queryClient.invalidateQueries({ queryKey: deliveryKeys.buildTemplates.all }),
      }),
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.buildTemplates('create'),
        mutationFn: deliveryApi.buildTemplates.create,
        onSuccess: () =>
          queryClient.invalidateQueries({ queryKey: deliveryKeys.buildTemplates.all }),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.buildTemplates('update'),
        mutationFn: ({ id, payload }: DeliveryUpdateInput<BuildTemplateInput>) =>
          deliveryApi.buildTemplates.update(id, payload),
        onSuccess: () =>
          queryClient.invalidateQueries({ queryKey: deliveryKeys.buildTemplates.all }),
      }),
    delete: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.buildTemplates('delete'),
        mutationFn: deliveryApi.buildTemplates.delete,
        onSuccess: () =>
          queryClient.invalidateQueries({ queryKey: deliveryKeys.buildTemplates.all }),
      }),
  },
  workflowTemplates: {
    publish: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.workflowTemplates('publish'),
        mutationFn: ({ id, expectedRevision }: { id: string; expectedRevision: number }) =>
          deliveryApi.workflowTemplates.publish(id, expectedRevision),
        onSuccess: () =>
          queryClient.invalidateQueries({ queryKey: deliveryKeys.workflowTemplates.all }),
      }),
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.workflowTemplates('create'),
        mutationFn: deliveryApi.workflowTemplates.create,
        onSuccess: () =>
          invalidateDeliveryKeys(queryClient, [
            deliveryKeys.workflowTemplates.all,
            deliveryKeys.environments.all,
            deliveryKeys.releaseBoard.all,
          ]),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.workflowTemplates('update'),
        mutationFn: ({ id, payload }: DeliveryUpdateInput<DeliveryRecordInput>) =>
          deliveryApi.workflowTemplates.update(id, payload),
        onSuccess: () =>
          invalidateDeliveryKeys(queryClient, [
            deliveryKeys.workflowTemplates.all,
            deliveryKeys.environments.all,
            deliveryKeys.releaseBoard.all,
          ]),
      }),
    delete: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.workflowTemplates('delete'),
        mutationFn: deliveryApi.workflowTemplates.delete,
        onSuccess: () =>
          invalidateDeliveryKeys(queryClient, [
            deliveryKeys.workflowTemplates.all,
            deliveryKeys.environments.all,
            deliveryKeys.releaseBoard.all,
          ]),
      }),
  },
  blueprints: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.blueprints('create'),
        mutationFn: deliveryApi.blueprints.create,
        onSuccess: () => queryClient.invalidateQueries({ queryKey: deliveryKeys.blueprints.all }),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.blueprints('update'),
        mutationFn: ({ id, payload }: DeliveryUpdateInput<DeliveryRecordInput>) =>
          deliveryApi.blueprints.update(id, payload),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: deliveryKeys.blueprints.all }),
      }),
    renderSpec: () =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.blueprints('render-spec'),
        mutationFn: deliveryApi.blueprints.renderSpec,
      }),
  },
  workflows: {
    approve: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.workflows('approve'),
        mutationFn: (payload: WorkflowDecisionInput) => deliveryApi.workflows.approve(payload),
        onSuccess: () => invalidateRuntimeQueries(queryClient),
      }),
    reject: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.workflows('reject'),
        mutationFn: (payload: WorkflowDecisionInput) => deliveryApi.workflows.reject(payload),
        onSuccess: () => invalidateRuntimeQueries(queryClient),
      }),
  },
  registries: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.registries('create'),
        mutationFn: (payload: RegistryInput) => deliveryApi.registries.create(payload),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: deliveryKeys.registries.all }),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.registries('update'),
        mutationFn: ({ id, payload }: DeliveryUpdateInput<RegistryInput>) =>
          deliveryApi.registries.update(id, payload),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: deliveryKeys.registries.all }),
      }),
    delete: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.registries('delete'),
        mutationFn: deliveryApi.registries.delete,
        onSuccess: () => queryClient.invalidateQueries({ queryKey: deliveryKeys.registries.all }),
      }),
  },
  executionTasks: {
    controlRollout: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.executionTasks('rollout'),
        mutationFn: ({ id, payload }: DeliveryUpdateInput<ProgressiveRolloutControlInput>) =>
          deliveryApi.executionTasks.controlRollout(id, payload),
        onSuccess: (state, { id }) => {
          queryClient.setQueryData(deliveryKeys.executionTasks.rollout(id), state)
          return invalidateRuntimeQueries(queryClient)
        },
      }),
    callback: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.executionTasks('callback'),
        mutationFn: (payload: ExecutionCallbackInput) =>
          deliveryApi.executionTasks.callback(payload),
        onSuccess: () => invalidateRuntimeQueries(queryClient),
      }),
    cancel: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.executionTasks('cancel'),
        mutationFn: (payload: ExecutionTaskActionInput) =>
          deliveryApi.executionTasks.cancel(payload),
        onSuccess: () => invalidateRuntimeQueries(queryClient),
      }),
    retry: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.executionTasks('retry'),
        mutationFn: (payload: ExecutionTaskActionInput) =>
          deliveryApi.executionTasks.retry(payload),
        onSuccess: () => invalidateRuntimeQueries(queryClient),
      }),
  },
  workloads: {
    restart: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.workloads('restart'),
        mutationFn: (payload: DeliveryWorkloadRestartInput) =>
          deliveryApi.workloads.restart(payload),
        onSuccess: () =>
          invalidateDeliveryKeys(queryClient, [
            deliveryKeys.workloads.all,
            deliveryKeys.applications.all,
          ]),
      }),
  },
  deployments: {
    rollback: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.deployments('rollback'),
        mutationFn: (payload: DeliveryDeploymentRollbackInput) =>
          deliveryApi.deployments.rollback(payload),
        onSuccess: () =>
          invalidateDeliveryKeys(queryClient, [
            deliveryKeys.deployments.all,
            deliveryKeys.environments.all,
            deliveryKeys.applications.all,
            deliveryKeys.releaseBoard.all,
          ]),
      }),
  },
  plans: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.plans('create'),
        mutationFn: (payload: DeliveryPlanRequest) => deliveryApi.plans.create(payload),
        onSuccess: (plan) =>
          invalidateDeliveryKeys(queryClient, [
            deliveryKeys.plans.detail(plan.id),
            deliveryKeys.applications.detail(plan.applicationId),
          ]),
      }),
    confirm: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.plans('confirm'),
        mutationFn: deliveryApi.plans.confirm,
        onSuccess: () => invalidateRuntimeQueries(queryClient),
      }),
    approval: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: deliveryMutationKeys.plans('approval'),
        mutationFn: ({
          id,
          action,
          comment,
        }: {
          id: string
          action: 'approve' | 'reject'
          comment?: string
        }) => deliveryApi.plans.approval(id, { action, comment }),
        onSuccess: () => invalidateRuntimeQueries(queryClient),
      }),
  },
}
