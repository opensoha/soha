import type { ApplicationEnvironment, ResourceRelationConfig } from '../types'
import { applicationEnvironmentPayload } from '../applications/environment-payload'

export function relationRulesPayload(
  binding: ApplicationEnvironment,
  targetId: string,
  config: ResourceRelationConfig,
) {
  const payload = applicationEnvironmentPayload(binding)
  if (!payload.targets.some((target) => target.id === targetId))
    throw new Error('目标已失效，请刷新后重新打开编辑器。')
  return {
    ...payload,
    targets: payload.targets.map((target) =>
      target.id === targetId
        ? { ...target, metadata: { ...target.metadata, resourceRelations: config } }
        : target,
    ),
  }
}
