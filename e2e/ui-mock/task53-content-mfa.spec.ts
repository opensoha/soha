import { expect, test } from '@playwright/test'
import type {
  ManagedContentDestination,
  ManagedContentDestinationConfig,
  ManagedContentRecord,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const pagePath = '/network-access/content-transfers'
const apiPath = `/api/v1${pagePath}`
const destinationId = '53000000-0000-4000-8000-000000000001'
const recordId = '53000000-0000-4000-8000-000000000002'
const timestamp = '2026-10-28T00:00:00Z'
const permissions = {
  transfer: ['network_access.content_transfers.view', 'network_access.content_transfers.create'],
  destination: [
    'network_access.content_destinations.view',
    'network_access.content_destinations.update',
  ],
}
function permissionSnapshot(keys: string[]) {
  return {
    body: {
      data: {
        permissionKeys: ['workbench.security.view', ...keys],
        visibleMenuIds: ['network-access-content-transfers'],
        visibleMenus: [
          {
            id: 'network-access-content-transfers',
            path: pagePath,
            labelZh: '受管内容传输',
          },
        ],
      },
    },
  }
}

test('persists the archive pin and MFA requirement, then clears MFA when switching to audit', async ({
  context,
  page,
}) => {
  const draft: ManagedContentDestinationConfig = {
    name: 'Task53 synthetic destination',
    kind: 's3',
    enabled: true,
    storageIntegrationId: 'synthetic-storage',
    policy: {
      mode: 'warn',
      requireStepUp: true,
      detectors: ['private_key'],
      rules: [],
      networkAction: 'none',
      minimumDataLevel: 'public',
      detectorVersion: 'content-dlp-text-v1',
    },
  }
  const destination: ManagedContentDestination = {
    id: destinationId,
    ownerId: 'browser-regression-user',
    revision: 3,
    publishedVersion: 0,
    draft,
    updatedAt: timestamp,
  }
  const writes: { expectedRevision: number; draft: ManagedContentDestinationConfig }[] = []
  const evidence = await installMocks(context, page, (url, method, request) => {
    if (url.pathname === '/api/v1/access/permission-snapshot')
      return permissionSnapshot(permissions.destination)
    if (url.pathname === `${apiPath}/destinations/admin` && method === 'GET')
      return { body: { data: [destination] } }
    if (url.pathname === `${apiPath}/destinations/${destinationId}` && method === 'PUT') {
      const body = request.postDataJSON() as (typeof writes)[number]
      writes.push(body)
      destination.draft = body.draft
      destination.revision += 1
      return { body: { data: destination } }
    }
    return undefined
  })
  await page.goto(pagePath)
  await page.getByRole('button', { name: /查看 \/ 编辑|View \/ Edit/ }).click()
  const mfa = page.getByRole('switch', { name: /命中放行前额外验证 MFA|Require MFA/ })
  await expect(mfa).toBeChecked()
  await page.getByLabel(/固定检测器版本|Pinned detector version/, { exact: true }).click()
  await page
    .locator('.ant-select-dropdown:visible .ant-select-item-option')
    .filter({ hasText: 'content-dlp-archive-v2' })
    .click()
  await page
    .locator('form')
    .getByRole('button', { name: /保存草稿|Save draft/ })
    .click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0].expectedRevision).toBe(3)
  expect(writes[0].draft.policy.requireStepUp).toBe(true)
  expect(writes[0].draft.policy.detectorVersion).toBe('content-dlp-archive-v2')
  await expect(
    page.locator('form').getByRole('button', { name: /保存草稿|Save draft/ }),
  ).toBeEnabled()
  await page.getByLabel(/内容模式|Content mode/, { exact: true }).click()
  await page
    .locator('.ant-select-dropdown:visible .ant-select-item-option')
    .filter({ hasText: /审计：可能放行|Audit: may allow/ })
    .click()
  await expect(mfa).not.toBeChecked()
  await expect(mfa).toBeDisabled()
  await page
    .locator('form')
    .getByRole('button', { name: /保存草稿|Save draft/ })
    .click()
  await expect.poll(() => writes.length).toBe(2)
  expect(writes[1].expectedRevision).toBe(4)
  expect(writes[1].draft.policy).toMatchObject({
    mode: 'audit',
    requireStepUp: false,
    detectorVersion: 'content-dlp-archive-v2',
  })
  verifyBrowserEvidence(evidence)
})

test('shows MFA guidance for blocked content and never resubmits after authorization is denied', async ({
  context,
  page,
}) => {
  let uploads = 0
  let authorizations = 0
  const record: ManagedContentRecord = {
    id: recordId,
    actorId: 'browser-regression-user',
    destinationId,
    revision: 1,
    sizeBytes: 32,
    gates: [{ destinationId, policyVersion: 7 }],
    createdAt: timestamp,
    updatedAt: timestamp,
    state: 'blocked',
    action: 'upload',
    appealStatus: 'none',
    fileName: 'synthetic.txt',
    decision: {
      eventId: recordId,
      mode: 'warn',
      status: 'matched',
      allowed: false,
      stepUpRequired: true,
      requiredAction: 'acknowledge',
      dataLevel: 'restricted',
      minimumDataLevel: 'public',
      detectorVersion: 'content-dlp-text-v1',
      policyVersion: 7,
      matchedRuleIds: ['credential_assignment'],
    },
  }
  const evidence = await installMocks(context, page, (url, method) => {
    if (url.pathname === '/api/v1/access/permission-snapshot')
      return permissionSnapshot(permissions.transfer)
    if (url.pathname === `${apiPath}/destinations` && method === 'GET')
      return {
        body: {
          data: [
            {
              id: destinationId,
              name: 'Task53 synthetic destination',
              kind: 's3',
              publishedVersion: 7,
              mode: 'warn',
            },
          ],
        },
      }
    if (url.pathname === `${apiPath}/uploads` && method === 'POST') {
      uploads += 1
      return { body: { data: { record } } }
    }
    if (url.pathname === `${apiPath}/records/${record.id}/authorize` && method === 'POST') {
      authorizations += 1
      return {
        status: 403,
        body: { error: { code: 'mfa_required', message: 'Task53 recent MFA required' } },
      }
    }
    return undefined
  })
  await page.goto(pagePath)
  await page.getByLabel(/目的连接|Destination/, { exact: true }).click()
  await page
    .locator('.ant-select-dropdown:visible .ant-select-item-option')
    .filter({ hasText: 'Task53 synthetic destination' })
    .click()
  await page.getByLabel(/选择上传文件|Select a file for upload/).setInputFiles({
    name: 'synthetic.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('api_key = synthetic-secret-value'),
  })
  await page.getByRole('button', { name: /检查并提交|Inspect and submit/ }).click()
  await expect(
    page.getByText(/放行还需近期 MFA 验证|Release also requires recent MFA/),
  ).toBeVisible()
  const security = page.getByRole('link', { name: /打开安全页|Open security settings/ })
  await expect(security).toHaveAttribute('href', '/portal/security')
  await expect(security).toHaveAttribute('target', '_blank')
  await expect(security).toHaveAttribute('rel', 'noopener noreferrer')
  expect(uploads).toBe(1)
  expect(authorizations).toBe(0)
  await page.getByRole('button', { name: /确认警告并重试原内容|Acknowledge and retry/ }).click()
  await page
    .getByRole('tooltip')
    .getByRole('button', { name: /^(确\s*定|OK)$/ })
    .click()
  await expect.poll(() => authorizations).toBe(1)
  await expect(page.getByText('Task53 recent MFA required').first()).toBeVisible()
  expect(uploads).toBe(1)
  verifyBrowserEvidence(evidence)
})
