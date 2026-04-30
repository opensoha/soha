import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  mainSidebar: [
    'index',
    {
      type: 'category',
      label: '架构',
      link: {
        type: 'doc',
        id: 'architecture/index',
      },
      items: [
        'architecture/application-delivery',
        'architecture/monitoring-and-alerting',
        'architecture/authorization',
        'architecture/ai-copilot',
        'architecture/agent-protocol',
        'architecture/multi-cluster-model',
        'architecture/event-model',
        'architecture/audit-model',
        'architecture/mcp-integration',
        'architecture/access-model',
      ],
    },
    {
      type: 'category',
      label: '开发',
      items: [
        'development/local-development',
        'development/frontend-structure',
        'development/backend-structure',
        'development/configuration',
        'development/database-migrations',
        'development/add-resource-module',
        'development/add-page',
        'development/add-mcp-integration',
      ],
    },
    {
      type: 'category',
      label: 'API',
      items: [
        'api/overview',
        'api/core-endpoints',
        'api/auth-and-errors',
      ],
    },
    {
      type: 'category',
      label: '运维',
      items: [
        'operations/configuration',
        'operations/role-authorization-assignment',
        'operations/agent-runtime',
        'operations/deployment',
        'operations/environment-variables',
        'operations/dependencies',
        'operations/docs-publishing',
        'operations/mcp',
        'operations/mcp-configuration',
      ],
    },
    {
      type: 'category',
      label: '路线图',
      items: ['roadmap/index'],
    },
    {
      type: 'category',
      label: '参考',
      items: [
        'reference/product-information-architecture',
        'reference/database-schema',
      ],
    },
  ],
};

export default sidebars;
