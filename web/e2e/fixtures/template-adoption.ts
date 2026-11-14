import { expect, type BrowserContext, type Page } from '@playwright/test'
import type {
  TemplateUpdatePreviewInput,
  TemplateUpdateBatchInput,
  TemplateUpdateTarget,
  TemplateUpdatePreview,
  TemplateMigrationAssessment,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { installMocks } from './mock-app'

const now = '2026-10-22T00:00:00Z'
export const template = {
  id: 'http',
  key: 'http',
  name: '标准应用配置',
  enabled: true,
  publishedVersion: 4,
  publicationState: 'published',
  revision: 4,
  createdAt: now,
  updatedAt: now,
  source: { renderer: 'raw_yaml', files: [] },
  parameterSchema: { type: 'object', properties: { replicas: { type: 'integer' } } },
  defaults: { replicas: 4 },
  artifacts: {},
  health: { mode: 'configuration_only', timeoutSeconds: 300 },
}
export const targets: TemplateUpdateTarget[] = Array.from({ length: 10 }, (_, i) => ({
  id: `target-${i}`,
  kind: 'deployment',
  templateId: 'http',
  applicationId: `app-${Math.floor(i / 4)}`,
  applicationName: ['零售业务', '实时计算', '基础服务'][Math.floor(i / 4)],
  serviceId: `svc-${i}`,
  serviceName: `服务 ${i + 1}`,
  applicationEnvironmentId: i === 7 ? 'prod' : 'test',
  environmentName: i === 7 ? '生产环境' : '测试环境',
  releaseTargetId: `release-${i}`,
  fromVersion: 1,
  mode: 'fixed',
  requiresApproval: i === 7,
  eligible: i !== 9,
  reason: i === 9 ? '独立副本不参与模板传播' : '',
  independentOverrides: ['environment.replicas'],
  bindingRevision: `revision-${i}`,
  scope: 'environment',
}))

export async function adoptionMocks(context: BrowserContext, page: Page, writable = true) {
  const state = {
    migration: undefined as TemplateMigrationAssessment | undefined,
    templateWrites: [] as Record<string, unknown>[],
    previews: [] as TemplateUpdatePreviewInput[],
    batches: [] as TemplateUpdateBatchInput[],
    policyWrites: [] as Record<string, unknown>[],
    failRead: false,
    failBatch: false,
    previewError: 0,
  }
  let frozen: TemplateUpdatePreview | undefined
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace('/api/v1', '')
    const data = (value: unknown) => ({ body: { data: value } })
    if (path === '/access/permission-snapshot')
      return data({
        permissionKeys: [
          'workbench.delivery.view',
          'delivery.deployment-templates.view',
          ...(writable
            ? [
                'delivery.application-environments.update',
                'delivery.workflows.trigger',
                'delivery.deployment-templates.update',
              ]
            : []),
        ],
        visibleMenuIds: ['deployment-templates'],
        visibleMenus: [
          { id: 'deployment-templates', path: '/deployment-templates', labelZh: '部署模板' },
        ],
      })
    if (path === '/deployment-templates/http' && method === 'PUT') {
      const input = request.postDataJSON()
      state.templateWrites.push(input)
      return data({ ...template, ...input })
    }
    if (path === '/delivery/documents/DeploymentTemplate/http/source')
      return data({ kind: 'DeploymentTemplate', objectId: 'http' })
    if (path === '/deployment-templates') return data([template])
    if (path === '/deployment-templates/http/versions')
      return data([1, 2, 3, 4].map((publishedVersion) => ({ ...template, publishedVersion })))
    if (path === '/delivery/template-updates/targets')
      return state.failRead
        ? {
            status: 503,
            body: {
              error: {
                code: 'unavailable',
                message: '暂时无法读取应用绑定',
                request_id: 'targets-read',
              },
            },
          }
        : data({ items: targets, total: 10, page: 1, pageSize: 20 })
    if (path === '/delivery/template-updates/policies' && method === 'GET') return data([])
    if (path === '/delivery/template-updates/policies' && method === 'PUT') {
      const input = request.postDataJSON()
      state.policyWrites.push(input)
      return data({ ...input, id: 'policy', version: 1, lastError: '', updatedAt: now })
    }
    if (path === '/delivery/template-updates/previews' && method === 'POST') {
      const input = request.postDataJSON() as TemplateUpdatePreviewInput
      state.previews.push(input)
      if (state.previewError)
        return {
          status: state.previewError,
          body: {
            error: {
              code: state.previewError === 403 ? 'forbidden' : 'unavailable',
              message: '无法重新比较模板',
              request_id: 'preview-retry',
            },
          },
        }
      const sequence = state.previews.length
      frozen = {
        id: `preview-${sequence}`,
        kind: 'deployment',
        templateId: 'http',
        toVersion: input.toVersion,
        digest: `digest-${sequence}`,
        configurationOnly: true,
        createdAt: now,
        expiresAt: '2026-10-23T00:00:00Z',
        candidates: input.targets.map((selection, i) => {
          const target = targets.find((item) => item.id === selection.targetId)!
          const changes = [
            { section: 'defaults' as const, path: 'replicas', before: '1', after: '4' },
          ]
          return {
            id: `candidate-${sequence}-${i}`,
            target,
            toVersion: 4,
            digest: `candidate-digest-${sequence}-${i}`,
            parameters: selection.parameters ?? { replicas: 1 },
            eligible: !state.migration?.blocked,
            reason: state.migration?.reason ?? '',
            requiresApproval: target.requiresApproval || Boolean(state.migration?.requiresApproval),
            wave: 0,
            cumulative: {
              migration: state.migration,
              fromVersion: 1,
              toVersion: 4,
              changes,
              renderStatus: 'available',
              diagnostics: [],
              historyGap: false,
            },
            versions: [1, 2, 3].map((fromVersion) => ({
              fromVersion,
              toVersion: fromVersion + 1,
              changes,
              renderStatus: 'available' as const,
              diagnostics: [],
              historyGap: false,
            })),
          }
        }),
      }
      return data(frozen)
    }
    if (
      /^\/delivery\/template-updates\/previews\/preview-\d+\/batches$/.test(path) &&
      method === 'POST'
    ) {
      state.batches.push(request.postDataJSON())
      return state.failBatch
        ? {
            status: 409,
            body: {
              error: {
                code: 'conflict',
                message: '绑定已变化，请重新比较',
                request_id: 'adoption-stale',
              },
            },
          }
        : data({ id: 'batch-adoption' })
    }
  })
  return { state, evidence }
}

export async function openAdoption(page: Page) {
  await page.goto('/deployment-templates')
  await page.getByRole('button', { name: '检查模板更新', exact: true }).click()
  const drawer = page.getByRole('dialog', { name: '模板采用 · http', exact: true })
  await expect(drawer.getByText('独立副本不参与模板传播', { exact: true })).toBeVisible()
  return drawer
}
