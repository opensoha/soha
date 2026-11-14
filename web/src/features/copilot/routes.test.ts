import { describe, expect, it, vi } from 'vitest'
import { validateRouteDefinitions } from '@/routes/definitions'
import { copilotGatewayRoutes, copilotRouteManifests } from './routes'

const routePages = vi.hoisted(() => ({
  relay: () => null,
  manifest: () => null,
  clients: () => null,
  tokens: () => null,
  governance: () => null,
  callLogs: () => null,
}))

vi.mock('./gateway/pages/relay-page', () => ({ AIGatewayRelayPage: routePages.relay }))
vi.mock('./gateway/pages/manifest-page', () => ({
  AIGatewayManifestPage: routePages.manifest,
}))
vi.mock('./gateway/pages/clients-page', () => ({
  AIGatewayClientsPage: routePages.clients,
}))
vi.mock('./gateway/pages/tokens-page', () => ({ AIGatewayTokensPage: routePages.tokens }))
vi.mock('./gateway/pages/governance-page', () => ({
  AIGatewayGovernancePage: routePages.governance,
}))
vi.mock('./gateway/pages/call-logs-page', () => ({
  AIGatewayCallLogsPage: routePages.callLogs,
}))
describe('Copilot route manifests', () => {
  it('loads every Gateway UI route from its own leaf module', async () => {
    const expectedPages = new Map([
      ['/ai-gateway/relay', routePages.relay],
      ['/ai-gateway/manifest', routePages.manifest],
      ['/ai-gateway/clients', routePages.clients],
      ['/ai-gateway/tokens', routePages.tokens],
      ['/ai-gateway/governance', routePages.governance],
      ['/ai-gateway/call-logs', routePages.callLogs],
    ])

    for (const route of copilotGatewayRoutes) {
      expect((await route.load()).default).toBe(expectedPages.get(route.meta.path))
    }
  })

  it('keeps only canonical Gateway leaf routes', () => {
    expect(copilotGatewayRoutes).toHaveLength(6)
    expect(copilotGatewayRoutes.every((route) => route.meta.parentId === 'ai-workbench')).toBe(true)
    expect(copilotGatewayRoutes.some((route) => 'wildcard' in route && route.wildcard)).toBe(false)
  })

  it('places Gateway and AI engineering routes in one workbench', () => {
    const routes = copilotRouteManifests.flatMap((manifest) => [...manifest])
    const ids = [
      'ai-workbench-overview',
      'ai-workbench-knowledge',
      'ai-workbench-context',
      'ai-workbench-agent-runs',
      'ai-workbench-agent-providers',
      'ai-workbench-evaluations',
      'ai-gateway-relay',
      'ai-gateway-governance',
    ]
    for (const id of ids) {
      const meta = routes.find((route) => route.meta.id === id)?.meta as
        | { workbenchId?: string }
        | undefined
      expect(meta?.workbenchId).toBe('ai')
    }
  })

  it('gates Agent Providers with its exact view permission', () => {
    const routes = copilotRouteManifests.flatMap((manifest) => [...manifest])
    const route = routes.find((item) => item.meta.id === 'ai-workbench-agent-providers')
    const meta = route?.meta as { permissionKey?: string } | undefined

    expect(meta?.permissionKey).toBe('ai.agent-providers.view')
  })

  it('forms one valid Copilot registry', () => {
    const routes = copilotRouteManifests.flatMap((manifest) => [...manifest])
    expect(routes).toHaveLength(31)
    expect(validateRouteDefinitions(routes)).toEqual([])
  })
})
