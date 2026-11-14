/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act, StrictMode } from 'react'
import { App as AntApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  BuildTemplatesPage,
  buildBuildTemplatePayload,
  buildBuildTemplatePayloadFromDesigner,
  type BuildTemplateFormValues,
} from './build-templates/page'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { ApplicationsPage } from './applications/list-page'
import { ExecutionTasksPage } from './execution-tasks/list-page'
import { ApiError } from '@/services/api-error'
import { ReleaseBundlesPage } from './release-bundles/list-page'
import { ReleasesPage } from './releases/list-page'
import { runtimeEvidencePath } from './template-usage-runtime-links'
import { ApplicationEnvironmentsPage } from './environments/list-page'
import { EnvironmentCatalogPage } from './environments/catalog-page'
import { ExecutionHistoryPage } from './release-board/page'
import { WorkflowCatalog } from './release-board/workflow-catalog'
import { WorkflowCenterRuns } from './release-board/center-runs'
import { WorkflowTemplatesPage } from './workflow-templates/page'
import { workflowTemplateDocument } from './documents/model'
import { parse, stringify } from 'yaml'
import { RegistriesPage } from './registries/page'
import { DeploymentTemplatesPage } from './deployment-templates/page'
import { DeliveryAnalysisPage } from './workbench/analysis-page'
import { ApplicationCreateRedirect } from './applications/legacy-redirect'
import { DeliveryTestingPage } from './workbench/testing-page'
const workflowDefinition = {
  schemaVersion: 2,
  mode: 'release_dag',
  nodes: [
    {
      id: 'build',
      type: 'build',
      name: '构建镜像',
      position: { x: 80, y: 120 },
      timeoutSeconds: 300,
      continueOnFailure: false,
      config: {},
    },
    {
      id: 'deploy',
      type: 'deploy_update_image',
      name: '更新镜像',
      position: { x: 280, y: 120 },
      timeoutSeconds: 300,
      continueOnFailure: false,
      config: {},
    },
    {
      id: 'verify',
      type: 'check_http',
      name: 'HTTP 验证',
      position: { x: 480, y: 120 },
      timeoutSeconds: 300,
      continueOnFailure: false,
      config: { url: 'https://example.com/healthz' },
    },
  ],
  edges: [
    { id: 'edge-build-deploy', source: 'build', target: 'deploy', condition: 'success' },
    { id: 'edge-deploy-verify', source: 'deploy', target: 'verify', condition: 'success' },
  ],
}

const defaultPermissionKeys = [
  'delivery.applications.view',
  'delivery.application.create',
  'delivery.application.update',
  'delivery.application-services.create',
  'delivery.application-environments.view',
  'delivery.application-environments.create',
  'delivery.application-environments.update',
  'delivery.application-environments.delete',
  'delivery.build-templates.create',
  'delivery.build-templates.update',
  'delivery.build-templates.delete',
  'delivery.workflow-templates.view',
  'delivery.workflow-templates.create',
  'delivery.workflow-templates.update',
  'delivery.workflow-templates.delete',
  'delivery.registries.create',
  'delivery.registries.update',
  'delivery.registries.delete',
  'delivery.release-board.view',
  'delivery.release-bundles.view',
  'delivery.execution-tasks.view',
  'delivery.workflows.view',
  'platform.clusters.view',
  'platform.namespaces.view',
  'platform.deployment.view',
  'platform.workloads.stateful-sets.view',
  'platform.workloads.daemon-sets.view',
  'platform.network.services.view',
  'platform.network.ingresses.view',
  'platform.configuration.horizontal-pod-autoscalers.view',
]

const readonlyPermissionKeys = [
  'delivery.applications.view',
  'delivery.application-environments.view',
  'delivery.build-templates.view',
  'delivery.workflow-templates.view',
  'delivery.registries.view',
  'delivery.release-board.view',
  'delivery.release-bundles.view',
  'delivery.execution-tasks.view',
  'delivery.workflows.view',
]

const testState = vi.hoisted(() => ({
  permissionSnapshot: {
    permissionKeys: [
      'delivery.applications.view',
      'delivery.application.create',
      'delivery.application.update',
      'delivery.application-services.create',
      'delivery.application-environments.view',
      'delivery.application-environments.create',
      'delivery.application-environments.update',
      'delivery.application-environments.delete',
      'delivery.build-templates.create',
      'delivery.build-templates.update',
      'delivery.build-templates.delete',
      'delivery.workflow-templates.create',
      'delivery.workflow-templates.update',
      'delivery.workflow-templates.delete',
      'delivery.registries.create',
      'delivery.registries.update',
      'delivery.registries.delete',
      'delivery.release-board.view',
      'delivery.release-bundles.view',
      'delivery.execution-tasks.view',
      'delivery.workflows.view',
      'platform.clusters.view',
      'platform.namespaces.view',
      'platform.deployment.view',
      'platform.workloads.stateful-sets.view',
      'platform.workloads.daemon-sets.view',
      'platform.network.services.view',
      'platform.network.ingresses.view',
      'platform.configuration.horizontal-pod-autoscalers.view',
    ],
    visibleMenuIds: [],
    visibleMenus: [],
  },
  forceHighBuildTemplateUsage: false,
  gitBuildTemplate: false,
  extraBuildTemplate: false,
  workflowRecipe: undefined as Record<string, unknown> | undefined,
  applicationsListError: false,
  catalogServiceFixtures: false,
  releaseBoardError: false,
  workflowsEmpty: false,
  workflowsError: false,
  gatewayManifestMode: 'approval' as 'approval' | 'restricted' | 'unavailable',
  apiGet: vi.fn(async (path: string) => {
    if (path === '/deployment-templates')
      return {
        data: Array.from({ length: 18 }, (_, index) => ({
          id: `deployment-${index + 1}`,
          key: `deployment-${index + 1}`,
          name: `部署模板 ${index + 1}`,
          source: { renderer: 'raw_yaml', files: [] },
          enabled: true,
          revision: 1,
          publicationState: 'draft',
        })),
      }

    const workflowFixtures = [
      {
        id: 'workflow-running',
        applicationId: 'app-1',
        workflowName: 'build-release-main',
        clusterId: 'cluster-a',
        namespace: 'test',
        deploymentName: 'erp-front',
        status: 'running',
        steps: [],
        nodeRuns: [
          { nodeId: 'checkout', name: '检出代码', type: 'checkout', status: 'completed' },
          { nodeId: 'build', name: '构建镜像', type: 'build_image', status: 'running' },
          { nodeId: 'release', name: '发布应用', type: 'deploy', status: 'pending' },
        ],
        metadata: {
          applicationName: 'ERP Front Main',
          bindingId: 'binding-1',
        },
        createdAt: '2026-05-08T11:40:00Z',
        updatedAt: '2026-05-08T12:00:00Z',
      },
      {
        id: 'workflow-1',
        applicationId: 'app-1',
        workflowName: 'deploy-prod',
        clusterId: 'cluster-a',
        namespace: 'prod',
        deploymentName: 'erp-front',
        status: 'waiting_approval',
        steps: [],
        nodeRuns: [
          {
            nodeId: 'approve',
            name: '人工审批',
            type: 'manual_approval',
            status: 'waiting_approval',
            summary: 'Waiting for production approver',
            startedAt: '2026-05-08T11:10:00Z',
          },
        ],
        metadata: {
          applicationName: 'ERP Front Main',
          bindingId: 'binding-prod',
          aiGatewayApprovalRequestId: 'approval-1',
          aiGatewayToolName: 'delivery.actions.trigger',
          aiGatewayApprovalPolicyRef: 'policy-prod',
        },
        createdAt: '2026-05-08T11:00:00Z',
        updatedAt: '2026-05-08T11:30:00Z',
      },
      {
        id: 'workflow-completed',
        applicationId: 'app-2',
        workflowName: 'mall-api-release',
        clusterId: 'cluster-b',
        namespace: 'staging',
        deploymentName: 'mall-api',
        status: 'completed',
        steps: [],
        nodeRuns: [
          { nodeId: 'build', name: '构建镜像', type: 'build_image', status: 'completed' },
          {
            nodeId: 'release',
            name: '发布应用',
            type: 'deploy',
            status: 'completed',
            releaseBundleId: 'bundle-mall',
          },
        ],
        metadata: { applicationName: 'Mall API', bindingId: 'binding-2' },
        createdAt: '2026-05-08T10:00:00Z',
        updatedAt: '2026-05-08T10:30:00Z',
      },
      {
        id: 'workflow-failed',
        applicationId: 'app-3',
        workflowName: 'billing-worker-release',
        clusterId: 'cluster-c',
        namespace: 'prod',
        deploymentName: 'billing-worker',
        status: 'failed',
        steps: [],
        nodeRuns: [
          { nodeId: 'build', name: '构建镜像', type: 'build_image', status: 'completed' },
          {
            nodeId: 'release',
            name: '发布应用',
            type: 'deploy',
            status: 'failed',
            executionTaskId: 'task-failed',
          },
        ],
        metadata: { applicationName: 'Billing Worker', bindingId: 'binding-3' },
        createdAt: '2026-05-08T09:00:00Z',
        updatedAt: '2026-05-08T09:20:00Z',
      },
    ]
    if (/^\/delivery\/documents\/[^/]+\/[^/]+\/source(?:\?|$)/.test(path)) {
      return {
        data:
          testState.gitBuildTemplate && path.includes('/BuildTemplate/tpl-1/')
            ? {
                association: {
                  sourceId: 'git-source',
                  kind: 'BuildTemplate',
                  objectId: 'tpl-1',
                  key: 'docker-node',
                  path: 'node.soha.yaml',
                  lastImportedRevision: 7,
                  resolvedCommit: 'a'.repeat(40),
                  sourceDigest: 'source',
                  normalizedSpecDigest: 'spec',
                  syncRunId: 'run-1',
                  removed: false,
                },
              }
            : {},
      }
    }
    if (path.startsWith('/ai-gateway/capabilities')) {
      if (testState.gatewayManifestMode === 'unavailable') {
        throw new Error('AI Gateway is unavailable')
      }
      const deliveryTools = [
        'delivery.applications.list',
        'delivery.applications.detail',
        'delivery.applications.create',
        'delivery.application_environments.list',
        'delivery.application_services.list',
        'delivery.build_sources.list',
        'delivery.release_targets.list',
        'delivery.release_bundles.list',
        'delivery.execution_tasks.list',
        'delivery.execution_logs.list',
        'delivery.onboarding.analyze_repo',
        'delivery.standards.dockerfile.generate',
        'delivery.standards.dockerfile.validate',
        'delivery.standards.helm.generate',
        'delivery.standards.k8s.validate',
        'delivery.spec.render',
        'delivery.application.bootstrap',
        'delivery.release.plan',
        'delivery.release_context.diff',
        'delivery.rollback.context',
        'delivery.actions.trigger',
        'diagnosis.release_failure.analyze',
        'k8s.pods.logs',
        'k8s.deployments.events',
      ]
      const tools =
        testState.gatewayManifestMode === 'restricted'
          ? deliveryTools
              .filter((name) =>
                ['delivery.applications.list', 'delivery.release_bundles.list'].includes(name),
              )
              .map((name) => ({
                name,
                title: name,
                domain: name.startsWith('delivery.') ? 'delivery' : 'diagnosis',
                action: 'read',
                riskLevel: 'read',
                permissionKeys: ['ai.gateway.invoke'],
                requiredScopes: ['application'],
                requiresApproval: false,
              }))
          : deliveryTools.map((name) => ({
              name,
              title: name,
              domain: name.startsWith('delivery.')
                ? 'delivery'
                : name.startsWith('k8s.')
                  ? 'kubernetes'
                  : 'diagnosis',
              action: name.includes('trigger')
                ? 'execute'
                : name.includes('generate') || name.includes('plan')
                  ? 'analyze'
                  : 'read',
              riskLevel:
                name === 'delivery.actions.trigger' || name === 'delivery.release.plan'
                  ? 'execute'
                  : 'read',
              permissionKeys: ['ai.gateway.invoke'],
              requiredScopes: name.startsWith('k8s.')
                ? ['cluster', 'namespace']
                : ['application', 'environment'],
              requiresApproval:
                name === 'delivery.actions.trigger' || name === 'delivery.release.plan',
            }))
      return {
        data: {
          name: 'soha AI Gateway',
          version: 'test',
          generatedAt: '2026-05-08T12:00:00Z',
          permissionKeys: ['ai.gateway.view', 'ai.gateway.invoke'],
          tools,
          skills: [
            {
              id: 'delivery-developer',
              name: 'Delivery Developer',
              category: 'delivery',
              capabilityRefs: tools.map((tool) => tool.name),
              requiredScopes: ['application', 'environment'],
            },
            {
              id: 'delivery-tester',
              name: 'Delivery Tester',
              category: 'delivery',
              capabilityRefs: tools.map((tool) => tool.name),
              requiredScopes: ['application', 'environment'],
            },
          ],
          summary: {
            toolCount: tools.length,
            resourceCount: 0,
            promptCount: 0,
            skillCount: 2,
            deniedCount: testState.gatewayManifestMode === 'restricted' ? 8 : 0,
          },
        },
      }
    }
    if (testState.catalogServiceFixtures && path === '/applications/app-1/services')
      return {
        data: [
          {
            id: 'catalog-service-1',
            applicationId: 'app-1',
            name: 'ERP web',
            key: 'erp-web',
            serviceKind: 'helm_release',
            enabled: true,
          },
        ],
      }
    if (testState.catalogServiceFixtures && path === '/applications/app-2/services')
      return { data: [] }
    if (path.startsWith('/delivery/workflow-catalog?'))
      return { data: { items: [], total: 0, applications: [], environments: [] } }
    if (path === '/applications') {
      if (testState.applicationsListError) {
        throw new Error('applications unavailable')
      }
      return {
        data: [
          {
            id: 'app-1',
            name: 'ERP Front Main',
            key: 'erp-front-main',
            description: '企业订单与库存管理门户',
            version: 3,
            group: 'erp-front, frontend',
            language: 'node',
            repositoryPath: 'erp/front/main',
            defaultBranch: 'main',
            enabled: true,
            buildSources: [
              {
                id: 'source-1',
                name: 'Repo Dockerfile',
                type: 'repo_dockerfile',
                enabled: true,
                isDefault: true,
                buildImage: '',
                defaultTag: '',
                config: { contextDir: '.', dockerfilePath: 'Dockerfile', builderKind: 'docker' },
              },
            ],
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-08T12:00:00Z',
          },
          {
            id: 'app-2',
            name: 'Mall API',
            key: 'mall-api',
            group: 'mall',
            language: 'go',
            repositoryPath: 'mall/api',
            defaultBranch: 'main',
            enabled: false,
            buildSources: [
              {
                id: 'source-2',
                name: 'Platform Template',
                type: 'platform_build_template',
                enabled: true,
                isDefault: true,
                buildImage: '',
                defaultTag: '',
                config: { buildTemplateId: 'tpl-1', contextDir: '.' },
              },
            ],
            createdAt: '2026-05-02T00:00:00Z',
            updatedAt: '2026-05-08T12:00:00Z',
          },
        ],
      }
    }
    if (path === '/application-environments') {
      return {
        data: [
          {
            id: 'binding-1',
            applicationId: 'app-1',
            environmentId: 'env-test',
            environmentKey: 'test',
            workflowTemplateId: 'wf-template-1',
            workflowTemplate: {
              id: 'wf-template-1',
              key: 'release-dag',
              name: 'Release DAG',
              category: 'release',
              definition: workflowDefinition,
              enabled: true,
              createdAt: '2026-05-01T00:00:00Z',
              updatedAt: '2026-05-08T12:00:00Z',
            },
            buildPolicy: { sourceId: 'source-1', refType: 'branch', imageTagMode: 'input' },
            releasePolicy: {
              actionKind: 'deploy',
              requiresApproval: false,
              verificationMode: 'workflow',
            },
            targets: [
              {
                id: 'target-1',
                applicationEnvironmentId: 'binding-1',
                clusterId: 'cluster-a',
                namespace: 'erp-test',
                targetKind: 'k8s_workload',
                executorKind: 'k8s_job_runner',
                workloadKind: 'Deployment',
                workloadName: 'erp-front',
                containerName: 'web',
                enabled: true,
              },
            ],
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-08T12:00:00Z',
          },
        ],
      }
    }
    if (path === '/delivery/environments') {
      return {
        data: [
          {
            id: 'env-test',
            key: 'test',
            name: '测试环境',
            tier: 'non-production',
            stageLevel: 20,
            sortOrder: 20,
            isProduction: false,
            requiresApproval: false,
            enabled: true,
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-08T12:00:00Z',
          },
        ],
      }
    }
    if (path.startsWith('/delivery/documents/WorkflowTemplate/wf-template-1/export')) {
      const format = new URLSearchParams(path.split('?')[1]).get('format')
      const document = workflowTemplateDocument({
        key: 'release-dag',
        name: 'Release DAG',
        definition: testState.workflowRecipe ?? workflowDefinition,
        enabled: true,
      })
      return {
        data: {
          format,
          document,
          content: format === 'json' ? JSON.stringify(document, null, 2) : stringify(document),
          normalizedSpecDigest: 'sha256:document',
        },
      }
    }
    if (path === '/workflow-templates') {
      return {
        data: [
          {
            id: 'wf-template-1',
            revision: 4,
            publishedVersion: 1,
            publicationState: 'published',
            key: 'release-dag',
            name: 'Release DAG',
            description: 'Standard build deploy verify flow',
            category: 'release',
            definition: testState.workflowRecipe ?? workflowDefinition,
            enabled: true,
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-08T12:00:00Z',
          },
        ],
      }
    }
    if (path === '/workflow-templates/wf-template-1/versions/1')
      return {
        data: {
          id: 'wf-template-1',
          key: 'release-dag',
          name: 'Release DAG',
          definition: testState.workflowRecipe ?? workflowDefinition,
          publishedVersion: 1,
          publicationState: 'published',
          enabled: true,
        },
      }
    if (path === '/workflow-templates/wf-template-1/usage') {
      return {
        data: {
          templateKind: 'workflow',
          templateId: 'wf-template-1',
          usageCount: 1,
          applicationCount: 1,
          environmentCount: 1,
          productionEnvironmentCount: 0,
          approvalBindingCount: 0,
          targetCount: 1,
          riskLevel: 'low',
          riskReasons: ['1 release targets'],
          recommendedAction: 'save_with_standard_review',
          applications: [{ id: 'app-1', name: 'ERP Front Main', key: 'erp-front-main' }],
          bindings: [
            {
              id: 'binding-1',
              applicationId: 'app-1',
              environmentId: 'env-test',
              environmentKey: 'test',
              requiresApproval: false,
              targetCount: 1,
              riskLevel: 'low',
              application: { id: 'app-1', name: 'ERP Front Main', key: 'erp-front-main' },
              environment: {
                id: 'env-test',
                key: 'test',
                name: '测试环境',
                isProduction: false,
                requiresApproval: false,
              },
            },
          ],
          lastExecutionSummary: {
            source: 'workflow_template_runtime',
            stateCounts: { succeeded: 1, failed: 1, running: 1, pending: 0 },
            statusCounts: { completed: 1, failed: 1, running: 1 },
            latest: {
              kind: 'workflow',
              id: 'workflow-usage-1',
              applicationId: 'app-1',
              applicationEnvironmentId: 'binding-1',
              workflowName: 'release-dag',
              status: 'running',
              observedAt: '2026-05-08T11:30:00Z',
            },
            items: [
              {
                kind: 'workflow',
                id: 'workflow-usage-1',
                applicationId: 'app-1',
                applicationEnvironmentId: 'binding-1',
                workflowName: 'release-dag',
                status: 'running',
                observedAt: '2026-05-08T11:30:00Z',
              },
              {
                kind: 'execution_task',
                id: 'task-usage-1',
                applicationId: 'app-1',
                applicationEnvironmentId: 'binding-1',
                releaseBundleId: 'bundle-1',
                taskKind: 'build_deploy',
                status: 'failed',
                observedAt: '2026-05-08T11:20:00Z',
              },
            ],
          },
        },
      }
    }
    if (path === '/clusters') {
      return { data: [] }
    }
    if (path === '/delivery/release-board') {
      if (testState.releaseBoardError) {
        throw new Error('release board unavailable')
      }
      return {
        data: [
          {
            applicationEnvironmentId: 'binding-1',
            applicationId: 'app-1',
            applicationName: 'ERP Front Main',
            environmentId: 'env-test',
            environmentName: '测试环境',
            requiresApproval: false,
            buildSource: {
              id: 'source-1',
              name: 'Repo Dockerfile',
              type: 'repo_dockerfile',
              enabled: true,
              isDefault: true,
            },
            targets: [
              {
                clusterId: 'cluster-a',
                namespace: 'erp-test',
                workloadName: 'erp-front',
                workloadKind: 'deployment',
              },
            ],
            latestBuild: {
              id: 'build-1',
              applicationId: 'app-1',
              status: 'completed',
              sourceSystem: 'application',
              createdAt: '2026-05-08T10:00:00Z',
              updatedAt: '2026-05-08T10:30:00Z',
            },
            latestBundle: {
              id: 'bundle-1',
              applicationId: 'app-1',
              applicationEnvironmentId: 'binding-1',
              version: '1.2.3',
              sourceType: 'build',
              status: 'completed',
              artifactRef: 'registry.local/erp-front:1.2.3',
              createdAt: '2026-05-08T10:30:00Z',
              updatedAt: '2026-05-08T10:40:00Z',
            },
            latestExecutionTask: {
              id: 'task-1',
              releaseBundleId: 'bundle-1',
              applicationId: 'app-1',
              applicationEnvironmentId: 'binding-1',
              taskKind: 'build_deploy',
              providerKind: 'ci_agent_runner',
              targetKind: 'k8s_workload',
              status: 'running',
              maxRetries: 1,
              attemptCount: 1,
              timeoutSeconds: 600,
              artifacts: [
                { kind: 'image', name: 'erp-front', ref: 'registry.local/erp-front:1.2.3' },
              ],
              createdAt: '2026-05-08T10:40:00Z',
              updatedAt: '2026-05-08T11:20:00Z',
            },
            latestWorkflow: {
              id: 'wf-1',
              applicationId: 'app-1',
              workflowName: 'deploy',
              status: 'running',
              steps: [],
              nodeRuns: [
                { nodeId: 'smoke', name: 'Smoke', type: 'smoke_test', status: 'completed' },
              ],
              createdAt: '2026-05-08T11:00:00Z',
              updatedAt: '2026-05-08T11:30:00Z',
            },
            latestRelease: {
              id: 'release-1',
              applicationId: 'app-1',
              clusterId: 'cluster-a',
              namespace: 'erp-test',
              deploymentName: 'erp-front',
              status: 'running',
              createdAt: '2026-05-08T11:15:00Z',
              updatedAt: '2026-05-08T11:25:00Z',
            },
          },
          {
            applicationEnvironmentId: 'binding-2',
            applicationId: 'app-2',
            applicationName: 'Mall API',
            environmentId: 'env-staging',
            environmentName: '预发环境',
            requiresApproval: false,
            targets: [
              {
                clusterId: 'cluster-b',
                namespace: 'mall-staging',
                workloadName: 'mall-api',
                workloadKind: 'deployment',
              },
            ],
            latestWorkflow: {
              id: 'wf-2',
              applicationId: 'app-2',
              workflowName: 'deploy',
              status: 'failed',
              steps: [],
              createdAt: '2026-05-08T11:00:00Z',
              updatedAt: '2026-05-08T11:30:00Z',
            },
          },
        ],
      }
    }
    if (path === '/build-templates') {
      return {
        data: [
          {
            id: 'tpl-1',
            revision: 7,
            publishedVersion: 1,
            publicationState: 'published',
            key: 'docker-node',
            name: 'Node Docker',
            description: 'Node standard docker build',
            builderKind: 'docker',
            dockerfileTemplate: 'FROM node:22',
            buildCommands: ['npm ci', 'npm run build'],
            variableSchema: { imageTag: { type: 'string', title: '镜像 Tag', required: true } },
            defaultVariables: { imageTag: 'latest' },
            enabled: true,
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-01T00:00:00Z',
          },
          ...(testState.extraBuildTemplate
            ? [{ id: 'tpl-2', revision: 1, key: 'git-build', name: 'Git Build', enabled: true }]
            : []),
        ],
      }
    }
    if (path === '/build-templates/tpl-2/usage') return { data: {} }
    if (path === '/build-templates/tpl-1/usage') {
      return {
        data: {
          templateKind: 'build',
          templateId: 'tpl-1',
          usageCount: 1,
          applicationCount: 1,
          environmentCount: 1,
          productionEnvironmentCount: testState.forceHighBuildTemplateUsage ? 1 : 0,
          approvalBindingCount: testState.forceHighBuildTemplateUsage ? 1 : 0,
          targetCount: 1,
          riskLevel: testState.forceHighBuildTemplateUsage ? 'high' : 'low',
          riskReasons: testState.forceHighBuildTemplateUsage
            ? ['1 production environment bindings']
            : ['1 release targets'],
          recommendedAction: testState.forceHighBuildTemplateUsage
            ? 'copy_template_before_editing'
            : 'save_with_standard_review',
          applications: [{ id: 'app-2', name: 'Mall API', key: 'mall-api' }],
          buildSources: [
            {
              applicationId: 'app-2',
              buildSourceId: 'source-2',
              buildSourceName: 'Platform Template',
              application: { id: 'app-2', name: 'Mall API', key: 'mall-api' },
              bindingCount: 1,
              riskLevel: 'low',
            },
          ],
          lastExecutionSummary: {
            source: 'build_template_runtime',
            stateCounts: { succeeded: 1, failed: 0, running: 1, pending: 0 },
            statusCounts: { completed: 1, running: 1 },
            latest: {
              kind: 'build',
              id: 'build-usage-1',
              applicationId: 'app-2',
              buildSourceId: 'source-2',
              status: 'completed',
              sourceSystem: 'manual',
              observedAt: '2026-05-08T10:00:00Z',
            },
            items: [
              {
                kind: 'build',
                id: 'build-usage-1',
                applicationId: 'app-2',
                buildSourceId: 'source-2',
                status: 'completed',
                sourceSystem: 'manual',
                observedAt: '2026-05-08T10:00:00Z',
              },
              {
                kind: 'release_bundle',
                id: 'bundle-usage-1',
                applicationId: 'app-2',
                applicationEnvironmentId: 'binding-2',
                version: '2.0.0',
                status: 'building',
                observedAt: '2026-05-08T10:30:00Z',
              },
            ],
          },
        },
      }
    }
    if (path === '/delivery/release-bundles') {
      return {
        data: [
          {
            id: 'bundle-1',
            applicationId: 'app-1',
            applicationEnvironmentId: 'binding-1',
            version: '1.2.3',
            sourceType: 'build',
            status: 'completed',
            artifactRef: 'registry.local/erp-front:1.2.3',
            artifactDigest: 'sha256:123',
            createdAt: '2026-05-08T10:30:00Z',
            updatedAt: '2026-05-08T10:40:00Z',
          },
          {
            id: 'bundle-2',
            applicationId: 'app-2',
            applicationEnvironmentId: 'binding-2',
            version: '2.0.0-rc1',
            sourceType: 'workflow',
            status: 'failed',
            createdAt: '2026-05-08T10:30:00Z',
            updatedAt: '2026-05-08T10:40:00Z',
          },
        ],
      }
    }
    if (path === '/delivery/execution-tasks') {
      return {
        data: [
          {
            id: 'task-running',
            releaseBundleId: 'bundle-1',
            applicationId: 'app-1',
            applicationEnvironmentId: 'binding-1',
            taskKind: 'build_deploy',
            providerKind: 'ci_agent_runner',
            targetKind: 'k8s_workload',
            status: 'running',
            maxRetries: 1,
            attemptCount: 1,
            timeoutSeconds: 600,
            callbackToken: 'token-running',
            artifacts: [
              { kind: 'image', name: 'erp-front', ref: 'registry.local/erp-front:1.2.3' },
            ],
            lastHeartbeatAt: '2026-05-08T11:20:00Z',
            createdAt: '2026-05-08T10:40:00Z',
            updatedAt: '2026-05-08T11:20:00Z',
          },
          {
            id: 'task-failed',
            releaseBundleId: 'bundle-2',
            applicationId: 'app-2',
            applicationEnvironmentId: 'binding-2',
            taskKind: 'verify',
            providerKind: 'k8s_job_runner',
            targetKind: 'quality_gate',
            status: 'failed',
            maxRetries: 2,
            attemptCount: 1,
            timeoutSeconds: 300,
            artifacts: [],
            createdAt: '2026-05-08T10:40:00Z',
            updatedAt: '2026-05-08T11:20:00Z',
          },
        ],
      }
    }
    if (
      path === '/delivery/execution-tasks/task-running/logs' ||
      path === '/delivery/execution-tasks/task-failed/logs'
    ) {
      return { data: [] }
    }
    if (path.startsWith('/delivery/execution-history?')) {
      if (testState.workflowsError) throw new Error('history unavailable')
      if (testState.workflowsEmpty) return { data: { items: [] } }
      const query = new URL(path, 'http://localhost').searchParams
      const runs = testState.permissionSnapshot.permissionKeys.includes('delivery.workflows.view')
        ? workflowFixtures
        : []
      const entries = [
        ...runs.map((run) => ({
          id: run.id,
          kind: 'application',
          createdAt: run.createdAt,
          application: run,
        })),
        {
          id: 'build-standalone',
          kind: 'build',
          createdAt: '2026-05-08T08:00:00Z',
          build: {
            id: 'build-standalone',
            applicationId: 'app-1',
            sourceSystem: 'application',
            status: 'completed',
            createdAt: '2026-05-08T08:00:00Z',
            metadata: { buildSourceName: 'Standalone Docker', applicationName: 'ERP Front Main' },
          },
        },
      ]
      return {
        data: {
          items: entries.filter((entry) => {
            const keyword = query.get('search')?.toLowerCase()
            if (keyword && !JSON.stringify(entry).toLowerCase().includes(keyword)) return false
            const status = 'application' in entry ? entry.application?.status : entry.build?.status
            switch (query.get('status')) {
              case 'running':
                return status === 'running' || status === 'queued'
              case 'approval':
                return status === 'waiting_approval'
              case 'failed':
                return status === 'failed'
              default:
                return true
            }
          }),
          nextCursor: query.has('cursor') ? undefined : 'next-page',
        },
      }
    }
    if (path === '/workflows' || path === '/workflows?limit=200') {
      if (testState.workflowsError) {
        throw new Error('workflows unavailable')
      }
      if (testState.workflowsEmpty) {
        return { data: [] }
      }
      return {
        data: workflowFixtures,
      }
    }
    if (path === '/registries') {
      return {
        items: [
          {
            id: 'registry-1',
            name: 'Harbor Prod',
            registryType: 'harbor',
            endpoint: 'https://harbor.example.com',
            namespace: 'delivery',
            username: 'robot$delivery',
            insecure: false,
            metadata: {
              secretConfigured: true,
              secretStorage: 'encrypted',
            },
            createdAt: '2026-05-08T11:00:00Z',
            updatedAt: '2026-05-08T11:30:00Z',
          },
        ],
      }
    }
    if (path === '/delivery-workflows' || path === '/delivery-batches?limit=200')
      return { data: [] }
    if (path === '/builds' || path === '/builds?limit=200') {
      return {
        data: [
          {
            id: 'build-1',
            applicationId: 'app-1',
            sourceSystem: 'application',
            status: 'completed',
            createdAt: '2026-05-08T10:00:00Z',
            updatedAt: '2026-05-08T10:30:00Z',
          },
        ],
      }
    }
    if (path === '/releases') {
      return {
        data: [
          {
            id: 'release-1',
            applicationId: 'app-1',
            clusterId: 'cluster-a',
            namespace: 'erp-test',
            deploymentName: 'erp-front',
            status: 'completed',
            createdAt: '2026-05-08T10:40:00Z',
            updatedAt: '2026-05-08T10:50:00Z',
          },
        ],
      }
    }
    throw new Error(`Unhandled GET ${path}`)
  }),
  apiPut: vi.fn(async (_path: string, body?: unknown) => ({ data: body })),
  apiPost: vi.fn(async (path: string, body?: unknown) => {
    if (path === '/delivery/documents/preview')
      return { data: { valid: true, candidates: [], diagnostics: [] } }
    if (path === '/applications') {
      return {
        data: {
          id: 'app-created',
          ...(body as Record<string, unknown>),
          group: '',
          language: '',
          createdAt: '2026-05-08T12:00:00Z',
          updatedAt: '2026-05-08T12:00:00Z',
        },
      }
    }
    if (path === '/workflows/workflow-1/approve') {
      return { data: body }
    }
    if (path === '/workflows/workflow-1/reject') {
      return { data: body }
    }
    throw new Error(`Unhandled POST ${path}`)
  }),
}))

vi.mock('@/features/auth/permission-snapshot', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, key: string) =>
    snapshot?.permissionKeys?.includes(key) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: testState.permissionSnapshot },
    isLoading: false,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: (path: string) => testState.apiGet(path),
    getEnvelope: (path: string) => testState.apiGet(path),
    post: (path: string, body?: unknown) => testState.apiPost(path, body),
    put: (path: string, body?: unknown) => testState.apiPut(path, body),
    delete: vi.fn(),
  },
}))

vi.mock('@/components/release-flow-dag-editor', () => ({
  ReleaseFlowDagEditor: (props: {
    className?: string
    initialDefinition?: Record<string, unknown>
    onChange?: (definition: Record<string, unknown>) => void
  }) => (
    <div className={props.className} data-testid="release-flow-dag-editor">
      <button
        type="button"
        onClick={() =>
          props.onChange?.({
            ...(props.initialDefinition ?? {}),
            nodes: [{ id: 'mock-build', type: 'build_image', name: 'Mock Build' }],
            edges: [],
          })
        }
      >
        Mock DAG change
      </button>
    </div>
  ),
}))

vi.mock('@/i18n', () => ({
  localeText: (localeCode: string, chinese: string, english: string) =>
    localeCode === 'zh_CN' ? chinese : english,
  useI18n: () => ({
    localeCode: 'zh_CN',
    t: (_key: string, fallback: string) => fallback,
  }),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function renderWithProviders(node: ReactNode, route = '/applications') {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <MemoryRouter initialEntries={[route]}>{node}</MemoryRouter>
        </AntApp>
      </QueryClientProvider>,
    )
  })

  // Flush dependent queries after selection and then the source ownership check.
  for (let index = 0; index < 4; index += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
  }

  return container
}

function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location-probe">{`${location.pathname}${location.search}`}</span>
}

function findButton(container: ParentNode, text: string) {
  const normalizedText = text.replace(/\s/g, '')
  const button = Array.from(container.querySelectorAll('button')).find((item) =>
    (item.getAttribute('aria-label') || item.textContent)
      ?.replace(/\s/g, '')
      .includes(normalizedText),
  ) as HTMLButtonElement | undefined
  if (!button) {
    const available = Array.from(container.querySelectorAll('button'))
      .map((item) => item.textContent?.trim())
      .filter(Boolean)
      .join(', ')
    throw new Error(`button not found: ${text}; available: ${available}`)
  }
  return button
}

function findToolbarButton(container: HTMLElement, toolbarSelector: string, text: string) {
  const toolbar = container.querySelector(toolbarSelector)
  if (!toolbar) {
    throw new Error(`toolbar not found: ${toolbarSelector}`)
  }
  return findButton(toolbar, text)
}

function hasButtonText(container: ParentNode, text: string) {
  return Array.from(container.querySelectorAll('button')).some((item) =>
    item.textContent?.includes(text),
  )
}

async function clickButton(button: HTMLButtonElement) {
  await act(async () => {
    button.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function setInputValue(input: HTMLInputElement | HTMLTextAreaElement, value: string) {
  const prototype =
    input instanceof HTMLTextAreaElement
      ? window.HTMLTextAreaElement.prototype
      : window.HTMLInputElement.prototype
  const setter = Object.getOwnPropertyDescriptor(prototype, 'value')?.set
  await act(async () => {
    setter?.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function clickTab(container: HTMLElement, text: string) {
  const tab = Array.from(container.querySelectorAll('[role="tab"]')).find((item) =>
    item.textContent?.includes(text),
  ) as HTMLElement | undefined
  if (!tab) {
    throw new Error(`tab not found: ${text}`)
  }
  await act(async () => {
    tab.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function waitForText(container: ParentNode, text: string) {
  for (let index = 0; index < 40; index += 1) {
    if (container.textContent?.includes(text)) return
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  }
}

describe('ApplicationsPage workspace layout', () => {
  function catalogOption(container: ParentNode, label: string) {
    return Array.from(container.querySelectorAll('label'))
      .find((item) => item.textContent === label)!
      .querySelector<HTMLInputElement>('input[type="radio"]')!
  }

  async function selectCatalogOption(container: ParentNode, label: string) {
    await act(async () => catalogOption(container, label).click())
  }

  beforeEach(() => {
    useAuthStore.setState({ user: null })
    usePreferencesStore.setState({ applicationShortcuts: {}, applicationListFilters: {} })
    testState.apiGet.mockClear()
    testState.apiPost.mockClear()
    testState.apiPut.mockClear()
    testState.forceHighBuildTemplateUsage = false
    testState.gitBuildTemplate = false
    testState.extraBuildTemplate = false
    testState.workflowRecipe = undefined
    testState.applicationsListError = false
    testState.releaseBoardError = false
    testState.workflowsEmpty = false
    testState.workflowsError = false
    testState.gatewayManifestMode = 'approval'
    testState.permissionSnapshot.permissionKeys = [...defaultPermissionKeys]
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
    document.body.innerHTML = ''
    testState.catalogServiceFixtures = false
    vi.clearAllMocks()
  })

  it('renders application workspaces as compact cards', async () => {
    const container = await renderWithProviders(<ApplicationsPage />)

    expect(container.textContent).not.toContain('接入应用/服务')
    expect(container.textContent).toContain('创建应用')
    expect(container.querySelector('[aria-label="应用分组"]')).not.toBeNull()
    expect(container.textContent).toContain('ERP Front Main')
    expect(container.textContent).toContain('全部')
    expect(container.textContent).toContain('erp-front')
    expect(container.textContent).toContain('企业订单与库存管理门户')
    expect(container.textContent).toContain('暂无备注')
    expect(container.textContent).toContain('已停用')
    expect(container.querySelector('[title="erp-front / frontend"]')).not.toBeNull()
    expect(container.textContent).toContain('mall')
    expect(container.textContent).not.toContain('执行中')
    expect(container.textContent).not.toContain('失败待处理')
    expect(container.textContent).not.toContain('环境范围')
    expect(container.querySelector('input[placeholder="搜索应用或服务类型"]')).not.toBeNull()
    const panel = container.querySelector('.soha-application-center-panel')!
    const toolbar = panel.querySelector(
      '.soha-management-query-card .soha-application-center-toolbar',
    )!
    expect(toolbar.querySelector('[aria-label="应用列表范围"]')).not.toBeNull()
    expect(toolbar.querySelector('[aria-label="刷新应用"]')).not.toBeNull()
    expect(panel.querySelector('.soha-application-card-grid')).not.toBeNull()
    expect(container.querySelector('.soha-application-card-grid')).not.toBeNull()
    expect(container.querySelectorAll('.soha-application-card')).toHaveLength(2)
    expect(container.querySelectorAll('.soha-application-card__more')).toHaveLength(2)
    expect(container.querySelector('.soha-application-card__metrics')).toBeNull()
    expect(container.querySelector('.soha-admin-table')).toBeNull()
    expect(container.querySelector('.soha-management-table-shell')).toBeNull()
    expect(container.querySelector('.soha-management-detail-header')).toBeNull()
    expect(container.querySelector('.soha-application-create-card')).toBeNull()
    expect(container.textContent).not.toContain('erp/front/main')
    expect(container.textContent).not.toContain('最近交付')
    expect(container.textContent).not.toContain('最新版本包')
    expect(container.textContent).not.toContain('部署目标')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery/release-board')
    expect(container.textContent).not.toContain('Repo Dockerfile')
    expect(container.textContent).not.toContain('erp-front / frontend')
    expect(container.textContent).not.toContain('进入应用')
    expect(container.textContent).not.toContain('按应用统一维护配置')
    expect(container.textContent).not.toContain('应用管理')
    expect(container.textContent).not.toContain('围绕应用聚合研发、测试和交付上下文')
    expect(container.textContent).not.toContain('应用详细清单')
  })

  it('filters bookmarked applications without showing another account bookmarks', async () => {
    const user = {
      userId: 'developer',
      userName: 'Developer',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    }
    useAuthStore.setState({ user })
    const container = await renderWithProviders(<ApplicationsPage />)
    const favorite = container.querySelector<HTMLButtonElement>(
      '[aria-label="收藏 ERP Front Main"]',
    )!
    await clickButton(favorite)
    await selectCatalogOption(container, '我的收藏')
    expect(container.querySelectorAll('.soha-application-card')).toHaveLength(1)
    expect(container.textContent).toContain('ERP Front Main')
    expect(container.textContent).not.toContain('Mall API')
    await act(async () => useAuthStore.setState({ user: { ...user, userId: 'tester' } }))
    await selectCatalogOption(container, '我的收藏')
    expect(container.querySelectorAll('.soha-application-card')).toHaveLength(0)
    expect(container.textContent).toContain('还没有收藏的应用')
    expect(
      container.querySelector(
        '.soha-application-center-panel .soha-management-state.is-borderless',
      ),
    ).not.toBeNull()
  })

  it('restores list filters after returning from an application', async () => {
    useAuthStore.setState({
      user: {
        userId: 'developer',
        userName: 'Developer',
        email: '',
        roles: [],
        teams: [],
        projects: [],
        tags: [],
      },
    })
    const container = await renderWithProviders(<ApplicationsPage />)
    await clickButton(
      container.querySelector<HTMLButtonElement>('[aria-label="收藏 ERP Front Main"]')!,
    )
    await selectCatalogOption(container, '我的收藏')
    await setInputValue(
      container.querySelector<HTMLInputElement>('input[placeholder="搜索应用或服务类型"]')!,
      'ERP',
    )
    await act(async () => roots.pop()!.unmount())
    const restored = await renderWithProviders(<ApplicationsPage />)
    expect(
      restored.querySelector<HTMLInputElement>('input[placeholder="搜索应用或服务类型"]')?.value,
    ).toBe('ERP')
    expect(catalogOption(restored, '我的收藏').checked).toBe(true)
    expect(restored.querySelectorAll('.soha-application-card')).toHaveLength(1)
    expect(restored.textContent).toContain('ERP Front Main')
  })

  it('creates an application and opens its workspace without configuring services', async () => {
    const container = await renderWithProviders(
      <>
        <ApplicationsPage />
        <LocationProbe />
      </>,
    )

    await clickButton(findButton(container, '创建应用'))

    const modal = document.querySelector('.ant-modal') as HTMLElement
    expect(modal.textContent).toContain('创建应用')
    expect(modal.textContent).not.toContain('快速创建')
    expect(modal.textContent).not.toContain('手工接入')
    expect(modal.textContent).not.toContain('AI 接入')
    expect(modal.textContent).toContain('应用分组')
    expect(modal.textContent).toContain('备注')
    expect(modal.textContent).not.toContain('语言')
    expect(modal.textContent).not.toContain('启用')
    expect(modal.querySelectorAll('.ant-form-item')).toHaveLength(4)

    await setInputValue(document.querySelector('#name') as HTMLInputElement, 'Payments')
    expect(document.querySelector<HTMLInputElement>('#key')?.value).toBe('payments')
    await setInputValue(document.querySelector('#name') as HTMLInputElement, 'Payments API')
    expect(document.querySelector<HTMLInputElement>('#key')?.value).toBe('payments-api')
    await setInputValue(document.querySelector('#key') as HTMLInputElement, 'payments-custom')
    await setInputValue(document.querySelector('#name') as HTMLInputElement, 'Payments Service')
    expect(document.querySelector<HTMLInputElement>('#key')?.value).toBe('payments-custom')
    await setInputValue(
      document.querySelector('#description') as HTMLTextAreaElement,
      '统一处理支付和退款',
    )
    const groupInput = document.querySelector('#group') as HTMLInputElement
    await act(async () => {
      groupInput.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    const frontendOption = Array.from(document.querySelectorAll('.ant-select-item-option')).find(
      (item) => item.textContent?.includes('frontend'),
    ) as HTMLElement
    await act(async () => {
      frontendOption.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await clickButton(modal.querySelector('button[type="submit"]') as HTMLButtonElement)

    expect(testState.apiPost).toHaveBeenCalledWith('/applications', {
      name: 'Payments Service',
      key: 'payments-custom',
      description: '统一处理支付和退款',
      group: 'frontend',
      enabled: true,
    })
    expect(testState.apiPost).toHaveBeenCalledTimes(1)
    expect(container.querySelector('[data-testid="location-probe"]')?.textContent).toBe(
      '/applications/app-created',
    )
  })

  it.each(['quick', 'manual', 'ai'])(
    'opens the same creation form for legacy mode %s',
    async (mode) => {
      const container = await renderWithProviders(
        <>
          <ApplicationsPage />
          <LocationProbe />
        </>,
        `/applications?action=create&mode=${mode}&templateId=blueprint-1`,
      )
      const modal = document.querySelector<HTMLElement>('.ant-modal')!
      expect(modal.querySelector('.ant-modal-title')?.textContent).toBe('创建应用')
      expect(modal.querySelectorAll('.ant-form-item')).toHaveLength(4)
      expect(modal.querySelector('.ant-segmented')).toBeNull()
      expect(modal.querySelector<HTMLInputElement>('#name')?.value).toBe('')
      expect(modal.querySelector<HTMLInputElement>('#key')?.value).toBe('')
      expect(modal.textContent).not.toContain('手工接入')
      expect(modal.textContent).not.toContain('AI 接入')
      expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery/blueprints')
      expect(testState.apiPost).not.toHaveBeenCalled()
      await clickButton(findButton(modal, '取消'))
      expect(container.querySelector('[data-testid="location-probe"]')?.textContent).toBe(
        '/applications',
      )
    },
  )

  it('requires create permission even when application editing is allowed', async () => {
    testState.permissionSnapshot.permissionKeys = defaultPermissionKeys.filter(
      (key) => key !== 'delivery.application.create',
    )
    const container = await renderWithProviders(<ApplicationsPage />, '/applications?action=create')
    expect(hasButtonText(container, '创建应用')).toBe(false)
    expect(container.querySelector('[aria-label="管理 ERP Front Main"]')).not.toBeNull()
    const modal = document.querySelector<HTMLElement>('.ant-modal')!
    expect(modal.textContent).toContain('无权创建应用')
    expect(modal.querySelector('form')).toBeNull()
    expect(testState.apiPost).not.toHaveBeenCalled()
  })

  it('isolates edited applications from new application drafts', async () => {
    const container = await renderWithProviders(
      <StrictMode>
        <ApplicationsPage />
      </StrictMode>,
    )
    const openEditor = async (name: string) => {
      await clickButton(container.querySelector<HTMLButtonElement>(`[aria-label="管理 ${name}"]`)!)
      const edit = Array.from(
        document.querySelectorAll<HTMLElement>(
          '.ant-dropdown:not(.ant-dropdown-hidden) [role="menuitem"]',
        ),
      ).find((item) => item.textContent === '编辑')!
      await act(async () => {
        edit.click()
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      return document.querySelector<HTMLElement>('.ant-modal')!
    }

    let modal = await openEditor('ERP Front Main')
    expect(modal.querySelector<HTMLInputElement>('#name')?.value).toBe('ERP Front Main')
    expect(modal.querySelector<HTMLTextAreaElement>('#description')?.value).toBe(
      '企业订单与库存管理门户',
    )
    await setInputValue(modal.querySelector<HTMLInputElement>('#name')!, 'ERP Portal')
    expect(modal.querySelector<HTMLInputElement>('#key')?.value).toBe('erp-front-main')
    await setInputValue(modal.querySelector<HTMLTextAreaElement>('#description')!, '')
    await clickButton(findButton(modal, '保存'))
    expect(testState.apiPut).toHaveBeenCalledWith(
      '/applications/app-1',
      expect.objectContaining({
        name: 'ERP Portal',
        key: 'erp-front-main',
        description: '',
        expectedVersion: 3,
        repositoryPath: 'erp/front/main',
      }),
    )

    modal = await openEditor('Mall API')
    expect(modal.querySelector<HTMLInputElement>('#name')?.value).toBe('Mall API')
    expect(modal.querySelector<HTMLTextAreaElement>('#description')?.value).toBe('')
    await setInputValue(modal.querySelector<HTMLTextAreaElement>('#description')!, '未保存的编辑')
    await clickButton(findButton(modal, '取消'))
    await clickButton(findButton(container, '创建应用'))
    modal = Array.from(document.querySelectorAll<HTMLElement>('.ant-modal')).find((item) =>
      item.querySelector('.ant-modal-title')?.textContent?.includes('创建应用'),
    )!
    expect(modal.querySelector<HTMLInputElement>('#name')?.value).toBe('')
    expect(modal.querySelector<HTMLInputElement>('#key')?.value).toBe('')
    expect(modal.querySelector<HTMLTextAreaElement>('#description')?.value).toBe('')
    expect(modal.querySelectorAll('.ant-select-selection-item')).toHaveLength(0)
    await setInputValue(modal.querySelector<HTMLInputElement>('#name')!, '支付 服务')
    expect(modal.querySelector<HTMLInputElement>('#key')?.value).toBe('支付-服务')
    expect(testState.apiPost).not.toHaveBeenCalledWith('/applications', expect.anything())
  })

  it('finds applications by their description', async () => {
    const container = await renderWithProviders(<ApplicationsPage />)
    await setInputValue(
      container.querySelector<HTMLInputElement>('input[placeholder="搜索应用或服务类型"]')!,
      '库存管理',
    )
    expect(container.querySelectorAll('.soha-application-card')).toHaveLength(1)
    expect(container.textContent).toContain('ERP Front Main')
    expect(container.textContent).not.toContain('Mall API')
  })

  it('searches readable service types and switches to a compact list', async () => {
    testState.catalogServiceFixtures = true
    testState.permissionSnapshot.permissionKeys.push('delivery.application-services.view')
    const container = await renderWithProviders(<ApplicationsPage />)
    expect(container.textContent).toContain('1 个服务')
    expect(container.textContent).toContain('0 个服务')
    expect(container.textContent).toContain('Helm Release')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/applications/app-1/runtime')

    await selectCatalogOption(container, '列表')
    expect(container.querySelector('.soha-application-card-list')).not.toBeNull()
    expect(container.querySelector('.soha-application-card-grid')).toBeNull()
    await setInputValue(
      container.querySelector<HTMLInputElement>('[aria-label="搜索应用或服务类型"]')!,
      'helm',
    )
    expect(container.querySelectorAll('.soha-application-card')).toHaveLength(1)
    expect(
      container.querySelector('[aria-label="查看应用 ERP Front Main"]')?.getAttribute('href'),
    ).toBe('/applications/app-1')
    expect(container.textContent).not.toContain('Mall API')
  })

  it('retains ordinary application search without service read permission', async () => {
    const container = await renderWithProviders(<ApplicationsPage />)
    expect(testState.apiGet).not.toHaveBeenCalledWith('/applications/app-1/services')
    expect(container.textContent).not.toContain('0 个服务')
    expect(container.textContent).not.toContain('Helm Release')
  })

  it('distinguishes filtered-empty results from request failures', async () => {
    const container = await renderWithProviders(<ApplicationsPage />)

    await setInputValue(
      container.querySelector('input[placeholder="搜索应用或服务类型"]') as HTMLInputElement,
      'missing-app',
    )
    expect(container.textContent).toContain('没有匹配的应用')
    expect(container.querySelectorAll('.soha-application-card')).toHaveLength(0)
  })

  it('shows a retryable error state when applications fail', async () => {
    testState.applicationsListError = true
    const container = await renderWithProviders(<ApplicationsPage />)

    expect(container.textContent).toContain('应用加载失败')
    expect(container.textContent).not.toContain('暂无应用')

    testState.applicationsListError = false
    await clickButton(container.querySelector('[aria-label="重试"]') as HTMLButtonElement)
    expect(container.textContent).toContain('ERP Front Main')
  })

  it('loads applications without depending on release status', async () => {
    testState.releaseBoardError = true
    const container = await renderWithProviders(<ApplicationsPage />)

    expect(container.textContent).toContain('ERP Front Main')
    expect(container.textContent).not.toContain('发布状态加载失败')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery/release-board')
  })

  it('builds typed build template payloads without form-only text fields', () => {
    const values = {
      key: 'node-docker',
      name: 'Node Docker',
      builderKind: 'docker',
      dockerfileTemplate: 'FROM node:22',
      buildCommandsText: '\n npm ci \n npm run build \n',
      variableSchemaText: '{"imageTag":{"type":"string"}}',
      defaultVariablesText: '{"imageTag":"main"}',
      enabled: true,
    } satisfies BuildTemplateFormValues

    expect(buildBuildTemplatePayload(values)).toEqual({
      key: 'node-docker',
      name: 'Node Docker',
      builderKind: 'docker',
      dockerfileTemplate: 'FROM node:22',
      buildCommands: ['npm ci', 'npm run build'],
      variableSchema: { imageTag: { type: 'string' } },
      defaultVariables: { imageTag: 'main' },
      enabled: true,
    })
  })

  it('renders build templates as a left-list and right-designer workspace', async () => {
    const container = await renderWithProviders(<BuildTemplatesPage />, '/build-templates')

    expect(testState.apiGet).toHaveBeenCalledWith('/build-templates')
    expect(testState.apiGet).toHaveBeenCalledWith('/build-templates/tpl-1/usage')
    expect(container.querySelector('.soha-build-template-workspace')).not.toBeNull()
    expect(container.querySelector('.soha-build-template-list')).not.toBeNull()
    const designer = container.querySelector('.soha-build-template-designer')!
    const list = container.querySelector('.soha-build-template-list')!
    expect(designer).not.toBeNull()
    expect(container.querySelector('.soha-template-designer-shell__toolbar')).toBeNull()
    expect(list.querySelector('[aria-label="导入文件"]')).not.toBeNull()
    expect(list.querySelector('[aria-label="刷新"]')).not.toBeNull()
    expect(findButton(designer, '保存草稿')).not.toBeNull()
    expect(list.textContent).not.toContain('保存草稿')
    expect(designer.querySelector('[aria-label="新建模板"]')).toBeNull()
    expect(
      container.querySelector('.soha-build-template-list [aria-label="新建模板"]'),
    ).not.toBeNull()
    expect(container.textContent).toContain('保存')
    expect(container.querySelector('[aria-label="取消更改"]')).not.toBeNull()
    expect(container.textContent).toContain('Node Docker')
    expect(container.textContent).toContain('docker-node')
    expect(container.textContent).toContain('命令 2')
    expect(container.textContent).toContain('变量 1')
    expect(container.textContent).toContain('基础信息')
    expect(container.textContent).toContain('Dockerfile')
    expect(container.textContent).toContain('构建命令')
    expect(container.textContent).toContain('变量')
    expect(container.textContent).toContain('YAML / JSON')
    expect(container.textContent).toContain('模板影响面')
    await waitForText(container, '成功 1')
    expect(container.textContent).toContain('成功 1')
    expect(container.textContent).toContain('运行中 1')
    expect(container.textContent).toContain('最近证据：构建: manual / completed')
    expect(container.textContent).toContain('版本包: 2.0.0')
    expect(container.textContent).toContain('跳转：')
    expect(container.querySelector('.soha-admin-table-shell')).toBeNull()
    expect(container.textContent).not.toContain('变量 Schema(JSON)')
    expect(container.textContent).not.toContain('默认变量(JSON)')
  })

  it('covers build template edit, variable, enable switch, and JSON preview interactions', async () => {
    const container = await renderWithProviders(<BuildTemplatesPage />, '/build-templates')

    expect(findToolbarButton(container, '.soha-build-template-list', '新建模板').disabled).toBe(
      false,
    )
    expect(findToolbarButton(container, '.soha-build-template-toolbar', '保存草稿').disabled).toBe(
      false,
    )
    expect(findToolbarButton(container, '.soha-build-template-toolbar', '废弃').disabled).toBe(
      false,
    )

    const enabledSwitch = container.querySelector(
      '.soha-build-template-list__item-actions button[role="switch"]',
    ) as HTMLButtonElement | null
    expect(enabledSwitch).not.toBeNull()
    expect(enabledSwitch?.disabled).toBe(false)

    await clickButton(findToolbarButton(container, '.soha-build-template-list', '新建模板'))
    expect(container.textContent).toContain('未保存')

    await clickTab(container, '变量')
    await clickButton(findButton(container, '添加变量'))
    expect(container.textContent).toContain('变量 2')

    await clickTab(container, 'YAML / JSON')
    await vi.waitFor(() =>
      expect(
        container.querySelector('textarea[aria-label="Soha Delivery 文档源码"]'),
      ).not.toBeNull(),
    )
    const content = container.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="Soha Delivery 文档源码"]',
    )!.value
    expect(parse(content).spec.variableSchema).toHaveProperty('imageTag')
    expect(parse(content).kind).toBe('BuildTemplate')
  })

  it('keeps a different template selected while the URL navigation settles', async () => {
    testState.extraBuildTemplate = true
    const container = await renderWithProviders(
      <>
        <BuildTemplatesPage />
        <LocationProbe />
      </>,
      '/build-templates?templateId=tpl-1',
    )
    await clickButton(
      container.querySelectorAll<HTMLButtonElement>('[aria-label="编辑构建模板"]')[1],
    )
    await vi.waitFor(() => {
      expect(container.querySelector<HTMLInputElement>('#key')?.value).toBe('git-build')
      expect(container.querySelector('[data-testid="location-probe"]')?.textContent).toContain(
        'templateId=tpl-2',
      )
    })
  })

  it('preserves imported constraints, multiline commands, empty defaults and deletion in the build form', () => {
    const original = {
      count: { type: 'integer', enum: [1, 2], minimum: 1, maximum: 2 },
      text: { type: 'string', minLength: 0, maxLength: 3 },
    }
    const values: BuildTemplateFormValues = {
      originalBuildCommands: ['echo a\necho b', 'echo c'],
      buildCommandsText: 'echo a\necho b\necho c',
      variableSchemaText: JSON.stringify(original),
      defaultVariablesText: '{"count":2,"text":""}',
      variables: [
        { key: 'count', type: 'integer', defaultValue: '2' },
        { key: 'text', type: 'string', defaultValue: '' },
      ],
    }
    const payload = buildBuildTemplatePayloadFromDesigner(values)
    expect(payload.buildCommands).toEqual(values.originalBuildCommands)
    expect(payload.variableSchema.count).toMatchObject(original.count)
    expect(payload.variableSchema.text).toMatchObject(original.text)
    expect(payload.defaultVariables).toEqual({ count: 2, text: '' })
    expect(buildBuildTemplatePayloadFromDesigner({ ...values, variables: [] })).toMatchObject({
      variableSchema: {},
      defaultVariables: {},
    })
    expect(() =>
      buildBuildTemplatePayloadFromDesigner({
        ...values,
        variables: [{ key: 'count', type: 'integer', defaultValue: '1.5' }],
      }),
    ).toThrow('整数')
  })

  it('saves a build draft with its loaded revision without changing published bindings', async () => {
    testState.forceHighBuildTemplateUsage = true
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const container = await renderWithProviders(<BuildTemplatesPage />, '/build-templates')

    await clickButton(findToolbarButton(container, '.soha-build-template-toolbar', '保存草稿'))

    expect(confirmSpy).not.toHaveBeenCalled()
    expect(testState.apiPut).toHaveBeenCalledWith(
      '/build-templates/tpl-1',
      expect.objectContaining({ publish: false, expectedRevision: 7 }),
    )
    confirmSpy.mockRestore()
  })

  it('keeps build template management controls disabled for readonly users', async () => {
    testState.permissionSnapshot.permissionKeys = [...readonlyPermissionKeys]
    const container = await renderWithProviders(<BuildTemplatesPage />, '/build-templates')

    expect(findToolbarButton(container, '.soha-build-template-list', '新建模板').disabled).toBe(
      true,
    )
    expect(findToolbarButton(container, '.soha-build-template-toolbar', '保存草稿').disabled).toBe(
      true,
    )
    expect(findToolbarButton(container, '.soha-build-template-toolbar', '废弃').disabled).toBe(true)
    expect(
      container.querySelector<HTMLButtonElement>(
        '.soha-build-template-list__item-actions button[role="switch"]',
      )?.disabled,
    ).toBe(true)
  })

  it('locks Git build definitions but keeps publication and copying available', async () => {
    testState.gitBuildTemplate = true
    const container = await renderWithProviders(<BuildTemplatesPage />, '/build-templates')
    expect(findToolbarButton(container, '.soha-build-template-toolbar', '保存草稿').disabled).toBe(
      true,
    )
    expect(findToolbarButton(container, '.soha-build-template-toolbar', '复制模板').disabled).toBe(
      false,
    )
    await clickTab(container, '来源')
    expect(container.textContent).toContain('Git 管理')
    await clickButton(findToolbarButton(container, '.soha-build-template-toolbar', '复制模板'))
    expect(findToolbarButton(container, '.soha-build-template-toolbar', '保存草稿').disabled).toBe(
      false,
    )
    await clickButton(findToolbarButton(container, '.soha-build-template-toolbar', '保存草稿'))
    expect(testState.apiPost).toHaveBeenCalledWith(
      '/build-templates',
      expect.objectContaining({ copiedFrom: { id: 'tpl-1', revision: 7 }, publish: false }),
    )
    expect(testState.apiPut).not.toHaveBeenCalled()
  })

  it('keeps the application environment index read-only and enters the owning app', async () => {
    const container = await renderWithProviders(
      <Routes>
        <Route path="/application-environments" element={<ApplicationEnvironmentsPage />} />
        <Route path="/applications/:applicationId" element={<LocationProbe />} />
      </Routes>,
      '/application-environments',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/application-environments')
    expect(testState.apiGet).toHaveBeenCalledWith('/applications')
    expect(container.textContent).toContain('ERP Front Main')
    expect(container.textContent).toContain('test')
    expect(container.textContent).toContain('Release DAG')
    expect(container.textContent).toContain('1')
    expect(hasButtonText(container, '新建绑定')).toBe(false)
    expect(hasButtonText(container, '导入集群服务')).toBe(false)
    expect(container.querySelector('[aria-label="编辑绑定"]')).toBeNull()
    expect(container.querySelector('[aria-label="删除绑定"]')).toBeNull()

    await clickButton(container.querySelector<HTMLButtonElement>('[aria-label="进入应用环境"]')!)
    expect(container.querySelector('[data-testid="location-probe"]')?.textContent).toBe(
      '/applications/app-1?tab=services&applicationEnvironmentId=binding-1',
    )
  })

  it('shows the read-only platform environment catalog and application usage', async () => {
    const container = await renderWithProviders(
      <EnvironmentCatalogPage />,
      '/delivery/environments',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/environments')
    expect(testState.apiGet).toHaveBeenCalledWith('/application-environments')
    expect(testState.apiGet).toHaveBeenCalledWith('/applications')
    expect(container.textContent).toContain('测试环境')
    expect(container.textContent).toContain('test')
    expect(container.textContent).toContain('ERP Front Main')
    expect(container.textContent).toContain('1 个应用')
    expect(hasButtonText(container, '新建环境')).toBe(false)
    expect(container.querySelector('[aria-label="编辑环境"]')).toBeNull()
    expect(container.querySelector('[aria-label="删除环境"]')).toBeNull()
  })

  it('browses the published workflow definition without mounting an editor, with JSON and usage on demand', async () => {
    const container = await renderWithProviders(<WorkflowTemplatesPage />, '/workflow-templates')
    await vi.waitFor(() =>
      expect(
        Array.from(container.querySelectorAll('[role=tab]')).some(
          (item) => item.textContent === 'YAML / JSON',
        ),
      ).toBe(true),
    )
    const list = container.querySelector('.soha-workflow-template-list')!
    const designer = container.querySelector('.soha-workflow-template-designer')!
    expect(list.querySelector('[aria-label="新建模板"]')).not.toBeNull()
    expect(list.querySelector('[aria-label="导入文件"]')).not.toBeNull()
    expect(list.querySelector('[aria-label="刷新"]')).not.toBeNull()
    expect(findButton(designer, '编辑草稿')).not.toBeNull()
    expect(designer.querySelector('[aria-label="新建模板"]')).toBeNull()
    expect(list.textContent).not.toContain('编辑草稿')
    expect(container.querySelector('.soha-template-designer-shell__toolbar')).toBeNull()
    expect(testState.apiGet).toHaveBeenCalledWith('/workflow-templates/wf-template-1/versions/1')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/workflow-templates/wf-template-1/usage')
    expect(container.textContent).toContain('Release DAG')
    expect(container.querySelector('[data-testid="release-flow-dag-editor"]')).toBeNull()
    await clickButton(
      Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find(
        (item) => item.textContent === 'YAML / JSON',
      ) as HTMLButtonElement,
    )
    await waitForText(container, 'mode: release_dag')
    expect(testState.apiGet).toHaveBeenCalledWith(
      '/delivery/documents/WorkflowTemplate/wf-template-1/export?format=yaml&version=1',
    )
    expect(container.querySelector('.soha-json-block')?.textContent).toContain(
      'kind: WorkflowTemplate',
    )
    await clickButton(
      Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find(
        (item) => item.textContent === '引用与使用',
      ) as HTMLButtonElement,
    )
    expect(testState.apiGet).toHaveBeenCalledWith('/workflow-templates/wf-template-1/usage')
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 30))
    })
    expect(container.textContent).toContain('模板影响面')
    expect(container.textContent).toContain('低风险')
  })

  it('edits workflow drafts explicitly and saves with the loaded draft revision', async () => {
    const container = await renderWithProviders(<WorkflowTemplatesPage />, '/workflow-templates')
    await vi.waitFor(() =>
      expect(
        Array.from(container.querySelectorAll('[role=tab]')).some(
          (item) => item.textContent === 'YAML / JSON',
        ),
      ).toBe(true),
    )
    await clickButton(findToolbarButton(container, '.soha-workflow-template-toolbar', '编辑草稿'))
    await vi.waitFor(() =>
      expect(document.body.querySelector('[data-testid="release-flow-dag-editor"]')).not.toBeNull(),
    )
    await clickButton(
      Array.from(document.body.querySelectorAll<HTMLButtonElement>('.ant-modal button')).find(
        (item) => item.textContent?.replace(/\s/g, '') === '保存草稿',
      )!,
    )
    await vi.waitFor(() =>
      expect(testState.apiPut).toHaveBeenCalledWith(
        '/workflow-templates/wf-template-1',
        expect.objectContaining({ publish: false, expectedRevision: 4 }),
      ),
    )
  })

  it('preserves a delivery recipe when editing and saving its draft', async () => {
    testState.workflowRecipe = {
      mode: 'delivery_batch',
      schemaVersion: 1,
      stages: ['build', 'plan', 'deploy', 'health'],
      executionMode: 'service_serial',
      stopOnFailure: false,
      maxConcurrency: 3,
    }
    const container = await renderWithProviders(<WorkflowTemplatesPage />, '/workflow-templates')
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 30))
    })
    await clickButton(findToolbarButton(container, '.soha-workflow-template-toolbar', '编辑草稿'))
    await vi.waitFor(() => expect(findButton(document.body, '保存草稿')).toBeTruthy())
    expect(document.body.querySelector('[data-testid="release-flow-dag-editor"]')).toBeNull()
    await clickButton(findButton(document.body, '保存草稿'))
    await vi.waitFor(() =>
      expect(testState.apiPut).toHaveBeenCalledWith(
        '/workflow-templates/wf-template-1',
        expect.objectContaining({
          definition: testState.workflowRecipe,
          publish: false,
          expectedRevision: 4,
        }),
      ),
    )
    expect(testState.apiPost).not.toHaveBeenCalled()
  })

  it('keeps unsupported legacy workflows readonly and offers their original definition for export', async () => {
    testState.apiPost.mockImplementationOnce(async () => ({
      data: {
        valid: false,
        candidates: [],
        diagnostics: [
          {
            path: 'template.json',
            document: 1,
            pointer: '/spec/definition/extension',
            code: 'unknown_field',
            message: 'unknown extension',
          },
        ],
      },
    }))
    const container = await renderWithProviders(<WorkflowTemplatesPage />, '/workflow-templates')
    await clickButton(findToolbarButton(container, '.soha-workflow-template-toolbar', '编辑草稿'))
    await waitForText(document.body, '此模板暂以只读方式保留')
    expect(document.body.querySelector('[data-testid="release-flow-dag-editor"]')).toBeNull()
    expect(findButton(document.body, '保存草稿').disabled).toBe(true)
    expect(findButton(document.body, '导出旧格式').disabled).toBe(false)
    expect(testState.apiPut).not.toHaveBeenCalled()
  })

  it('keeps workflow template writes disabled for readonly users while leaving JSON readable', async () => {
    testState.permissionSnapshot.permissionKeys = [...readonlyPermissionKeys]
    const container = await renderWithProviders(<WorkflowTemplatesPage />, '/workflow-templates')
    await vi.waitFor(() =>
      expect(
        Array.from(container.querySelectorAll('[role=tab]')).some(
          (item) => item.textContent === 'YAML / JSON',
        ),
      ).toBe(true),
    )
    for (const label of ['新建模板', '导入文件'])
      expect(findToolbarButton(container, '.soha-workflow-template-list', label).disabled).toBe(
        true,
      )
    for (const label of ['编辑草稿', '复制模板', '废弃'])
      expect(findToolbarButton(container, '.soha-workflow-template-toolbar', label).disabled).toBe(
        true,
      )
    await clickButton(
      Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find(
        (item) => item.textContent === 'YAML / JSON',
      ) as HTMLButtonElement,
    )
    await waitForText(container, 'mode: release_dag')
    expect(testState.apiGet).toHaveBeenCalledWith(
      '/delivery/documents/WorkflowTemplate/wf-template-1/export?format=yaml&version=1',
    )
    expect(container.querySelector('.soha-json-block')?.textContent).toContain(
      'kind: WorkflowTemplate',
    )
    expect(container.querySelector('[data-testid="release-flow-dag-editor"]')).toBeNull()
  })

  it('aligns deployment template tools and resets local pagination when searching', async () => {
    testState.permissionSnapshot.permissionKeys = ['delivery.deployment-templates.view']
    const container = await renderWithProviders(<DeploymentTemplatesPage />)
    const header = container.querySelector('.soha-admin-table-header')!
    expect(header.querySelector('[aria-label="切换表格密度"]')).not.toBeNull()
    expect(header.querySelector('[aria-label="刷新列表"]')).not.toBeNull()
    expect(header.querySelector('[aria-label="列设置"]')).not.toBeNull()
    expect(header.textContent).not.toContain('新建模板')
    expect(container.querySelector('.soha-admin-table-toolbar')).toBeNull()
    const nextPage = container.querySelector('.ant-pagination-item-2') as HTMLElement
    await act(async () => {
      nextPage.click()
    })
    expect(container.querySelector('tbody')?.textContent).toContain('部署模板 18')
    expect(container.querySelector('tbody')?.textContent).not.toContain('部署模板 2')
    await setInputValue(
      container.querySelector('[aria-label="搜索部署模板"]') as HTMLInputElement,
      '部署模板 2',
    )
    expect(container.querySelector('tbody')?.textContent).not.toContain('部署模板 2')
    expect(container.querySelector('.ant-pagination-item-active')?.textContent).toBe('2')
    await clickButton(findButton(container, '查询'))
    expect(container.querySelector('tbody')?.textContent).toContain('部署模板 2')
    expect(container.querySelector('.ant-pagination-item-active')?.textContent).toBe('1')
    await clickButton(findButton(container, '重置'))
    expect(container.querySelectorAll('tbody tr[data-row-key]')).toHaveLength(15)
    const count = testState.apiGet.mock.calls.filter(
      ([path]) => path === '/deployment-templates',
    ).length
    await clickButton(header.querySelector('[aria-label="刷新列表"]') as HTMLButtonElement)
    expect(
      testState.apiGet.mock.calls.filter(([path]) => path === '/deployment-templates'),
    ).toHaveLength(count + 1)
    await clickButton(header.querySelector('[aria-label="切换表格密度"]') as HTMLButtonElement)
    expect(container.querySelector('.ant-table-medium')).not.toBeNull()
  })

  it('keeps the shared delivery table controls together and functional', async () => {
    const container = await renderWithProviders(<RegistriesPage />, '/registries')
    const header = container.querySelector('.soha-admin-table-header')!
    expect(header.textContent).toContain('添加仓库')
    expect(container.querySelector('.soha-admin-table-toolbar')).toBeNull()
    expect(header.querySelector('[aria-label="列设置"]')).not.toBeNull()
    const count = testState.apiGet.mock.calls.filter(([path]) => path === '/registries').length
    await clickButton(header.querySelector('[aria-label="刷新"]') as HTMLButtonElement)
    expect(testState.apiGet.mock.calls.filter(([path]) => path === '/registries')).toHaveLength(
      count + 1,
    )
    await clickButton(header.querySelector('[aria-label="切换表格密度"]') as HTMLButtonElement)
    expect(container.querySelector('.ant-table-medium')).not.toBeNull()
  })

  it('hides registry save actions for readonly users', async () => {
    testState.permissionSnapshot.permissionKeys = [...readonlyPermissionKeys]
    const container = await renderWithProviders(<RegistriesPage />, '/registries')

    expect(testState.apiGet).toHaveBeenCalledWith('/registries')
    expect(container.textContent).toContain('Harbor Prod')
    expect(container.querySelector('.soha-metadata-tag')?.textContent).toBe('harbor')
    expect(hasButtonText(container, '添加仓库')).toBe(false)
    expect(container.querySelector('[aria-label="编辑仓库"]')).toBeNull()
    expect(container.querySelector('[aria-label="删除仓库"]')).toBeNull()
  })

  it('submits canonical registry fields without exposing the stored secret', async () => {
    const container = await renderWithProviders(<RegistriesPage />, '/registries')

    await clickButton(container.querySelector('[aria-label="编辑仓库"]') as HTMLButtonElement)
    const modal = document.querySelector('.ant-modal') as HTMLElement
    expect(modal.querySelector<HTMLInputElement>('#name')?.value).toBe('Harbor Prod')
    expect(modal.querySelector<HTMLInputElement>('#secret')?.value).toBe('')

    await setInputValue(modal.querySelector('#secret') as HTMLInputElement, 'rotated-token')
    await setInputValue(
      modal.querySelector('#metadata_allowedCIDRs') as HTMLInputElement,
      '10.20.0.0/16',
    )
    await setInputValue(
      modal.querySelector('#metadata_authEndpoint') as HTMLInputElement,
      'https://auth.example.com',
    )
    await clickButton(modal.querySelector('button[type="submit"]') as HTMLButtonElement)

    expect(testState.apiPut).toHaveBeenCalledWith('/registries/registry-1', {
      endpoint: 'https://harbor.example.com',
      insecure: false,
      name: 'Harbor Prod',
      namespace: 'delivery',
      registryType: 'harbor',
      secret: 'rotated-token',
      username: 'robot$delivery',
      metadata: {
        allowedCIDRs: '10.20.0.0/16',
        authEndpoint: 'https://auth.example.com',
        caCertificate: undefined,
      },
    })
  })

  it('keeps release history read only and links to its detail', async () => {
    testState.permissionSnapshot.permissionKeys = [
      ...defaultPermissionKeys,
      'delivery.releases.trigger',
    ]
    const container = await renderWithProviders(
      <>
        <ReleasesPage />
        <LocationProbe />
      </>,
      '/releases',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/releases')
    expect(container.textContent).toContain('app-1')
    expect(container.querySelector('[aria-label="部署"]')).toBeNull()
    await clickButton(container.querySelector('[aria-label="查看发布详情"]') as HTMLButtonElement)
    expect(container.querySelector('[data-testid="location-probe"]')?.textContent).toBe(
      '/releases/release-1',
    )
  })

  it.each([
    '/release-board?view=definitions',
    '/release-board?tab=workflows',
    '/release-board?tab=builds&kind=build',
  ])('keeps workflow definitions and separate execution records at %s', async (path) => {
    const container = await renderWithProviders(<WorkflowCatalog />, path)
    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/workflow-catalog?offset=0&limit=12')
    expect(container.querySelector('section[aria-label="工作流中心"]')).not.toBeNull()
    expect(container.querySelector('.soha-workflow-catalog.ant-card')).toBeNull()
    expect(
      container.querySelector('.soha-management-query-card .soha-workflow-catalog__toolbar'),
    ).not.toBeNull()
    expect(container.querySelector('.soha-management-query-card form')).toBeNull()
    expect(container.querySelector('[role="tablist"]')).toBeNull()
    expect(container.querySelector('[aria-label="筛选环境"]')).toBeNull()
    expect(container.querySelector('.soha-release-board__runs')).toBeNull()
    expect(container.querySelector('a[href="/execution-history"]')).toBeNull()
    expect(testState.apiGet).not.toHaveBeenCalledWith('/workflows?limit=200')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery-batches?limit=200')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/builds?limit=200')
  })

  it('shows genuine running and pending approval runs and opens the selected stage', async () => {
    const container = await renderWithProviders(<WorkflowCatalog />, '/release-board')
    expect(testState.apiGet).toHaveBeenCalledWith(
      '/delivery/execution-history?status=running&limit=12',
    )
    expect(testState.apiGet).toHaveBeenCalledWith(
      '/delivery/execution-history?status=approval&limit=12',
    )
    expect(container.textContent).toContain('build-release-main')
    expect(container.textContent).toContain('deploy-prod')
    expect(container.textContent).not.toContain('mall-api-release')
    expect(
      container.querySelector('a[href="/workflows/workflow-running?stage=build"]'),
    ).not.toBeNull()
    const approvalRow = findButton(container, 'deploy-prod')
    expect(approvalRow.textContent).toContain('待审批')
    expect(approvalRow.textContent).not.toContain('workflow-1')
    await clickButton(approvalRow)
    expect(container.querySelector('[aria-label="所选执行"] header')?.textContent).toContain(
      'workflow-1',
    )
    expect(container.textContent).toContain('Waiting for production approver')
    expect(container.querySelector('a[href="/workflows/workflow-1?stage=approve"]')).not.toBeNull()
    await act(async () => {
      Array.from(container.querySelectorAll('label'))
        .find((label) => label.textContent === '最近执行')
        ?.click()
    })
    await waitForText(container, 'mall-api-release')
    expect(container.textContent).toContain('mall-api-release')
    expect(container.textContent).not.toContain('deploy-prod')
    expect(container.querySelector('a[href="/execution-history"]')).not.toBeNull()
    expect(
      container.querySelector('a[href="/delivery/testing?releaseBundleId=bundle-mall"]'),
    ).not.toBeNull()
    await clickButton(findButton(container, 'billing-worker-release'))
    expect(
      container.querySelector('a[href="/delivery/analysis?executionTaskId=task-failed"]'),
    ).not.toBeNull()
  })

  it.each([false, true])(
    'gates workflow center stage evidence by its view permissions (%s)',
    async (allowed) => {
      testState.permissionSnapshot.permissionKeys = [
        'delivery.workflows.view',
        ...(allowed
          ? [
              'delivery.applications.view',
              'delivery.execution-tasks.view',
              'docker.operations.view',
              'delivery.release-bundles.view',
            ]
          : []),
      ]
      const detail = '/delivery/batches/batch-test'
      const container = await renderWithProviders(
        <WorkflowCenterRuns
          active
          selectedKey={null}
          onSelect={() => undefined}
          rows={[
            {
              id: 'batch-test',
              key: 'batch/batch-test',
              name: 'Batch test',
              status: 'running',
              createdAt: '2026-11-06T12:00:00Z',
              href: detail,
              targets: [
                {
                  id: 'target-1',
                  applicationId: 'app-1',
                  application: 'Catalog',
                  stages: [
                    {
                      key: 'deploy-1',
                      name: '部署',
                      status: 'running',
                      href: detail + '?targetId=target-1',
                      evidence: [
                        { kind: 'plan', href: detail + '?planId=plan-private' },
                        { kind: 'task', href: detail + '?evidenceId=task-private' },
                        { kind: 'docker', href: '/compute/tasks/operations?taskId=docker-private' },
                        { kind: 'build', href: detail + '?evidenceId=build-private' },
                        { kind: 'bundle', href: detail + '?evidenceId=bundle-private' },
                      ],
                    },
                  ],
                },
              ],
            },
          ]}
        />,
      )
      expect(container.querySelectorAll('a[href*="-private"]')).toHaveLength(allowed ? 5 : 0)
      expect(container.querySelector(`a[href="${detail}?targetId=target-1"]`)).not.toBeNull()
    },
  )

  it.each([401, 403, 503])(
    'keeps history evidence only for transient read failures (HTTP %s)',
    async (status) => {
      const view = await renderWithProviders(<ExecutionHistoryPage />, '/execution-history')
      expect(view.textContent).toContain('build-release-main')
      testState.apiGet.mockRejectedValueOnce(
        new ApiError(status, 'history read rejected', {
          method: 'GET',
          requestId: 'req-history-read',
        }),
      )
      await clickButton(findButton(view, '刷新执行记录'))
      await waitForText(view, 'req-history-read')
      if (status === 503) {
        expect(view.textContent).toContain('build-release-main')
        expect(view.textContent).toContain('上次读取结果')
      } else expect(view.textContent).not.toContain('build-release-main')
    },
  )

  it('keeps an unknown history approval on its original workflow and blocks a second decision', async () => {
    testState.permissionSnapshot.permissionKeys = [
      ...defaultPermissionKeys,
      'delivery.application-environments.approve',
    ]
    const view = await renderWithProviders(<ExecutionHistoryPage />, '/execution-history')
    testState.apiPost.mockRejectedValueOnce(
      new ApiError(503, 'history approval response lost', {
        method: 'POST',
        requestId: 'req-history-approval',
      }),
    )
    await clickButton(
      view.querySelector<HTMLButtonElement>('[aria-label="批准工作流 deploy-prod"]')!,
    )
    await clickButton(
      document.querySelector<HTMLButtonElement>('.ant-popconfirm-buttons .ant-btn-primary')!,
    )
    await waitForText(view, '请求结果不明')
    expect(view.querySelector('.soha-operation-feedback')?.textContent).toContain('workflow-1')
    expect(view.textContent).toContain('req-history-approval')
    await clickButton(findButton(view, '刷新执行记录'))
    expect(
      view.querySelector<HTMLButtonElement>('[aria-label="批准工作流 deploy-prod"]')?.disabled,
    ).toBe(true)
    expect(testState.apiPost.mock.calls.filter(([path]) => path.endsWith('/approve'))).toHaveLength(
      1,
    )
  })

  it('renders permission-scoped workflow runs as a release card board', async () => {
    testState.permissionSnapshot.permissionKeys = [
      ...defaultPermissionKeys,
      'delivery.application-environments.approve',
    ]
    const container = await renderWithProviders(
      <>
        <ExecutionHistoryPage />
        <LocationProbe />
      </>,
      '/execution-history',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/execution-history?status=all&limit=12')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery/release-board')
    expect(container.querySelector('section[aria-label="执行记录"]')).not.toBeNull()
    expect(container.querySelector('.soha-release-board__runs.ant-card')).toBeNull()
    expect(container.querySelector('.soha-management-query-card')).toBeNull()
    expect(
      container.querySelector('.soha-management-query-form [aria-label="刷新执行记录"]'),
    ).not.toBeNull()
    expect(container.textContent).not.toContain('最近 200 条')
    expect(container.textContent).not.toContain('正在进行')
    expect(container.textContent).not.toContain('执行历史')
    expect(container.textContent).toContain('build-release-main')
    expect(container.textContent).toContain('ERP Front Main')
    expect(container.textContent).toContain('Mall API')
    expect(container.textContent).toContain('Billing Worker')
    expect(container.textContent).toContain('2/3 节点')
    expect(container.textContent).toContain('当前节点：构建镜像')
    expect(container.querySelectorAll('.soha-release-run-card')).toHaveLength(5)
    expect(container.querySelectorAll('.soha-overview-metric-card')).toHaveLength(0)
    expect(container.querySelector('[role=tablist]')).toBeNull()
    expect(container.querySelector('a[href="/release-board"]')).toBeNull()
    expect(container.textContent).toContain('Standalone Docker')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery-batches?limit=200')
    expect(container.textContent).not.toContain('环境绑定')
    expect(container.textContent).not.toContain('候选版本')
    expect(container.querySelector('table')).toBeNull()

    const runsPanel = container.querySelector('.soha-release-board__runs') as HTMLElement
    expect(runsPanel).not.toBeNull()
    expect(runsPanel.textContent).toContain('下一页')
    expect(runsPanel.querySelector('[aria-label="按状态筛选执行记录"]')).not.toBeNull()
    expect(runsPanel.querySelector('[aria-label="批准工作流 deploy-prod"]')).not.toBeNull()
    expect(runsPanel.querySelector('[aria-label="拒绝工作流 deploy-prod"]')).not.toBeNull()

    await clickButton(
      runsPanel.querySelector('[aria-label="批准工作流 deploy-prod"]') as HTMLButtonElement,
    )
    expect(testState.apiPost).not.toHaveBeenCalledWith('/workflows/workflow-1/approve', {
      comment: 'Approved from execution history',
    })
    await clickButton(
      document.querySelector('.ant-popconfirm-buttons .ant-btn-primary') as HTMLButtonElement,
    )
    expect(testState.apiPost).toHaveBeenCalledWith('/workflows/workflow-1/approve', {
      comment: 'Approved from execution history',
    })

    await clickButton(
      container.querySelector('[aria-label="查看工作流 build-release-main"]') as HTMLButtonElement,
    )
    expect(container.querySelector('[data-testid="location-probe"]')?.textContent).toBe(
      '/workflows/workflow-running',
    )

    await setInputValue(
      runsPanel.querySelector('[aria-label="搜索执行记录"]') as HTMLInputElement,
      'billing',
    )
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 25))
    })
    expect(runsPanel.querySelectorAll('.soha-release-run-card')).toHaveLength(1)
    expect(runsPanel.textContent).toContain('Billing Worker')
    expect(runsPanel.textContent).not.toContain('Mall API')

    await setInputValue(
      runsPanel.querySelector('[aria-label="搜索执行记录"]') as HTMLInputElement,
      '',
    )
    const failedFilter = Array.from(
      runsPanel.querySelectorAll<HTMLElement>('.ant-segmented-item'),
    ).find((item) => item.textContent === '失败') as HTMLElement
    await act(async () => {
      failedFilter.click()
    })
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 25))
    })
    expect(runsPanel.querySelectorAll('.soha-release-run-card')).toHaveLength(1)
    expect(runsPanel.textContent).toContain('billing-worker-release')
    expect(runsPanel.textContent).not.toContain('build-release-main')
  })

  it('restores history filters and cursor, then clears paging when filters change', async () => {
    const container = await renderWithProviders(
      <>
        <ExecutionHistoryPage />
        <LocationProbe />
      </>,
      '/execution-history?tab=builds&applicationId=app-1&search=billing&status=failed&cursor=older',
    )
    expect(testState.apiGet).toHaveBeenCalledWith(
      '/delivery/execution-history?applicationId=app-1&status=failed&search=billing&cursor=older&limit=12',
    )
    expect(container.querySelector('[data-testid="location-probe"]')?.textContent).not.toContain(
      'tab=',
    )
    await setInputValue(
      container.querySelector('[aria-label="搜索执行记录"]') as HTMLInputElement,
      'mall',
    )
    expect(container.querySelector('[data-testid="location-probe"]')?.textContent).not.toContain(
      'cursor=',
    )
    expect(testState.apiGet).toHaveBeenCalledWith(
      '/delivery/execution-history?applicationId=app-1&status=failed&search=mall&limit=12',
    )
  })

  it('shows workflow board loading failures and empty results distinctly', async () => {
    testState.workflowsError = true
    const failed = await renderWithProviders(<ExecutionHistoryPage />, '/execution-history')

    expect(failed.textContent).toContain('执行记录读取失败')
    expect(failed.textContent).not.toContain('暂无匹配的执行记录')

    testState.workflowsError = false
    await clickButton(failed.querySelector('[aria-label="重试读取执行记录"]') as HTMLButtonElement)
    expect(failed.textContent).toContain('build-release-main')

    testState.workflowsEmpty = true
    const empty = await renderWithProviders(<ExecutionHistoryPage />, '/execution-history')
    expect(empty.textContent).toContain('暂无匹配的执行记录')
    expect(empty.textContent).not.toContain('正在进行')
    expect(empty.textContent).not.toContain('执行历史')
  })

  it('does not request workflow records without workflow view permission', async () => {
    testState.permissionSnapshot.permissionKeys = defaultPermissionKeys.filter(
      (permission) => permission !== 'delivery.workflows.view',
    )
    const container = await renderWithProviders(<ExecutionHistoryPage />, '/execution-history')

    expect(testState.apiGet).not.toHaveBeenCalledWith('/workflows?limit=200')
    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/execution-history?status=all&limit=12')
    expect(container.textContent).toContain('Standalone Docker')
    expect(container.textContent).not.toContain('build-release-main')
    expect(container.querySelector('.soha-workflow-catalog')).toBeNull()
  })

  it('renders execution task summary for delivery triage', async () => {
    const container = await renderWithProviders(<ExecutionTasksPage />, '/delivery/execution-tasks')

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/execution-tasks')
    expect(container.textContent).toContain('任务总数')
    expect(container.textContent).toContain('1 个执行中')
    expect(container.textContent).toContain('阻塞任务')
    expect(container.textContent).toContain('1 个可重试')
    expect(container.textContent).toContain('交付物线索')
    expect(container.textContent).toContain('回调可用')
    expect(container.textContent).toContain('task-running')
    expect(container.textContent).toContain('binding-1')
    expect(container.textContent).toContain('1 · erp-front')
    expect(container.textContent).toContain('task-failed')
    expect(container.querySelector('[aria-label="查看执行日志"]')).toBeNull()
    expect(container.querySelector('[aria-label="查看执行详情"]')).not.toBeNull()
    expect(container.querySelectorAll('.soha-overview-metric-card')).toHaveLength(4)
    expect(container.querySelector('.soha-application-signal-card')).toBeNull()
  })

  it.each([
    { error: new ApiError(409, 'retry rejected', { method: 'POST' }), unknown: false },
    { error: new ApiError(503, 'retry response lost', { method: 'POST' }), unknown: true },
  ])(
    'keeps retry feedback on its submitted task (unknown=$unknown)',
    async ({ error, unknown }) => {
      testState.permissionSnapshot.permissionKeys.push('delivery.execution-tasks.retry')
      testState.apiPost.mockRejectedValueOnce(error)
      const container = await renderWithProviders(
        <ExecutionTasksPage />,
        '/delivery/execution-tasks',
      )
      await clickButton(container.querySelector<HTMLButtonElement>('[aria-label="重试执行任务"]')!)
      await waitForText(container, error.message)
      const feedback = container.querySelector('.soha-operation-feedback')
      expect(feedback?.textContent).toContain('task-failed')
      expect(feedback?.textContent).not.toContain('task-running')
      expect(feedback?.textContent).toContain(unknown ? '请求结果不明' : '操作请求失败')
      expect(
        container.querySelector<HTMLButtonElement>('[aria-label="重试执行任务"]')?.disabled,
      ).toBe(unknown)
      expect(testState.apiPost).toHaveBeenCalledWith(
        '/delivery/execution-tasks/task-failed/retry',
        expect.objectContaining({ reason: 'Retried from execution tasks console' }),
      )
    },
  )

  it.each([
    {
      action: 'cancel',
      label: '取消执行任务',
      target: 'task-running',
      receipt: '取消请求已提交，请核对任务状态',
    },
    {
      action: 'retry',
      label: '重试执行任务',
      target: 'task-failed',
      receipt: '任务已重新入队，请查看执行记录',
    },
  ])(
    'shows one persistent acknowledgement and refreshes task evidence after $action',
    async ({ action, label, target, receipt }) => {
      testState.permissionSnapshot.permissionKeys.push(`delivery.execution-tasks.${action}`)
      testState.apiPost.mockResolvedValueOnce({ data: {} })
      const container = await renderWithProviders(
        <ExecutionTasksPage />,
        '/delivery/execution-tasks',
      )
      const reads = testState.apiGet.mock.calls.filter(
        ([path]) => path === '/delivery/execution-tasks',
      ).length
      await clickButton(container.querySelector<HTMLButtonElement>(`[aria-label="${label}"]`)!)
      if (action === 'cancel') {
        expect(testState.apiPost).not.toHaveBeenCalled()
        await clickButton(
          document.querySelector<HTMLButtonElement>('.ant-popconfirm-buttons .ant-btn-primary')!,
        )
      }
      await waitForText(container, receipt)
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      expect(container.querySelector('.soha-operation-feedback')?.textContent).toContain(receipt)
      expect(container.querySelector('.soha-operation-feedback')?.textContent).toContain(target)
      expect(document.querySelector('.ant-message-notice')).toBeNull()
      expect(testState.apiPost).toHaveBeenCalledTimes(1)
      expect(
        testState.apiGet.mock.calls.filter(([path]) => path === '/delivery/execution-tasks').length,
      ).toBeGreaterThan(reads)
    },
  )

  it('keeps legacy execution focus without opening a duplicate log modal', async () => {
    const container = await renderWithProviders(
      <ExecutionTasksPage />,
      '/delivery/execution-tasks?executionTaskId=task-running&releaseBundleId=bundle-1',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/execution-tasks')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery/execution-tasks/task-running/logs')
    expect(container.textContent).toContain('已定位执行任务 task-running')
    expect(container.textContent).toContain('executionTaskId=task-running')
    expect(container.textContent).toContain('releaseBundleId=bundle-1')
    expect(container.textContent).toContain('已定位')
    expect(document.body.textContent).not.toContain('任务日志 · task-running')
  })

  it('opens the dedicated execution task detail from the list', async () => {
    const container = await renderWithProviders(
      <>
        <ExecutionTasksPage />
        <LocationProbe />
      </>,
      '/delivery/execution-tasks',
    )

    await clickButton(container.querySelector('[aria-label="查看执行详情"]') as HTMLButtonElement)
    expect(container.querySelector('[data-testid="location-probe"]')?.textContent).toBe(
      '/delivery/execution-tasks/task-running',
    )
  })

  it('renders release bundle candidate summary', async () => {
    const container = await renderWithProviders(
      <>
        <ReleaseBundlesPage />
        <LocationProbe />
      </>,
      '/delivery/release-bundles',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/release-bundles')
    expect(container.textContent).toContain('候选版本')
    expect(container.textContent).toContain('1 个可验证 / 可推广')
    expect(container.textContent).toContain('阻塞版本')
    expect(container.textContent).toContain('缺少交付物')
    expect(container.textContent).toContain('1.2.3')
    expect(container.textContent).toContain('bundle-1')
    expect(container.textContent).toContain('registry.local/erp-front:1.2.3')
    expect(container.textContent).toContain('2.0.0-rc1')
    expect(container.querySelectorAll('.soha-overview-metric-card')).toHaveLength(4)
    expect(container.querySelector('.soha-application-signal-card')).toBeNull()
    await clickButton(container.querySelector('[aria-label="查看版本包详情"]') as HTMLButtonElement)
    expect(container.querySelector('[data-testid="location-probe"]')?.textContent).toBe(
      '/delivery/release-bundles/bundle-1',
    )
  })

  it('highlights focused release bundle from runtime evidence link', async () => {
    const container = await renderWithProviders(
      <ReleaseBundlesPage />,
      '/delivery/release-bundles?releaseBundleId=bundle-1',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/release-bundles')
    expect(container.textContent).toContain('已定位版本包 bundle-1')
    expect(container.textContent).toContain('releaseBundleId=bundle-1')
    expect(container.textContent).toContain('已定位')
  })

  it('routes runtime evidence directly to dedicated detail pages', () => {
    expect(runtimeEvidencePath({ kind: 'build', id: 'build-1' } as any)).toBe(
      '/builds/build-1?highlight=build-1',
    )
    expect(runtimeEvidencePath({ kind: 'workflow', id: 'workflow-1' } as any)).toBe(
      '/workflows/workflow-1?highlight=workflow-1',
    )
    expect(runtimeEvidencePath({ kind: 'release', id: 'release-1' } as any)).toBe(
      '/releases/release-1?highlight=release-1',
    )
    expect(runtimeEvidencePath({ kind: 'release_bundle', id: 'bundle-1' } as any)).toBe(
      '/delivery/release-bundles/bundle-1?highlight=bundle-1',
    )
    expect(runtimeEvidencePath({ kind: 'execution_task', id: 'task-running' } as any)).toBe(
      '/delivery/execution-tasks/task-running?highlight=task-running',
    )
    expect(
      runtimeEvidencePath({
        kind: 'environment',
        id: 'binding-1',
        applicationId: 'app-1',
        applicationEnvironmentId: 'binding-1',
      } as any),
    ).toBe('/applications/app-1?tab=services&applicationEnvironmentId=binding-1')
  })

  it('renders testing verification with global candidates and opens the existing AI assist', async () => {
    const container = await renderWithProviders(
      <>
        <DeliveryTestingPage />
        <LocationProbe />
      </>,
      '/delivery/testing?releaseBundleId=bundle-2',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/release-bundles')
    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/execution-tasks')
    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/release-board')
    expect(container.querySelector('[aria-label="按验证判断筛选"]')).not.toBeNull()
    const judgment = container.querySelector('[aria-label="晋级判断"]')
    expect(judgment?.textContent).toContain('2.0.0-rc1')
    expect(judgment?.textContent).toContain('禁止晋级')
    expect(container.textContent).toContain('1.2.3')
    expect(container.querySelectorAll('.soha-overview-metric-card')).toHaveLength(0)
    expect(testState.apiGet).not.toHaveBeenCalledWith(
      '/ai-gateway/capabilities?source=delivery-workbench&skillId=delivery-tester',
    )

    await clickButton(findButton(container, '验证辅助'))
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    expect(testState.apiGet).toHaveBeenCalledWith(
      '/ai-gateway/capabilities?source=delivery-workbench&skillId=delivery-tester',
    )
    expect(
      document.body
        .querySelector('[aria-label="AI Gateway 验证辅助说明"]')
        ?.getAttribute('tabindex'),
    ).toBe('0')
    expect(document.body.textContent).toContain('需要审批')
    expect(document.body.textContent).not.toContain('可以汇总版本、任务日志、diff 和验证证据')
  })

  it('filters the problem queue and links selected failed task evidence to the original record', async () => {
    const container = await renderWithProviders(
      <DeliveryAnalysisPage />,
      '/delivery/analysis?status=investigate&executionTaskId=task-failed',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/execution-tasks')
    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/release-board')
    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/release-bundles')
    expect(container.querySelector('[aria-label="按问题状态筛选"]')).not.toBeNull()
    const queue = container.querySelector('[aria-label="待处理线索"]')
    const evidence = container.querySelector('[aria-label="问题证据"]')
    expect(queue?.querySelectorAll('button[aria-pressed]')).toHaveLength(1)
    expect(queue?.querySelector('button[aria-pressed="true"]')?.textContent).toContain('待排查')
    expect(evidence?.textContent).toContain('task-failed')
    expect(evidence?.textContent).toContain('已观察到的事实')
    expect(evidence?.textContent).toContain('查看影响面')
    expect(
      evidence?.querySelector('a[href="/delivery/execution-tasks/task-failed"]')?.textContent,
    ).toContain('查看任务日志')
    expect(container.querySelectorAll('.soha-overview-metric-card')).toHaveLength(0)
  })

  it('keeps unavailable validation evidence distinct from a clear candidate result', async () => {
    testState.permissionSnapshot.permissionKeys = ['delivery.release-bundles.view']
    const container = await renderWithProviders(
      <DeliveryTestingPage />,
      '/delivery/testing?judgment=passed',
    )

    expect(testState.apiGet).toHaveBeenCalledWith('/delivery/release-bundles')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery/execution-tasks')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/delivery/release-board')
    expect(container.textContent).toContain('没有符合当前判断的候选版本')
    await clickButton(findButton(container, '清除筛选'))
    expect(container.textContent).toContain('证据待核对')
    expect(container.textContent).toContain('无查看权限')
  })

  it('redirects legacy onboarding links to simple application creation', async () => {
    const container = await renderWithProviders(
      <Routes>
        <Route path="/delivery/onboarding" element={<ApplicationCreateRedirect />} />
        <Route path="/applications" element={<LocationProbe />} />
      </Routes>,
      '/delivery/onboarding?mode=manual&templateId=blueprint-1',
    )

    const location = container.querySelector('[data-testid="location-probe"]')?.textContent ?? ''
    expect(location).toBe('/applications?action=create')
  })
})
