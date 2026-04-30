import React from 'react';
import ComponentCreator from '@docusaurus/ComponentCreator';

export default [
  {
    path: '/docs/__docusaurus/debug/',
    component: ComponentCreator('/docs/__docusaurus/debug/', '472'),
    exact: true
  },
  {
    path: '/docs/__docusaurus/debug/config/',
    component: ComponentCreator('/docs/__docusaurus/debug/config/', '62e'),
    exact: true
  },
  {
    path: '/docs/__docusaurus/debug/content/',
    component: ComponentCreator('/docs/__docusaurus/debug/content/', '149'),
    exact: true
  },
  {
    path: '/docs/__docusaurus/debug/globalData/',
    component: ComponentCreator('/docs/__docusaurus/debug/globalData/', '59b'),
    exact: true
  },
  {
    path: '/docs/__docusaurus/debug/metadata/',
    component: ComponentCreator('/docs/__docusaurus/debug/metadata/', '943'),
    exact: true
  },
  {
    path: '/docs/__docusaurus/debug/registry/',
    component: ComponentCreator('/docs/__docusaurus/debug/registry/', 'ab0'),
    exact: true
  },
  {
    path: '/docs/__docusaurus/debug/routes/',
    component: ComponentCreator('/docs/__docusaurus/debug/routes/', 'cd1'),
    exact: true
  },
  {
    path: '/docs/',
    component: ComponentCreator('/docs/', 'eb8'),
    routes: [
      {
        path: '/docs/',
        component: ComponentCreator('/docs/', '80e'),
        routes: [
          {
            path: '/docs/',
            component: ComponentCreator('/docs/', '4f2'),
            routes: [
              {
                path: '/docs/api/auth-and-errors/',
                component: ComponentCreator('/docs/api/auth-and-errors/', 'b21'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/api/core-endpoints/',
                component: ComponentCreator('/docs/api/core-endpoints/', '8a1'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/api/overview/',
                component: ComponentCreator('/docs/api/overview/', '663'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/',
                component: ComponentCreator('/docs/architecture/', '840'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/access-model/',
                component: ComponentCreator('/docs/architecture/access-model/', '3b5'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/agent-protocol/',
                component: ComponentCreator('/docs/architecture/agent-protocol/', 'a26'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/ai-copilot/',
                component: ComponentCreator('/docs/architecture/ai-copilot/', 'f6e'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/application-delivery/',
                component: ComponentCreator('/docs/architecture/application-delivery/', '9d4'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/audit-model/',
                component: ComponentCreator('/docs/architecture/audit-model/', '563'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/authorization/',
                component: ComponentCreator('/docs/architecture/authorization/', 'aa7'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/event-model/',
                component: ComponentCreator('/docs/architecture/event-model/', 'be4'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/mcp-integration/',
                component: ComponentCreator('/docs/architecture/mcp-integration/', 'd5f'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/monitoring-and-alerting/',
                component: ComponentCreator('/docs/architecture/monitoring-and-alerting/', 'eb0'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/architecture/multi-cluster-model/',
                component: ComponentCreator('/docs/architecture/multi-cluster-model/', '3b5'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/add-mcp-integration/',
                component: ComponentCreator('/docs/development/add-mcp-integration/', '1ab'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/add-page/',
                component: ComponentCreator('/docs/development/add-page/', 'ca4'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/add-resource-module/',
                component: ComponentCreator('/docs/development/add-resource-module/', '712'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/backend-structure/',
                component: ComponentCreator('/docs/development/backend-structure/', '2bc'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/configuration/',
                component: ComponentCreator('/docs/development/configuration/', '3f8'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/database-migrations/',
                component: ComponentCreator('/docs/development/database-migrations/', '149'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/frontend-structure/',
                component: ComponentCreator('/docs/development/frontend-structure/', '381'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/development/local-development/',
                component: ComponentCreator('/docs/development/local-development/', '4f4'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/agent-runtime/',
                component: ComponentCreator('/docs/operations/agent-runtime/', '3e2'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/configuration/',
                component: ComponentCreator('/docs/operations/configuration/', '42b'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/dependencies/',
                component: ComponentCreator('/docs/operations/dependencies/', 'e49'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/deployment/',
                component: ComponentCreator('/docs/operations/deployment/', '1aa'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/docs-publishing/',
                component: ComponentCreator('/docs/operations/docs-publishing/', '447'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/environment-variables/',
                component: ComponentCreator('/docs/operations/environment-variables/', 'c20'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/mcp-configuration/',
                component: ComponentCreator('/docs/operations/mcp-configuration/', '2c3'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/mcp/',
                component: ComponentCreator('/docs/operations/mcp/', '9f4'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/operations/role-authorization-assignment/',
                component: ComponentCreator('/docs/operations/role-authorization-assignment/', 'bec'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/reference/database-schema/',
                component: ComponentCreator('/docs/reference/database-schema/', 'fe8'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/reference/product-information-architecture/',
                component: ComponentCreator('/docs/reference/product-information-architecture/', '59e'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/roadmap/',
                component: ComponentCreator('/docs/roadmap/', '696'),
                exact: true,
                sidebar: "mainSidebar"
              },
              {
                path: '/docs/',
                component: ComponentCreator('/docs/', '2f8'),
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
