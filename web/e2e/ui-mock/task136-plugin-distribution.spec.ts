import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

const manifest = {
  id: 'example.distribution-smoke',
  name: 'Distribution Smoke Pack',
  version: '0.1.0',
  publisher: 'example',
  type: 'skill-pack',
  runtime: { mode: 'manifest-only' },
}
const sourceInfo = {
  kind: 'github-repo',
  canonicalSource: 'https://github.com/example/soha-skill-pack',
  commitSha: '0123456789abcdef0123456789abcdef01234567',
  rawManifestChecksum: `sha256:${'e'.repeat(64)}`,
  official: false,
}
const installed = {
  ...manifest,
  manifest,
  source: 'github-repo',
  sourceInfo,
  status: 'disabled',
  checksumStatus: 'verified',
  signatureStatus: 'not_provided',
  installedAt: '2026-11-21T00:00:00Z',
  updatedAt: '2026-11-21T00:00:00Z',
  installedBy: 'browser-regression-user',
}

test('Task136 B8 reviews signed releases and rolls back with storage references present', async ({
  context,
  page,
}) => {
  const width = 390
  await page.setViewportSize({ width, height: 900 })
  const publisherKeyFingerprint = 'f'.repeat(64)
  const executable = {
    ...manifest,
    version: '0.2.0',
    runtime: { mode: 'executable-package-v1' },
    components: {
      backend: {
        driverId: 'example.blob',
        protocol: 'object-storage/v1',
        platforms: { 'linux/amd64': { name: 'driver', sha256: 'a'.repeat(64), byteLength: 1 } },
      },
    },
  }
  const detail = `/plugins/${manifest.id}`
  const repositoryUrl = sourceInfo.canonicalSource
  let previews = 0
  let installedVersion = ''
  let rollbacks = 0
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace(/^\/api\/v1/, '')
    if (path === '/access/permission-snapshot') {
      const snapshot = permissions(true, true)
      snapshot.data.permissionKeys.push(
        'plugin.manage',
        'plugin.lifecycle',
        'plugin.remove',
        'settings.system-integrations.view',
        'software.package.view',
      )
      return { body: snapshot }
    }
    if (path === '/plugins/marketplace') return { body: { items: [] } }
    if (path === '/plugins/updates') return { body: { items: [] } }
    if (path === '/plugins/installed') return { body: { data: [] } }
    if (path === '/extensions/runtime') return { body: { data: [] } }
    if (path === '/system-integrations') {
      expect(url.searchParams.get('category')).toBe('storage')
      expect(url.searchParams.get('providerType')).toBe('example.blob')
      return { body: { items: [{ id: 'archive', name: 'Archive storage' }] } }
    }
    const currentItem = {
      ...installed,
      version: installedVersion,
      manifest: { ...executable, version: installedVersion },
      sourceInfo: { ...sourceInfo, publisherKeyFingerprint },
      signatureStatus: 'verified',
      status: 'enabled',
      rollbackVersion: rollbacks ? executable.version : manifest.version,
      rollbackRequiresPermissionConfirmation: true,
      runtimeReady: true,
      referenceCount: 1,
    }
    if (path === detail && method === 'GET') return { body: { data: currentItem } }
    if (path === detail + '/manifest') return { body: { data: currentItem.manifest } }
    if (path === '/plugins/repository/preview') {
      previews += 1
      expect(request.postDataJSON()).toEqual(
        previews === 3
          ? { repositoryUrl, trustPublisherFingerprint: publisherKeyFingerprint }
          : { repositoryUrl },
      )
      return {
        body: {
          data: {
            manifest: executable,
            sourceInfo,
            expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(),
            previewToken: previews === 3 ? 'signed-install-proof' : 'preview-only',
            checksumStatus: 'verified',
            signatureStatus: ['not_provided', 'untrusted', 'verified'][previews - 1],
            installEligibility: [
              'signed-release-required',
              'publisher-trust-required',
              'confirmable',
            ][previews - 1],
            ...(previews > 1
              ? {
                  publisherKeyFingerprint,
                  releaseAttestation: {
                    schemaVersion: '1',
                    pluginId: executable.id,
                    version: executable.version,
                    repository: repositoryUrl,
                    commitSha: sourceInfo.commitSha,
                    rawManifestSha256: 'e'.repeat(64),
                    components: executable.components,
                  },
                }
              : {}),
          },
        },
      }
    }
    if (path === '/plugins/repository/install') {
      expect(request.postDataJSON()).toEqual({ previewToken: 'signed-install-proof' })
      installedVersion = executable.version
      return {
        body: {
          data: {
            ...currentItem,
            version: installedVersion,
            status: 'installed',
            runtimeReady: false,
            referenceCount: 0,
          },
        },
      }
    }
    if (path === detail + '/repository/rollback') {
      expect(request.postDataJSON()).toEqual({
        expectedVersion: executable.version,
        expectedUpdatedAt: installed.updatedAt,
        version: manifest.version,
        confirmPermissionIncrease: true,
      })
      rollbacks += 1
      installedVersion = manifest.version
      return { body: { data: { ...currentItem, version: installedVersion } } }
    }
  })

  await page.goto('/plugins/marketplace')
  await page.getByRole('button', { name: /从仓库安装$/ }).click()
  const dialog = page.getByRole('dialog')
  const confirmation = dialog.getByRole('button', { name: '确认安装' })
  await dialog.getByLabel('仓库地址').fill(repositoryUrl)
  await dialog.getByRole('button', { name: /^预\s*览$/ }).click()
  await expect(dialog.getByText(/请先为此提交发布签名 Release/)).toBeVisible()
  await expect(confirmation).toHaveCount(0)
  await dialog.getByRole('button', { name: '重新预览' }).click()
  await expect(dialog.getByText(publisherKeyFingerprint, { exact: true })).toBeVisible()
  const geometry = await dialog.evaluate((node) => ({
    visible: node.getBoundingClientRect().width,
    scroll: node.scrollWidth,
  }))
  expect(geometry.visible).toBeLessThanOrEqual(width)
  expect(geometry.scroll).toBeLessThanOrEqual(geometry.visible + 1)
  const trust = dialog.getByRole('button', { name: '信任发布者并重新预览' })
  await expect(trust).toBeDisabled()
  await dialog.getByRole('checkbox', { name: /我已核对并信任/ }).check()
  await trust.click()
  await expect(dialog.getByText('发布者签名已验证')).toBeVisible()
  expect(installedVersion).toBe('')
  await confirmation.click()
  await expect(dialog.getByText(/安装记录已保存/).first()).toBeVisible()
  await expect(dialog.getByText(/仓库安装默认不启用/)).toBeVisible()

  await page.goto(`/plugins/installed/${manifest.id}`)
  await expect(page.getByText('Archive storage')).toBeVisible()
  await expect(page.getByRole('button', { name: /停\s*用$/ })).toBeDisabled()
  await expect(page.getByRole('button', { name: /移\s*除$/ })).toBeDisabled()
  await expect(page.getByText('已就绪', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '回退版本' }).click()
  const confirmRollback = dialog.getByRole('button', { name: '确认回退' })
  await expect(confirmRollback).toBeDisabled()
  await expect(dialog.getByText(/无需访问 GitHub 或市场/)).toBeVisible()
  await dialog.getByRole('checkbox', { name: /我已知悉回退/ }).check()
  await confirmRollback.click()
  await expect(dialog).toBeHidden()
  await expect(page.getByRole('main').getByText('v0.1.0', { exact: true })).toBeVisible()
  expect([previews, rollbacks]).toEqual([3, 1])
  verifyBrowserEvidence(evidence)
})

function permissions(canInstall: boolean, canUpgrade = false) {
  return {
    data: {
      permissionKeys: [
        'workbench.settings.view',
        'plugin.view',
        ...(canInstall ? ['plugin.install'] : []),
        ...(canUpgrade ? ['plugin.upgrade'] : []),
      ],
      visibleMenuIds: ['settings-extensions', 'settings-extensions-marketplace'],
      visibleMenus: [
        { id: 'settings-extensions', path: '/settings/extensions', labelZh: '扩展' },
        {
          id: 'settings-extensions-marketplace',
          parentId: 'settings-extensions',
          path: '/plugins/marketplace',
          labelZh: '市场',
        },
      ],
    },
  }
}

test('Task136 previews pinned evidence, recovers an expired proof and installs with the market offline', async ({
  context,
  page,
}) => {
  const width = 390
  await page.setViewportSize({ width, height: 900 })
  let previews = 0
  let writes = 0
  let installedSuccessfully = false
  const otherSource = { ...sourceInfo, canonicalSource: 'https://github.com/other/soha-skill-pack' }
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace(/^\/api\/v1/, '')
    if (path === '/access/permission-snapshot') return { body: permissions(true) }
    if (path === '/plugins/updates' && method === 'GET') return { body: { items: [] } }
    if (path === '/plugins/marketplace' && method === 'GET') {
      return { status: 502, body: { error: { code: 'unavailable', message: 'market offline' } } }
    }
    if (path === '/plugins/installed' && method === 'GET') {
      return { body: { data: installedSuccessfully ? [installed] : [] } }
    }
    if (path === '/plugins/repository/preview' && method === 'POST') {
      const source = installedSuccessfully ? otherSource : sourceInfo
      expect(request.postDataJSON()).toEqual({ repositoryUrl: source.canonicalSource })
      previews += 1
      return {
        body: {
          data: {
            manifest,
            sourceInfo: source,
            previewToken: `fixture-preview-${previews}`,
            expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(),
            checksumStatus: 'verified',
            signatureStatus: 'not_provided',
          },
        },
      }
    }
    if (path === '/plugins/repository/install' && method === 'POST') {
      writes += 1
      expect(request.postDataJSON()).toEqual({ previewToken: `fixture-preview-${writes}` })
      if (writes === 1) {
        return {
          status: 409,
          body: { error: { code: 'plugin_preview_invalid', message: 'Preview again' } },
        }
      }
      if (writes === 3) {
        return {
          status: 409,
          body: { error: { code: 'plugin_source_conflict', message: 'Plugin source conflict' } },
        }
      }
      installedSuccessfully = true
      return { status: 201, body: { data: installed } }
    }
  })

  await page.goto('/plugins/marketplace')
  await page.getByRole('button', { name: /从仓库安装$/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('仓库地址').fill(sourceInfo.canonicalSource)
  const preview = dialog.getByRole('button', { name: /(?:^| )预\s*览$/ })
  const confirm = dialog.getByRole('button', { name: /确认安装$/ })
  await preview.click()
  await expect(dialog).toContainText(sourceInfo.commitSha)
  await expect(dialog).toContainText(sourceInfo.rawManifestChecksum)
  await expect(dialog).not.toContainText('fixture-preview-1')
  const bounds = await dialog.boundingBox()
  expect(bounds?.x).toBeGreaterThanOrEqual(0)
  expect((bounds?.x ?? width) + (bounds?.width ?? 1)).toBeLessThanOrEqual(width + 1)
  await confirm.click()
  await expect(dialog.getByText(/预览已过期或内容已变化/)).toBeVisible()
  await expect(confirm).toBeDisabled()
  await expect(dialog.getByLabel('仓库地址')).toHaveValue(sourceInfo.canonicalSource)
  await preview.click()
  await confirm.click()
  await dialog.getByRole('button', { name: '查看已安装插件' }).click()
  await expect(page).toHaveURL(/installation=installed/)
  await expect(page.getByRole('heading', { name: manifest.name, exact: true })).toBeVisible()
  expect([previews, writes]).toEqual([2, 2])

  await page.getByRole('button', { name: /从仓库安装$/ }).click()
  await dialog.getByLabel('仓库地址').fill(otherSource.canonicalSource)
  await preview.click()
  await confirm.click()
  await expect(dialog.getByText('Plugin source conflict')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '查看已安装记录与当前来源' })).toBeEnabled()
  await expect(confirm).toBeDisabled()
  await expect(dialog.getByText(/预览已过期或内容已变化/)).toBeHidden()
  expect([previews, writes]).toEqual([3, 3])
  verifyBrowserEvidence(evidence)
})

test('Task136 shows an installed repository plugin to viewers while keeping preview unavailable', async ({
  context,
  page,
}) => {
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname.replace(/^\/api\/v1/, '')
    if (path === '/access/permission-snapshot') return { body: permissions(false) }
    if (path === '/plugins/updates' && method === 'GET') return { body: { items: [] } }
    if (path === '/plugins/installed' && method === 'GET') return { body: { data: [installed] } }
  })
  await page.goto('/plugins/marketplace?installation=installed')
  await expect(page.getByText(manifest.name, { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /从仓库安装$/ })).toBeDisabled()
  expect(evidence.requests.some((path) => path.startsWith('/api/v1/plugins/marketplace'))).toBe(
    false,
  )
  verifyBrowserEvidence(evidence)
})

test('Task136 requires a separate acknowledgement for added permissions on same-source upgrade', async ({
  context,
  page,
}) => {
  const target = {
    ...manifest,
    version: '0.2.0',
    permissions: { required: ['plugin.view'], domain: ['workspace.read'] },
  }
  const nextSource = { ...sourceInfo, commitSha: 'a'.repeat(40) }
  const nextItem = {
    ...installed,
    version: target.version,
    manifest: target,
    sourceInfo: nextSource,
    requestedPermissions: target.permissions,
    updatedAt: '2026-11-21T00:01:00Z',
  }
  const detail = `/plugins/${manifest.id}`
  const changedItem = { ...installed, updatedAt: '2026-11-21T00:00:30Z' }
  let previews = 0
  let writes = 0
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace(/^\/api\/v1/, '')
    if (path === '/access/permission-snapshot') return { body: permissions(true, true) }
    if (path === '/plugins/updates' && method === 'GET') return { body: { items: [] } }
    if (path === '/extensions/runtime' && method === 'GET') return { body: { data: [] } }
    if (path === detail && method === 'GET') {
      return { body: { data: writes ? nextItem : previews ? changedItem : installed } }
    }
    if (path === detail + '/manifest' && method === 'GET') {
      return { body: { data: writes ? target : manifest } }
    }
    if (path === '/plugins/repository/preview' && method === 'POST') {
      expect(request.postDataJSON()).toEqual({ repositoryUrl: sourceInfo.canonicalSource })
      previews += 1
      return {
        body: {
          data: {
            manifest: target,
            sourceInfo: nextSource,
            previewToken: 'fixture-upgrade',
            expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(),
            checksumStatus: 'verified',
            signatureStatus: 'not_provided',
          },
        },
      }
    }
    if (path === detail + '/repository/upgrade' && method === 'POST') {
      expect(request.postDataJSON()).toEqual({
        previewToken: 'fixture-upgrade',
        confirmPermissionIncrease: true,
      })
      writes += 1
      return { body: { data: nextItem } }
    }
  })
  await page.goto(`/plugins/installed/${manifest.id}`)
  await page.getByRole('button', { name: /升\s*级$/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '预览升级' }).click()
  await expect(dialog.getByText('已安装插件在预览期间有变更，请重新预览。')).toBeVisible()
  const confirm = dialog.getByRole('button', { name: '确认升级' })
  await expect(confirm).toBeDisabled()
  await dialog.getByRole('button', { name: '预览升级' }).click()
  await expect(dialog.getByText('workspace.read', { exact: true }).first()).toBeVisible()
  await expect(confirm).toBeDisabled()
  expect(writes).toBe(0)
  await dialog.getByRole('checkbox', { name: /我已知悉/ }).check()
  await confirm.click()
  await expect(dialog).toBeHidden()
  await expect(page.getByRole('main').getByText('v0.2.0', { exact: true })).toBeVisible()
  expect(writes).toBe(1)
  verifyBrowserEvidence(evidence)
})

test('Task136 requires explicit branch selection and can use a branch from a later page', async ({
  context,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 })
  const repositoryUrl = sourceInfo.canonicalSource
  const selectedRef = 'refs/heads/release/stable'
  let previews = 0
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace(/^\/api\/v1/, '')
    if (path === '/access/permission-snapshot') return { body: permissions(true) }
    if (path === '/plugins/marketplace' && method === 'GET') return { body: { items: [] } }
    if (path === '/plugins/installed' && method === 'GET') return { body: { data: [] } }
    if (path === '/plugins/repository/refs' && method === 'POST') {
      const branchPage = request.postDataJSON().page
      expect(request.postDataJSON()).toEqual({ repositoryUrl, kind: 'branch', page: branchPage })
      return {
        body: {
          data: {
            refs: [{ name: branchPage === 1 ? 'main' : 'release/stable' }],
            hasMore: branchPage === 1,
          },
        },
      }
    }
    if (path === '/plugins/repository/preview' && method === 'POST') {
      previews += 1
      if (previews === 1) {
        expect(request.postDataJSON()).toEqual({ repositoryUrl })
        return {
          status: 409,
          body: {
            error: { code: 'plugin_repository_branch_required', message: 'Choose a branch' },
          },
        }
      }
      expect(request.postDataJSON()).toEqual({ repositoryUrl, ref: selectedRef })
      return {
        body: {
          data: {
            manifest,
            sourceInfo: { ...sourceInfo, pinnedRef: selectedRef },
            previewToken: 'fixture-branch-preview',
            expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(),
          },
        },
      }
    }
  })
  await page.goto('/plugins/marketplace')
  await page.getByRole('button', { name: /从仓库安装$/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('仓库地址').fill(repositoryUrl)
  const preview = dialog.getByRole('button', { name: /(?:^| )预\s*览$/ })
  const confirm = dialog.getByRole('button', { name: '确认安装' })
  await preview.click()
  await expect(dialog.getByText(/请选择分支后预览/)).toBeVisible()
  await expect(preview).toBeDisabled()
  await expect(confirm).toBeDisabled()
  await dialog.getByRole('button', { name: '下一页' }).click()
  const select = dialog.getByRole('combobox', { name: '仓库分支' })
  await expect(select).toBeEnabled()
  await select.press('ArrowDown')
  await select.press('Enter')
  await expect(preview).toBeEnabled()
  expect(previews).toBe(1)
  await preview.click()
  await expect(dialog.getByText(selectedRef, { exact: true })).toBeVisible()
  await expect(confirm).toBeEnabled()
  expect(previews).toBe(2)
  verifyBrowserEvidence(evidence)
})
