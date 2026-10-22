import type { ApplicationEnvironment, ResourceRelationConfig } from '../types'

// Preserve unrelated binding settings and freeze the read revision at editor open.
export function relationRulesPayload(
  binding: ApplicationEnvironment,
  targetId: string,
  config: ResourceRelationConfig,
) {
  if (!binding.updatedAt || !binding.targets?.some((target) => target.id === targetId))
    throw new Error('环境版本或目标已失效，请刷新后重新打开编辑器。')
  return {
    applicationId: binding.applicationId,
    environmentId: binding.environmentId,
    expectedUpdatedAt: binding.updatedAt,
    alias: binding.alias,
    clusterId: binding.clusterId,
    namespace: binding.namespace,
    registryId: binding.registryId,
    strategyProfileId: binding.strategyProfileId,
    promotionPolicyId: binding.promotionPolicyId,
    artifactPolicyId: binding.artifactPolicyId,
    workflowTemplateId: binding.workflowTemplateId,
    workflowTemplateVersion: binding.workflowTemplateVersion,
    buildPolicy: binding.buildPolicy,
    releasePolicy: binding.releasePolicy,
    resourceSelector: binding.resourceSelector,
    targets: binding.targets.map((target) => ({
      id: target.id,
      clusterId: target.clusterId,
      namespace: target.namespace,
      targetKind: target.targetKind,
      executorKind: target.executorKind,
      groupKey: target.groupKey,
      waveKey: target.waveKey,
      regionKey: target.regionKey,
      configRef: target.configRef,
      workloadKind: target.workloadKind,
      workloadName: target.workloadName,
      containerName: target.containerName,
      docker: target.docker,
      helm: target.helm,
      enabled: target.enabled,
      metadata:
        target.id === targetId
          ? { ...target.metadata, resourceRelations: config }
          : target.metadata,
    })),
  }
}
