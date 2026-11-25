const DEMO_USER = {
  userId: 'usr-pilot-01',
  userName: 'shanchui',
  email: 'shanchui@opensoha.io',
  roles: ['platform-admin'],
  teams: ['infra-core'],
  projects: ['soha-control-plane'],
  tags: [],
}

const DEMO_VMS = [
  {
    id: 'vm-prod-api-01',
    externalId: '101',
    name: 'prod-gateway-01',
    provider: 'pve',
    connectionId: 'conn-pve-sh',
    connectionName: 'pve-prod-shanghai',
    node: 'pve-node-01',
    status: 'running',
    powerState: 'running',
    flavorName: 'c8m16-nvme',
    cpu: 8,
    memoryMiB: 16384,
    diskGiB: 160,
    bootImageName: 'ubuntu-24.04-lts-cloudimg',
    ipAddresses: ['10.24.10.11', '172.16.0.11'],
    network: 'vmbr0',
    config: { hostname: 'prod-gateway-01.sh.internal', osType: 'l26' },
    updatedAt: '2026-11-24T09:18:00Z',
  },
  {
    id: 'vm-ai-infer-01',
    externalId: '102',
    name: 'ai-vllm-worker-a100',
    provider: 'kubevirt',
    connectionId: 'conn-kv-gpu',
    connectionName: 'kubevirt-ai-cluster',
    namespace: 'ai-serving',
    node: 'gpu-worker-03',
    status: 'running',
    powerState: 'running',
    flavorName: 'gpu-a100-32c128g',
    cpu: 32,
    memoryMiB: 131072,
    diskGiB: 500,
    bootImageName: 'rocky-9.4-cuda-12.6',
    ipAddresses: ['10.42.8.24'],
    network: 'ovn-k8s-default',
    config: { hostname: 'ai-vllm-worker-a100', osType: 'linux' },
    updatedAt: '2026-11-24T09:22:00Z',
  },
  {
    id: 'vm-pg-primary-01',
    externalId: '103',
    name: 'pg-vector-primary',
    provider: 'pve',
    connectionId: 'conn-pve-sh',
    connectionName: 'pve-prod-shanghai',
    node: 'pve-node-02',
    status: 'running',
    powerState: 'running',
    flavorName: 'm16m64-io',
    cpu: 16,
    memoryMiB: 65536,
    diskGiB: 1024,
    bootImageName: 'debian-12-bookworm',
    ipAddresses: ['10.24.12.50'],
    network: 'vmbr1-storage',
    config: { hostname: 'pg-vector-primary.sh.internal', osType: 'l26' },
    updatedAt: '2026-11-24T08:55:00Z',
  },
  {
    id: 'vm-redis-mesh-02',
    externalId: '104',
    name: 'cache-dragonfly-02',
    provider: 'pve',
    connectionId: 'conn-pve-sh',
    connectionName: 'pve-prod-shanghai',
    node: 'pve-node-03',
    status: 'error',
    powerState: 'running',
    flavorName: 'c8m32-nvme',
    cpu: 8,
    memoryMiB: 32768,
    diskGiB: 120,
    bootImageName: 'ubuntu-24.04-lts-cloudimg',
    ipAddresses: ['10.24.14.19'],
    network: 'vmbr0',
    config: { hostname: 'cache-dragonfly-02.sh.internal', osType: 'l26' },
    updatedAt: '2026-11-24T09:29:00Z',
  },
  {
    id: 'vm-runner-build-04',
    externalId: '105',
    name: 'ci-ephemeral-runner-04',
    provider: 'kubevirt',
    connectionId: 'conn-kv-gpu',
    connectionName: 'kubevirt-ai-cluster',
    namespace: 'delivery-runners',
    node: 'cpu-worker-07',
    status: 'provisioning',
    powerState: 'starting',
    flavorName: 'c16m32-ephemeral',
    cpu: 16,
    memoryMiB: 32768,
    diskGiB: 200,
    bootImageName: 'ubuntu-24.04-runner',
    ipAddresses: ['10.42.19.88'],
    network: 'ovn-k8s-default',
    config: { hostname: 'ci-runner-04', osType: 'linux' },
    updatedAt: '2026-11-24T09:30:10Z',
  },
  {
    id: 'vm-staging-sandbox-01',
    externalId: '106',
    name: 'dev-agent-sandbox-01',
    provider: 'pve',
    connectionId: 'conn-pve-hz',
    connectionName: 'pve-edge-hangzhou',
    node: 'hz-edge-01',
    status: 'stopped',
    powerState: 'stopped',
    flavorName: 'c4m8-std',
    cpu: 4,
    memoryMiB: 8192,
    diskGiB: 80,
    bootImageName: 'debian-12-bookworm',
    ipAddresses: ['10.36.4.102'],
    network: 'vmbr0',
    config: { hostname: 'dev-agent-sandbox-01', osType: 'l26' },
    updatedAt: '2026-11-23T21:10:00Z',
  },
  {
    id: 'vm-obs-tempo-01',
    externalId: '107',
    name: 'obs-tempo-ingester-01',
    provider: 'pve',
    connectionId: 'conn-pve-sh',
    connectionName: 'pve-prod-shanghai',
    node: 'pve-node-01',
    status: 'running',
    powerState: 'running',
    flavorName: 'c8m32-nvme',
    cpu: 8,
    memoryMiB: 32768,
    diskGiB: 400,
    bootImageName: 'ubuntu-24.04-lts-cloudimg',
    ipAddresses: ['10.24.16.31'],
    network: 'vmbr0',
    config: { hostname: 'obs-tempo-ingester-01', osType: 'l26' },
    updatedAt: '2026-11-24T07:40:00Z',
  },
  {
    id: 'vm-bastion-zero-01',
    externalId: '108',
    name: 'ztna-relay-edge-01',
    provider: 'pve',
    connectionId: 'conn-pve-hz',
    connectionName: 'pve-edge-hangzhou',
    node: 'hz-edge-02',
    status: 'degraded',
    powerState: 'running',
    flavorName: 'c4m8-std',
    cpu: 4,
    memoryMiB: 8192,
    diskGiB: 64,
    bootImageName: 'rocky-9.4-minimal',
    ipAddresses: ['10.36.2.5'],
    network: 'vmbr0',
    config: { hostname: 'ztna-relay-edge-01', osType: 'l26' },
    updatedAt: '2026-11-24T09:12:00Z',
  },
]

export function createPilotMockPlugin() {
  let loggedIn = true

  return {
    name: 'soha-ui-pilot-mock',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const rawUrl = req.url || '/'
        if (!rawUrl.startsWith('/api/')) {
          return next()
        }

        const url = new URL(rawUrl, 'http://127.0.0.1')
        const pathname = url.pathname
        const method = (req.method || 'GET').toUpperCase()

        const sendJson = (body, status = 200) => {
          res.statusCode = status
          res.setHeader('Content-Type', 'application/json; charset=utf-8')
          res.end(JSON.stringify(body))
        }

        if (pathname === '/api/v1/auth/login-options') {
          return sendJson({
            data: {
              localPasswordLoginEnabled: true,
              verification: { sliderEnabled: false },
              branding: {
                appTitle: 'Soha',
                sidebarTitle: 'Soha',
                slogan: '面向现代基础设施与 AI 工作流的精密统一控制面。',
                loginLogoUrl: '',
                expandedLogoUrl: '',
                collapsedLogoUrl: '',
                faviconUrl: '',
              },
            },
          })
        }

        if (pathname === '/api/v1/auth/providers') {
          return sendJson({
            data: [
              { id: 'sso-oidc', type: 'oidc', name: 'Enterprise SSO (OIDC)', enabled: true },
            ],
          })
        }

        if (pathname === '/api/v1/auth/login' && method === 'POST') {
          loggedIn = true
          return sendJson({
            data: {
              tokens: {
                accessToken: 'pilot-access-token',
                refreshToken: 'pilot-refresh-token',
                tokenType: 'Bearer',
                expiresIn: 7200,
                expiresAt: '2099-01-01T00:00:00Z',
              },
              user: DEMO_USER,
            },
          })
        }

        if (pathname === '/api/v1/auth/logout' && method === 'POST') {
          loggedIn = false
          return sendJson({ data: { ok: true } })
        }

        if (pathname === '/api/v1/auth/refresh' && method === 'POST') {
          if (!loggedIn) {
            return sendJson({ error: { code: 'unauthenticated', message: 'Not signed in' } }, 401)
          }
          return sendJson({
            data: {
              tokens: {
                accessToken: 'pilot-access-token',
                refreshToken: 'pilot-refresh-token',
                tokenType: 'Bearer',
                expiresIn: 7200,
                expiresAt: '2099-01-01T00:00:00Z',
              },
              user: DEMO_USER,
            },
          })
        }

        if (pathname === '/api/v1/settings/branding') {
          return sendJson({
            data: {
              appTitle: 'Soha',
              sidebarTitle: 'Soha',
              slogan: '面向现代基础设施与 AI 工作流的精密统一控制面。',
              loginLogoUrl: '',
              expandedLogoUrl: '',
              collapsedLogoUrl: '',
              faviconUrl: '',
            },
          })
        }

        if (pathname === '/api/v1/access/permission-snapshot') {
          return sendJson({
            data: {
              permissionKeys: [
                'workbench.compute.view',
                'workbench.platform.view',
                'workbench.home.view',
                'workbench.ai.view',
                'workbench.settings.view',
                'workspace.resource.view',
                'identity.portal.view',
                'virtualization.overview.view',
                'virtualization.vms.view',
                'virtualization.vms.create',
                'virtualization.vms.power',
                'virtualization.vms.resize',
                'virtualization.clusters.view',
                'virtualization.clusters.test',
                'virtualization.images.view',
                'virtualization.flavors.view',
                'virtualization.operations.view',
                'virtualization.sync.view',
                'virtualization.sync.sync',
                'docker.overview.view',
                'docker.hosts.view',
                'docker.projects.view',
                'docker.services.view',
                'docker.operations.view',
                'platform.clusters.view',
                'platform.deployment.view',
                'platform.deployment.manage',
                'platform.resource-creation.use',
                'platform.pods.logs',
                'platform.pods.exec',
                'observe.ai.view',
                'observe.ai.chat',
              ],
              visibleMenuIds: [
                'compute-workbench',
                'compute-workbench-overview',
                'compute-workbench-tasks-operations',
                'virtualization-workbench',
                'virtualization-workbench-vms',
                'virtualization-workbench-clusters',
                'virtualization-workbench-images',
                'virtualization-workbench-flavors',
                'docker-workbench',
                'docker-workbench-hosts',
                'docker-workbench-projects',
                'clusters',
                'workloads',
                'workloads-deployments',
              ],
              visibleMenus: [
                {
                  id: 'compute-workbench-overview',
                  path: '/compute/overview',
                  labelZh: '总览',
                  labelEn: 'Overview',
                  iconKey: 'gauge',
                },
                {
                  id: 'compute-workbench-tasks-operations',
                  path: '/compute/tasks/operations',
                  labelZh: '任务中心',
                  labelEn: 'Task Center',
                  iconKey: 'tasks',
                },
                {
                  id: 'virtualization-workbench',
                  path: '/compute/virtualization',
                  labelZh: '虚拟化',
                  labelEn: 'Virtualization',
                  iconKey: 'desktop',
                },
                {
                  id: 'virtualization-workbench-vms',
                  parentId: 'virtualization-workbench',
                  path: '/compute/virtualization/vms',
                  labelZh: '虚拟机',
                  labelEn: 'Virtual Machines',
                  iconKey: 'desktop',
                },
                {
                  id: 'virtualization-workbench-clusters',
                  parentId: 'virtualization-workbench',
                  path: '/compute/virtualization/clusters',
                  labelZh: '集群连接',
                  labelEn: 'Clusters',
                  iconKey: 'cluster',
                },
                {
                  id: 'virtualization-workbench-images',
                  parentId: 'virtualization-workbench',
                  path: '/compute/virtualization/images',
                  labelZh: '镜像与模板',
                  labelEn: 'Images & Templates',
                  iconKey: 'results',
                },
                {
                  id: 'virtualization-workbench-flavors',
                  parentId: 'virtualization-workbench',
                  path: '/compute/virtualization/flavors',
                  labelZh: '规格',
                  labelEn: 'Flavors',
                  iconKey: 'workbench',
                },
                {
                  id: 'docker-workbench',
                  path: '/compute/runtimes',
                  labelZh: '容器运行时',
                  labelEn: 'Container Runtime',
                  iconKey: 'server',
                },
                {
                  id: 'docker-workbench-hosts',
                  parentId: 'docker-workbench',
                  path: '/compute/runtimes/hosts',
                  labelZh: '运行时主机',
                  labelEn: 'Runtime Hosts',
                  iconKey: 'server',
                },
                {
                  id: 'docker-workbench-projects',
                  parentId: 'docker-workbench',
                  path: '/compute/runtimes/projects',
                  labelZh: '容器管理',
                  labelEn: 'Container Management',
                  iconKey: 'workbench',
                },
                {
                  id: 'clusters',
                  path: '/clusters',
                  labelZh: 'K8s 集群',
                  labelEn: 'Clusters',
                  iconKey: 'cluster',
                },
                {
                  id: 'workloads',
                  path: '/workloads',
                  labelZh: '工作负载',
                  labelEn: 'Workloads',
                  iconKey: 'server',
                },
                {
                  id: 'workloads-deployments',
                  parentId: 'workloads',
                  path: '/workloads/deployments',
                  labelZh: 'Deployments',
                  labelEn: 'Deployments',
                },
              ],
            },
          })
        }

        if (pathname === '/api/v1/compute/overview') {
          return sendJson({
            data: {
              generatedAt: '2026-11-24T09:30:00Z',
              partial: false,
              freshness: { status: 'fresh' },
              warnings: [],
              virtualization: {
                status: 'available',
                summary: {
                  vmsTotal: 42,
                  vmsRunning: 36,
                  vmsStopped: 4,
                  vmsError: 2,
                  connectionsTotal: 3,
                  connectionsHealthy: 2,
                  connectionsDegraded: 1,
                },
              },
              agents: {
                status: 'available',
                summary: { total: 14, online: 13, offline: 1 },
              },
              runtimes: {
                status: 'available',
                summary: { total: 12, available: 11, error: 1, waitingAgent: 0 },
              },
              runtimeWorkloads: {
                status: 'available',
                summary: { projects: 9, services: 28 },
              },
              tasks: {
                status: 'available',
                summary: { queued: 1, running: 3, failed: 1 },
              },
              attention: [
                {
                  code: 'runtime_host_unavailable',
                  severity: 'critical',
                  summary: '节点心跳超时，建议检查 Agent 隧道连通性',
                  resources: [
                    {
                      domain: 'virtualization',
                      kind: 'vm',
                      id: 'vm-redis-mesh-02',
                      displayName: 'cache-dragonfly-02 (10.24.14.19)',
                    },
                  ],
                },
                {
                  code: 'virtualization_connection_unavailable',
                  severity: 'warning',
                  summary: '边缘节点池指标采集延迟升高 (p95 > 420ms)',
                  resources: [
                    {
                      domain: 'virtualization',
                      kind: 'connection',
                      id: 'conn-pve-hz',
                      displayName: 'pve-edge-hangzhou',
                    },
                  ],
                },
              ],
              providerHealth: [
                {
                  domain: 'virtualization',
                  providerKey: 'Proxmox VE',
                  status: 'healthy',
                  checkedAt: '2026-11-24T09:29:40Z',
                },
                {
                  domain: 'virtualization',
                  providerKey: 'KubeVirt',
                  status: 'healthy',
                  checkedAt: '2026-11-24T09:29:52Z',
                },
                {
                  domain: 'container_runtime',
                  providerKey: 'Docker Engine',
                  status: 'degraded',
                  checkedAt: '2026-11-24T09:28:15Z',
                },
              ],
            },
          })
        }

        if (pathname === '/api/v1/compute/provider-instances') {
          return sendJson({
            data: {
              items: [
                {
                  domain: 'virtualization',
                  providerKey: 'pve',
                  instanceRef: 'conn-pve-sh',
                  displayName: 'pve-prod-shanghai',
                  endpoint: 'https://pve-sh.internal:8006',
                  status: 'healthy',
                  observedAt: '2026-11-24T09:29:40Z',
                },
                {
                  domain: 'virtualization',
                  providerKey: 'kubevirt',
                  instanceRef: 'conn-kv-gpu',
                  displayName: 'kubevirt-ai-cluster',
                  endpoint: 'https://k8s-gpu.internal:6443',
                  status: 'healthy',
                  observedAt: '2026-11-24T09:29:52Z',
                },
                {
                  domain: 'virtualization',
                  providerKey: 'pve',
                  instanceRef: 'conn-pve-hz',
                  displayName: 'pve-edge-hangzhou',
                  endpoint: 'https://pve-hz.internal:8006',
                  status: 'degraded',
                  observedAt: '2026-11-24T09:26:12Z',
                },
              ],
            },
          })
        }

        if (pathname === '/api/v1/virtualization/vms') {
          const q = (url.searchParams.get('search') || url.searchParams.get('keyword') || '')
            .trim()
            .toLowerCase()
          const statusFilter = (url.searchParams.get('status') || '').trim().toLowerCase()
          const providerFilter = (url.searchParams.get('provider') || '').trim().toLowerCase()
          const connFilter = (url.searchParams.get('connectionId') || '').trim()
          const filtered = DEMO_VMS.filter((vm) => {
            if (
              q &&
              !vm.name.toLowerCase().includes(q) &&
              !(vm.ipAddresses || []).some((ip) => ip.includes(q)) &&
              !(vm.node || '').toLowerCase().includes(q)
            ) {
              return false
            }
            if (statusFilter && statusFilter !== 'all' && vm.status !== statusFilter) return false
            if (providerFilter && providerFilter !== 'all' && vm.provider !== providerFilter)
              return false
            if (connFilter && connFilter !== 'all' && vm.connectionId !== connFilter) return false
            return true
          })
          return sendJson({
            data: {
              items: filtered,
              total: filtered.length,
              page: Number(url.searchParams.get('page') || 1),
              pageSize: Number(url.searchParams.get('pageSize') || 10),
            },
          })
        }

        if (pathname === '/api/v1/virtualization/clusters') {
          return sendJson({
            data: [
              {
                id: 'conn-pve-sh',
                name: 'pve-prod-shanghai',
                provider: 'pve',
                status: 'healthy',
                endpoint: 'https://pve-sh.internal:8006',
              },
              {
                id: 'conn-kv-gpu',
                name: 'kubevirt-ai-cluster',
                provider: 'kubevirt',
                status: 'healthy',
                endpoint: 'https://k8s-gpu.internal:6443',
              },
              {
                id: 'conn-pve-hz',
                name: 'pve-edge-hangzhou',
                provider: 'pve',
                status: 'degraded',
                endpoint: 'https://pve-hz.internal:8006',
              },
            ],
          })
        }

        if (pathname === '/api/v1/modules') {
          return sendJson({
            data: {
              modules: {
                virtualization: true,
                compute: true,
                docker: true,
                platform: true,
                ai: true,
                delivery: true,
                monitoring: true,
                security: true,
              },
            },
          })
        }

        if (
          pathname === '/api/v1/virtualization/flavors' ||
          pathname === '/api/v1/virtualization/images' ||
          pathname === '/api/v1/virtualization/operations' ||
          pathname === '/api/v1/announcements' ||
          pathname === '/api/v1/secrets'
        ) {
          return sendJson({ data: [] })
        }

        if (pathname === '/api/v1/copilot/workbench/runtime-profiles') {
          return sendJson({ items: [] })
        }

        if (pathname === '/api/v1/clusters') {
          return sendJson({
            data: [
              {
                id: 'cluster-a',
                name: 'prod-k8s-shanghai',
                region: 'cn-east-2',
                environment: 'production',
                labels: { tier: 'core' },
                connectionMode: 'agent',
                version: 'v1.31.2',
                health: { status: 'healthy' },
              },
              {
                id: 'cluster-gpu',
                name: 'ai-infer-gpu-pool',
                region: 'cn-north-4',
                environment: 'production',
                labels: { accel: 'nvidia-a100' },
                connectionMode: 'agent',
                version: 'v1.30.6',
                health: { status: 'healthy' },
              },
            ],
          })
        }

        if (pathname === '/api/v1/clusters/capabilities') {
          return sendJson({
            data: [
              {
                key: 'workload.mutations',
                direct: { status: 'supported' },
                agent: { status: 'supported' },
                requiresApproval: false,
                riskLevel: 'medium',
                requiredScopes: [],
              },
              {
                key: 'resource.yaml.apply',
                direct: { status: 'supported' },
                agent: { status: 'supported' },
                requiresApproval: false,
                riskLevel: 'high',
                requiredScopes: [],
              },
              {
                key: 'resource.yaml.view',
                direct: { status: 'supported' },
                agent: { status: 'supported' },
                requiresApproval: false,
                riskLevel: 'low',
                requiredScopes: [],
              },
            ],
          })
        }

        if (pathname.endsWith('/workloads/deployments')) {
          const nsFilter = (url.searchParams.get('namespace') || '').trim()
          const deployments = [
            {
              name: 'soha-api-gateway',
              namespace: 'default',
              uid: 'dep-uid-01',
              labels: { app: 'soha-gateway', tier: 'edge' },
              desiredReplicas: 4,
              readyReplicas: 4,
              updatedReplicas: 4,
              available: 4,
              ageSeconds: 86400 * 18,
              allowedActions: ['update', 'restart', 'scale', 'rollback', 'delete'],
            },
            {
              name: 'heya-copilot-orchestrator',
              namespace: 'ai-serving',
              uid: 'dep-uid-02',
              labels: { app: 'heya-copilot', model: 'claude-sonnet' },
              desiredReplicas: 3,
              readyReplicas: 3,
              updatedReplicas: 3,
              available: 3,
              ageSeconds: 86400 * 9,
              allowedActions: ['update', 'restart', 'scale', 'rollback', 'delete'],
            },
            {
              name: 'vllm-qwen-serving',
              namespace: 'ai-serving',
              uid: 'dep-uid-03',
              labels: { app: 'vllm', gpu: 'a100-80g' },
              desiredReplicas: 4,
              readyReplicas: 3,
              updatedReplicas: 4,
              available: 3,
              ageSeconds: 3600 * 14,
              allowedActions: ['update', 'restart', 'scale', 'rollback', 'delete'],
            },
            {
              name: 'otel-collector-gateway',
              namespace: 'monitoring',
              uid: 'dep-uid-04',
              labels: { app: 'otel-collector', role: 'gateway' },
              desiredReplicas: 2,
              readyReplicas: 2,
              updatedReplicas: 2,
              available: 2,
              ageSeconds: 86400 * 42,
              allowedActions: ['update', 'restart', 'scale', 'rollback', 'delete'],
            },
            {
              name: 'dragonfly-cache-operator',
              namespace: 'default',
              uid: 'dep-uid-05',
              labels: { app: 'dragonfly-operator' },
              desiredReplicas: 2,
              readyReplicas: 1,
              updatedReplicas: 2,
              available: 1,
              ageSeconds: 86400 * 6,
              allowedActions: ['update', 'restart', 'scale', 'rollback', 'delete'],
            },
            {
              name: 'ztna-admission-webhook',
              namespace: 'default',
              uid: 'dep-uid-06',
              labels: { app: 'ztna-webhook', security: 'enforced' },
              desiredReplicas: 2,
              readyReplicas: 2,
              updatedReplicas: 2,
              available: 2,
              ageSeconds: 86400 * 31,
              allowedActions: ['update', 'restart', 'scale', 'rollback', 'delete'],
            },
            {
              name: 'tempo-trace-querier',
              namespace: 'monitoring',
              uid: 'dep-uid-07',
              labels: { app: 'tempo', component: 'querier' },
              desiredReplicas: 3,
              readyReplicas: 0,
              updatedReplicas: 3,
              available: 0,
              ageSeconds: 1840,
              allowedActions: ['update', 'restart', 'scale', 'rollback', 'delete'],
            },
          ]
          const filtered =
            nsFilter && nsFilter !== 'all'
              ? deployments.filter((item) => item.namespace === nsFilter)
              : deployments
          return sendJson({ data: filtered })
        }

        if (pathname.endsWith('/namespaces')) {
          return sendJson({
            data: [
              { name: 'default', status: 'Active', labels: {} },
              { name: 'ai-serving', status: 'Active', labels: {} },
              { name: 'monitoring', status: 'Active', labels: {} },
            ],
          })
        }

        return sendJson({ data: [] })
      })
    },
  }
}
