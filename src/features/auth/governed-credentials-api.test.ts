import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/services/api-client'
import type {
  CreatedPersonalAccessToken,
  CreatedServiceAccountToken,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { createGatewayCredential, rotateGatewayCredential } from './governed-credentials-api'

vi.mock('@/services/api-client', () => ({ api: { get: vi.fn(), post: vi.fn() } }))
const binding = { mode: 'gateway_only', aiClientId: 'codex', skillId: 'k8s-sre' }

describe('governed credential compatibility boundary', () => {
  beforeEach(() => vi.resetAllMocks())
  for (const serviceAccount of [false, true]) {
    for (const rotate of [false, true]) {
      const resource = serviceAccount ? 'service-account-tokens' : 'personal-access-tokens'
      const createPath = serviceAccount
        ? '/ai-gateway/service-accounts/ci/tokens'
        : `/ai-gateway/${resource}`
      const expectedPath = rotate ? `/ai-gateway/${resource}/previous/rotate` : createPath
      for (const scenario of [
        'supported',
        'missing-capability',
        'capability-error',
        'old-instance',
        'wrong-prefix',
        'changed-binding',
        'ordinary',
      ]) {
        it(`${resource} ${rotate ? 'rotation' : 'creation'}: ${scenario}`, async () => {
          const metadata = scenario === 'ordinary' ? {} : { governedAI: binding }
          vi.mocked(api.get).mockImplementation(async (path) => {
            if (path === '/auth/cli/options') {
              if (scenario === 'capability-error') throw new Error('unavailable')
              return { data: scenario === 'missing-capability' ? {} : { supportsGovernedAI: true } }
            }
            return { data: [{ id: 'previous', metadata }] }
          })
          vi.mocked(api.post).mockImplementation(async (path) => {
            if (path.endsWith('/revoke')) return { status: 'ok' }
            if (scenario === 'old-instance') throw new Error('not found')
            const prefix =
              scenario === 'wrong-prefix'
                ? 'soha_pat_test'
                : serviceAccount
                  ? 'soha_gsat_test'
                  : 'soha_gpat_test'
            return {
              data: {
                token: {
                  id: 'new',
                  tokenPrefix: prefix,
                  metadata:
                    scenario === 'changed-binding'
                      ? { governedAI: { ...binding, aiClientId: 'other' } }
                      : metadata,
                },
                value: prefix + '-synthetic-secret',
              },
            }
          })
          const action = () =>
            rotate
              ? rotateGatewayCredential<CreatedPersonalAccessToken | CreatedServiceAccountToken>(
                  'previous',
                  undefined,
                  serviceAccount,
                )
              : createGatewayCredential<CreatedPersonalAccessToken | CreatedServiceAccountToken>(
                  createPath,
                  { name: 'test', metadata },
                  serviceAccount,
                )
          if (scenario === 'supported' || scenario === 'ordinary')
            await expect(action()).resolves.toBeDefined()
          else await expect(action()).rejects.not.toThrow('synthetic-secret')
          if (scenario === 'missing-capability' || scenario === 'capability-error') {
            expect(api.post).not.toHaveBeenCalled()
          } else {
            const target =
              scenario === 'ordinary'
                ? expectedPath
                : expectedPath.replace('/ai-gateway/', '/ai-gateway/governed-credentials/')
            expect(api.post).toHaveBeenNthCalledWith(
              1,
              target,
              rotate ? undefined : { name: 'test', metadata },
            )
          }
          if (scenario !== 'ordinary')
            expect(api.post).not.toHaveBeenCalledWith(expectedPath, expect.anything())
          if (scenario === 'wrong-prefix' || scenario === 'changed-binding') {
            expect(api.post).toHaveBeenLastCalledWith(`/ai-gateway/${resource}/new/revoke`)
          }
          if (scenario === 'ordinary') expect(api.get).not.toHaveBeenCalledWith('/auth/cli/options')
        })
      }
    }
  }
})
