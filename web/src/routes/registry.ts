import { accessRoutes } from '@/features/access/routes'
import { authRoutes, authUtilityRoutes } from '@/features/auth/routes'
import { copilotRouteManifests } from '@/features/copilot/routes'
import { companionRoutes } from '@/features/companion/routes'
import { computeRoutes } from '@/features/compute/routes'
import { deliveryRoutes } from '@/features/delivery/routes'
import { identityRouteManifests } from '@/features/identity/routes'
import { observabilityRouteManifests } from '@/features/observability/routes'
import { networkAccessRoutes } from '@/features/network-access/routes'
import { platformRouteManifests, platformShellRoutes } from '@/features/platform/routes'
import { pluginRoutes } from '@/features/plugins/routes'
import { providerPortalRoutes } from '@/features/provider-portal/routes'
import { secretRoutes } from '@/features/secrets'
import { settingsRoutes } from '@/features/settings/routes'
import { systemRoutes } from '@/features/system/routes'
import { assertValidRouteDefinitions, resolveRouteDefinitions } from './definitions'
import { fallbackRoutes } from './fallback-routes'
import type { AppRouteDefinition, AppRouteShell } from './route-types'

export const featureRouteManifests = [
  accessRoutes,
  authRoutes,
  companionRoutes,
  ...copilotRouteManifests,
  ...platformRouteManifests,
  computeRoutes,
  deliveryRoutes,
  networkAccessRoutes,
  ...observabilityRouteManifests,
  providerPortalRoutes,
  pluginRoutes,
  secretRoutes,
  ...identityRouteManifests,
  settingsRoutes,
  systemRoutes,
] as const

export const appRouteDefinitions: readonly AppRouteDefinition[] = [
  ...platformShellRoutes,
  ...authUtilityRoutes,
  ...featureRouteManifests.flatMap((manifest) => [...manifest]),
  ...fallbackRoutes,
]

assertValidRouteDefinitions(appRouteDefinitions)

export const registeredRouteDefinitions = resolveRouteDefinitions(appRouteDefinitions)

export const routeMeta = registeredRouteDefinitions.map((definition) => definition.meta)

export function getRegisteredRoutesByShell(shell: AppRouteShell) {
  return registeredRouteDefinitions.filter((definition) => definition.shell === shell)
}
