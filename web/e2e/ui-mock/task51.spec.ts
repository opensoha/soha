import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

for (const width of [1440, 390])
  test(`task51 bounded capacity and credential references ${width}px`, async ({
    page,
    context,
  }) => {
    const policy = {
      sourceId: 'physical-one',
      sourceConfirmed: true,
      revision: 2,
      mode: 'enforced',
      cpu: { enabled: true, percent: 200 },
      memory: { enabled: false, percent: 100 },
      configured: true,
    }
    const saves: unknown[] = []
    const evidence = await installMocks(context, page, (url, method, request) => {
      const path = url.pathname
      if (path === '/api/v1/access/permission-snapshot')
        return {
          body: {
            data: {
              permissionKeys: [
                'workbench.settings.view',
                'workbench.compute.view',
                'virtualization.clusters.view',
                'virtualization.clusters.update',
                'virtualization.vms.view',
                'virtualization.storage.view',
                'secret.view',
                'secret.use',
              ],
              visibleMenuIds: ['virtualization-workbench-clusters', 'settings-secrets'],
              visibleMenus: [
                {
                  id: 'virtualization-workbench-clusters',
                  path: '/compute/virtualization/clusters',
                  labelZh: '集群',
                },
                { id: 'settings-secrets', path: '/settings/secrets', labelZh: '凭证' },
              ],
            },
          },
        }
      if (path === '/api/v1/modules')
        return {
          body: {
            data: [
              {
                descriptor: {
                  id: 'virtualization',
                  name: 'Virtualization',
                  defaultPath: '/compute/virtualization',
                },
                enabled: true,
              },
            ],
          },
        }
      if (path === '/api/v1/virtualization/clusters')
        return {
          body: {
            data: [
              {
                id: 'pve-one',
                name: 'Task51 PVE',
                provider: 'pve',
                enabled: true,
                endpoint: 'https://pve.example',
                verifyTls: true,
                config: {},
                vmCount: 1,
              },
            ],
          },
        }
      if (path === '/api/v1/virtualization/operations') return { body: { data: [] } }
      if (path === '/api/v1/virtualization/clusters/pve-one/capacity-policy') {
        if (method === 'PUT') {
          saves.push(request.postDataJSON())
          return { body: { data: { ...policy, revision: 3, ...request.postDataJSON() } } }
        }
        return { body: { data: policy } }
      }
      if (path === '/api/v1/virtualization/capacity/check')
        return {
          body: {
            data: {
              connectionId: 'pve-one',
              status: 'insufficient',
              reason: 'Actual memory is insufficient',
              diagnostics: [
                {
                  node: 'node-one',
                  checks: [
                    {
                      check: 'runtime',
                      dimension: 'memory',
                      status: 'rejected',
                      limit: 4096,
                      committed: 0,
                      pending: 1024,
                      demand: 4096,
                      reason: 'runtime memory',
                    },
                  ],
                },
              ],
            },
          },
        }
      if (path === '/api/v1/secrets')
        return {
          body: {
            items: [
              {
                id: 'credential-one',
                name: 'Task51 SSH key',
                scopeType: 'workspace',
                scopeId: 'default',
                kind: 'ssh_keypair',
                status: 'active',
                currentVersion: 2,
                createdBy: 'owner',
                createdAt: '2026-10-10T00:00:00Z',
                updatedAt: '2026-10-10T00:00:00Z',
                bindings: [{ targetType: 'vm', targetRef: 'vm-one', purpose: 'vm_auth' }],
              },
            ],
          },
        }
      if (path === '/api/v1/secrets/credential-one/references')
        return {
          body: {
            items: [
              {
                targetId: 'vm-one',
                vmId: 'vm-one',
                usage: 'login_binding',
                version: 1,
                bindingRevision: 2,
                status: 'active',
              },
            ],
          },
        }
      return undefined
    })
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/compute/virtualization/clusters')
    await page.getByRole('button', { name: '容量与超分', exact: true }).click()
    const drawer = page.getByRole('dialog')
    await expect(drawer.getByText('允许 CPU 超分', { exact: true })).toBeVisible()
    await expect(drawer.getByText('允许内存超分', { exact: true })).toBeVisible()
    await page.locator('#cpu_enabled').click()
    await drawer.getByRole('button', { name: '保存容量策略' }).click()
    await expect.poll(() => saves.length).toBe(1)
    expect(saves[0]).toMatchObject({
      cpu: { enabled: false, percent: 100 },
      memory: { enabled: false, percent: 100 },
      expectedRevision: 2,
    })
    await drawer.getByRole('button', { name: '检查两道准入' }).click()
    await expect(drawer.getByText('Actual memory is insufficient')).toBeVisible()
    const bounds = await drawer.boundingBox()
    expect(bounds?.x).toBeGreaterThanOrEqual(0)
    expect((bounds?.x ?? 0) + (bounds?.width ?? 0)).toBeLessThanOrEqual(width + 1)
    await page.keyboard.press('Escape')
    await expect(drawer).toBeHidden()
    await page.goto('/settings/secrets')
    await page.getByRole('button', { name: '使用 Secret', exact: true }).click()
    await expect(page.getByRole('dialog').getByText('实际 VM 引用')).toBeVisible()
    await expect(page.getByRole('dialog')).toContainText('login_binding')
    await expect(page.getByRole('dialog')).toContainText('v1')
    await page.keyboard.press('Escape')
    verifyBrowserEvidence(evidence)
  })
