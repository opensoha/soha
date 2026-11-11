import { useQuery } from '@tanstack/react-query'
import { api } from '@/services/api-client'
import type { ApiResponse, RuntimeMenuNode, WorkbenchModuleStatus } from '@/types'

export const moduleStatusQueryKey = ['modules'] as const
export const GLOBAL_ASSISTANT_FEATURE = 'assistant.global'

type ModuleStatusMap = Record<string, { enabled?: boolean } | boolean | undefined>

export type ModuleStatusEnvelope =
  | ApiResponse<WorkbenchModuleStatus[]>
  | WorkbenchModuleStatus[]
  | {
      data?: WorkbenchModuleStatus[] | { modules?: ModuleStatusMap }
      items?: WorkbenchModuleStatus[]
      modules?: ModuleStatusMap
    }

function moduleItems(response: ModuleStatusEnvelope | undefined): WorkbenchModuleStatus[] {
  if (!response) return []
  if (Array.isArray(response)) return response
  if (Array.isArray(response.data)) return response.data
  if ('items' in response && Array.isArray(response.items)) return response.items
  return []
}

function moduleMap(response: ModuleStatusEnvelope | undefined): ModuleStatusMap | undefined {
  if (!response || Array.isArray(response)) return undefined
  if ('modules' in response && response.modules) return response.modules
  if (response.data && !Array.isArray(response.data) && response.data.modules) {
    return response.data.modules
  }
  return undefined
}

export function getWorkbenchModuleEnabled(
  response: ModuleStatusEnvelope | undefined,
  moduleId: string,
) {
  const mapped = moduleMap(response)?.[moduleId]
  if (typeof mapped === 'boolean') return mapped
  if (mapped && typeof mapped === 'object' && typeof mapped.enabled === 'boolean')
    return mapped.enabled

  const item = moduleItems(response).find((status) => status.descriptor.id === moduleId)
  return item?.enabled === true
}

export function getWorkbenchModuleFeature(
  response: ModuleStatusEnvelope | undefined,
  moduleId: string,
  feature: string,
) {
  const item = moduleItems(response).find((status) => status.descriptor.id === moduleId)
  return item?.enabled === true && item.features?.[feature] === true
}

export function useModuleStatuses() {
  return useQuery({
    queryKey: moduleStatusQueryKey,
    queryFn: () => api.get<ModuleStatusEnvelope>('/modules'),
    staleTime: 30_000,
  })
}

export function useWorkbenchModuleEnabled(moduleId: string) {
  const query = useModuleStatuses()
  return {
    ...query,
    moduleEnabled: getWorkbenchModuleEnabled(query.data, moduleId),
  }
}

export function useWorkbenchModuleFeature(moduleId: string, feature: string) {
  const query = useModuleStatuses()
  return { ...query, featureEnabled: getWorkbenchModuleFeature(query.data, moduleId, feature) }
}

const AI_WORKBENCH_MENU_FEATURES: Record<string, string[]> = {
  'ai-workbench-knowledge-pipelines': [
    'knowledge.external_connectors',
    'knowledge.async_ingestion',
  ],
  'ai-workbench-evaluation-lifecycle': [
    'evaluation.candidate_executor',
    'evaluation.isolated_replay',
    'evaluation.release_gate',
    'evaluation.feedback_sampling',
  ],
  'ai-workbench-memory': ['memory.long_term'],
  'ai-workbench-provider-fleet': ['agent.fleet_rollout', 'agent.conformance_suite'],
  'ai-workbench-environments': ['agent.environment_management'],
  'ai-workbench-production-operations': ['ai.production_operations'],
}

export function areWorkbenchModuleFeaturesEnabled(
  response: ModuleStatusEnvelope | undefined,
  moduleId: string,
  features: string[],
) {
  return features.every((feature) => getWorkbenchModuleFeature(response, moduleId, feature))
}

export function filterMenuByModuleFeatures(
  nodes: RuntimeMenuNode[],
  response: ModuleStatusEnvelope | undefined,
): RuntimeMenuNode[] {
  const filterNode = (node: RuntimeMenuNode): RuntimeMenuNode | null => {
    const requirements = AI_WORKBENCH_MENU_FEATURES[node.id]
    if (requirements && !areWorkbenchModuleFeaturesEnabled(response, 'ai', requirements)) {
      return null
    }
    const children = (node.children ?? [])
      .map(filterNode)
      .filter((item): item is RuntimeMenuNode => Boolean(item))
    return { ...node, children: children.length > 0 ? children : undefined }
  }
  return nodes.map(filterNode).filter((item): item is RuntimeMenuNode => Boolean(item))
}
