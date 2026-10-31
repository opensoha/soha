/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act } from 'react'
import { App as AntApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApplicationDetailPage } from './applications/detail-page'
import { WorkflowCatalog } from './release-board/workflow-catalog'
import { ApplicationNavigation, ApplicationIdentity, ApplicationBreadcrumb } from './navigation'
import { ApplicationWorkflowDesignerPage } from './applications/workflow-designer-page'
import { deliveryQueries } from './queries'
import { I18nProvider } from '@/i18n'
import { api } from '@/services/api-client'
import { ApiError } from '@/services/api-error'
import { usePreferencesStore } from '@/stores/preferences-store'

const workflowDefinition = {
  schemaVersion: 2,
  mode: 'release_dag',
  nodes: [
    {
      id: 'approval',
      type: 'manual_approval',
      name: '审批',
      position: { x: 120, y: 120 },
      timeoutSeconds: 300,
      continueOnFailure: false,
      config: {},
    },
    {
      id: 'deploy',
      type: 'deploy_update_image',
      name: '更新镜像',
      position: { x: 320, y: 120 },
      timeoutSeconds: 300,
      continueOnFailure: false,
      config: {},
    },
  ],
  edges: [{ id: 'edge-1', source: 'approval', target: 'deploy', condition: 'success' }],
}

const testState = vi.hoisted(() => ({
  permissionSnapshot: {
    permissionKeys: [
      'delivery.applications.view',
      'delivery.application.update',
      'delivery.application-environments.create',
      'delivery.application-environments.update',
      'delivery.application-environments.delete',
      'delivery.application-services.view',
      'delivery.application-services.create',
      'delivery.application-services.update',
      'delivery.application-services.delete',
      'delivery.workflows.view',
      'delivery.builds.trigger',
      'delivery.workflows.trigger',
      'delivery.releases.trigger',
      'access.scope-grants.view',
      'access.users.view',
    ],
    visibleMenuIds: [],
    visibleMenus: [],
  },
  manifestTarget: false,
  manifestDeployments: [] as Record<string, unknown>[],
  preflightStatus: 'completed',
  detailWithoutWorkflow: false,
  detailWithoutValidationNodes: false,
  detailWithoutImageTagDefaults: false,
  deliveryActionsAgentStatus: 'available' as 'available' | 'partial' | 'unsupported',
  deliveryClusterConnectionMode: 'direct_kubeconfig',
  runtimeStatus: 'ok' as 'ok' | 'not-found' | 'error',
  runtimePending: false,
  serviceLegacyRepository: false,
  serviceCatalogStatus: 'ready' as 'ready' | 'pending' | 'error',
  runtimeWithProductionEnvironment: false,
  runtimeWithUnavailableEnvironment: false,
  repositoriesEmpty: false,
  gitProjectsEmpty: false,
  createdRepository: undefined as Record<string, unknown> | undefined,
  lastDeliveryPlan: undefined as undefined | Record<string, unknown>,
  readErrors: new Map<string, Error>(),
  apiGet: vi.fn(async (path: string) => {
    if (testState.readErrors.has(path)) throw testState.readErrors.get(path)
    if (path === '/delivery/execution-tasks/preflight-1')
      return {
        data: {
          id: 'preflight-1',
          status: testState.preflightStatus,
          result: {
            preflight: {
              ready: testState.preflightStatus === 'completed',
              renderedDigest: 'sha256:fixed',
            },
          },
        },
      }
    if (path.includes('/application-environments/') && path.endsWith('/runtime')) {
      const name = path.split('/workloads/')[1].split('/runtime')[0]
      const prod = path.includes('binding-production')
      const clusterId = prod ? 'cluster-production' : 'cluster-a'
      const namespace = prod ? 'checkout-production' : 'checkout-test'
      return {
        data: {
          workload: {
            applicationEnvironmentId: prod ? 'binding-production' : 'binding-test',
            clusterId,
            namespace,
            workloadKind: 'Deployment',
            workloadName: name,
            desiredReplicas: 1,
            readyReplicas: 1,
            updatedReplicas: 1,
            availableReplicas: 1,
          },
          pods: [
            {
              name: name + '-pod',
              namespace,
              phase: 'Running',
              nodeName: 'node-1',
              podIp: '10.0.0.1',
              readyContainers: '1/1',
              restarts: 0,
              ageSeconds: 60,
            },
          ],
          deployment: { relatedResources: [], containers: [] },
          services: [],
          ingresses: [],
        },
      }
    }
    if (path.includes('/metrics?')) return { data: { configured: false, series: [] } }

    if (path.startsWith('/delivery/workflow-catalog?')) {
      const items = [
        {
          id: 'build_source/app-1/source-api',
          sourceKind: 'build_source',
          sourceId: 'source-api',
          name: 'API Dockerfile',
          context: 'buildkit',
          enabled: true,
          scopes: [{ applicationId: 'app-1', applicationName: 'Checkout Platform' }],
        },
        ...(testState.permissionSnapshot.permissionKeys.includes('delivery.workflows.view')
          ? [
              {
                id: 'application_workflow/app-1/binding-test',
                sourceKind: 'application_workflow',
                sourceId: 'binding-test',
                name: 'Release DAG',
                context: '',
                enabled: true,
                scopes: [
                  {
                    applicationId: 'app-1',
                    applicationName: 'Checkout Platform',
                    applicationEnvironmentId: 'binding-test',
                    environmentId: 'env-test',
                    environmentName: '测试环境',
                  },
                ],
              },
            ]
          : []),
      ]
      return {
        data: {
          items,
          total: items.length,
          applications: [{ value: 'app-1', label: 'Checkout Platform' }],
          environments: [],
        },
      }
    }
    if (path.startsWith('/delivery-batches?')) return { data: [] }
    if (path === '/applications') {
      return {
        data: [
          {
            id: 'app-1',
            version: 7,
            name: 'Checkout Platform',
            key: 'checkout-platform',
            group: 'commerce',
            language: 'go',
            repositoryPath: 'commerce/checkout',
            defaultBranch: 'main',
            defaultTag: testState.detailWithoutImageTagDefaults ? undefined : 'latest',
            enabled: true,
            buildSources: [
              {
                id: 'source-api',
                name: 'API Dockerfile',
                type: 'repo_dockerfile',
                enabled: true,
                isDefault: true,
                defaultTag: testState.detailWithoutImageTagDefaults ? undefined : 'latest',
                config: {
                  repositoryId: 'repository-api',
                  repositoryBindings: [
                    {
                      repositoryId: 'repository-api',
                      checkoutPath: '.',
                      defaultBranch: 'main',
                      allowCommitSelection: true,
                      submodules: false,
                    },
                    {
                      repositoryId: 'repository-shared',
                      checkoutPath: 'shared',
                      defaultBranch: 'main',
                      allowCommitSelection: false,
                      submodules: true,
                    },
                  ],
                },
              },
            ],
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/users') {
      return {
        data: [
          {
            id: 'user-1',
            username: 'release-owner',
            displayName: 'Release Owner',
            roles: [],
            teams: [],
            projects: [],
            tags: [],
            loginSources: [],
          },
        ],
      }
    }
    if (path === '/access/users') {
      return {
        data: [
          {
            id: 'user-1',
            username: 'release-owner',
            displayName: 'Release Owner',
            roles: [],
            teams: [],
            projects: [],
            tags: [],
            loginSources: [],
          },
        ],
      }
    }
    if (path === '/access/scope-grants') {
      return {
        data: [
          {
            id: 'grant-1',
            subjectType: 'user',
            subjectId: 'user-1',
            businessLineId: 'commerce',
            environmentIds: [],
            applicationIds: ['app-1'],
            scopeType: 'delivery',
            role: 'release-manager',
            effect: 'allow',
            enabled: true,
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/application-environments') {
      return {
        data: [
          {
            id: 'binding-test',
            applicationId: 'app-1',
            environmentId: 'env-test',
            alias: '集成测试',
            clusterId: 'cluster-a',
            namespace: 'checkout-test',
            registryId: 'registry-a',
            workflowTemplateId: 'wf-template-1',
            buildPolicy: { sourceId: 'source-api', refType: 'branch' },
            releasePolicy: { actionKind: 'deploy', requiresApproval: false },
            resourceSelector: { matchLabels: { app: 'checkout-api' } },
            targets: [
              {
                id: 'target-1',
                ...(testState.manifestTarget
                  ? {
                      executorKind: 'manifest_ssa',
                      targetKind: 'kustomize_overlay',
                      configRef: 'manifest-binding-1',
                      metadata: { serviceId: 'svc-api', manifestPackageName: 'Checkout config' },
                    }
                  : {}),
                applicationEnvironmentId: 'binding-test',
                clusterId: 'cluster-a',
                namespace: 'checkout-test',
                workloadKind: 'Deployment',
                workloadName: 'checkout-api',
                containerName: 'api',
                enabled: true,
              },
            ],
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/delivery/environments') {
      return {
        data: [
          { id: 'env-production', key: 'prod', name: '生产环境', isProduction: true },
          {
            id: 'env-test',
            key: 'test',
            name: '测试环境',
            stageLevel: 1,
            sortOrder: 1,
            isProduction: false,
            requiresApproval: false,
            enabled: true,
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
          {
            id: 'env-staging',
            key: 'staging',
            name: '预发布环境',
            stageLevel: 2,
            sortOrder: 2,
            isProduction: false,
            requiresApproval: false,
            enabled: true,
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/registries') {
      return {
        items: [
          {
            id: 'registry-a',
            name: 'Harbor 主仓库',
            type: 'harbor',
            endpoint: 'https://harbor.example.com',
            enabled: true,
          },
        ],
      }
    }
    if (path === '/clusters/cluster-a/namespaces') {
      return { data: [{ name: 'checkout-staging', status: 'Active' }] }
    }
    if (path === '/workflow-templates/wf-template-1/versions/1')
      return {
        data: {
          id: 'wf-template-1',
          name: 'Release DAG',
          publishedVersion: 1,
          definition: workflowDefinition,
        },
      }
    if (path === '/workflow-templates') {
      return {
        data: [
          {
            id: 'wf-template-1',
            key: 'release-dag',
            name: 'Release DAG',
            publishedVersion: 1,
            revision: 1,
            publicationState: 'published',
            enabled: true,
            definition: workflowDefinition,
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/clusters') {
      return {
        data: [
          {
            id: 'cluster-a',
            name: 'cluster-a',
            connectionMode: testState.deliveryClusterConnectionMode,
            status: 'ready',
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/clusters/capabilities') {
      return {
        data: [
          {
            key: 'delivery.actions',
            label: 'Delivery actions',
            category: 'delivery',
            direct: { status: 'available' },
            agent: {
              status: testState.deliveryActionsAgentStatus,
              notes:
                testState.deliveryActionsAgentStatus === 'available'
                  ? []
                  : [
                      'build actions remain available; deploy, build-deploy, verification, and rollback against agent-connected targets require delivery runner parity',
                    ],
            },
          },
        ],
      }
    }
    if (path === '/delivery/manifest-packages?applicationId=app-1&page=1&pageSize=20') {
      return {
        data: {
          items: [
            {
              id: 'manifest-1',
              name: 'Checkout ingress',
              applicationId: 'app-1',
              serviceId: 'svc-api',
              renderer: 'raw_yaml',
              status: 'draft',
              currentRevision: 0,
              files: [],
              bindings: [],
              createdAt: '2026-05-01T00:00:00Z',
              updatedAt: '2026-05-10T00:00:00Z',
            },
          ],
          total: 1,
          page: 1,
          pageSize: 20,
        },
      }
    }
    if (
      path ===
      '/delivery/manifest-packages?applicationId=app-1&serviceId=svc-api&page=1&pageSize=20'
    ) {
      return {
        data: {
          items: [
            {
              id: 'manifest-1',
              name: 'Checkout ingress',
              applicationId: 'app-1',
              serviceId: 'svc-api',
              renderer: 'raw_yaml',
              status: 'draft',
              currentRevision: 0,
              files: [],
              bindings: [],
              createdAt: '2026-05-01T00:00:00Z',
              updatedAt: '2026-05-10T00:00:00Z',
            },
          ],
          total: 1,
          page: 1,
          pageSize: 20,
        },
      }
    }
    if (path === '/applications/app-1/runtime') {
      if (testState.runtimePending) await new Promise<never>(() => {})
      if (testState.runtimeStatus === 'not-found') {
        throw new ApiError(404, 'application not found')
      }
      if (testState.runtimeStatus === 'error') {
        throw new ApiError(500, 'runtime unavailable')
      }
      const defaultTag = testState.detailWithoutImageTagDefaults ? undefined : 'latest'
      return {
        data: {
          application: {
            id: 'app-1',
            version: 7,
            name: 'Checkout Platform',
            key: 'checkout-platform',
            group: 'commerce',
            language: 'go',
            repositoryPath: 'commerce/checkout',
            defaultBranch: 'main',
            defaultTag,
            enabled: true,
            buildSources: [
              {
                id: 'source-api',
                name: 'API Dockerfile',
                type: 'repo_dockerfile',
                enabled: true,
                isDefault: true,
                defaultTag,
                config: {
                  repositoryId: 'repository-api',
                  repositoryBindings: [
                    {
                      repositoryId: 'repository-api',
                      checkoutPath: '.',
                      defaultBranch: 'main',
                      allowCommitSelection: true,
                      submodules: false,
                    },
                    {
                      repositoryId: 'repository-shared',
                      checkoutPath: 'shared',
                      defaultBranch: 'main',
                      allowCommitSelection: false,
                      submodules: true,
                    },
                  ],
                },
              },
            ],
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
          environments: [
            {
              applicationEnvironmentId: 'binding-test',
              environmentId: 'env-test',
              environmentName: '测试环境',
              requiresApproval: false,
              manifestDeployments: testState.manifestDeployments,
              workloads: testState.manifestDeployments.length
                ? []
                : [
                    {
                      applicationEnvironmentId: 'binding-test',
                      clusterId: 'cluster-a',
                      namespace: 'checkout-test',
                      workloadKind: 'Deployment',
                      workloadName: 'checkout-api',
                      serviceId: 'svc-api',
                      serviceKey: 'api',
                      desiredReplicas: 2,
                      readyReplicas: 2,
                      updatedReplicas: 2,
                      availableReplicas: 2,
                      latestBundle: {
                        id: 'bundle-1',
                        applicationId: 'app-1',
                        applicationEnvironmentId: 'binding-test',
                        version: '1.2.3',
                        sourceType: 'build',
                        status: 'completed',
                        artifactRef: 'registry.example.com/checkout/api:1.2.3',
                        createdAt: '2026-05-10T00:00:00Z',
                        updatedAt: '2026-05-10T00:00:00Z',
                      },
                    },
                  ],
            },
            ...(testState.runtimeWithProductionEnvironment
              ? [
                  {
                    applicationEnvironmentId: 'binding-production',
                    environmentId: 'env-production',
                    environmentName: '线上',
                    requiresApproval: true,
                    workloads: [
                      {
                        applicationEnvironmentId: 'binding-production',
                        clusterId: 'cluster-production',
                        namespace: 'checkout-production',
                        workloadKind: 'StatefulSet',
                        workloadName: 'payments-worker',
                        serviceKey: 'payments',
                        desiredReplicas: 3,
                        readyReplicas: 2,
                        updatedReplicas: 3,
                        availableReplicas: 2,
                      },
                    ],
                  },
                ]
              : []),
            ...(testState.runtimeWithUnavailableEnvironment
              ? [
                  {
                    applicationEnvironmentId: 'binding-down',
                    environmentId: 'env-down',
                    environmentName: '不可用环境',
                    status: 'unavailable',
                    requiresApproval: false,
                    workloads: [],
                  },
                ]
              : []),
          ],
        },
      }
    }
    if (path === '/applications/app-1/detail') {
      const defaultTag = testState.detailWithoutImageTagDefaults ? undefined : 'latest'
      return {
        data: {
          application: {
            id: 'app-1',
            version: 7,
            name: 'Checkout Platform',
            key: 'checkout-platform',
            group: 'commerce',
            language: 'go',
            repositoryPath: 'commerce/checkout',
            defaultBranch: 'main',
            defaultTag,
            enabled: true,
            buildSources: [
              {
                id: 'source-api',
                name: 'API Dockerfile',
                type: 'repo_dockerfile',
                enabled: true,
                isDefault: true,
                defaultTag,
                config: {
                  repositoryId: 'repository-api',
                  repositoryBindings: [
                    {
                      repositoryId: 'repository-api',
                      checkoutPath: '.',
                      defaultBranch: 'main',
                      allowCommitSelection: true,
                      submodules: false,
                    },
                    {
                      repositoryId: 'repository-shared',
                      checkoutPath: 'shared',
                      defaultBranch: 'main',
                      allowCommitSelection: false,
                      submodules: true,
                    },
                  ],
                },
              },
            ],
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
          bindings: [
            {
              applicationEnvironmentId: 'binding-test',
              environmentId: 'env-test',
              environmentName: '测试环境',
              workflowTemplateId: 'wf-template-1',
              workflowTemplateName: 'Release DAG',
              workflowTemplate: testState.detailWithoutWorkflow
                ? undefined
                : {
                    id: 'wf-template-1',
                    key: 'release-dag',
                    name: 'Release DAG',
                    category: 'release',
                    definition: testState.detailWithoutValidationNodes
                      ? {
                          ...workflowDefinition,
                          nodes: workflowDefinition.nodes.filter(
                            (node) =>
                              node.type !== 'check_http' &&
                              node.type !== 'check_k8s_event' &&
                              node.type !== 'smoke_test' &&
                              node.type !== 'verify' &&
                              node.type !== 'check',
                          ),
                        }
                      : {
                          ...workflowDefinition,
                          nodes: [
                            ...workflowDefinition.nodes,
                            {
                              id: 'verify',
                              type: 'verify',
                              name: 'AI 回归验证',
                              executorKind: 'mcp',
                              targetKind: 'ai_test',
                              capabilityRef: 'testing.ui.run',
                              providerRef: 'external-test-platform',
                              artifactKinds: ['test_report', 'screenshot', 'junit'],
                              config: { url: 'https://example.com/healthz' },
                            },
                          ],
                        },
                  },
              targetCount: 1,
              targets: [
                {
                  id: 'target-1',
                  ...(testState.manifestTarget
                    ? {
                        executorKind: 'manifest_ssa',
                        targetKind: 'kustomize_overlay',
                        configRef: 'manifest-binding-1',
                        metadata: { serviceId: 'svc-api', manifestPackageName: 'Checkout config' },
                      }
                    : {}),
                  applicationEnvironmentId: 'binding-test',
                  clusterId: 'cluster-a',
                  namespace: 'checkout-test',
                  workloadKind: 'Deployment',
                  workloadName: 'checkout-api',
                  containerName: 'api',
                  enabled: true,
                },
              ],
              latestBundle: {
                id: 'bundle-1',
                applicationId: 'app-1',
                applicationEnvironmentId: 'binding-test',
                version: '1.2.3',
                sourceType: 'build',
                status: 'completed',
                artifactRef: 'registry.example.com/checkout/api:1.2.3',
                artifactDigest: 'sha256:abc',
                createdAt: '2026-05-10T00:00:00Z',
                updatedAt: '2026-05-10T00:00:00Z',
              },
              latestExecutionTask: {
                id: 'task-1',
                applicationId: 'app-1',
                releaseBundleId: 'bundle-1',
                taskKind: 'build_release',
                providerKind: 'ci_agent_runner',
                targetKind: 'k8s_workload',
                status: 'completed',
                maxRetries: 1,
                attemptCount: 1,
                timeoutSeconds: 600,
                artifacts: [
                  {
                    kind: 'image',
                    name: 'checkout-api',
                    ref: 'registry.example.com/checkout/api:1.2.3',
                  },
                ],
                createdAt: '2026-05-10T00:00:00Z',
                updatedAt: '2026-05-10T00:00:00Z',
              },
              latestBuild: {
                id: 'build-1',
                applicationId: 'app-1',
                sourceSystem: 'application',
                status: 'completed',
                createdAt: '2026-05-10T00:00:00Z',
              },
              latestWorkflow: {
                id: 'workflow-1',
                applicationId: 'app-1',
                workflowName: 'release-dag',
                status: 'completed',
                steps: [],
                nodeRuns: [
                  {
                    nodeId: 'approval',
                    name: '审批',
                    type: 'manual_approval',
                    status: 'completed',
                  },
                  {
                    nodeId: 'deploy',
                    name: '更新镜像',
                    type: 'deploy_update_image',
                    status: 'completed',
                  },
                ],
                metadata: { nodes: workflowDefinition.nodes },
                createdAt: '2026-05-10T00:00:00Z',
                updatedAt: '2026-05-10T00:00:00Z',
              },
              latestRelease: {
                id: 'release-1',
                applicationId: 'app-1',
                clusterId: 'cluster-a',
                namespace: 'checkout-test',
                deploymentName: 'checkout-api',
                status: 'completed',
                createdAt: '2026-05-10T00:00:00Z',
              },
            },
          ],
          latestBundle: {
            id: 'bundle-1',
            applicationId: 'app-1',
            applicationEnvironmentId: 'binding-test',
            version: '1.2.3',
            sourceType: 'build',
            status: 'completed',
            artifactRef: 'registry.example.com/checkout/api:1.2.3',
            artifactDigest: 'sha256:abc',
            createdAt: '2026-05-10T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
          latestExecutionTask: {
            id: 'task-1',
            applicationId: 'app-1',
            releaseBundleId: 'bundle-1',
            taskKind: 'build_release',
            providerKind: 'ci_agent_runner',
            targetKind: 'k8s_workload',
            status: 'completed',
            maxRetries: 1,
            attemptCount: 1,
            timeoutSeconds: 600,
            artifacts: [
              {
                kind: 'image',
                name: 'checkout-api',
                ref: 'registry.example.com/checkout/api:1.2.3',
              },
            ],
            createdAt: '2026-05-10T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
          latestBuild: {
            id: 'build-1',
            applicationId: 'app-1',
            sourceSystem: 'application',
            status: 'completed',
            createdAt: '2026-05-10T00:00:00Z',
          },
          latestWorkflow: {
            id: 'workflow-1',
            applicationId: 'app-1',
            workflowName: 'release-dag',
            status: 'completed',
            steps: [],
            nodeRuns: [
              { nodeId: 'approval', name: '审批', type: 'manual_approval', status: 'completed' },
              {
                nodeId: 'deploy',
                name: '更新镜像',
                type: 'deploy_update_image',
                status: 'completed',
              },
            ],
            metadata: { nodes: workflowDefinition.nodes },
            createdAt: '2026-05-10T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
          latestRelease: {
            id: 'release-1',
            applicationId: 'app-1',
            clusterId: 'cluster-a',
            namespace: 'checkout-test',
            deploymentName: 'checkout-api',
            status: 'completed',
            createdAt: '2026-05-10T00:00:00Z',
          },
        },
      }
    }
    if (path === '/applications/app-1/services') {
      if (testState.serviceCatalogStatus === 'pending') return new Promise<never>(() => {})
      if (testState.serviceCatalogStatus === 'error') throw new Error('service catalog unavailable')
      return {
        data: [
          {
            id: 'svc-api',
            applicationId: 'app-1',
            key: 'api',
            name: 'Checkout API',
            serviceKind: 'kubernetes_workload',
            ownerTeam: 'checkout-dev',
            repositoryPath: 'commerce/checkout/api',
            buildSourceId: testState.serviceLegacyRepository ? undefined : 'source-api',
            enabled: true,
            containers: [
              {
                id: 'svc-api:api',
                serviceId: 'svc-api',
                name: 'api',
                imageRepository: 'registry.example.com/checkout/api',
                runtimePorts: [8080],
              },
            ],
            createdAt: '2026-05-10T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/repositories?applicationId=app-1') {
      if (testState.createdRepository) return { data: [testState.createdRepository] }
      if (testState.repositoriesEmpty) return { data: [] }
      return {
        data: [
          {
            id: 'repository-api',
            name: 'checkout-api',
            provider: 'gitlab',
            protocol: 'https',
            url: 'https://git.example.com/commerce/checkout-api.git',
            path: 'commerce/checkout-api',
            defaultBranch: 'main',
            applicationIds: ['app-1'],
          },
          {
            id: 'repository-shared',
            name: 'checkout-shared',
            provider: 'git',
            protocol: 'ssh',
            url: 'git@git.example.com:commerce/checkout-shared.git',
            path: 'commerce/checkout-shared',
            defaultBranch: 'main',
            applicationIds: ['app-1'],
          },
        ],
      }
    }
    if (path === '/integrations/gitlab/projects') {
      return {
        items: testState.gitProjectsEmpty
          ? []
          : [
              {
                id: 'gitlab-project-1',
                name: 'checkout-imported',
                path: 'checkout-imported',
                pathWithNamespace: 'commerce/checkout-imported',
                defaultBranch: 'main',
                webUrl: 'https://git.example.com/commerce/checkout-imported.git',
              },
            ],
        data: undefined,
      }
    }
    if (path === '/integrations/gitlab/branches?projectId=gitlab-project-1') {
      return { items: [{ name: 'main', commitSha: 'abc123' }], data: undefined }
    }
    if (path === '/build-templates') {
      return {
        data: [
          {
            id: 'build-template-1',
            key: 'container-standard',
            name: '标准容器构建',
            buildCommands: ['docker build .'],
            variableSchema: {},
            defaultVariables: {},
            enabled: true,
          },
        ],
      }
    }
    if (path === '/delivery/release-bundles/bundle-1/artifacts') {
      return {
        data: [
          {
            id: 'artifact-1',
            releaseBundleId: 'bundle-1',
            applicationId: 'app-1',
            kind: 'image',
            name: 'checkout-api',
            ref: 'registry.example.com/checkout/api:1.2.3',
            digest: 'sha256:abc',
            status: 'completed',
          },
        ],
      }
    }
    if (path === '/delivery/execution-tasks/task-1/artifacts') {
      return {
        data: [
          {
            id: 'artifact-2',
            executionTaskId: 'task-1',
            applicationId: 'app-1',
            kind: 'image',
            name: 'checkout-api',
            ref: 'registry.example.com/checkout/api:1.2.3',
            digest: 'sha256:abc',
            status: 'completed',
          },
        ],
      }
    }
    if (path === '/builds?applicationId=app-1') {
      return {
        items: [
          {
            id: 'build-1',
            applicationId: 'app-1',
            sourceSystem: 'application',
            status: 'completed',
            createdAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/builds') {
      return {
        items: [
          {
            id: 'build-1',
            applicationId: 'app-1',
            sourceSystem: 'application',
            status: 'completed',
            createdAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/releases?applicationId=app-1') {
      return {
        items: [
          {
            id: 'release-1',
            applicationId: 'app-1',
            clusterId: 'cluster-a',
            namespace: 'checkout-test',
            deploymentName: 'checkout-api',
            status: 'completed',
            createdAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/releases') {
      return {
        items: [
          {
            id: 'release-1',
            applicationId: 'app-1',
            clusterId: 'cluster-a',
            namespace: 'checkout-test',
            deploymentName: 'checkout-api',
            status: 'completed',
            createdAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/workflows?applicationId=app-1') {
      return {
        items: [
          {
            id: 'workflow-1',
            applicationId: 'app-1',
            workflowName: 'release-dag',
            status: 'completed',
            steps: [],
            nodeRuns: [
              { nodeId: 'approval', name: '审批', type: 'manual_approval', status: 'completed' },
              {
                nodeId: 'deploy',
                name: '更新镜像',
                type: 'deploy_update_image',
                status: 'completed',
              },
            ],
            metadata: { nodes: workflowDefinition.nodes },
            createdAt: '2026-05-10T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/workflows') {
      return {
        items: [
          {
            id: 'workflow-1',
            applicationId: 'app-1',
            workflowName: 'release-dag',
            status: 'completed',
            steps: [],
            nodeRuns: [
              { nodeId: 'approval', name: '审批', type: 'manual_approval', status: 'completed' },
              {
                nodeId: 'deploy',
                name: '更新镜像',
                type: 'deploy_update_image',
                status: 'completed',
              },
            ],
            metadata: { nodes: workflowDefinition.nodes },
            createdAt: '2026-05-10T00:00:00Z',
            updatedAt: '2026-05-10T00:00:00Z',
          },
        ],
      }
    }
    throw new Error(`Unhandled GET ${path}`)
  }),
}))

const defaultPermissionKeys = [
  'delivery.applications.view',
  'delivery.application.update',
  'delivery.application-environments.create',
  'delivery.application-environments.update',
  'delivery.application-environments.delete',
  'delivery.application-services.view',
  'delivery.application-services.create',
  'delivery.application-services.update',
  'delivery.application-services.delete',
  'delivery.workflows.view',
  'delivery.builds.trigger',
  'delivery.workflows.trigger',
  'delivery.releases.trigger',
  'access.scope-grants.view',
  'access.users.view',
]

const readonlyPermissionKeys = [
  'delivery.workflows.view',
  'delivery.applications.view',
  'delivery.application-environments.view',
  'delivery.application-services.view',
]

vi.mock('@/features/auth/permission-snapshot', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, key: string) =>
    snapshot?.permissionKeys?.includes(key) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: testState.permissionSnapshot },
    isLoading: false,
  }),
}))

vi.mock('@/features/observability', () => ({
  LogExplorer: () => null,
}))

vi.mock('@/components/pod-terminal', () => ({
  PodTerminal: () => null,
}))

vi.mock('@/components/resource-metrics-panel', () => ({
  ResourceMetricsPanel: () => null,
}))

vi.mock('@/components/release-flow-dag-editor', () => ({
  ReleaseFlowDagEditor: ({ initialDefinition }: { initialDefinition: { nodes: unknown[] } }) => (
    <div data-testid="release-flow-dag-editor">{`${initialDefinition.nodes.length} 个节点`}</div>
  ),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: async (path: string) => {
      const body = await testState.apiGet(path)
      if (body && typeof body === 'object' && 'items' in body && !('data' in body)) {
        return { data: (body as { items: unknown }).items }
      }
      return body
    },
    getEnvelope: async (path: string) => testState.apiGet(path),
    post: vi.fn(async (path: string, body?: unknown) => {
      if (path === '/builds/trigger')
        return { data: { id: 'build-direct', applicationId: 'app-1', status: 'queued' } }
      if (path === '/applications/app-1/services')
        return { data: { ...(body as Record<string, unknown>), id: 'service-created' } }
      if (path === '/repositories') {
        const repository = {
          id: 'repository-imported',
          ...(body as Record<string, unknown>),
        }
        testState.createdRepository = repository
        return { data: repository }
      }
      if (path === '/delivery/plans') {
        const payload = body as Record<string, unknown>
        const plan = {
          id: 'plan-1',
          source: 'manual',
          status: 'draft',
          ...(testState.manifestTarget
            ? {
                manifestSnapshots: [
                  {
                    deliveryPlanId: 'plan-1',
                    packageId: 'manifest-1',
                    bindingId: 'manifest-binding-1',
                    targetId: 'target-1',
                    bindingVersion: 1,
                    applicationEnvironmentId: 'binding-test',
                    clusterId: 'cluster-a',
                    namespace: 'checkout-test',
                    revision: payload.manifestRevision || 3,
                    revisionDigest: 'sha256:revision',
                    packageUpdatedAt: '2026-05-10T01:00:00Z',
                    rendererVersion: 'kustomize/v1',
                    inputDigest: 'sha256:input',
                    renderedDigest: 'sha256:fixed',
                    sourceCommit: 'commit-123',
                    documents: [
                      {
                        apiVersion: 'v1',
                        kind: 'ConfigMap',
                        name: 'config',
                        namespace: 'checkout-test',
                        path: 'rendered.yaml',
                        index: 0,
                        contentDigest: 'sha256:doc',
                        content: 'kind: ConfigMap',
                      },
                    ],
                    preflightTaskId: 'preflight-1',
                    expectedGeneration: 0,
                  },
                ],
              }
            : {}),
          applicationId: payload.applicationId,
          applicationName: 'Checkout Platform',
          applicationEnvironmentId: payload.applicationEnvironmentId,
          environmentKey: 'test',
          action: payload.action,
          targetId: payload.targetId,
          targetSummary: 'cluster-a / checkout-test / checkout-api',
          buildSourceId: payload.buildSourceId,
          refType: payload.refType,
          refName: payload.refName,
          imageTag: payload.imageTag,
          riskLevel: payload.action === 'build' ? 'low' : 'medium',
          requiresApproval: payload.action !== 'build',
          impact: { applicationId: payload.applicationId, action: payload.action },
          rollbackStrategy:
            payload.action === 'build'
              ? 'Build only; no runtime rollback required.'
              : 'Use rollback context if rollout fails.',
          createdAt: '2026-05-10T01:00:00Z',
          updatedAt: '2026-05-10T01:00:00Z',
        }
        testState.lastDeliveryPlan = plan
        return { data: plan }
      }
      if (path === '/delivery/plans/plan-1/confirm') {
        const plan = testState.lastDeliveryPlan ?? {}
        return {
          data: {
            plan: {
              ...plan,
              status: 'confirmed',
              confirmedAt: '2026-05-10T01:01:00Z',
              updatedAt: '2026-05-10T01:01:00Z',
            },
            result: {
              action: plan.action ?? 'build',
              applicationId: plan.applicationId ?? 'app-1',
              applicationEnvironmentId: plan.applicationEnvironmentId ?? 'binding-test',
              relatedIds: { executionTaskId: 'task-planned' },
            },
          },
        }
      }
      return { data: { id: 'ok', path, body } }
    }),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{`${location.pathname}${location.search}`}</output>
}

async function renderWithProviders(
  node: ReactNode,
  route = '/applications/app-1',
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  await act(async () => {
    root.render(
      <I18nProvider>
        <AntApp>
          <QueryClientProvider client={queryClient}>
            <MemoryRouter initialEntries={[route]}>
              <Routes>
                <Route path="/execution-history" element={<LocationProbe />} />
                <Route
                  path="/applications/:applicationId/application-environments/:applicationEnvironmentId/workloads/:workloadName"
                  element={<LocationProbe />}
                />
                <Route
                  path="/applications/:applicationId/workflows/design"
                  element={
                    <>
                      <ApplicationBreadcrumb />
                      <ApplicationIdentity />
                      <ApplicationNavigation />
                      {node}
                      <LocationProbe />
                    </>
                  }
                />
                <Route
                  path="/applications/:applicationId"
                  element={
                    <>
                      <ApplicationBreadcrumb />
                      <ApplicationIdentity />
                      <ApplicationNavigation />
                      {node}
                      <LocationProbe />
                    </>
                  }
                />
              </Routes>
            </MemoryRouter>
          </QueryClientProvider>
        </AntApp>
      </I18nProvider>,
    )
  })

  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })

  if (route.includes('/workflows/design')) {
    await vi.waitFor(async () => {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(
        container.querySelector(
          '[data-testid="release-flow-dag-editor"], .soha-read-feedback, .soha-management-state.is-not-found',
        ),
      ).not.toBeNull()
    })
  }

  return container
}

async function openSection(container: HTMLElement, text: string) {
  const name =
    ({ 服务配置: '服务与构建', 扩展资源: '资源清单', 权限: '访问权限' } as Record<string, string>)[
      text
    ] || text
  if (name === '服务' || name === '工作流') {
    await act(async () => {
      document.querySelector<HTMLButtonElement>('.ant-drawer-open .ant-drawer-close')?.click()
    })
    if (name === '工作流')
      await act(async () => {
        findButton(container, '构建与更新').click()
      })
    return
  }
  if (!document.querySelector('[aria-label="设置分区"]')) {
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="应用设置"]')!.click()
    })
  }
  await act(async () => {
    findButton(document.querySelector('[aria-label="设置分区"]') as HTMLElement, name).click()
  })
}

function findButton(container: HTMLElement, text: string) {
  const buttons = [
    ...container.querySelectorAll('button'),
    ...document.querySelectorAll<HTMLButtonElement>('.ant-drawer-open button'),
  ]
  const button =
    buttons.find((item) => item.textContent?.replace(/\s/g, '') === text.replace(/\s/g, '')) ||
    buttons.find((item) => item.textContent?.includes(text))
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

function findModal(text: string) {
  const modal = Array.from(document.body.querySelectorAll<HTMLElement>('.ant-modal')).find((item) =>
    item.textContent?.includes(text),
  )
  if (!modal) {
    throw new Error(`modal not found: ${text}`)
  }
  return modal
}

describe('ApplicationDetailPage workbench', () => {
  beforeEach(() => {
    usePreferencesStore.setState({ localeCode: 'zh_CN' })
    testState.apiGet.mockClear()
    testState.readErrors.clear()
    testState.permissionSnapshot.permissionKeys = [...defaultPermissionKeys]
    testState.manifestTarget = false
    testState.manifestDeployments = []
    testState.preflightStatus = 'completed'
    testState.detailWithoutWorkflow = false
    testState.detailWithoutValidationNodes = false
    testState.detailWithoutImageTagDefaults = false
    testState.deliveryActionsAgentStatus = 'available'
    testState.deliveryClusterConnectionMode = 'direct_kubeconfig'
    testState.runtimeStatus = 'ok'
    testState.runtimePending = false
    testState.serviceCatalogStatus = 'ready'
    testState.serviceLegacyRepository = false
    testState.runtimeWithProductionEnvironment = false
    testState.runtimeWithUnavailableEnvironment = false
    testState.repositoriesEmpty = false
    testState.gitProjectsEmpty = false
    testState.createdRepository = undefined
    vi.mocked(api.post).mockClear()
    vi.mocked(api.put).mockClear()
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        matches: false,
        media: '',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })

    Object.defineProperty(window, 'getComputedStyle', {
      writable: true,
      value: vi.fn().mockReturnValue({
        width: '0px',
        height: '0px',
        overflow: 'auto',
        getPropertyValue: () => '',
      }),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) root.unmount()
    })
    roots = []
    for (const container of containers) container.remove()
    containers = []
    vi.clearAllMocks()
  })

  it('keeps one service pane beneath settings and workflow drawers without application tabs', async () => {
    const container = await renderWithProviders(<ApplicationDetailPage />)
    expect(testState.apiGet).toHaveBeenCalledWith('/applications/app-1/runtime')
    expect(container.querySelector('.soha-application-navigation')).toBeNull()
    expect(container.querySelectorAll('[aria-label="服务列表"] button')).toHaveLength(1)
    const pane = container.querySelector('.soha-service-console__body')
    await openSection(container, '环境配置')
    expect(document.querySelector('.ant-drawer-open')?.textContent).toContain('新增环境')
    expect(document.querySelector('.soha-application-settings__content')?.textContent).toContain(
      '部署与审批',
    )
    await openSection(container, '权限')
    const permissions = document.querySelector('.soha-application-settings__content')!
    expect(permissions.textContent).not.toContain('部署与审批')
    expect(permissions.querySelector('details')?.open).toBe(false)
    expect(permissions.querySelector('summary')?.textContent).toContain('查看我的权限')
    expect(
      Array.from(document.querySelectorAll('[aria-label="设置分区"] button')).map(
        (item) => item.textContent,
      ),
    ).toEqual(['服务与构建', '环境配置', '访问权限', '资源清单'])
    await vi.waitFor(() => expect(document.body.textContent).toContain('Release Owner'))
    await openSection(container, '扩展资源')
    await vi.waitFor(() =>
      expect(testState.apiGet).toHaveBeenCalledWith(
        '/delivery/manifest-packages?applicationId=app-1&page=1&pageSize=20',
      ),
    )
    await openSection(container, '工作流')
    expect(document.querySelector('.ant-drawer-open')?.textContent).toContain('构建与发布工作流')
    expect(document.body.querySelectorAll('[role="tablist"]')).toHaveLength(0)
    expect(container.querySelector('.soha-service-console__body')).toBe(pane)
    await openSection(container, '服务')
    expect(container.querySelector('.soha-service-console__body')).toBe(pane)
    expect(container.querySelector('[aria-label="应用环境"]')).not.toBeNull()
  }, 15_000)

  it('keeps the selected service open and preserves scoped Pods and actions', async () => {
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=services',
    )
    await act(async () => {
      await vi.dynamicImportSettled()
    })
    const service = container.querySelector<HTMLButtonElement>('[aria-label="服务列表"] button')!
    expect(service.textContent).toContain('Checkout API')
    expect(service.getAttribute('aria-current')).toBe('page')
    await act(async () => {
      service.click()
      await vi.dynamicImportSettled()
    })
    expect(container.querySelectorAll('.soha-service-console__body')).toHaveLength(1)
    expect(
      container.querySelector('.soha-service-overview .soha-service-overview__status')?.textContent,
    ).toContain('运行正常')
    expect(
      container.querySelector('.soha-service-overview__readiness')?.getAttribute('aria-label'),
    ).toBe('实例就绪 2 / 2')
    expect(container.querySelectorAll('.soha-rollout-stages > li')).toHaveLength(3)
    expect(container.querySelector('.soha-service-summary details')).toBeNull()
    const serviceActions = container.querySelector('.soha-service-actions')
    expect(serviceActions?.textContent).toContain('构建与更新')
    expect(serviceActions?.textContent).toContain('资源清单')
    expect(serviceActions?.textContent).not.toContain('编辑服务')
    expect(container.querySelector('.soha-pod-card')?.textContent).toContain('checkout-api-pod')
    expect(container.querySelector('.soha-service-summary')?.textContent).toContain(
      'commerce/checkout/api',
    )
    expect(
      container.querySelector('.soha-service-summary .soha-service-runtime-summary'),
    ).not.toBeNull()
    expect(container.querySelector('.soha-service-card-list')).toBeNull()
    expect(container.querySelector('[data-testid="location"]')?.textContent).toBe(
      '/applications/app-1?tab=services&applicationEnvironmentId=binding-test&serviceId=svc-api&serviceTab=pods',
    )
    const selector = container.querySelector('.soha-service-selector')!
    expect(selector.querySelector('[aria-label="新建服务"]')).toBeNull()
    await act(async () => {
      selector.querySelector<HTMLButtonElement>('[aria-label="应用设置"]')!.click()
    })
    const settingsURL = new URL(
      container.querySelector('[data-testid="location"]')!.textContent!,
      'http://localhost',
    )
    expect(Object.fromEntries(settingsURL.searchParams)).toEqual({
      tab: 'services',
      settings: 'application',
      applicationEnvironmentId: 'binding-test',
      serviceId: 'svc-api',
      serviceTab: 'pods',
    })
    const settings = document.querySelector('.ant-drawer-open')!
    expect(settings.textContent).toContain('应用设置')
    expect(settings.textContent).toContain('新建服务')
    expect(settings.querySelector('[aria-label="编辑服务 Checkout API"]')).not.toBeNull()
    await act(async () => {
      settings.querySelector<HTMLButtonElement>('.ant-drawer-close')!.click()
    })
    expect(container.querySelector('[data-testid="location"]')?.textContent).toBe(
      '/applications/app-1?tab=services&applicationEnvironmentId=binding-test&serviceId=svc-api&serviceTab=pods',
    )
  })

  it('does not silently use the first environment for an invalid shared link', async () => {
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=services&applicationEnvironmentId=missing',
    )
    expect(container.textContent).toContain('所选环境不存在或不可访问')
    expect(container.querySelector('.soha-application-service-runtime-list')).toBeNull()
    expect(container.querySelector('[data-testid="location"]')?.textContent).toContain(
      'applicationEnvironmentId=missing',
    )
  })

  it('falls back to services for a removed test page while preserving environment', async () => {
    testState.runtimeWithProductionEnvironment = true
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=verification&applicationEnvironmentId=binding-production',
    )
    expect(container.textContent).toContain('线上')
    expect(container.textContent).not.toContain('Release DAG')
    expect(container.querySelector('.soha-service-console')).not.toBeNull()
    expect(container.textContent).not.toContain('验证范围')
  })

  it.each(['pending', 'error'] as const)(
    'keeps runtime usable and exposes %s service catalog state',
    async (status) => {
      testState.serviceCatalogStatus = status
      const container = await renderWithProviders(
        <ApplicationDetailPage />,
        '/applications/app-1?tab=services',
      )
      expect(container.textContent).toContain(
        status === 'pending' ? '正在加载服务档案' : '服务档案加载失败',
      )
      expect(container.textContent).toContain('checkout-api')
      expect(container.textContent).not.toContain('尚未接入服务')
      if (status === 'error') expect(findButton(container, '重试服务档案')).not.toBeNull()
    },
  )

  it('keeps service configuration available when the selected environment has no deployment', async () => {
    testState.runtimeWithProductionEnvironment = true
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=services&applicationEnvironmentId=binding-production&serviceId=svc-api',
    )
    expect(container.textContent).toContain('当前环境尚未部署此服务')
    expect(container.querySelector('[role="tablist"]')).toBeNull()
    expect(
      Array.from(container.querySelectorAll('[role="tab"]')).some(
        (tab) => tab.textContent === '配置',
      ),
    ).toBe(false)
    expect(container.querySelector('.soha-service-summary')?.textContent).toContain('负责人 / 团队')
    expect(container.querySelector('[data-testid="location"]')?.textContent).toContain(
      'applicationEnvironmentId=binding-production',
    )
  })

  it('opens a project canvas from a reusable template and saves an application workflow', async () => {
    const container = await renderWithProviders(
      <ApplicationWorkflowDesignerPage />,
      '/applications/app-1/workflows/design?bindingId=binding-test&source=template&templateId=wf-template-1&name=测试发布',
    )

    expect(container.textContent).toContain('项目工作流设计')
    expect(container.textContent).toContain('基于 Release DAG')
    expect(container.querySelector('[data-testid="release-flow-dag-editor"]')?.textContent).toBe(
      '2 个节点',
    )
    vi.mocked(api.put).mockResolvedValueOnce({ data: { id: 'binding-test' } } as never)

    await act(async () => {
      findButton(container, '保存工作流').dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(api.post).not.toHaveBeenCalledWith('/workflow-templates', expect.anything())
    expect(api.put).toHaveBeenCalledWith(
      '/applications/app-1/application-environments/binding-test/workflow',
      {
        name: '测试发布',
        description: '应用内工作流',
        definition: workflowDefinition,
        enabled: true,
      },
    )
    expect(container.querySelector('[data-testid="location"]')?.textContent).toBe(
      '/applications/app-1?tab=delivery',
    )
  })

  it('localizes the application workflow designer in English', async () => {
    usePreferencesStore.setState({ localeCode: 'en_US' })
    const container = await renderWithProviders(
      <ApplicationWorkflowDesignerPage />,
      '/applications/app-1/workflows/design?bindingId=binding-test&source=template&templateId=wf-template-1',
    )

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 30))
    })

    expect(container.textContent).toContain('Application workflow designer')
    expect(container.textContent).toContain('Based on Release DAG')
    expect(findButton(container, 'Save workflow')).not.toBeNull()
    expect(container.textContent).not.toContain('项目工作流设计')
  })

  it('retries a failed environment read before opening the workflow canvas', async () => {
    testState.readErrors.set(
      '/application-environments',
      new ApiError(503, 'environment unavailable'),
    )
    const container = await renderWithProviders(
      <ApplicationWorkflowDesignerPage />,
      '/applications/app-1/workflows/design?bindingId=binding-test&source=template&templateId=wf-template-1',
    )
    expect(container.textContent).toContain('应用环境读取失败')
    expect(container.querySelector('[data-testid="release-flow-dag-editor"]')).toBeNull()
    testState.readErrors.clear()
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="重试读取应用环境"]')!.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.querySelector('[data-testid="release-flow-dag-editor"]')?.textContent).toBe(
      '2 个节点',
    )
    expect(container.textContent).toContain('未保存')
  })

  it.each([401, 403])(
    'retains a workflow draft after 503, then hides it after %s',
    async (status) => {
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      const container = await renderWithProviders(
        <ApplicationWorkflowDesignerPage />,
        '/applications/app-1/workflows/design?bindingId=binding-test&source=template&templateId=wf-template-1',
        client,
      )
      const editor = container.querySelector('[data-testid="release-flow-dag-editor"]')
      const input = container.querySelector<HTMLInputElement>('[aria-label="工作流名称"]')!
      await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
          input,
          '保留的草稿',
        )
        input.dispatchEvent(new Event('input', { bubbles: true }))
      })
      testState.readErrors.set('/application-environments', new ApiError(503, 'temporary failure'))
      await act(async () => {
        await client.refetchQueries({ queryKey: deliveryQueries.environments.list(true).queryKey })
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(container.textContent).toContain('应用环境未更新，正在展示上次读取结果')
      expect(container.querySelector('[data-testid="release-flow-dag-editor"]')).toBe(editor)
      expect(input.value).toBe('保留的草稿')
      testState.readErrors.set('/application-environments', new ApiError(status, 'access lost'))
      await act(async () => {
        await client.refetchQueries({ queryKey: deliveryQueries.environments.list(true).queryKey })
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(client.getQueryData(deliveryQueries.environments.list(true).queryKey)).toBeDefined()
      expect(container.querySelector('[data-testid="release-flow-dag-editor"]')).toBeNull()
      expect(container.querySelector('[aria-label="工作流名称"]')).toBeNull()
      expect(container.textContent).toContain(
        status === 401 ? '应用环境登录状态失效' : '应用环境无访问权限',
      )
    },
  )

  it('keeps the submitted workflow identity and draft on a rejected save', async () => {
    const container = await renderWithProviders(
      <ApplicationWorkflowDesignerPage />,
      '/applications/app-1/workflows/design?bindingId=binding-test&source=template&templateId=wf-template-1&name=原始工作流',
    )
    vi.mocked(api.put).mockRejectedValueOnce(
      new ApiError(409, 'revision changed', { requestId: 'workflow-conflict' }),
    )
    await act(async () => {
      findButton(container, '保存工作流').click()
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    const input = container.querySelector<HTMLInputElement>('[aria-label="工作流名称"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        input,
        '下一次草稿',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    const feedback = container.querySelector('.soha-operation-feedback')!
    expect(feedback.textContent).toContain('操作请求失败')
    expect(feedback.textContent).toContain('集成测试 · 原始工作流')
    expect(feedback.textContent).toContain('workflow-conflict')
    expect(feedback.textContent).not.toContain('下一次草稿')
    expect(input.value).toBe('下一次草稿')
    expect(findButton(container, '保存工作流').disabled).toBe(false)
    expect(container.querySelector('[data-testid="release-flow-dag-editor"]')).not.toBeNull()
  })

  it('fences a pending save and checks an unknown outcome with reads only', async () => {
    let rejectSave!: (error: Error) => void
    vi.mocked(api.put).mockImplementationOnce(
      () =>
        new Promise((_resolve, reject) => {
          rejectSave = reject
        }),
    )
    const container = await renderWithProviders(
      <ApplicationWorkflowDesignerPage />,
      '/applications/app-1/workflows/design?bindingId=binding-test&source=template&templateId=wf-template-1',
    )
    const editor = container.querySelector('[data-testid="release-flow-dag-editor"]')!
    await act(async () => {
      const button = findButton(container, '保存工作流')
      button.click()
      button.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await vi.waitFor(async () => {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(api.put).toHaveBeenCalledTimes(1)
      expect((editor.parentElement as HTMLElement).inert).toBe(true)
    })
    expect(container.querySelector<HTMLInputElement>('[aria-label="工作流名称"]')!.disabled).toBe(
      true,
    )
    await act(async () => {
      rejectSave(
        new ApiError(503, 'response lost', { method: 'PUT', requestId: 'workflow-unknown' }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.textContent).toContain('请求结果不明')
    expect(container.textContent).toContain('workflow-unknown')
    expect(findButton(container, '保存工作流').disabled).toBe(true)
    expect((editor.parentElement as HTMLElement).inert).toBe(false)
    const reads = testState.apiGet.mock.calls.filter(
      ([path]) => path === '/application-environments',
    ).length
    await act(async () => {
      findButton(container, '核对环境记录').click()
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(
      testState.apiGet.mock.calls.filter(([path]) => path === '/application-environments'),
    ).toHaveLength(reads + 1)
    expect(api.put).toHaveBeenCalledTimes(1)
    expect(api.post).not.toHaveBeenCalled()
    expect(findButton(container, '保存工作流').disabled).toBe(true)
    expect(container.querySelector('[data-testid="release-flow-dag-editor"]')).toBe(editor)
  })

  it('keeps the original template version and canvas when its catalog changes', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const container = await renderWithProviders(
      <ApplicationWorkflowDesignerPage />,
      '/applications/app-1/workflows/design?bindingId=binding-test&source=template&templateId=wf-template-1',
      client,
    )
    const editor = container.querySelector('[data-testid="release-flow-dag-editor"]')
    const key = deliveryQueries.workflowTemplates.list(true).queryKey
    await act(async () => {
      client.setQueryData(key, (previous) =>
        previous?.map((template) => ({ ...template, publishedVersion: 2 })),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.querySelector('[data-testid="release-flow-dag-editor"]')).toBe(editor)
    expect(testState.apiGet).not.toHaveBeenCalledWith(
      '/workflow-templates/wf-template-1/versions/2',
    )
  })

  it('does not mark an open draft as saved when a workflow appears in a background read', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const container = await renderWithProviders(
      <ApplicationWorkflowDesignerPage />,
      '/applications/app-1/workflows/design?bindingId=binding-test',
      client,
    )
    const editor = container.querySelector('[data-testid="release-flow-dag-editor"]')
    await act(async () => {
      client.setQueryData(deliveryQueries.environments.list(true).queryKey, (previous) =>
        previous?.map((binding) => ({
          ...binding,
          workflowTemplate: {
            id: 'external-workflow',
            key: 'external',
            name: '其他操作者的工作流',
            revision: 2,
            enabled: true,
            createdAt: '2026-10-16T00:00:00Z',
            updatedAt: '2026-10-16T00:00:00Z',
            definition: workflowDefinition,
          },
        })),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.querySelector('[data-testid="release-flow-dag-editor"]')).toBe(editor)
    expect(container.textContent).toContain('未保存')
    expect(container.textContent).not.toContain('已同步')
    expect(container.textContent).not.toContain('基于 其他操作者的工作流')
  })

  it('shows a not-found state for an invalid template version instead of a blank page', async () => {
    const container = await renderWithProviders(
      <ApplicationWorkflowDesignerPage />,
      '/applications/app-1/workflows/design?bindingId=binding-test&source=template&templateId=wf-template-1&templateVersion=-1',
    )
    expect(container.textContent).toContain('未找到所选工作流模板版本')
    expect(container.querySelector('[data-testid="release-flow-dag-editor"]')).toBeNull()
  })

  it('renders service configuration while runtime query is pending', async () => {
    testState.runtimePending = true
    const container = await renderWithProviders(<ApplicationDetailPage />)
    expect(container.querySelector('[aria-label="应用导航"]')).not.toBeNull()
    expect(container.querySelector('.soha-service-console')).not.toBeNull()
    expect(container.textContent).toContain('Checkout API')
    expect(container.textContent).toContain('读取状态')
    expect(container.textContent).not.toContain('未部署')
  })

  it('filters services by the selected environment', async () => {
    testState.runtimeWithProductionEnvironment = true
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=services',
    )

    expect(container.querySelector('[aria-label="服务列表"] button')).not.toBeNull()
    expect(container.textContent).not.toContain('payments-worker')

    expect(container.querySelectorAll('[aria-label="应用环境"]')).toHaveLength(1)
    expect(container.querySelector('.soha-application-service-environment-grid')).toBeNull()
    await act(async () => {
      container
        .querySelector('[aria-label="应用环境"]')!
        .dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    })
    const productionOption = Array.from(
      document.querySelectorAll<HTMLElement>('.ant-select-item-option'),
    ).find((item) => item.textContent?.includes('线上'))!
    expect(productionOption.textContent).toBe('线上 PROD')
    await act(async () => {
      productionOption.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(container.textContent).toContain('当前环境尚未部署此服务')
    expect(container.querySelector('.soha-pod-card')).toBeNull()
    expect(container.querySelector('[aria-label="服务列表"] button')).not.toBeNull()
    expect(container.querySelector('[data-testid="location"]')?.textContent).toContain(
      'applicationEnvironmentId=binding-production',
    )
  })

  it('shows cluster-unavailable environments instead of treating them as undeployed', async () => {
    testState.runtimeWithUnavailableEnvironment = true
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=services',
    )

    await act(async () => {
      container
        .querySelector('[aria-label="应用环境"]')!
        .dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    })
    const option = Array.from(
      document.querySelectorAll<HTMLElement>('.ant-select-item-option'),
    ).find((item) => item.textContent?.includes('不可用环境'))!
    await act(async () => {
      option.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.textContent).toContain('集群不可用')
    expect(container.querySelector('.soha-rollout-progress')).toBeNull()
  })

  it('creates an application environment from the service context switcher', async () => {
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=services',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/environments')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/registries')
    await openSection(container, '环境配置')

    await act(async () => {
      findButton(container, '新增环境').dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/environments')
    expect(testState.apiGet).toHaveBeenCalledWith('/registries')
    const modal = findModal('新增环境')
    expect(modal.textContent).toContain('新增环境')
    expect(modal.textContent).toContain('环境目录')
    expect(modal.textContent).toContain('环境别名')
    expect(modal.textContent).toContain('集群')
    expect(modal.textContent).toContain('Namespace')
    expect(modal.textContent).toContain('镜像仓库')
    expect(modal.textContent).toContain('发布目标')
  })

  it('keeps repository, build definition, and artifact output in service configuration', async () => {
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=application',
    )

    expect(
      document.querySelector('[aria-label="设置分区"] [aria-current="page"]')?.textContent,
    ).toContain('服务与构建')
    expect(document.body.textContent).toContain('服务 → Git 仓库 → 构建定义 → 产物镜像')
    expect(document.querySelector('.ant-collapse-item-active')?.textContent).toContain(
      'Checkout API',
    )
    expect(document.querySelector('.soha-application-settings__content details')).toBeNull()
    expect(document.querySelector('.soha-application-settings__content')?.textContent).toContain(
      '共享代码仓库',
    )
    expect(document.querySelector('.soha-application-settings__content')?.textContent).toContain(
      '共享构建定义',
    )

    await act(async () => {
      findButton(container, '新建服务').dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    const serviceEditor = document.querySelector(
      '.soha-application-settings__content',
    ) as HTMLElement
    expect(document.querySelectorAll('.ant-drawer-open')).toHaveLength(1)
    expect(serviceEditor.textContent).toContain('新建服务组件')
    expect(serviceEditor.textContent).toContain('服务来源')
    expect(serviceEditor.textContent).toContain('已有镜像')
    expect(serviceEditor.textContent).toContain('产物与部署')
    expect(
      serviceEditor.querySelector('input[value="image"]')?.getAttribute('checked'),
    ).not.toBeNull()
    expect(serviceEditor.querySelector('#buildSourceId')).toBeNull()
    expect(api.post).not.toHaveBeenCalledWith('/repositories', expect.anything())
    await act(async () => {
      findButton(serviceEditor, '取消').click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await act(async () => {
      findButton(container, '添加构建').dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    const buildModal = findModal('构建方式')
    expect(buildModal.textContent).toContain('构建方式')
    expect(buildModal.textContent).toContain('源码仓库')
    expect(buildModal.textContent).toContain('添加检出项')
    expect(buildModal.textContent).toContain('运行时允许选择 Commit')
    expect(buildModal.textContent).toContain('拉取 Git Submodule')
    expect(buildModal.textContent).toContain('产物镜像')
    expect(buildModal.textContent).toContain('Dockerfile')
    expect(buildModal.textContent).toContain('构建完成后自动推送产物镜像')
  })

  it('saves an image service without inheriting the default build and selects the returned service', async () => {
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=application&applicationEnvironmentId=binding-test',
    )
    await act(async () => {
      findButton(container, '新建服务').click()
    })
    const editor = document.querySelector('.soha-application-settings__content') as HTMLElement
    const setInput = async (id: string, value: string) => {
      const input = editor.querySelector<HTMLInputElement>(`#${id}`)!
      await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
          input,
          value,
        )
        input.dispatchEvent(new Event('input', { bubbles: true }))
      })
    }
    await setInput('key', 'image-api')
    await setInput('name', 'Image API')
    await act(async () => {
      findButton(editor, '下一步').click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await act(async () => {
      findButton(editor, '下一步').click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await setInput('containers_0_imageRepository', 'registry.example.com/api')
    await setInput('containers_0_defaultTagTemplate', 'stable')
    await setInput('containers_0_runtimePortsText', '8080, 9090')
    await act(async () => {
      findButton(editor, '下一步').click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await act(async () => {
      findButton(editor, '保存服务').click()
      findButton(editor, '保存服务').click()
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(api.post).toHaveBeenCalledWith(
      '/applications/app-1/services',
      expect.objectContaining({
        key: 'image-api',
        buildSourceId: '',
        repositoryId: '',
        metadata: { sourceMode: 'image' },
        containers: [
          expect.objectContaining({
            imageRepository: 'registry.example.com/api',
            runtimePorts: [8080, 9090],
          }),
        ],
      }),
    )
    expect(api.put).not.toHaveBeenCalled()
    expect(
      vi.mocked(api.post).mock.calls.filter(([path]) => path === '/applications/app-1/services'),
    ).toHaveLength(1)
    const location = container.querySelector('[data-testid="location"]')?.textContent || ''
    expect(location).toContain('serviceId=service-created')
    expect(location).toContain('applicationEnvironmentId=binding-test')
    expect(location).toContain('settings=environment-bindings')
  })

  it.each([
    [false, false],
    [true, false],
    [false, true],
  ])(
    'keeps a repository draft local and saves only the active build type (external pipeline: %s, retry: %s)',
    async (externalPipeline, retryService) => {
      testState.repositoriesEmpty = true
      const container = await renderWithProviders(
        <ApplicationDetailPage />,
        '/applications/app-1?tab=application',
      )
      await act(async () => {
        findButton(container, '新建服务').click()
      })
      const editor = document.querySelector('.soha-application-settings__content') as HTMLElement
      const click = async (element: HTMLElement) => {
        await act(async () => {
          element.click()
          await new Promise((resolve) => setTimeout(resolve, 0))
        })
      }
      const setInput = async (id: string, value: string) => {
        const input = editor.querySelector<HTMLInputElement>(`#${id}`)!
        await act(async () => {
          Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
            input,
            value,
          )
          input.dispatchEvent(new Event('input', { bubbles: true }))
        })
      }
      await setInput('key', 'source-api')
      await setInput('name', 'Source API')
      await click(editor.querySelector<HTMLInputElement>('input[value="build"]')!)
      await click(editor.querySelector<HTMLInputElement>('input[value="new"]')!)
      await click(findButton(editor, '从代码源接入'))
      expect(document.querySelectorAll('.ant-drawer-open')).toHaveLength(1)
      expect(
        Array.from(document.querySelectorAll<HTMLElement>('.ant-modal')).filter(
          (modal) => modal.style.display !== 'none',
        ),
      ).toHaveLength(0)
      await act(async () => {
        editor
          .querySelector('#repositoryDraft_gitlabProjectId')!
          .dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      await click(
        Array.from(document.querySelectorAll<HTMLElement>('.ant-select-item-option')).find((item) =>
          item.textContent?.includes('commerce/checkout-imported'),
        )!,
      )
      await click(findButton(editor, '加入草稿'))
      expect(api.post).not.toHaveBeenCalled()
      expect(editor.textContent).toContain('checkout-imported')
      await click(findButton(editor, '下一步'))
      await setInput('buildSource_name', 'Source API build')
      if (externalPipeline) {
        await act(async () => {
          editor
            .querySelector('#buildSource_type')!
            .dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
          await new Promise((resolve) => setTimeout(resolve, 0))
        })
        await click(
          Array.from(document.querySelectorAll<HTMLElement>('.ant-select-item-option')).find(
            (item) => item.textContent?.includes('GitLab CI'),
          )!,
        )
        await setInput('buildSource_buildImage', 'registry.example.com/source-api')
        await setInput('buildSource_config_externalPipeline_pipelineTag', 'soha-v1')
        await setInput('buildSource_config_externalPipeline_artifactJob', 'publish')
        await act(async () => {
          editor
            .querySelector('#buildSource_config_externalPipeline_registryId')!
            .dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
          await new Promise((resolve) => setTimeout(resolve, 0))
        })
        await click(
          Array.from(document.querySelectorAll<HTMLElement>('.ant-select-item-option')).find(
            (item) => item.textContent?.includes('Harbor 主仓库'),
          )!,
        )
      } else {
        await setInput('buildSource_buildImage', 'registry.example.com/source-api')
      }
      await click(findButton(editor, '下一步'))
      if (!externalPipeline) {
        expect(editor.querySelector<HTMLInputElement>('#containers_0_imageRepository')?.value).toBe(
          'registry.example.com/source-api',
        )
      }
      if (retryService) {
        vi.mocked(api.post)
          .mockImplementationOnce(vi.mocked(api.post).getMockImplementation()!)
          .mockRejectedValueOnce(new ApiError(400, '服务保存被拒绝'))
      }
      await click(findButton(editor, '下一步'))
      await click(findButton(editor, '保存服务'))
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      if (retryService) {
        expect(editor.textContent).toContain('部分配置已保存')
        expect(editor.querySelector<HTMLInputElement>('#buildSource_name')?.value).toBe(
          'Source API build',
        )
        await click(findButton(editor, '继续保存'))
        expect(
          vi.mocked(api.post).mock.calls.filter(([path]) => path === '/repositories'),
        ).toHaveLength(1)
        expect(
          vi.mocked(api.put).mock.calls.filter(([path]) => path === '/applications/app-1'),
        ).toHaveLength(1)
      }
      expect(api.post).toHaveBeenCalledWith(
        '/repositories',
        expect.objectContaining({ applicationIds: ['app-1'], gitlabProjectId: 'gitlab-project-1' }),
      )
      const update = vi
        .mocked(api.put)
        .mock.calls.find(([path]) => path === '/applications/app-1')?.[1] as {
        buildSources: Array<{
          id: string
          name: string
          config: { repositoryBindings: Array<{ repositoryId: string }> }
        }>
      }
      const source = update.buildSources.find((item) => item.name === 'Source API build')!
      expect(source.id).toMatch(/^[0-9a-f-]{36}$/)
      if (externalPipeline) {
        expect(source.config).toEqual(
          expect.objectContaining({
            externalPipeline: {
              provider: 'gitlab',
              pipelineTag: 'soha-v1',
              artifactJob: 'publish',
              registryId: 'registry-a',
            },
          }),
        )
        expect(source.config).not.toHaveProperty('dockerfilePath')
        expect(source.config).not.toHaveProperty('buildTemplateId')
      }
      expect(source.config.repositoryBindings).toEqual([
        expect.objectContaining({ repositoryId: 'repository-imported' }),
      ])
      await vi.waitFor(async () => {
        await act(async () => {
          await new Promise((resolve) => setTimeout(resolve, 0))
        })
        expect(api.post).toHaveBeenCalledWith(
          '/applications/app-1/services',
          expect.objectContaining({
            buildSourceId: source.id,
            repositoryId: 'repository-imported',
            name: 'Source API',
          }),
        )
      })
    },
    15_000,
  )

  it('copies a multi-repository shared build without changing the original definition', async () => {
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=application',
    )
    await act(async () => {
      document.querySelector<HTMLButtonElement>('[aria-label="编辑服务 Checkout API"]')!.click()
    })
    const editor = document.querySelector('.soha-application-settings__content') as HTMLElement
    const click = async (text: string) => {
      await act(async () => {
        findButton(editor, text).click()
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    }
    await click('复制为此服务构建')
    expect(editor.textContent).toContain('checkout-shared')
    expect(
      editor
        .querySelector<HTMLInputElement>('#buildSource_config_repositoryBindings_1_submodules')
        ?.getAttribute('aria-checked'),
    ).toBe('true')
    await click('下一步')
    await act(async () => {
      const input = editor.querySelector<HTMLInputElement>('#buildSource_buildImage')!
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        input,
        'registry.example.com/copy-api',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await click('下一步')
    await click('下一步')
    await click('保存服务')
    const update = vi
      .mocked(api.put)
      .mock.calls.find(([path]) => path === '/applications/app-1')?.[1] as {
      buildSources: Array<{
        id: string
        name: string
        isDefault: boolean
        config: { repositoryBindings: Array<{ repositoryId: string; submodules?: boolean }> }
      }>
    }
    expect(update.buildSources.find((item) => item.id === 'source-api')?.isDefault).toBe(true)
    const copy = update.buildSources.find((item) => item.name === 'API Dockerfile 副本')!
    expect(copy.id).not.toBe('source-api')
    expect(copy.isDefault).toBe(false)
    expect(copy.config.repositoryBindings).toHaveLength(2)
    expect(copy.config.repositoryBindings[1]).toMatchObject({
      repositoryId: 'repository-shared',
      submodules: true,
    })
    expect(api.put).toHaveBeenCalledWith(
      '/applications/app-1/services/svc-api',
      expect.objectContaining({
        buildSourceId: copy.id,
        containers: [expect.objectContaining({ id: 'svc-api:api', runtimePorts: [8080] })],
      }),
    )
    expect(container.querySelector('[data-testid="location"]')?.textContent).toContain(
      'serviceId=svc-api',
    )
  })

  it('clears build associations when an existing service switches to configuration only', async () => {
    await renderWithProviders(<ApplicationDetailPage />, '/applications/app-1?tab=application')
    await act(async () => {
      document.querySelector<HTMLButtonElement>('[aria-label="编辑服务 Checkout API"]')!.click()
    })
    const editor = document.querySelector('.soha-application-settings__content') as HTMLElement
    const click = async (element: HTMLElement) => {
      await act(async () => {
        element.click()
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    }
    await click(findButton(editor, '复制为此服务构建'))
    await click(editor.querySelector<HTMLInputElement>('input[value="configuration"]')!)
    await click(findButton(editor, '下一步'))
    await click(findButton(editor, '下一步'))
    await click(findButton(editor, '下一步'))
    await click(findButton(editor, '保存服务'))
    expect(api.put).toHaveBeenCalledTimes(1)
    expect(api.put).toHaveBeenCalledWith(
      '/applications/app-1/services/svc-api',
      expect.objectContaining({
        buildSourceId: '',
        repositoryId: '',
        repositoryPath: '',
        defaultBranch: '',
        metadata: { sourceMode: 'configuration' },
      }),
    )
    expect(api.post).not.toHaveBeenCalled()
  })

  it('preserves legacy repository links when editing a service without a build definition', async () => {
    testState.serviceLegacyRepository = true
    await renderWithProviders(<ApplicationDetailPage />, '/applications/app-1?tab=application')
    await act(async () => {
      document.querySelector<HTMLButtonElement>('[aria-label="编辑服务 Checkout API"]')!.click()
    })
    const editor = document.querySelector('.soha-application-settings__content') as HTMLElement
    expect(editor.textContent).toContain('沿用现有来源')
    for (const label of ['下一步', '下一步', '下一步', '保存服务']) {
      await act(async () => {
        findButton(editor, label).click()
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    }
    expect(api.put).toHaveBeenCalledWith(
      '/applications/app-1/services/svc-api',
      expect.objectContaining({
        repositoryPath: 'commerce/checkout/api',
        containers: [expect.objectContaining({ id: 'svc-api:api' })],
      }),
    )
    expect(api.put).toHaveBeenCalledTimes(1)
    expect(api.post).not.toHaveBeenCalled()
  })

  it('preserves a shared build draft on conflict and reloads the complete application snapshot', async () => {
    await renderWithProviders(<ApplicationDetailPage />, '/applications/app-1?tab=application')
    const click = async (element: HTMLElement) => {
      await act(async () => {
        element.click()
        await new Promise((resolve) => setTimeout(resolve, 0))
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
    }
    await click(document.querySelector<HTMLButtonElement>('[aria-label="编辑构建源"]')!)
    const modal = findModal('编辑构建')
    for (const [id, value] of [
      ['name', 'My unsaved build'],
      ['buildImage', 'registry.example.com/api'],
    ]) {
      await act(async () => {
        const input = modal.querySelector<HTMLInputElement>(`#${id}`)!
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
          input,
          value,
        )
        input.dispatchEvent(new Event('input', { bubbles: true }))
      })
    }
    vi.mocked(api.put).mockRejectedValueOnce(new ApiError(409, '应用配置版本已变化'))
    await click(findButton(modal, '保存'))
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 30))
    })
    expect(api.put).toHaveBeenCalledWith(
      '/applications/app-1',
      expect.objectContaining({ expectedVersion: 7 }),
    )
    expect(modal.querySelector<HTMLInputElement>('#name')?.value).toBe('My unsaved build')
    expect(modal.textContent).toContain('当前草稿已保留')

    const previousGet = testState.apiGet.getMockImplementation()!
    const detail = (await previousGet('/applications/app-1/detail')) as {
      data: { application: Record<string, unknown> }
    }
    const latest = {
      ...detail.data.application,
      name: 'Updated application',
      version: 9,
      buildSources: [
        {
          id: 'source-api',
          name: 'Server build',
          type: 'repo_dockerfile',
          enabled: true,
          isDefault: true,
          buildImage: 'registry.example.com/api',
          config: {
            repositoryId: 'repository-api',
            repositoryBindings: [
              { repositoryId: 'repository-api', checkoutPath: '.', defaultBranch: 'main' },
            ],
            dockerfilePath: 'Dockerfile',
            contextDir: '.',
            builderKind: 'docker',
          },
        },
        {
          id: 'source-other',
          name: 'Other build',
          type: 'external_pipeline',
          enabled: true,
          isDefault: false,
          config: {},
        },
      ],
    }
    testState.apiGet.mockImplementation(async (path: string) =>
      path === '/applications/app-1/detail'
        ? ({ data: { ...detail.data, application: latest } } as never)
        : previousGet(path),
    )
    try {
      await click(findButton(modal, '重新加载构建'))
      expect(modal.querySelector<HTMLInputElement>('#name')?.value).toBe('Server build')
      await click(findButton(modal, '保存'))
      expect(api.put).toHaveBeenLastCalledWith(
        '/applications/app-1',
        expect.objectContaining({
          name: 'Updated application',
          expectedVersion: 9,
          buildSources: expect.arrayContaining([
            expect.objectContaining({ id: 'source-other' }),
            expect.objectContaining({ id: 'source-api', name: 'Server build' }),
          ]),
        }),
      )
    } finally {
      testState.apiGet.mockImplementation(previousGet)
    }
  })

  it('connects a configured source repository and selects it for the current checkout', async () => {
    testState.repositoriesEmpty = true
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=application',
    )

    await act(async () => {
      findButton(container, '添加构建').dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    const buildModal = findModal('已接入仓库')
    expect(buildModal.textContent).toContain('已接入仓库')
    expect(buildModal.textContent).toContain('从代码源接入')

    await act(async () => {
      findButton(buildModal, '从代码源接入').dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    const repositoryModals = Array.from(document.querySelectorAll<HTMLElement>('.ant-modal'))
    const repositoryModal = repositoryModals[repositoryModals.length - 1]!
    expect(repositoryModal.textContent).toContain('从代码源接入仓库')
    expect(repositoryModal.textContent).toContain('代码源仓库')
    expect(testState.apiGet).toHaveBeenCalledWith('/integrations/gitlab/projects')

    await act(async () => {
      repositoryModal
        .querySelector<HTMLInputElement>('#gitlabProjectId')!
        .dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    const projectOption = Array.from(
      document.querySelectorAll<HTMLElement>('.ant-select-item-option'),
    ).find((item) => item.textContent?.includes('commerce/checkout-imported'))!
    await act(async () => {
      projectOption.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await act(async () => {
      const currentRepositoryModal = Array.from(
        document.querySelectorAll<HTMLElement>('.ant-modal'),
      ).find((modal) => modal.textContent?.includes('从代码源接入仓库'))!
      currentRepositoryModal
        .querySelector<HTMLButtonElement>('button[type="submit"]')!
        .dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(api.post).toHaveBeenCalledWith(
      '/repositories',
      expect.objectContaining({
        gitlabProjectId: 'gitlab-project-1',
        applicationIds: ['app-1'],
      }),
    )
    expect(buildModal.textContent).toContain('checkout-imported · commerce/checkout-imported')
    expect(buildModal.querySelector<HTMLInputElement>('input[id$="_defaultBranch"]')?.value).toBe(
      'main',
    )
  })

  it('explains when a saved code source has no authorized repositories', async () => {
    testState.repositoriesEmpty = true
    testState.gitProjectsEmpty = true
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=application',
    )

    await act(async () => {
      findButton(container, '添加构建').click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    const buildModal = findModal('已接入仓库')
    await act(async () => {
      findButton(buildModal, '从代码源接入').click()
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    const repositoryModals = Array.from(document.querySelectorAll<HTMLElement>('.ant-modal'))
    const repositoryModal = repositoryModals[repositoryModals.length - 1]!
    await act(async () => {
      repositoryModal
        .querySelector<HTMLInputElement>('#gitlabProjectId')!
        .dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('没有可用仓库，请确认代码源已启用并完成授权')
    expect(findButton(document.body, '检查代码源设置')).not.toBeNull()
  })

  it('ignores stale evidence params without loading evidence panels', async () => {
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=delivery&buildId=build-1',
    )

    expect(testState.apiGet).not.toHaveBeenCalledWith('/builds?applicationId=app-1')
    expect(container.querySelector('.soha-page > .ant-alert')).toBeNull()
    expect(container.textContent).not.toContain('交付证据定位')
    expect(document.querySelector('.ant-drawer-open')?.textContent).toContain('构建与更新')
    expect(document.body.textContent).toContain('Release DAG')
    expect(container.textContent).not.toContain('构建 / 发布记录')
  })

  it.each(['reconciling', 'converged'])(
    'shows applied Manifest resources as %s without inventing workload replicas',
    async (phase) => {
      testState.manifestTarget = true
      testState.manifestDeployments = [
        {
          id: 'deployment-1',
          packageId: 'manifest-1',
          bindingId: 'manifest-binding-1',
          generation: 2,
          spec: { desiredRevision: 2, desiredDigest: 'sha256:applied' },
          status: {
            phase,
            observedGeneration: 2,
            appliedRevision: 2,
            appliedDigest: 'sha256:applied',
            conditions: [
              {
                type: 'Healthy',
                status: phase === 'converged' ? 'true' : 'false',
                observedGeneration: 2,
              },
            ],
            inventory: [
              {
                apiVersion: 'v1',
                kind: 'ConfigMap',
                namespace: 'checkout-test',
                name: 'api-settings',
                health: 'healthy',
              },
            ],
            lastExecutionTaskId: 'apply-1',
          },
        },
      ]
      const container = await renderWithProviders(
        <ApplicationDetailPage />,
        '/applications/app-1?serviceId=svc-api&applicationEnvironmentId=binding-test',
      )
      expect(container.textContent).toContain(
        phase === 'converged' ? '资源已就绪' : '已应用，等待就绪',
      )
      expect(container.textContent).toContain('ConfigMap / api-settings')
      expect(container.textContent).not.toContain('当前环境尚未部署此服务')
      expect(container.querySelector('[aria-label^="实例就绪"]')).toBeNull()
      expect(container.querySelector('a[href="/delivery/execution-tasks/apply-1"]')).not.toBeNull()
    },
  )

  it.each(['completed', 'failed'])(
    'deploys a Manifest without a build and gates confirmation on %s preflight',
    async (status) => {
      testState.manifestTarget = true
      testState.deliveryClusterConnectionMode = 'agent'
      testState.deliveryActionsAgentStatus = 'partial'
      testState.preflightStatus = status
      testState.detailWithoutWorkflow = true
      testState.detailWithoutImageTagDefaults = true
      testState.permissionSnapshot.permissionKeys = defaultPermissionKeys.filter(
        (key) => !['delivery.builds.trigger', 'delivery.workflows.trigger'].includes(key),
      )
      const container = await renderWithProviders(
        <ApplicationDetailPage />,
        '/applications/app-1?serviceId=svc-api&applicationEnvironmentId=binding-test',
      )
      await act(async () => {
        findButton(container, '部署配置').click()
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      const modal = findModal('部署配置')
      expect(modal.textContent).not.toContain('构建定义')
      expect(modal.textContent).not.toContain('镜像 Tag')
      await act(async () => {
        const input = modal.querySelector('#manifestRevision') as HTMLInputElement
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '2')
        input.dispatchEvent(new Event('input', { bubbles: true }))
      })
      await act(async () => {
        findButton(modal, '部署配置').click()
        await new Promise((resolve) => setTimeout(resolve, 0))
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(api.post).toHaveBeenCalledWith('/delivery/plans', {
        applicationId: 'app-1',
        applicationEnvironmentId: 'binding-test',
        action: 'deploy',
        targetId: 'target-1',
        manifestRevision: 2,
        source: 'manual',
      })
      expect(document.body.textContent).toContain('sha256:fixed')
      expect(document.body.textContent).toContain('commit-123')
      await vi.waitFor(async () => {
        await act(async () => {
          await new Promise((resolve) => setTimeout(resolve, 0))
        })
        expect(findButton(document.body, '确认执行').disabled).toBe(status !== 'completed')
      })
      const confirm = findButton(document.body, '确认执行')
      if (status === 'completed') {
        await act(async () => {
          confirm.click()
          await new Promise((resolve) => setTimeout(resolve, 0))
        })
        expect(api.post).toHaveBeenCalledWith('/delivery/plans/plan-1/confirm', {})
      } else {
        expect(api.post).not.toHaveBeenCalledWith(
          '/delivery/plans/plan-1/confirm',
          expect.anything(),
        )
      }
    },
  )

  it('creates and confirms a DeliveryPlan when a workflow is run', async () => {
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=delivery',
    )
    await act(async () => {
      findButton(container, '运行').dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await act(async () => {
      findButton(document.body, '运行工作流').dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(api.post).toHaveBeenCalledWith(
      '/delivery/plans',
      expect.objectContaining({
        applicationId: 'app-1',
        action: 'build_deploy',
        applicationEnvironmentId: 'binding-test',
        targetId: 'target-1',
        buildSourceId: 'source-api',
        refType: 'branch',
        refName: 'main',
        repositoryRefs: [
          { repositoryId: 'repository-api', refType: 'branch', refName: 'main' },
          { repositoryId: 'repository-shared', refType: 'branch', refName: 'main' },
        ],
      }),
    )
    expect(api.post).not.toHaveBeenCalledWith(
      '/applications/app-1/delivery-actions',
      expect.anything(),
    )
    expect(document.body.textContent).toContain('交付计划确认')
    expect(document.body.textContent).toContain('确认前不会触发执行')

    await act(async () => {
      findButton(document.body, '确认执行').dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(api.post).toHaveBeenCalledWith('/delivery/plans/plan-1/confirm', {})
    expect(document.body.textContent).toContain('计划已确认并触发执行')
  })

  it.each(['tab=overview', 'settings=overview', 'tab=capabilities', 'settings=capabilities'])(
    'opens service settings for removed section URL %s',
    async (sectionQuery) => {
      const container = await renderWithProviders(
        <ApplicationDetailPage />,
        `/applications/app-1?${sectionQuery}&applicationEnvironmentId=binding-test&serviceId=svc-api`,
      )
      const location = container.querySelector('[data-testid="location"]') as HTMLOutputElement

      expect(document.body.textContent).not.toContain('能力就绪')
      expect(document.body.textContent).not.toContain('外部 AI 测试平台尚未接入')
      expect(document.querySelector('#application-settings-overview')).toBeNull()
      expect(document.querySelector('#application-settings-capabilities')).toBeNull()
      expect(
        document.querySelector('[aria-label="设置分区"] [aria-current="page"]')?.textContent,
      ).toBe('服务与构建')
      expect(document.querySelector('.soha-application-settings__content')?.textContent).toContain(
        '新建服务',
      )
      expect(location.textContent).toContain('applicationEnvironmentId=binding-test')
      expect(location.textContent).toContain('serviceId=svc-api')

      await openSection(container, '权限')
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(location.textContent).toContain('settings=permissions')

      await openSection(container, '工作流')
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(location.textContent).toContain('tab=delivery')
    },
  )

  it.each(['not-found', 'error'] as const)(
    'keeps the application workspace available for %s runtime failures',
    async (runtimeStatus) => {
      testState.runtimeStatus = runtimeStatus
      const container = await renderWithProviders(<ApplicationDetailPage />)

      expect(container.querySelector('.soha-service-console')).not.toBeNull()
      expect(container.textContent).toContain('状态读取失败')
      expect(container.textContent).toContain('Checkout API')
      expect(container.querySelector('.soha-service-pods-panel')?.textContent).toContain('重试')
      expect(container.textContent).not.toContain('应用不存在')
      expect(container.textContent).not.toContain('应用加载失败')
    },
  )

  it('disables target delivery actions for agent clusters without delivery runner parity', async () => {
    testState.deliveryClusterConnectionMode = 'agent'
    testState.deliveryActionsAgentStatus = 'partial'

    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=delivery',
    )

    await act(async () => {
      findButton(container, '运行').dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    const runButton = findButton(document.body, '运行工作流')

    expect(document.body.textContent).toContain('当前集群连接模式仅部分支持该能力')
    expect(runButton.disabled).toBe(true)

    await act(async () => {
      runButton.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(api.post).not.toHaveBeenCalledWith('/delivery/plans', expect.anything())
    expect(api.post).not.toHaveBeenCalledWith(
      '/applications/app-1/delivery-actions',
      expect.anything(),
    )
  })

  it('disables running when the workflow template is missing', async () => {
    testState.detailWithoutWorkflow = true
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=delivery',
    )

    expect(findButton(container, '运行').disabled).toBe(true)
  })

  it('shows scoped build definitions without workflow or execution permissions', async () => {
    testState.permissionSnapshot.permissionKeys = ['delivery.applications.view']
    const container = await renderWithProviders(<WorkflowCatalog />)
    const catalog = container.querySelector('.soha-workflow-catalog')!

    expect(catalog.textContent).toContain('API Dockerfile')
    expect(catalog.textContent).not.toContain('Release DAG')
    expect(catalog.querySelectorAll('[role="listitem"]')).toHaveLength(1)
    expect(catalog.querySelector('.ant-btn-primary')).toBeNull()
    expect(catalog.querySelector('[role="listitem"]')?.textContent).toContain('仅构建')
    await act(async () => {
      catalog.querySelector<HTMLButtonElement>('[aria-label="配置"]')!.click()
    })
    expect(container.querySelector('[data-testid="location"]')?.textContent).toContain(
      'buildSourceId=source-api',
    )
    expect(api.post).not.toHaveBeenCalled()
  })

  it('unifies workflow and build entries and opens the shared build form without executing', async () => {
    const container = await renderWithProviders(<WorkflowCatalog />)
    const catalog = container.querySelector('.soha-workflow-catalog')!
    const buildCard = Array.from(catalog.querySelectorAll('[role="listitem"]')).find((item) =>
      item.textContent?.includes('API Dockerfile'),
    )!
    expect(catalog.querySelector('[role="tablist"]')).toBeNull()
    expect(catalog.querySelectorAll('[role="listitem"]')).toHaveLength(2)
    expect(catalog.textContent).toContain('Release DAG')
    for (const item of catalog.querySelectorAll('[role="listitem"]')) {
      expect(item.querySelector('.ant-btn-primary')?.textContent).toContain('运行')
      expect(
        item.querySelector('.soha-workflow-catalog__card-actions [aria-label="配置"]'),
      ).not.toBeNull()
      expect(
        item.querySelector('.soha-workflow-catalog__card-actions [aria-label="查看记录"]'),
      ).not.toBeNull()
      expect(item.querySelector('.soha-execution-trend__heading a')).toBeNull()
    }
    await act(async () => {
      buildCard.querySelector<HTMLButtonElement>('.ant-btn-primary')!.click()
    })
    expect(container.querySelector('[data-testid="location"]')?.textContent).toContain(
      'buildSourceId=source-api&launch=build',
    )
    await act(async () => {
      buildCard.querySelector<HTMLButtonElement>('[aria-label="查看记录"]')!.click()
    })
    expect(container.querySelector('[data-testid="location"]')?.textContent).toBe(
      '/execution-history?applicationId=app-1&buildSourceId=source-api',
    )
    expect(api.post).not.toHaveBeenCalled()
  })

  it('prefills build launch context and requires a separate plan confirmation', async () => {
    testState.detailWithoutWorkflow = true
    testState.permissionSnapshot.permissionKeys = defaultPermissionKeys.filter(
      (permission) => !permission.startsWith('delivery.workflows.'),
    )
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=delivery&applicationEnvironmentId=binding-test&buildSourceId=source-api&launch=build',
    )
    expect(document.body.textContent).toContain('运行构建')
    expect(document.querySelector('.ant-drawer-open')).toBeNull()
    expect(container.querySelector('[data-testid="location"]')?.textContent).not.toContain(
      'launch=',
    )
    expect(api.post).not.toHaveBeenCalled()
    await act(async () => {
      findButton(document.body, '运行构建').click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(api.post).toHaveBeenCalledWith(
      '/delivery/plans',
      expect.objectContaining({
        action: 'build',
        applicationId: 'app-1',
        applicationEnvironmentId: 'binding-test',
        buildSourceId: 'source-api',
      }),
    )
    expect(api.post).not.toHaveBeenCalledWith('/delivery/plans/plan-1/confirm', expect.anything())
    expect(document.body.textContent).toContain('交付计划确认')
    expect(document.querySelector('.ant-drawer-open')).toBeNull()
  })

  it.each([false, true])(
    'builds without an environment or workload (runtime unavailable: %s)',
    async (runtimeUnavailable) => {
      const previousGet = testState.apiGet.getMockImplementation()!
      testState.apiGet.mockImplementation(async (path: string) => {
        if (path === '/applications/app-1/runtime' && runtimeUnavailable)
          throw new Error('Runtime unavailable')
        const result = await previousGet(path)
        if (path === '/applications/app-1/detail' && result.data && 'bindings' in result.data)
          result.data.bindings = []
        if (path === '/applications/app-1/runtime' && result.data && 'environments' in result.data)
          result.data.environments = []
        return result
      })
      try {
        await renderWithProviders(
          <ApplicationDetailPage />,
          '/applications/app-1?tab=delivery&buildSourceId=source-api&launch=build',
        )
        const button = findButton(document.body, '运行构建')
        expect(button.disabled).toBe(false)
        expect(api.post).not.toHaveBeenCalled()
        await act(async () => {
          button.click()
          await new Promise((resolve) => setTimeout(resolve, 30))
        })
        expect(api.post).toHaveBeenCalledWith(
          '/builds/trigger',
          expect.objectContaining({
            applicationId: 'app-1',
            buildSourceId: 'source-api',
            refName: expect.any(String),
          }),
        )
        expect(api.post).not.toHaveBeenCalledWith('/delivery/plans', expect.anything())
        const payload = vi
          .mocked(api.post)
          .mock.calls.find(([path]) => path === '/builds/trigger')?.[1]
        expect(payload).not.toHaveProperty('applicationEnvironmentId')
      } finally {
        testState.apiGet.mockImplementation(previousGet)
      }
    },
  )

  it('disables running for readonly users', async () => {
    testState.permissionSnapshot.permissionKeys = [...readonlyPermissionKeys]
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=delivery',
    )

    const runButton = findButton(container, '运行')
    expect(runButton.disabled).toBe(true)

    await act(async () => {
      runButton.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(api.post).not.toHaveBeenCalledWith('/delivery/plans', expect.anything())
    expect(api.post).not.toHaveBeenCalledWith(
      '/applications/app-1/delivery-actions',
      expect.anything(),
    )
  })

  it('hides workflow and build editing controls for readonly users', async () => {
    testState.permissionSnapshot.permissionKeys = [...readonlyPermissionKeys]
    const workflowContainer = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=delivery',
    )
    expect(workflowContainer.querySelector('.soha-service-console')).not.toBeNull()
    const workflowPanel = document.querySelector(
      '.ant-drawer-open .soha-application-workflow-list',
    )!
    const workflowButtons = Array.from(workflowPanel.querySelectorAll('button')).map((button) =>
      button.textContent?.trim(),
    )

    expect(workflowButtons).not.toContain('创建工作流')
    expect(workflowButtons).not.toContain('设计')

    const serviceContainer = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=services&serviceId=svc-api',
    )
    const serviceDrawer = serviceContainer.querySelector('[aria-label="服务工作台"]')!

    expect(serviceDrawer.textContent).not.toContain('添加构建')
    expect(serviceDrawer.textContent).not.toContain('编辑构建')
  })

  it('disables workflow execution when image tag defaults are missing', async () => {
    testState.detailWithoutImageTagDefaults = true
    const container = await renderWithProviders(
      <ApplicationDetailPage />,
      '/applications/app-1?tab=delivery',
    )

    await act(async () => {
      findButton(container, '运行').dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(findButton(document.body, '运行工作流').disabled).toBe(true)
  })
})
