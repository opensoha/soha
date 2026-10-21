// Synthetic API data for Task114 browser checks; never used by production code.
const createdAt = '2026-10-21T08:00:00Z'
const applications = [
  {
    id: 'retail',
    key: 'retail',
    name: '零售业务',
    group: '零售研发',
    ownerTeam: '零售研发',
    enabled: true,
    version: 1,
    buildSources: [],
    createdAt,
    updatedAt: createdAt,
  },
  {
    id: 'analytics',
    key: 'analytics',
    name: '实时计算',
    group: '数据平台',
    ownerTeam: '数据平台',
    enabled: true,
    version: 1,
    buildSources: [],
    createdAt,
    updatedAt: createdAt,
  },
]
const names = [
  '订单 API',
  '支付服务',
  '交易前台',
  '运营后台',
  '商品搜索',
  '业务消息',
  'API 网关',
  '消息通知',
  '库存服务',
  '报表任务',
]
const keys = [
  'order-api',
  'payment-api',
  'checkout-web',
  'admin-console',
  'search',
  'messages',
  'gateway',
  'notification',
  'inventory',
  'reports',
]
const services = names.map((name, index) => ({
  id: keys[index],
  applicationId: 'retail',
  key: keys[index],
  name,
  serviceKind: index === 2 || index === 3 ? 'helm_release' : 'kubernetes_workload',
  enabled: true,
  containers: [],
  createdAt,
  updatedAt: createdAt,
}))
const podNames = new Set(
  services.flatMap((service) => [0, 1].map((index) => `${service.key}-pod-${index}`)),
)
const catalog = ['dev', 'test', 'prod'].map((id, index) => ({
  id,
  key: id,
  name: ['开发环境', '测试环境', '生产环境'][index],
  isProduction: id === 'prod',
  requiresApproval: id === 'prod',
  enabled: true,
  stageLevel: index,
  sortOrder: index,
  createdAt,
  updatedAt: createdAt,
}))
function environments(app: string) {
  return catalog.map((item) => ({
    applicationEnvironmentId: `${app}-${item.id}`,
    environmentId: item.id,
    environmentName: item.name,
    environmentKey: item.key,
    requiresApproval: item.requiresApproval,
    status: 'available',
    workloads:
      app === 'retail' && item.id === 'test'
        ? services.map((service) => workload(service.key))
        : [],
  }))
}
function workload(name: string) {
  return {
    applicationEnvironmentId: 'retail-test',
    clusterId: 'cluster-a',
    namespace: 'retail-test',
    serviceId: name,
    serviceKey: name,
    workloadKind: 'Deployment',
    workloadName: name,
    desiredReplicas: 2,
    readyReplicas: 2,
    updatedReplicas: 2,
    availableReplicas: 2,
    healthStatus: 'healthy',
  }
}
export function task114Response(
  url: URL,
  method: string,
): { body: unknown; status?: number } | undefined {
  const path = url.pathname.replace(/^\/api\/v1/, '')
  if (method !== 'GET') return undefined
  const data = (value: unknown) => ({ body: { data: value } })
  if (path === '/access/permission-snapshot')
    return data({
      permissionKeys: [
        'workbench.delivery.view',
        'workspace.application.view',
        'delivery.applications.view',
        'delivery.application-services.view',
        'delivery.application-services.create',
        'delivery.application-environments.create',
        'platform.pods.view',
      ],
      visibleMenuIds: ['applications'],
      visibleMenus: [{ id: 'applications', path: '/applications', labelZh: '应用中心' }],
    })
  if (path === '/applications') return data(applications)
  if (path === '/application-environments') return data([])
  if (path === '/delivery/environments') return data(catalog)
  if (path === '/clusters')
    return data([
      {
        id: 'cluster-a',
        name: '研发测试集群',
        connectionMode: 'direct_kubeconfig',
        health: { status: 'healthy' },
        version: 'v1.31.0',
        createdAt,
        updatedAt: createdAt,
      },
    ])
  if (path === '/clusters/capabilities') return data([])
  if (path === '/repositories') return data([])
  if (
    /^\/clusters\/cluster-a\/workloads\/pods\/[^/]+\/metrics$/.test(path) &&
    podNames.has(path.split('/')[5]) &&
    url.searchParams.get('namespace') === 'retail-test'
  )
    return data({
      configured: true,
      provider: 'prometheus',
      resourceKind: 'Pod',
      resourceName: path.split('/')[5],
      namespace: 'retail-test',
      rangeMinutes: Number(url.searchParams.get('rangeMinutes') || 60),
      generatedAt: createdAt,
      series: [
        {
          key: 'cpu',
          label: 'CPU',
          unit: 'cores',
          latest: 0.184,
          points: [
            { timestamp: '2026-10-21T07:30:00Z', value: 0.1 },
            { timestamp: '2026-10-21T08:00:00Z', value: 0.184 },
          ],
        },
        {
          key: 'memory',
          label: '内存',
          unit: 'bytes',
          latest: 432013312,
          points: [
            { timestamp: '2026-10-21T07:30:00Z', value: 400000000 },
            { timestamp: '2026-10-21T08:00:00Z', value: 432013312 },
          ],
        },
      ],
    })
  const app = applications.find((item) => path.startsWith(`/applications/${item.id}/`))
  if (!app) return undefined
  if (path === `/applications/${app.id}/runtime`)
    return data({
      application: app,
      services: app.id === 'retail' ? services : [],
      environments: environments(app.id),
    })
  if (path === `/applications/${app.id}/detail`)
    return data({
      application: app,
      services: app.id === 'retail' ? services : [],
      environments: [],
      bindings: [],
      builds: [],
      workflows: [],
      releases: [],
    })
  if (path === `/applications/${app.id}/services`) return data(app.id === 'retail' ? services : [])
  const match = path.match(
    /^\/applications\/retail\/application-environments\/retail-test\/workloads\/([^/]+)\/runtime$/,
  )
  if (match)
    return data({
      workload: workload(match[1]),
      pods: [0, 1].map((index) => ({
        name: `${match[1]}-pod-${index}`,
        namespace: 'retail-test',
        phase: 'Running',
        readyContainers: '2/2',
        restarts: index,
        nodeName: 'worker-1',
        podIp: `10.0.0.${index + 1}`,
        ageSeconds: 600,
      })),
      deployment: {
        name: match[1],
        namespace: 'retail-test',
        desiredReplicas: 2,
        readyReplicas: 2,
        updatedReplicas: 2,
        available: 2,
        containers: [],
        relatedResources: [],
      },
      services: [],
      ingresses: [],
    })
  return undefined
}
