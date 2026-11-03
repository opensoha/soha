import {
  test as base,
  expect,
  type Page,
  type BrowserContext,
  type Request,
} from '@playwright/test'
import { baselineResponse } from './baseline-data'
import { runContext } from '../../scripts/e2e-run-context.mjs'

export type BrowserEvidence = {
  fatal: string[]
  failures: string[]
  missing: string[]
  requests: string[]
}
type MockResponse = { body: unknown; status?: number } | undefined
export type MockResolver = (url: URL, method: string, request: Request) => MockResponse

export async function installMocks(context: BrowserContext, page: Page, resolve?: MockResolver) {
  const evidence: BrowserEvidence = { fatal: [], failures: [], missing: [], requests: [] }
  page.on('pageerror', () => evidence.fatal.push('pageerror'))
  page.on('console', (message) => {
    if (
      message.type() === 'error' &&
      /Uncaught|Failed to load module script|Minified React error|Error:.*React/i.test(
        message.text(),
      )
    )
      evidence.fatal.push('fatal console')
  })
  page.on('requestfailed', (request) => {
    if (!request.failure()?.errorText.includes('ERR_ABORTED'))
      evidence.failures.push(new URL(request.url()).pathname)
  })
  await context.route('**/*', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.origin !== runContext().baseURL) {
      evidence.missing.push('external origin blocked')
      return route.abort('blockedbyclient')
    }
    if (!url.pathname.startsWith('/api/')) return route.continue()
    evidence.requests.push(url.pathname + url.search)
    const selected = resolve?.(url, request.method(), request)
    const body = baselineResponse(url.pathname)
    const scoped =
      !url.pathname.includes('/workloads/deployments/api/') ||
      url.searchParams.get('namespace') === 'monitoring'
    const response =
      selected ?? (body && scoped && request.method() === 'GET' ? { body } : undefined)
    // Refresh is POST in the actual API client; mock authentication never proves real login.
    const refresh: MockResponse =
      url.pathname === '/api/v1/auth/refresh' && request.method() === 'POST' ? { body } : undefined
    if (!response && !refresh)
      evidence.missing.push(`${request.method()} ${url.pathname}${url.search}`)
    await route.fulfill({
      status: (response ?? refresh)?.status ?? (response || refresh ? 200 : 500),
      json: (response ?? refresh)?.body ?? {
        error: { code: 'mock_missing', message: 'Undefined mock' },
      },
    })
  })
  return evidence
}

export function verifyBrowserEvidence(evidence: BrowserEvidence) {
  expect(evidence.fatal, 'fatal console/runtime').toEqual([])
  expect(evidence.failures, 'network failures').toEqual([])
  expect(evidence.missing, 'missing mocks or forbidden egress').toEqual([])
}

export async function verifyPage(page: Page, route: string, texts: string[]) {
  await expect(page).toHaveURL(new URL(route, runContext().baseURL).href)
  await expect(page.locator('#root > *').first(), 'React root rendered').toBeVisible()
  for (const text of texts) await expect(page.locator('body')).toContainText(text)
}

export const test = base.extend<{ evidence: BrowserEvidence }>({
  evidence: [
    async ({ context, page }, use) => {
      const evidence = await installMocks(context, page)
      await use(evidence)
      verifyBrowserEvidence(evidence)
    },
    { auto: true },
  ],
})
export { expect }
