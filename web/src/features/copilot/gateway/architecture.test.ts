import { describe, expect, it } from 'vitest'
import gatewayPageSource from './page-coordinator.tsx?raw'
import callLogsPageSource from './pages/call-logs-page.tsx?raw'
import clientsPageSource from './pages/clients-page.tsx?raw'
import governancePageSource from './pages/governance-page.tsx?raw'
import manifestPageSource from './pages/manifest-page.tsx?raw'
import relayPageSource from './pages/relay-page.tsx?raw'
import tokensPageSource from './pages/tokens-page.tsx?raw'
import gatewayGovernanceSource from './sections/governance.tsx?raw'

const routePageSources = [
  callLogsPageSource,
  clientsPageSource,
  governancePageSource,
  manifestPageSource,
  relayPageSource,
  tokensPageSource,
]
const legacyGatewayModules = import.meta.glob('../ai-gateway-{page,model}.{ts,tsx}')

describe('AI Gateway capability boundary', () => {
  it('keeps transport behind the canonical gateway data layer', () => {
    expect(gatewayPageSource).not.toContain("from '@/services/api-client'")
    expect(gatewayPageSource).toContain("from './queries'")
    expect(gatewayPageSource).toContain("from './mutations'")
  })

  it('loads every route section through an independent dynamic import', () => {
    const sections = ['models', 'manifest', 'clients', 'tokens', 'governance', 'audit']

    sections.forEach((section) => {
      expect(gatewayPageSource).toContain(`import('./sections/${section}')`)
    })
    expect(gatewayPageSource).toContain("import('./editor-drawer')")
    expect(gatewayGovernanceSource).toContain("import('./policies')")
    expect(gatewayGovernanceSource).toContain("import('./approvals')")
    expect(gatewayPageSource).not.toContain('legacy-ai-gateway')
  })

  it('keeps route leaves on the Gateway boundary with an explicit section', () => {
    routePageSources.forEach((source) => {
      expect(source).toContain("from '../page-coordinator'")
      expect(source).toMatch(/<GatewayPageCoordinator section="[^"]+" \/>/)
      expect(source).not.toContain('ai-gateway-page')
    })
    expect(gatewayPageSource).not.toContain('useLocation')
    expect(gatewayPageSource).not.toContain('gatewaySectionFromPath')
  })

  it('removes the old page and model compatibility paths', () => {
    expect(Object.keys(legacyGatewayModules)).toEqual([])
  })
})
