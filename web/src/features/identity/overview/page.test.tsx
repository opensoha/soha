/** @vitest-environment jsdom */

import type { SystemIntegrationCategory } from '@/features/settings'
import type { AuditLog, OnlineUser } from '@/features/system'
import { act, type ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IdentityOutpost } from '../outposts'
import type { IdentityProvider } from '../providers'
import type { IdentityApplication } from '../shared/types'
import { IdentityOverviewPage } from './page'
import { useIdentityOverviewData } from './use-overview-data'

const navigateMock = vi.hoisted(() => vi.fn())

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => navigateMock }
})

vi.mock('@ant-design/icons', () => {
  const Icon = () => <span aria-hidden="true" />
  return {
    ApiOutlined: Icon,
    AppstoreOutlined: Icon,
    AuditOutlined: Icon,
    CheckCircleOutlined: Icon,
    CloudServerOutlined: Icon,
    DatabaseOutlined: Icon,
    FileOutlined: Icon,
    KeyOutlined: Icon,
    LinkOutlined: Icon,
    SafetyCertificateOutlined: Icon,
    UserSwitchOutlined: Icon,
  }
})

vi.mock('antd', async (importOriginal) => ({
  ...(await importOriginal<typeof import('antd')>()),
  Button: ({
    children,
    disabled,
    onClick,
  }: {
    children?: ReactNode
    disabled?: boolean
    onClick?: () => void
  }) => (
    <button disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
  Card: ({
    children,
    extra,
    title,
  }: {
    children?: ReactNode
    extra?: ReactNode
    title?: ReactNode
  }) => (
    <section>
      <h2>{title}</h2>
      {extra}
      {children}
    </section>
  ),
  Space: ({ children }: { children?: ReactNode }) => <>{children}</>,
  Tag: ({
    children,
    className,
    color,
  }: {
    children?: ReactNode
    className?: string
    color?: string
  }) => (
    <span className={className} data-color={color}>
      {children}
    </span>
  ),
  Typography: {
    Text: ({ children }: { children?: ReactNode }) => <span>{children}</span>,
  },
}))

vi.mock('@/components/management-list', () => ({
  ManagementState: ({ title }: { title?: ReactNode }) => <div>{title}</div>,
}))

vi.mock('@/components/overview-visuals', () => ({
  OverviewChip: ({
    helper,
    label,
    tone,
    value,
  }: {
    helper?: ReactNode
    label?: ReactNode
    tone?: string
    value?: ReactNode
  }) => (
    <div data-testid={`chip-${String(label)}`} data-tone={tone}>
      {label}:{value}:{helper}
    </div>
  ),
  OverviewHeroCard: ({
    eyebrowLabel,
    value,
    caption,
    loading,
    ctaLabel,
    ctaTo,
    embeddedPods,
    graphicDeck,
  }: {
    eyebrowLabel?: ReactNode
    value?: ReactNode
    caption?: ReactNode
    loading?: boolean
    ctaLabel?: string
    ctaTo?: string
    embeddedPods?: Array<{
      key: string
      label?: ReactNode
      value?: ReactNode
      helper?: ReactNode
      loading?: boolean
      tone?: string
    }>
    graphicDeck?: Array<{
      key: string
      actionLabel?: string
      actionTo?: string
    }>
  }) => (
    <div>
      <div
        data-testid={`metric-${String(eyebrowLabel)}`}
        data-loading={String(Boolean(loading))}
      >
        {eyebrowLabel}:{value}:{caption}
      </div>
      {ctaLabel && ctaTo ? (
        <button onClick={() => navigateMock(ctaTo)}>{ctaLabel}</button>
      ) : null}
      {graphicDeck?.map((deck) =>
        deck.actionLabel && deck.actionTo ? (
          <button key={deck.key} onClick={() => navigateMock(deck.actionTo)}>
            {deck.actionLabel}
          </button>
        ) : null,
      )}
      {embeddedPods?.map((pod) => (
        <div
          key={pod.key}
          data-testid={`metric-${String(pod.label)}`}
          data-loading={String(Boolean(pod.loading))}
          data-tone={pod.tone}
        >
          {pod.label}:{pod.value}:{pod.helper}
        </div>
      ))}
    </div>
  ),
  OverviewMetricCard: ({
    helper,
    label,
    loading,
    tone,
    value,
  }: {
    helper?: ReactNode
    label?: ReactNode
    loading?: boolean
    tone?: string
    value?: ReactNode
  }) => (
    <div
      data-testid={`metric-${String(label)}`}
      data-loading={String(Boolean(loading))}
      data-tone={tone}
    >
      {label}:{value}:{helper}
    </div>
  ),
  OverviewSectionBar: ({
    description,
    extra,
    title,
  }: {
    description?: ReactNode
    extra?: ReactNode
    title?: ReactNode
  }) => (
    <div>
      {title}:{description}
      {extra}
    </div>
  ),
}))

vi.mock('./use-overview-data', () => ({
  useIdentityOverviewData: vi.fn(),
}))

const applications: IdentityApplication[] = [
  {
    id: 'application-1',
    slug: 'grafana',
    name: 'Grafana',
    tags: [],
    providerType: 'link',
    portalVisible: true,
    featured: false,
    sortOrder: 1,
    status: 'enabled',
    createdAt: '2026-07-08T00:00:00Z',
    updatedAt: '2026-07-09T00:00:00Z',
  },
  {
    id: 'application-2',
    slug: 'harbor',
    name: 'Harbor',
    tags: [],
    providerType: 'link',
    portalVisible: true,
    featured: false,
    sortOrder: 2,
    status: 'disabled',
    createdAt: '2026-07-10T00:00:00Z',
    updatedAt: 'invalid-date',
  },
]

const providers: IdentityProvider[] = [
  {
    id: 'provider-1',
    applicationId: 'application-1',
    name: 'Grafana OIDC',
    type: 'oidc',
    enabled: true,
    status: 'enabled',
    createdAt: '2026-07-08T00:00:00Z',
    updatedAt: '2026-07-09T00:00:00Z',
  },
  {
    id: 'provider-2',
    applicationId: 'application-1',
    name: 'Backup OIDC',
    type: 'oidc',
    enabled: false,
    status: 'disabled',
    createdAt: '2026-07-08T00:00:00Z',
    updatedAt: '2026-07-09T00:00:00Z',
  },
  {
    id: 'provider-3',
    applicationId: 'application-2',
    name: 'Harbor Proxy',
    type: 'proxy',
    enabled: true,
    status: 'disabled',
    createdAt: '2026-07-08T00:00:00Z',
    updatedAt: '2026-07-09T00:00:00Z',
  },
]

const outposts: IdentityOutpost[] = [
  {
    id: 'outpost-1',
    name: 'Online',
    mode: 'embedded',
    status: 'online',
    runtimeStatus: 'available',
    configurationVersion: 0,
    createdAt: '2026-07-08T00:00:00Z',
    updatedAt: '2026-07-09T00:00:00Z',
  },
  {
    id: 'outpost-2',
    name: 'Offline',
    mode: 'agent',
    status: 'offline',
    runtimeStatus: 'unavailable',
    configurationVersion: 0,
    createdAt: '2026-07-08T00:00:00Z',
    updatedAt: '2026-07-09T00:00:00Z',
  },
]

const sessions: OnlineUser[] = [
  {
    id: 'session-1',
    userId: 'user-1',
    userName: 'Yamabuki',
    email: 'yamabuki@example.com',
    providerType: 'oidc',
    status: 'active',
    loginTime: '2026-07-10T00:00:00Z',
    lastSeenAt: '2026-07-10T00:05:00Z',
    expiry: '2026-07-10T08:00:00Z',
  },
  {
    id: 'session-2',
    userId: 'user-2',
    userName: 'Soha',
    email: 'soha@example.com',
    providerType: 'proxy',
    status: 'active',
    loginTime: '2026-07-10T00:00:00Z',
    lastSeenAt: '2026-07-10T00:05:00Z',
    expiry: '2026-07-10T08:00:00Z',
  },
]

const audits: AuditLog[] = Array.from({ length: 8 }, (_, index) => ({
  id: `audit-${index}`,
  createdAt: `2026-07-10T00:0${index}:00Z`,
  actorId: `actor-${index}`,
  actorName: index === 1 ? '' : `Actor ${index}`,
  action: `action-${index}`,
  resourceKind: index === 2 ? 'provider-kind' : '',
  resourceName: index === 1 ? 'resource-name-1' : '',
  result: index === 0 ? 'success' : index === 1 ? 'denied' : 'unknown',
  summary: index === 0 ? 'summary-0' : '',
}))

const roots: Root[] = []
const containers: HTMLElement[] = []
const refreshAll = vi.fn()

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(useIdentityOverviewData).mockReturnValue({
    reads: Object.fromEntries(
      [
        'applications',
        'providers',
        'outposts',
        'sessions',
        'audits',
        'softwareStorage',
        'storageIntegrations',
        'runtime',
      ].map((key) => [
        key,
        {
          data: [],
          error: null,
          isEnabled: true,
          isError: false,
          isFetching: false,
          refetch: vi.fn(),
        },
      ]),
    ) as unknown as ReturnType<typeof useIdentityOverviewData>['reads'],
    applications,
    providers,
    outposts,
    sessions,
    audits,
    softwareStorage: {
      backend: 's3',
      objectCount: 3,
      totalBytes: 21 * 1024,
      items: [],
    },
    storageIntegrations: [
      {
        id: 'storage-1',
        category: 'storage' as SystemIntegrationCategory,
        providerType: 's3',
        name: 'Local MinIO',
        enabled: true,
        configuration: [],
        credentialKeys: [],
        healthStatus: 'healthy',
        version: 1,
        createdAt: '2026-07-08T00:00:00Z',
        updatedAt: '2026-07-09T00:00:00Z',
      },
    ],
    softwareError: false,
    runtime: undefined,
    loading: {
      applications: false,
      providers: false,
      outposts: false,
      sessions: false,
      software: false,
    },
    permissions: {
      applications: true,
      providers: true,
      outposts: true,
      sessions: true,
      audit: true,
      software: true,
      storageIntegrations: true,
    },
    refreshAll,
  })
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  for (const container of containers.splice(0)) container.remove()
})

async function renderPage() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)
  const root = createRoot(container)
  roots.push(root)
  await act(async () => {
    root.render(
      <AntdApp>
        <IdentityOverviewPage />
      </AntdApp>,
    )
  })
  return container
}

function buttonByText(container: HTMLElement, label: string) {
  const normalized = label.replace(/\s+/g, '')
  const button = Array.from(container.querySelectorAll('button')).find(
    (candidate) => candidate.textContent?.replace(/\s+/g, '') === normalized,
  )
  if (!(button instanceof HTMLButtonElement)) throw new Error(`Button not found: ${label}`)
  return button
}

async function clickButton(container: HTMLElement, label: string) {
  await act(async () => buttonByText(container, label).click())
}

describe('IdentityOverviewPage', () => {
  it('renders the spatial hero metrics and only the first six audit events', async () => {
    const container = await renderPage()
    const bentoTop = container.querySelector('.soha-overview-bento-top')
    const summaryGrid = container.querySelector('.soha-overview-summary-grid')

    expect(bentoTop?.nextElementSibling).toBe(summaryGrid)
    expect(summaryGrid?.textContent).toContain('最近审计')
    expect(summaryGrid?.textContent).toContain('软件库')

    expect(container.querySelector('[data-testid="metric-应用目录"]')?.textContent).toContain(
      '应用目录:2:1 个已启用',
    )
    expect(container.querySelector('[data-testid="metric-Provider"]')?.textContent).toContain(
      'Provider:3:1 个已启用 / 2 OIDC / 1 Proxy / 0 SAML',
    )
    expect(container.querySelector('[data-testid="metric-Outpost"]')?.textContent).toContain(
      'Outpost:2:1 个在线',
    )
    expect(container.querySelector('[data-testid="metric-活跃会话"]')?.textContent).toContain(
      '活跃会话:2',
    )
    expect(container.querySelector('[data-testid="chip-存储源"]')?.textContent).toContain(
      '存储源:1:S3',
    )
    expect(container.querySelector('[data-testid="chip-已启用"]')?.textContent).toContain(
      '已启用:1:1 个连接正常',
    )
    expect(container.querySelector('[data-testid="chip-文件数量"]')?.textContent).toContain(
      '文件数量:3:全部存储合计',
    )
    expect(container.querySelector('[data-testid="chip-已用空间"]')?.textContent).toContain(
      '已用空间:21 KiB:全部软件包占用',
    )
    expect(container.textContent).toContain('action-0')
    expect(container.textContent).toContain('action-5')
    expect(container.textContent).not.toContain('action-6')
    expect(container.textContent).not.toContain('action-7')
    expect(container.textContent).toContain('summary-0')
    expect(container.textContent).toContain('resource-name-1')
    expect(container.textContent).toContain('provider-kind')
    expect(container.textContent).toContain('actor-1')
    expect(container.querySelectorAll('.soha-status-tag')).toHaveLength(6)
    expect(container.querySelector('[data-color="success"]')?.textContent).toBe('成功')
    expect(container.querySelector('[data-color="error"]')?.textContent).toBe('denied')
    expect(container.textContent).not.toContain(
      '查看 Provider Portal、下游 Provider、活跃会话和身份审计的运行状态。',
    )
    expect(container.textContent).not.toContain('运行入口')
    expect(container.textContent).not.toContain('最近身份相关操作和协议访问记录')
  })

  it('keeps the application, provider, software, and audit navigation canonical', async () => {
    const container = await renderPage()
    const targets = [
      ['管理应用目录', '/identity/applications'],
      ['身份源', '/identity/providers'],
      ['软件库', '/internal-workbench/software'],
      ['审计', '/system/audit'],
    ] as const

    for (const [label, path] of targets) {
      await clickButton(container, label)
      expect(navigateMock).toHaveBeenLastCalledWith(path)
    }

    expect(navigateMock).not.toHaveBeenCalledWith('/identity/audit')
    expect(navigateMock).not.toHaveBeenCalledWith('/internal-workbench/applications')
  })

  it('preserves permission states without disabling card-level route links', async () => {
    vi.mocked(useIdentityOverviewData).mockReturnValue({
      reads: Object.fromEntries(
        [
          'applications',
          'providers',
          'outposts',
          'sessions',
          'audits',
          'softwareStorage',
          'storageIntegrations',
          'runtime',
        ].map((key) => [
          key,
          {
            data: [],
            error: null,
            isEnabled: false,
            isError: false,
            isFetching: false,
            refetch: vi.fn(),
          },
        ]),
      ) as unknown as ReturnType<typeof useIdentityOverviewData>['reads'],
      applications: [],
      providers: [],
      outposts: [],
      sessions: [],
      audits: [],
      softwareStorage: undefined,
      storageIntegrations: [],
      softwareError: false,
      runtime: undefined,
      loading: {
        applications: false,
        providers: false,
        outposts: false,
        sessions: false,
        software: false,
      },
      permissions: {
        applications: false,
        providers: false,
        outposts: false,
        sessions: false,
        audit: false,
        software: false,
        storageIntegrations: false,
      },
      refreshAll,
    })
    const container = await renderPage()

    expect(container.querySelector('[data-testid="metric-Provider"]')?.textContent).toContain(
      'Provider:—:当前数据不可用',
    )
    expect(container.textContent).toContain('无软件库权限')
    expect(container.textContent).toContain('无审计权限')
    expect(buttonByText(container, '身份源').disabled).toBe(false)
    expect(buttonByText(container, '审计').disabled).toBe(false)
  })

  it('keeps the authorized empty audit state distinct from no permission', async () => {
    vi.mocked(useIdentityOverviewData).mockReturnValue({
      reads: Object.fromEntries(
        [
          'applications',
          'providers',
          'outposts',
          'sessions',
          'audits',
          'softwareStorage',
          'storageIntegrations',
          'runtime',
        ].map((key) => [
          key,
          {
            data: [],
            error: null,
            isEnabled: true,
            isError: false,
            isFetching: false,
            refetch: vi.fn(),
          },
        ]),
      ) as unknown as ReturnType<typeof useIdentityOverviewData>['reads'],
      applications,
      providers,
      outposts,
      sessions,
      audits: [],
      softwareStorage: {
        backend: 's3',
        objectCount: 3,
        totalBytes: 21 * 1024,
        items: [],
      },
      storageIntegrations: [],
      softwareError: false,
      runtime: undefined,
      loading: {
        applications: false,
        providers: false,
        outposts: false,
        sessions: false,
        software: false,
      },
      permissions: {
        applications: true,
        providers: true,
        outposts: true,
        sessions: true,
        audit: true,
        software: true,
        storageIntegrations: false,
      },
      refreshAll,
    })
    const container = await renderPage()

    expect(container.textContent).toContain('暂无审计记录')
    expect(container.textContent).not.toContain('无审计权限')
  })
})
