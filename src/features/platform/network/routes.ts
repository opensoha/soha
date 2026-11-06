import { defineRoutes } from '@/routes/definitions'

export const networkViewPermissions = [
  'platform.network.topology.view',
  'platform.network.services.view',
  'platform.network.ingresses.view',
  'platform.network.gateway-classes.view',
  'platform.network.gateways.view',
  'platform.network.http-routes.view',
  'platform.network.backend-tls-policies.view',
  'platform.network.grpc-routes.view',
  'platform.network.reference-grants.view',
  'platform.network.endpoint-slices.view',
  'platform.network.ingress-classes.view',
  'platform.network.network-policies.view',
  'platform.network.port-forwards.view',
]

export const networkRoutes = defineRoutes([
  {
    meta: {
      id: 'network-backends',
      path: '/network/backends',
      title: '服务与后端',
      description: '服务与后端',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'network',
      menuId: 'network-backends',
      permissionKeysAny: [
        'platform.network.services.view',
        'platform.network.endpoint-slices.view',
      ],
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./group-entry')
      return { default: module.NetworkBackendsEntry }
    },
  },
  {
    meta: {
      id: 'network-routing',
      path: '/network/routing',
      title: '入口与路由',
      description: '入口与路由',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      parentId: 'network',
      menuId: 'network-routing',
      permissionKeysAny: [
        'platform.network.ingresses.view',
        'platform.network.ingress-classes.view',
        'platform.network.gateway-classes.view',
        'platform.network.gateways.view',
        'platform.network.http-routes.view',
        'platform.network.grpc-routes.view',
        'platform.network.backend-tls-policies.view',
        'platform.network.reference-grants.view',
      ],
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./group-entry')
      return { default: module.NetworkRoutingEntry }
    },
  },
  {
    meta: {
      id: 'network-services',
      path: '/network/services',
      title: 'Services',
      description: '服务管理',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-services',
      permissionKey: 'platform.network.services.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./services/list-page')
      return { default: module.NetworkServicesPage }
    },
  },
  {
    meta: {
      id: 'network-service-detail',
      path: '/network/services/:serviceName',
      title: 'Service Detail',
      description: '服务详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'network-services',
    load: async () => {
      const module = await import('./services/detail-page')
      return { default: module.ServiceDetailPage }
    },
  },
  {
    meta: {
      id: 'network-ingresses',
      path: '/network/ingresses',
      title: 'Ingresses',
      description: '入口管理',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-ingresses',
      permissionKey: 'platform.network.ingresses.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./ingresses/list-page')
      return { default: module.NetworkIngressesPage }
    },
  },
  {
    meta: {
      id: 'network-ingress-detail',
      path: '/network/ingresses/:name',
      title: 'Ingress Detail',
      description: 'Ingress 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'network-ingresses',
    load: async () => {
      const module = await import('./ingresses/detail-page')
      return { default: module.IngressDetailPage }
    },
  },
  {
    meta: {
      id: 'network-ingressclasses',
      path: '/network/ingressclasses',
      title: 'IngressClasses',
      description: 'Ingress 类',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-ingressclasses',
      permissionKey: 'platform.network.ingress-classes.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./resources/ingressclasses-list-page')
      return { default: module.NetworkIngressClassesPage }
    },
  },
  {
    meta: {
      id: 'network-ingressclass-detail',
      path: '/network/ingressclasses/:name',
      title: 'IngressClass Detail',
      description: 'IngressClass 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'network-ingressclasses',
    load: async () => {
      const module = await import('./resources/ingressclass-detail-page')
      return { default: module.IngressClassDetailPage }
    },
  },
  {
    meta: {
      id: 'network-topology',
      path: '/network/topology',
      title: '网络拓扑',
      description: '入口、路由、Service 与后端的网络拓扑',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-topology',
      permissionKey: 'platform.network.topology.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./topology/page')
      return { default: module.NetworkTopologyPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-gatewayclasses',
      path: '/network/gateway-api/gatewayclasses',
      title: 'GatewayClasses',
      description: 'Gateway API 控制器类',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-gateway-api-gatewayclasses',
      permissionKey: 'platform.network.gateway-classes.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway-api/gatewayclasses-list-page')
      return { default: module.NetworkGatewayClassesPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-gatewayclass-detail',
      path: '/network/gateway-api/gatewayclasses/:name',
      title: 'GatewayClass Detail',
      description: 'GatewayClass 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'network-gateway-api-gatewayclasses',
    load: async () => {
      const module = await import('./gateway-api/gatewayclass-detail-page')
      return { default: module.GatewayClassDetailPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-gateways',
      path: '/network/gateway-api/gateways',
      title: 'Gateways',
      description: 'Gateway API 网关',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-gateway-api-gateways',
      permissionKey: 'platform.network.gateways.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway-api/gateways-list-page')
      return { default: module.NetworkGatewaysPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-gateway-detail',
      path: '/network/gateway-api/gateways/:name',
      title: 'Gateway Detail',
      description: 'Gateway 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'network-gateway-api-gateways',
    load: async () => {
      const module = await import('./gateway-api/gateway-detail-page')
      return { default: module.GatewayDetailPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-httproutes',
      path: '/network/gateway-api/httproutes',
      title: 'HTTPRoutes',
      description: 'Gateway API HTTP 路由',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-gateway-api-httproutes',
      permissionKey: 'platform.network.http-routes.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway-api/httproutes-page')
      return { default: module.NetworkHTTPRoutesPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-httproute-detail',
      path: '/network/gateway-api/httproutes/:name',
      title: 'HTTPRoute Detail',
      description: 'HTTPRoute 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'network-gateway-api-httproutes',
    load: async () => {
      const module = await import('./gateway-api/httproute-detail-page')
      return { default: module.HTTPRouteDetailPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-backendtlspolicies',
      path: '/network/gateway-api/backendtlspolicies',
      title: 'BackendTLSPolicies',
      description: 'Gateway API 后端 TLS 策略',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-gateway-api-backendtlspolicies',
      permissionKey: 'platform.network.backend-tls-policies.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway-api/backend-tls-policies-page')
      return { default: module.NetworkBackendTLSPoliciesPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-backendtlspolicy-detail',
      path: '/network/gateway-api/backendtlspolicies/:name',
      title: 'BackendTLSPolicy Detail',
      description: 'BackendTLSPolicy 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'network-gateway-api-backendtlspolicies',
    load: async () => {
      const module = await import('./gateway-api/backend-tls-policy-detail-page')
      return { default: module.BackendTLSPolicyDetailPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-grpcroutes',
      path: '/network/gateway-api/grpcroutes',
      title: 'GRPCRoutes',
      description: 'Gateway API gRPC 路由',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-gateway-api-grpcroutes',
      permissionKey: 'platform.network.grpc-routes.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway-api/grpc-routes-page')
      return { default: module.NetworkGRPCRoutesPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-grpcroute-detail',
      path: '/network/gateway-api/grpcroutes/:name',
      title: 'GRPCRoute Detail',
      description: 'GRPCRoute 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'network-gateway-api-grpcroutes',
    load: async () => {
      const module = await import('./gateway-api/grpcroute-detail-page')
      return { default: module.GRPCRouteDetailPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-referencegrants',
      path: '/network/gateway-api/referencegrants',
      title: 'ReferenceGrants',
      description: 'Gateway API 跨 namespace 引用授权',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-gateway-api-referencegrants',
      permissionKey: 'platform.network.reference-grants.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway-api/reference-grants-page')
      return { default: module.NetworkReferenceGrantsPage }
    },
  },
  {
    meta: {
      id: 'network-gateway-api-referencegrant-detail',
      path: '/network/gateway-api/referencegrants/:name',
      title: 'ReferenceGrant Detail',
      description: 'ReferenceGrant 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'network-gateway-api-referencegrants',
    load: async () => {
      const module = await import('./gateway-api/reference-grant-detail-page')
      return { default: module.ReferenceGrantDetailPage }
    },
  },
  {
    meta: {
      id: 'network-endpointslices',
      path: '/network/endpointslices',
      title: 'EndpointSlices',
      description: '服务后端切片',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-endpointslices',
      permissionKey: 'platform.network.endpoint-slices.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./resources/endpointslices-list-page')
      return { default: module.NetworkEndpointSlicesPage }
    },
  },
  {
    meta: {
      id: 'network-endpointslice-detail',
      path: '/network/endpointslices/:name',
      title: 'EndpointSlice Detail',
      description: 'EndpointSlice 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'network-endpointslices',
    load: async () => {
      const module = await import('./resources/endpointslice-detail-page')
      return { default: module.EndpointSliceDetailPage }
    },
  },
  {
    meta: {
      id: 'network-networkpolicies',
      path: '/network/networkpolicies',
      title: 'NetworkPolicies',
      description: '网络策略',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-networkpolicies',
      permissionKey: 'platform.network.network-policies.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./resources/networkpolicies-list-page')
      return { default: module.NetworkPoliciesPage }
    },
  },
  {
    meta: {
      id: 'network-networkpolicy-detail',
      path: '/network/networkpolicies/:name',
      title: 'NetworkPolicy Detail',
      description: 'NetworkPolicy 详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'network-networkpolicies',
    load: async () => {
      const module = await import('./resources/networkpolicy-detail-page')
      return { default: module.NetworkPolicyDetailPage }
    },
  },
  {
    meta: {
      id: 'network-port-forward',
      path: '/network/port-forward',
      title: 'Port Forward',
      description: '端口转发',
      icon: 'IconConnection',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'network',
      menuId: 'network-port-forward',
      permissionKey: 'platform.network.port-forwards.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./port-forward/page')
      return { default: module.NetworkPortForwardPage }
    },
  },
  {
    meta: {
      id: 'network-gateways-legacy-redirect',
      path: '/network/gateways',
      title: 'Gateways',
      description: 'Gateway API 网关兼容入口',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'network-gateway-api-gateways',
    redirectTo: '/network/gateway-api/gateways',
  },
  {
    meta: {
      id: 'network-gateway-api-redirect',
      path: '/network/gateway-api',
      title: 'Gateway API',
      description: 'Gateway API 兼容入口',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'network-gateway-api-gatewayclasses',
    redirectTo: '/network/gateway-api/gatewayclasses',
  },
] as const)
