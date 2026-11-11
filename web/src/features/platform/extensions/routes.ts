import { defineRoutes } from '@/routes/definitions'

export const extensionRoutes = defineRoutes([
  {
    meta: {
      id: 'extensions',
      path: '/extensions',
      title: 'CRD',
      description: 'CRD 管理',
      icon: 'IconPuzzle',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      menuId: 'extensions',
      permissionKey: 'platform.extensions.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./crds/list-page')
      return { default: module.CRDPage }
    },
  },
  {
    meta: {
      id: 'extensions-group-detail',
      path: '/extensions/apis/:groupName',
      title: 'CRD API Detail',
      description: 'CRD API 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'extensions',
    load: async () => {
      const module = await import('./crds/api-group-detail-page')
      return { default: module.CRDApiGroupDetailPage }
    },
  },
  {
    meta: {
      id: 'helm',
      path: '/helm',
      title: 'Helm',
      description: 'Helm 管理',
      icon: 'IconPuzzle',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: true,
      menuId: 'helm',
      permissionKey: 'platform.helm.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    redirectTo: '/helm/releases',
  },
  {
    meta: {
      id: 'helm-releases',
      path: '/helm/releases',
      title: 'Helm Releases',
      description: 'Helm 发布',
      icon: 'IconPuzzle',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'helm',
      menuId: 'helm-releases',
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'helm',
    load: async () => {
      const module = await import('./helm/releases/list-page')
      return { default: module.HelmReleasesPage }
    },
  },
  {
    meta: {
      id: 'helm-release-detail',
      path: '/helm/releases/:releaseName',
      title: 'Helm Release Detail',
      description: 'Helm 发布详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'helm-releases',
    load: async () => {
      const module = await import('./helm/releases/detail-page')
      return { default: module.HelmReleaseDetailPage }
    },
  },
  {
    meta: {
      id: 'helm-charts',
      path: '/helm/charts',
      title: 'Helm Charts',
      description: 'Helm 图表',
      icon: 'IconPuzzle',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'helm',
      menuId: 'helm-charts',
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'helm',
    load: async () => {
      const module = await import('./helm/charts/page')
      return { default: module.HelmChartsPage }
    },
  },
] as const)
