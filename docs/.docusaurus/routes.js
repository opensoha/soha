import React from 'react';
import ComponentCreator from '@docusaurus/ComponentCreator';

export default [
  {
    path: '/docs/',
    component: ComponentCreator('/docs/', 'abe'),
    routes: [
      {
        path: '/docs/',
        component: ComponentCreator('/docs/', 'e84'),
        routes: [
          {
            path: '/docs/',
            component: ComponentCreator('/docs/', '51b'),
            routes: [
              {
                path: '/docs/api/auth-and-errors/',
                component: ComponentCreator('/docs/api/auth-and-errors/', '83d'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/api/core-endpoints/',
                component: ComponentCreator('/docs/api/core-endpoints/', 'dee'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/api/overview/',
                component: ComponentCreator('/docs/api/overview/', '0ad'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/',
                component: ComponentCreator('/docs/architecture/', 'c21'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/access-model/',
                component: ComponentCreator('/docs/architecture/access-model/', '346'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/agent-protocol/',
                component: ComponentCreator('/docs/architecture/agent-protocol/', 'bb9'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/ai-copilot/',
                component: ComponentCreator('/docs/architecture/ai-copilot/', 'b58'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/application-delivery/',
                component: ComponentCreator('/docs/architecture/application-delivery/', '116'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/audit-model/',
                component: ComponentCreator('/docs/architecture/audit-model/', '83c'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/authorization/',
                component: ComponentCreator('/docs/architecture/authorization/', '0f5'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/event-model/',
                component: ComponentCreator('/docs/architecture/event-model/', 'f42'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/mcp-integration/',
                component: ComponentCreator('/docs/architecture/mcp-integration/', 'b5f'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/monitoring-and-alerting/',
                component: ComponentCreator('/docs/architecture/monitoring-and-alerting/', '28d'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/multi-cluster-model/',
                component: ComponentCreator('/docs/architecture/multi-cluster-model/', '78f'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/add-mcp-integration/',
                component: ComponentCreator('/docs/development/add-mcp-integration/', '239'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/add-page/',
                component: ComponentCreator('/docs/development/add-page/', '3fa'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/add-resource-module/',
                component: ComponentCreator('/docs/development/add-resource-module/', 'a73'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/backend-structure/',
                component: ComponentCreator('/docs/development/backend-structure/', '3fc'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/configuration/',
                component: ComponentCreator('/docs/development/configuration/', '74f'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/database-migrations/',
                component: ComponentCreator('/docs/development/database-migrations/', 'c88'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/frontend-structure/',
                component: ComponentCreator('/docs/development/frontend-structure/', '274'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/local-development/',
                component: ComponentCreator('/docs/development/local-development/', '01b'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/agent-runtime/',
                component: ComponentCreator('/docs/operations/agent-runtime/', '41f'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/configuration/',
                component: ComponentCreator('/docs/operations/configuration/', '86c'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/dependencies/',
                component: ComponentCreator('/docs/operations/dependencies/', '52f'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/deployment/',
                component: ComponentCreator('/docs/operations/deployment/', '1b0'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/docs-publishing/',
                component: ComponentCreator('/docs/operations/docs-publishing/', 'ffe'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/environment-variables/',
                component: ComponentCreator('/docs/operations/environment-variables/', 'a09'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/mcp-configuration/',
                component: ComponentCreator('/docs/operations/mcp-configuration/', '3cc'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/mcp/',
                component: ComponentCreator('/docs/operations/mcp/', '63c'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/role-authorization-assignment/',
                component: ComponentCreator('/docs/operations/role-authorization-assignment/', '63a'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/reference/database-schema/',
                component: ComponentCreator('/docs/reference/database-schema/', '6ef'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/reference/product-information-architecture/',
                component: ComponentCreator('/docs/reference/product-information-architecture/', 'ead'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/roadmap/',
                component: ComponentCreator('/docs/roadmap/', 'fa1'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/',
                component: ComponentCreator('/docs/', 'fc1'),
                exact: true,
                sidebar: "mainSidebar"
              }
            ]
          }
        ]
      }
    ]
  },
  {
    path: '*',
    component: ComponentCreator('*'),
  },
];
