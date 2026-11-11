import { defineRoutes } from '@/routes/definitions'
import { identityApplicationRoutes } from './applications/routes'
import { identityLoginRecordRoutes } from './login-records/routes'
import { identityOutpostRoutes } from './outposts/routes'
import { internalWorkbenchOverviewRoutes } from './overview/routes'
import { identityPolicyRoutes } from './policies/routes'
import { identityProviderRoutes } from './providers/routes'
import { internalWorkbenchSoftwareRoutes } from './software/routes'

export const identityParentRoutes = defineRoutes([
  {
    meta: {
      id: 'identity',
      path: '/identity',
      title: 'Identity',
      description: '身份工作台',
      icon: 'IconShield',
      group: 'identity',
      workbenchId: 'security',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      menuId: 'identity',
      permissionStrategy: 'any-child',
      scopeMode: 'passive',
      workspace: 'system',
    },
    shell: 'app',
    redirectTo: '/internal-workbench/overview',
  },
] as const)

export const identityRouteManifests = [
  identityParentRoutes,
  internalWorkbenchOverviewRoutes,
  internalWorkbenchSoftwareRoutes,
  identityApplicationRoutes,
  identityProviderRoutes,
  identityOutpostRoutes,
  identityLoginRecordRoutes,
  identityPolicyRoutes,
] as const
