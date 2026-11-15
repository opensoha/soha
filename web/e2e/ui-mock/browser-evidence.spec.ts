import { test, expect } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'

test('browser evidence keeps runtime details and rejects only the existing fatal console patterns', async ({
  context,
  page,
}) => {
  const evidence = await installMocks(context, page)
  await page.goto('/clusters')
  await expect(page.locator('#root > *').first()).toBeVisible()
  await page.evaluate(() => console.error('ordinary diagnostic console message'))
  verifyBrowserEvidence(evidence)

  await page.evaluate(() => {
    setTimeout(() => {
      throw new TypeError('browser-evidence-runtime-probe')
    }, 0)
  })
  await expect.poll(() => evidence.fatal.length).toBe(1)
  const runtime = JSON.parse(evidence.fatal[0].slice('pageerror '.length))
  expect(runtime).toMatchObject({
    name: 'TypeError',
    message: 'browser-evidence-runtime-probe',
    url: page.url(),
  })
  expect(runtime.stack).toContain('browser-evidence-runtime-probe')
  expect(Number.isNaN(Date.parse(runtime.time))).toBe(false)
  expect(() => verifyBrowserEvidence(evidence)).toThrow('browser-evidence-runtime-probe')

  const message = 'Uncaught Error: browser-evidence-console-probe'
  await page.evaluate((text) => console.error(text), message)
  await expect.poll(() => evidence.fatal.length).toBe(2)
  const fatalConsole = JSON.parse(evidence.fatal[1].slice('fatal console '.length))
  expect(fatalConsole).toMatchObject({ message, url: page.url() })
  expect(fatalConsole.location).toMatchObject({
    url: expect.any(String),
    lineNumber: expect.any(Number),
    columnNumber: expect.any(Number),
  })
  expect(Number.isNaN(Date.parse(fatalConsole.time))).toBe(false)
  expect(() => verifyBrowserEvidence({ ...evidence, fatal: [evidence.fatal[1]] })).toThrow(message)
})
