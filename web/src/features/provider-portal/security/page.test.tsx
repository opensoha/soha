/** @vitest-environment jsdom */

import { ApiError } from '@/services/api-error'

import { useAuthStore } from '@/stores/auth-store'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { PortalSecurityPage } from './page'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), delete: vi.fn() }))
const capabilityState = vi.hoisted(() => ({ webauthn: false, stepUp: false, totp: false }))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const security = {
  principal: {
    userId: 'user-1',
    userName: 'admin',
    email: 'admin@example.test',
    roles: ['platform-admin'],
    teams: ['operations'],
    projects: [],
    tags: ['on-call'],
  },
  mfaEnabled: true,
  linkedSources: ['oidc'],
  activeSession: 2,
  recentLoginAt: '2026-01-01T00:00:00Z',
}

const mountedRoots: Root[] = []

beforeAll(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn(() => ({
      matches: false,
      media: '',
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

beforeEach(() => {
  vi.clearAllMocks()
  useAuthStore.setState({
    user: {
      userId: 'user-1',
      userName: 'admin',
      email: 'admin@example.test',
      avatarUrl: 'https://example.test/avatar.png',
      avatarFit: 'contain',
      roles: ['platform-admin'],
      teams: ['operations'],
      projects: [],
      tags: ['on-call'],
    },
  })
  capabilityState.totp = false
  capabilityState.webauthn = false
  capabilityState.stepUp = false
  Object.defineProperty(window, 'PublicKeyCredential', {
    configurable: true,
    value: undefined,
  })
  Object.defineProperty(navigator, 'credentials', {
    configurable: true,
    value: undefined,
  })
  apiMocks.get.mockImplementation(async (path: string) => {
    if (path === '/portal/security') return { data: security }
    if (path === '/identity/capabilities') {
      return {
        data: {
          samlApplicationProvider: { available: false, status: 'unavailable' },
          samlLoginSource: { available: false, status: 'unavailable' },
          totp: { available: capabilityState.totp, status: 'available' },
          webauthn: {
            available: capabilityState.webauthn,
            status: capabilityState.webauthn ? 'available' : 'unavailable',
          },
          recoveryCodes: { available: false, status: 'unavailable' },
          stepUp: {
            available: capabilityState.stepUp,
            status: capabilityState.stepUp ? 'available' : 'unavailable',
          },
          outpost: {
            controlPlane: { available: true, status: 'available' },
            embeddedRuntime: { available: true, status: 'available' },
            agentRuntime: { available: false, status: 'unavailable' },
            kubernetesArtifact: { available: false, status: 'unavailable' },
            externalProtocol: { available: false, status: 'unavailable' },
          },
        },
      }
    }
    if (path === '/identity/mfa/credentials') return { items: [] }
    throw new Error(`Unhandled GET ${path}`)
  })
})

afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  useAuthStore.getState().clearAuth()
  document.body.innerHTML = ''
})

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

describe('Provider Portal security page', () => {
  it('renders the unwrapped security summary and principal memberships', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    mountedRoots.push(root)
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    await act(async () => {
      root.render(
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <MemoryRouter initialEntries={['/portal/security']}>
              <PortalSecurityPage />
            </MemoryRouter>
          </QueryClientProvider>
        </AntdApp>,
      )
    })
    await flushAsyncWork()

    expect(apiMocks.get).toHaveBeenCalledWith('/portal/security')
    expect(container.textContent).toContain('admin@example.test')
    expect(container.textContent).toContain('platform-admin')
    expect(container.textContent).toContain('operations')
    expect(container.textContent).toContain('on-call')
    expect(container.textContent).toContain('oidc')
    expect(
      container
        .querySelector('.soha-portal-principal .soha-portal-user-avatar img')
        ?.getAttribute('src'),
    ).toBe('https://example.test/avatar.png')
    expect(container.querySelector('button[aria-label="Account menu"]')).not.toBeNull()
  })

  it.each(['/identity/capabilities', '/identity/mfa/credentials'])(
    'keeps a visible explanation when MFA dependency %s is denied',
    async (path) => {
      capabilityState.totp = true
      const original = apiMocks.get.getMockImplementation()!
      apiMocks.get.mockImplementation((p: string) =>
        p === path
          ? Promise.reject(new ApiError(403, 'MFA dependency refused', { method: 'GET' }))
          : original(p),
      )
      const container = document.createElement('div')
      document.body.appendChild(container)
      const root = createRoot(container)
      mountedRoots.push(root)
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      await act(async () =>
        root.render(
          <AntdApp>
            <QueryClientProvider client={queryClient}>
              <MemoryRouter>
                <PortalSecurityPage />
              </MemoryRouter>
            </QueryClientProvider>
          </AntdApp>,
        ),
      )
      await flushAsyncWork()
      await vi.waitFor(async () => {
        await flushAsyncWork()
        expect(container.textContent).toContain('MFA dependency refused')
      })
      expect(container.textContent).toContain(
        path.endsWith('capabilities') ? 'MFA 运行能力无访问权限' : 'MFA 凭据无访问权限',
      )
      expect(container.textContent).toContain(security.principal.email)
    },
  )

  it('does not reveal a late TOTP challenge after a denied security read', async () => {
    capabilityState.totp = true
    let resolveChallenge!: (value: unknown) => void
    apiMocks.post.mockReturnValue(
      new Promise((resolve) => {
        resolveChallenge = resolve
      }),
    )
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    mountedRoots.push(root)
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    await act(async () =>
      root.render(
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <MemoryRouter>
              <PortalSecurityPage />
            </MemoryRouter>
          </QueryClientProvider>
        </AntdApp>,
      ),
    )
    await flushAsyncWork()
    const button = [...container.querySelectorAll('button')].find(
      (b) => b.textContent === 'Add authenticator',
    )
    expect(button).toBeDefined()
    await act(async () => button?.click())
    apiMocks.get.mockImplementation(async () => {
      throw new ApiError(403, 'Access denied', { method: 'GET' })
    })
    await act(async () => {
      await queryClient.refetchQueries()
    })
    await flushAsyncWork()
    await act(async () =>
      resolveChallenge({
        data: {
          challengeId: 'late-challenge',
          secret: 'private-totp-canary',
          provisioningUri: 'otpauth://private-totp-canary',
        },
      }),
    )
    await flushAsyncWork()
    expect(document.body.querySelector('.ant-modal')).toBeNull()
    expect(document.body.textContent).not.toContain('private-totp-canary')
    expect(
      JSON.stringify(
        queryClient
          .getMutationCache()
          .getAll()
          .map((m) => m.state.data),
      ),
    ).not.toContain('private-totp-canary')
    expect(apiMocks.post).toHaveBeenCalledOnce()
  })

  it('authenticates a step-up assertion with exact server WebAuthn options', async () => {
    capabilityState.webauthn = true
    capabilityState.stepUp = true
    Object.defineProperty(window, 'PublicKeyCredential', {
      configurable: true,
      value: class PublicKeyCredential {},
    })
    const getCredential = vi.fn().mockResolvedValue({
      id: 'credential-1',
      response: {
        clientDataJSON: Uint8Array.from([7, 8]).buffer,
        authenticatorData: Uint8Array.from([9, 10]).buffer,
        signature: Uint8Array.from([11, 12]).buffer,
        userHandle: Uint8Array.from([13, 14]).buffer,
      },
    })
    Object.defineProperty(navigator, 'credentials', {
      configurable: true,
      value: { get: getCredential, create: vi.fn() },
    })
    apiMocks.post.mockImplementation(async (path: string) => {
      if (path === '/identity/mfa/webauthn/authenticate') {
        return {
          data: {
            challengeId: 'challenge-1',
            challenge: 'AQID',
            rpId: 'soha.example.test',
            timeoutMilliseconds: 60_000,
            userVerification: 'required',
            allowCredentialIds: ['BAUG'],
            expiresAt: '2026-07-27T00:01:00Z',
          },
        }
      }
      if (path === '/identity/mfa/webauthn/challenges/challenge-1/verify') {
        return { data: { verified: true } }
      }
      throw new Error(`Unhandled POST ${path}`)
    })

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    mountedRoots.push(root)
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    await act(async () => {
      root.render(
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <MemoryRouter initialEntries={['/portal/security']}>
              <PortalSecurityPage />
            </MemoryRouter>
          </QueryClientProvider>
        </AntdApp>,
      )
    })
    await flushAsyncWork()

    const verifyButton = [...container.querySelectorAll('button')].find(
      (button) => button.textContent === 'Verify passkey',
    )
    expect(verifyButton?.hasAttribute('disabled')).toBe(false)
    await act(async () => verifyButton?.click())
    await flushAsyncWork()

    expect(apiMocks.post).toHaveBeenCalledWith('/identity/mfa/webauthn/authenticate', {
      purpose: 'step_up',
    })
    const publicKey = getCredential.mock.calls[0]?.[0]?.publicKey
    expect(Array.from(publicKey.challenge)).toEqual([1, 2, 3])
    expect(publicKey.rpId).toBe('soha.example.test')
    expect(publicKey.timeout).toBe(60_000)
    expect(publicKey.userVerification).toBe('required')
    expect(Array.from(publicKey.allowCredentials[0].id)).toEqual([4, 5, 6])
    expect(publicKey.allowCredentials[0].type).toBe('public-key')
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/identity/mfa/webauthn/challenges/challenge-1/verify',
      {
        credentialId: 'credential-1',
        clientDataJSON: 'Bwg',
        authenticatorData: 'CQo',
        signature: 'Cww',
        userHandle: 'DQ4',
      },
    )
  })
})
