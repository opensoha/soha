export function baselineResponse(path: string) {
  if (path === '/api/v1/auth/refresh') {
    return {
      data: {
        tokens: {
          accessToken: 'browser-regression-token',
          refreshToken: 'browser-regression-refresh-token',
          tokenType: 'Bearer',
          expiresIn: 3600,
          expiresAt: '2099-01-01T00:00:00Z',
        },
        user: {
          userId: 'browser-regression-user',
          userName: 'browser-regression',
          email: 'browser-regression@soha.local',
          roles: [],
          teams: [],
          projects: [],
          tags: [],
        },
      },
    }
  }
  if (path === '/api/v1/access/permission-snapshot') {
    return {
      data: {
        permissionKeys: [
          'workbench.platform.view',
          'workspace.resource.view',
          'platform.clusters.view',
          'platform.deployment.view',
        ],
        visibleMenuIds: ['clusters', 'workloads', 'workloads-deployments'],
        visibleMenus: [
          { id: 'clusters', path: '/clusters', labelZh: '集群' },
          { id: 'workloads', path: '/workloads', labelZh: '工作负载' },
          {
            id: 'workloads-deployments',
            parentId: 'workloads',
            path: '/workloads/deployments',
            labelZh: 'Deployments',
          },
        ],
      },
    }
  }
  if (path === '/api/v1/settings/branding') {
    return {
      data: {
        appTitle: 'Soha',
        sidebarTitle: 'Soha',
        loginLogoUrl: '',
        expandedLogoUrl: '',
        collapsedLogoUrl: '',
        faviconUrl: '',
      },
    }
  }
  if (path === '/api/v1/modules') {
    return { data: [] }
  }
  if (path === '/api/v1/copilot/workbench/runtime-profiles') {
    return { items: [] }
  }
  if (path === '/api/v1/clusters') {
    return {
      data: [
        {
          id: 'cluster-a',
          name: 'prod-cluster',
          region: 'standard_kubernetes',
          environment: 'production',
          labels: {},
          connectionMode: 'agent',
          version: 'v1.31.0',
          health: { status: 'healthy' },
        },
      ],
    }
  }
  if (path === '/api/v1/clusters/capabilities') {
    return { data: [] }
  }
  if (path === '/api/v1/clusters/cluster-a/namespaces') {
    return {
      data: [{ name: 'monitoring', status: 'Active', labels: {} }],
    }
  }
  if (path === '/api/v1/clusters/cluster-a/workloads/deployments/api/detail') {
    return {
      data: {
        name: 'api',
        namespace: 'monitoring',
        desiredReplicas: 2,
        readyReplicas: 2,
        updatedReplicas: 2,
        availableReplicas: 2,
        observedGeneration: 1,
        strategy: 'RollingUpdate',
        labels: { app: 'api' },
        selector: { app: 'api' },
        pods: [],
        relatedResources: [],
      },
    }
  }
  if (path === '/api/v1/clusters/cluster-a/workloads/deployments/api/rollout-status') {
    return {
      data: {
        name: 'api',
        namespace: 'monitoring',
        revision: '3',
        status: 'ready',
        message: 'Deployment is available',
        desiredReplicas: 2,
        updatedReplicas: 2,
        readyReplicas: 2,
        availableReplicas: 2,
        observedGeneration: 1,
        conditions: [],
      },
    }
  }
  if (path === '/api/v1/clusters/cluster-a/workloads/deployments/api/rollouts') {
    return { data: [] }
  }
  if (path === '/api/v1/clusters/cluster-a/resources/graph') {
    return {
      data: {
        rootId: 'deployment-api',
        clusterId: 'cluster-a',
        generatedAt: '2026-10-30T10:00:00Z',
        warnings: [],
        evidence: [],
        nodes: [
          {
            id: 'deployment-api',
            resource: {
              clusterId: 'cluster-a',
              namespace: 'monitoring',
              kind: 'Deployment',
              name: 'api',
              apiVersion: 'apps/v1',
              scopeMode: 'namespace',
            },
          },
        ],
        edges: [],
      },
    }
  }
  if (
    [
      '/api/v1/application-environments',
      '/api/v1/applications',
      '/api/v1/builds',
      '/api/v1/workflows',
      '/api/v1/releases',
    ].includes(path)
  ) {
    return { data: [] }
  }
  return undefined
}
