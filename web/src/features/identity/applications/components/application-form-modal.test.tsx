/** @vitest-environment jsdom */

import { act, type ComponentProps } from 'react'
import { App as AntdApp } from 'antd'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '@/i18n'
import type { IdentityApplication } from '../../shared/types'
import { ApplicationFormModal } from './application-form-modal'

const testState = vi.hoisted(() => ({
  enabled: {} as Record<string, boolean | undefined>,
  message: { error: vi.fn(), info: vi.fn() },
  selectChange: {} as Record<string, (value: unknown) => void>,
  permissionKeys: [] as string[],
  subjectControlled: false,
  subjectModes: {} as Record<string, string | undefined>,
  subjectTypeDisabled: undefined as boolean | undefined,
}))

vi.mock('@tanstack/react-query', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-query')>()
  return {
    ...actual,
    useQuery: (options: { enabled?: boolean; queryKey: readonly unknown[] }) => {
      const resource = String(options.queryKey[1])
      testState.enabled[resource] = options.enabled
      const data =
        resource === 'users'
          ? [{ id: 'user-1', displayName: 'Cached User', username: 'cached', email: '' }]
          : resource === 'roles'
            ? [{ id: 'role-1', name: 'Cached Role' }]
            : [{ id: 'team-1', name: 'Cached Team', path: '', slug: 'cached-team' }]
      return { data, isLoading: false }
    },
  }
})

vi.mock('@/features/auth', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, permission: string) =>
    snapshot?.permissionKeys?.includes(permission) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: testState.permissionKeys } },
    isError: false,
    isLoading: false,
  }),
}))

vi.mock('antd', async (importOriginal) => {
  const actual = await importOriginal<typeof import('antd')>()
  const ActualSelect = actual.Select
  return {
    ...actual,
    App: Object.assign(actual.App, { useApp: () => ({ message: testState.message }) }),
    Select: (props: ComponentProps<typeof ActualSelect>) => {
      const selectId = props.id || props['aria-label']
      if (selectId && props.onChange)
        testState.selectChange[selectId] = props.onChange as (value: unknown) => void
      if (typeof props.placeholder === 'string' && props.placeholder.includes('（可多选）')) {
        testState.subjectControlled =
          Array.isArray(props.value) && typeof props.onChange === 'function'
        testState.subjectModes[props.placeholder] = props.mode
      }
      if (props['aria-label'] === '主体类型') {
        testState.subjectTypeDisabled = props.disabled
      }
      return <ActualSelect {...props} />
    },
  }
})

const application: IdentityApplication = {
  id: 'app-1',
  slug: 'app-1',
  name: 'App 1',
  tags: [],
  providerType: 'link',
  portalVisible: true,
  featured: false,
  sortOrder: 0,
  status: 'enabled',
  assignments: [{ subjectType: 'role', subjectId: 'role-1', effect: 'allow' }],
  createdAt: '2026-08-18T00:00:00Z',
  updatedAt: '2026-08-18T00:00:00Z',
}

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

async function renderModal(
  subjectType: 'role' | 'tag' = 'role',
  subjectId = subjectType === 'tag' ? 'production' : 'role-1',
) {
  await act(async () => {
    root.render(
      <I18nProvider>
        <AntdApp>
          <ApplicationFormModal
            application={{
              ...application,
              assignments: [
                {
                  subjectType,
                  subjectId,
                  effect: 'allow',
                },
              ],
            }}
            open
            providerOptions={[]}
            providerOptionsLoading={false}
            saving={false}
            stepUpAvailable
            tagOptions={[]}
            onCancel={() => undefined}
            onSubmit={() => undefined}
          />
        </AntdApp>
      </I18nProvider>,
    )
    await Promise.resolve()
  })
}

describe('ApplicationFormModal permissions', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockImplementation(() => ({
        matches: false,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    )
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    )
    Object.defineProperty(window, 'getComputedStyle', {
      writable: true,
      value: vi.fn().mockReturnValue({
        getPropertyValue: () => '',
        overflow: 'auto',
        overflowX: 'auto',
        overflowY: 'auto',
      }),
    })
  })

  beforeEach(() => {
    testState.enabled = {}
    testState.selectChange = {}
    testState.permissionKeys = []
    testState.subjectControlled = false
    testState.subjectModes = {}
    testState.subjectTypeDisabled = undefined
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => {
      root.unmount()
      // Ant Design defers clearing field errors by 10 ms without cancelling on unmount.
      await new Promise((resolve) => setTimeout(resolve, 20))
    })
    container.remove()
  })

  it('disables and masks assignment lookups without their exact view permissions', async () => {
    await renderModal()

    expect(testState.enabled).toEqual({ outposts: false, users: false, roles: false, teams: false })
    expect(document.body.textContent).not.toContain('Cached Role')
  })

  it('enables the team lookup with the group view permission', async () => {
    testState.permissionKeys = ['access.groups.view']

    await renderModal()

    expect(testState.enabled).toEqual({ outposts: false, users: false, roles: false, teams: true })
  })

  it('only allows free text for tag assignments', async () => {
    await renderModal()
    await renderModal('tag')

    expect(testState.subjectModes).toMatchObject({
      '选择角色（可多选）': 'multiple',
      '输入标签（可多选）': 'tags',
    })
    expect(testState.subjectControlled).toBe(true)
  })

  it('locks the subject type after selecting an object and unlocks it when empty', async () => {
    await renderModal('role', '')
    expect(testState.subjectTypeDisabled).toBe(false)

    await renderModal()
    expect(testState.subjectTypeDisabled).toBe(true)
  })

  it('keeps publishing controls before an unboxed access control section', async () => {
    await renderModal()

    const modal = document.body.querySelector('.ant-modal')
    const content = modal?.textContent ?? ''
    expect(modal?.querySelector('.ant-card')).toBeNull()
    expect(modal?.querySelectorAll('.soha-identity-publish-controls .ant-switch')).toHaveLength(3)
    expect(modal?.querySelector('.soha-identity-publish-controls .ant-input-number')).toBeNull()
    expect(modal?.querySelectorAll('.soha-identity-policy-time-window .ant-picker')).toHaveLength(2)
    expect(modal?.querySelector('input[type="time"]')).toBeNull()
    expect(content.indexOf('门户可见')).toBeLessThan(content.indexOf('访问控制'))
    expect(content.indexOf('推荐应用')).toBeLessThan(content.indexOf('访问控制'))
    expect(modal?.querySelector('[aria-label="访问控制说明"]')).not.toBeNull()
    expect(modal?.querySelector('[aria-label="访问条件说明"]')).not.toBeNull()
    expect(content).not.toContain('每行只配置一种主体类型')
    expect(content).not.toContain('通过访问对象校验后')
  })
  async function renderWizard(canConfigureProvider = true) {
    const onOnboard = vi.fn()
    const onCancel = vi.fn()
    await act(async () =>
      root.render(
        <I18nProvider>
          <AntdApp>
            <ApplicationFormModal
              application={null}
              open
              providerOptions={[]}
              providerOptionsLoading={false}
              saving={false}
              stepUpAvailable
              tagOptions={[]}
              onCancel={onCancel}
              onSubmit={vi.fn()}
              onOnboard={onOnboard}
              canConfigureProvider={canConfigureProvider}
              samlAvailable
            />
          </AntdApp>
        </I18nProvider>,
      ),
    )
    return { onOnboard, onCancel }
  }

  async function input(id: string, value: string) {
    const node = document.getElementById(id)
    if (!(node instanceof HTMLInputElement)) throw new Error('Input not found: ' + id)
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(node, value)
      node.dispatchEvent(new Event('input', { bubbles: true }))
    })
  }

  async function select(id: string, value: unknown) {
    if (!testState.selectChange[id]) throw new Error('Select not found: ' + id)
    await act(async () => testState.selectChange[id](value))
  }

  async function click(label: string) {
    const button = Array.from(document.querySelectorAll('button')).find(
      (node) => node.textContent?.replace(/\s/g, '') === label,
    )
    if (!button)
      throw new Error(
        'Button not found: ' +
          label +
          '; errors: ' +
          Array.from(
            document.querySelectorAll(
              '.ant-form-item-explain-error, .ant-message-notice-content, .ant-steps-item-active',
            ),
          )
            .map((item) => item.textContent)
            .join('; '),
      )
    await act(async () => {
      button.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  }

  it.each(['link', 'oidc', 'saml', 'proxy'])(
    'collects %s in the real form and submits once with explicit scope and disabled status',
    async (type) => {
      const { onOnboard } = await renderWizard()
      await input('name', 'Example App')
      await input('slug', 'example-app')
      await select('providerType', type)
      await click('下一步')
      if (type === 'oidc') await input('redirectRules_0_value', 'https://app.example.com/callback')
      if (type === 'saml') {
        await input('samlEntityId', 'https://app.example.com/saml')
        await select('samlAcsUrls', ['https://app.example.com/acs'])
      }
      if (type === 'proxy') await select('proxyExternalHosts', ['app.example.com'])
      if (type !== 'link') await click('下一步')
      await click('下一步')
      expect(onOnboard).not.toHaveBeenCalled()
      await select('访问范围', 'all_authenticated')
      await click('下一步')
      expect(onOnboard).not.toHaveBeenCalled()
      await click('保存并查看接入说明')
      expect(onOnboard).toHaveBeenCalledTimes(1)
      const payload = onOnboard.mock.calls[0][0]
      expect(payload).toMatchObject({
        accessMode: 'all_authenticated',
        application: {
          name: 'Example App',
          slug: 'example-app',
          providerType: type,
          status: 'disabled',
          assignments: [],
        },
      })
      if (type === 'link') expect(payload.provider).toBeUndefined()
      else expect(payload.provider).toMatchObject({ applicationId: '', type })
      if (type === 'oidc')
        expect(payload.oidcClient).toMatchObject({
          providerId: '',
          redirectUris: ['https://app.example.com/callback'],
          requirePkce: true,
        })
    },
  )

  it('cancels collected data without submitting and allows a draft without provider permission', async () => {
    const { onOnboard, onCancel } = await renderWizard(false)
    await input('name', 'Later')
    await input('slug', 'later')
    await select('providerType', 'proxy')
    await click('下一步')
    expect(document.body.textContent).toContain('缺少创建认证接入的权限')
    expect(testState.enabled.outposts).toBe(false)
    await click('取消')
    expect(onCancel).toHaveBeenCalledTimes(1)
    expect(onOnboard).not.toHaveBeenCalled()
    await click('下一步')
    await select('访问范围', 'all_authenticated')
    await click('下一步')
    await click('保存并查看接入说明')
    expect(onOnboard.mock.calls[0][0].provider).toBeUndefined()
    expect(onOnboard.mock.calls[0][0].application.status).toBe('disabled')
  })
})
