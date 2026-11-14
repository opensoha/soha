import { describe, expect, it, vi } from 'vitest'
import { validateRouteDefinitions } from '@/routes/definitions'
import { copilotObserveRouteManifests, copilotObserveRoutes } from './routes'

const routePages = vi.hoisted(() => ({
  chat: () => null,
  heya: () => null,
  rootCause: () => null,
  performance: () => null,
  operations: () => null,
  mcp: () => null,
  dataSources: () => null,
  skills: () => null,
  tools: () => null,
  modelSettings: () => null,
  overview: () => null,
  knowledge: () => null,
  context: () => null,
  agentRuns: () => null,
  agentProviders: () => null,
  evaluation: () => null,
  knowledgePipelines: () => null,
  environments: () => null,
  providerFleet: () => null,
  evaluationLifecycle: () => null,
  memory: () => null,
  productionOperations: () => null,
}))

vi.mock('../workbench/pages/chat-page', () => ({ AIWorkbenchChatPage: routePages.chat }))
vi.mock('../workbench/pages/heya-page', () => ({ HeyaPage: routePages.heya }))
vi.mock('../workbench/pages/root-cause-page', () => ({
  AIWorkbenchRootCausePage: routePages.rootCause,
}))
vi.mock('../workbench/pages/performance-page', () => ({
  AIWorkbenchPerformancePage: routePages.performance,
}))
vi.mock('./operations/page', () => ({ AIOperationsPage: routePages.operations }))
vi.mock('./mcp/page', () => ({ AIMCPPage: routePages.mcp }))
vi.mock('./data-sources/page', () => ({ AIDataSourcesPage: routePages.dataSources }))
vi.mock('./skills/page', () => ({ AISkillsPage: routePages.skills }))
vi.mock('./tools/page', () => ({ AIToolsPage: routePages.tools }))
vi.mock('./model-settings/page', () => ({
  AIModelSettingsPage: routePages.modelSettings,
}))
vi.mock('./overview/page', () => ({ AIObserveOverviewPage: routePages.overview }))
vi.mock('../knowledge/page', () => ({ KnowledgeCenterPage: routePages.knowledge }))
vi.mock('../context/page', () => ({ ContextInspectorPage: routePages.context }))
vi.mock('../agent-runs/page', () => ({ AgentRunsPage: routePages.agentRuns }))
vi.mock('../agent-providers/page', () => ({ AgentProvidersPage: routePages.agentProviders }))
vi.mock('../evaluation/page', () => ({ EvaluationStudioPage: routePages.evaluation }))
vi.mock('../knowledge-production/page', () => ({
  KnowledgeProductionPage: routePages.knowledgePipelines,
}))
vi.mock('../environments/page', () => ({ EnvironmentsPage: routePages.environments }))
vi.mock('../provider-fleet/page', () => ({ ProviderFleetPage: routePages.providerFleet }))
vi.mock('../evaluation-lifecycle/page', () => ({
  EvaluationLifecyclePage: routePages.evaluationLifecycle,
}))
vi.mock('../memory/page', () => ({ MemoryPoliciesPage: routePages.memory }))
vi.mock('../production-operations/page', () => ({
  AIProductionOperationsPage: routePages.productionOperations,
}))
describe('Copilot Observe route manifests', () => {
  it('uses an authenticated focus shell only for conversation routes', () => {
    const focused = copilotObserveRoutes.filter((route) => route.shell === 'focus')
    expect(focused.map((route) => route.meta.path)).toEqual([
      '/ai-workbench/chat',
      '/ai-workbench/root-cause',
      '/ai-workbench/performance',
      '/heya',
      '/heya/access',
    ])
    for (const route of focused) {
      expect(route.meta).toMatchObject({
        requiresAuth: true,
        permissionKey:
          route.meta.path === '/heya/access' ? 'ai.environments.update' : 'observe.ai.chat',
      })
    }
  })
  it('loads each canonical UI from its leaf module', async () => {
    const loaded = new Map<string, unknown>()
    for (const route of copilotObserveRoutes) {
      if ('load' in route) loaded.set(route.meta.path, (await route.load()).default)
    }

    expect(copilotObserveRoutes[0].redirectTo).toBe('/ai-workbench/overview')
    expect(loaded.get('/ai-workbench/overview')).toBe(routePages.overview)
    expect(loaded.get('/ai-workbench/knowledge')).toBe(routePages.knowledge)
    expect(loaded.get('/ai-workbench/context')).toBe(routePages.context)
    expect(loaded.get('/ai-workbench/agent-runs')).toBe(routePages.agentRuns)
    expect(loaded.get('/ai-workbench/agent-providers')).toBe(routePages.agentProviders)
    expect(loaded.get('/ai-workbench/evaluations')).toBe(routePages.evaluation)
    expect(loaded.get('/ai-workbench/knowledge-pipelines')).toBe(routePages.knowledgePipelines)
    expect(loaded.get('/ai-workbench/environments')).toBe(routePages.environments)
    expect(loaded.get('/ai-workbench/provider-fleet')).toBe(routePages.providerFleet)
    expect(loaded.get('/ai-workbench/evaluation-lifecycle')).toBe(routePages.evaluationLifecycle)
    expect(loaded.get('/ai-workbench/memory')).toBe(routePages.memory)
    expect(loaded.get('/ai-workbench/production-operations')).toBe(routePages.productionOperations)
    expect(loaded.get('/ai-workbench/chat')).toBe(routePages.chat)
    expect(loaded.get('/heya')).toBe(routePages.heya)
    expect(loaded.get('/heya/access')).toBe(routePages.heya)
    expect(loaded.get('/ai-workbench/root-cause')).toBe(routePages.rootCause)
    expect(loaded.get('/ai-workbench/performance')).toBe(routePages.performance)
    expect(loaded.get('/ai-workbench/inspection')).toBe(routePages.operations)
    expect(loaded.get('/ai-workbench/mcp')).toBe(routePages.mcp)
    expect(loaded.get('/ai-workbench/data-sources')).toBe(routePages.dataSources)
    expect(loaded.get('/ai-workbench/skills')).toBe(routePages.skills)
    expect(loaded.get('/ai-workbench/tool-settings')).toBe(routePages.tools)
    expect(loaded.get('/ai-workbench/model-settings')).toBe(routePages.modelSettings)
  })

  it('has unique, valid route definitions', () => {
    const routes = copilotObserveRouteManifests.flatMap((manifest) => [...manifest])
    expect(routes).toHaveLength(25)
    expect(validateRouteDefinitions(routes)).toEqual([])
  })
})
