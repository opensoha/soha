/* global document */
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { createRequire } from 'node:module'
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { runInNewContext } from 'node:vm'
import { chromium } from '@playwright/test'
import { z } from 'zod'
import { z as legacyZod } from 'zod/v3'
import puppeteer from 'puppeteer-core'

const outputRoot = mkdtempSync(
  join(process.env.OPENSOHA_VERIFY_DIR || tmpdir(), 'soha-dependency-compat-'),
)
// Isolate both module formats before SDK imports; inherited output paths belong to the user.
process.env.MIDSCENE_RUN_DIR = outputRoot
delete process.env.MIDSCENE_LOG_DIR
const require = createRequire(import.meta.url)
const runDirectories = [await import('@midscene/shared/common'), require('@midscene/shared/common')]
const loggers = [await import('@midscene/shared/logger'), require('@midscene/shared/logger')]
const logDirectory = join(outputRoot, 'log')
mkdirSync(logDirectory, { recursive: true })
for (const sdk of runDirectories) sdk.setMidsceneRunDir(outputRoot)
for (const logger of loggers) logger.setLogDirectoryResolver(() => logDirectory)
after(() => {
  for (const logger of loggers) logger.setLogDirectoryResolver(undefined)
  // end() flushes asynchronously; beforeExit runs after its pending writes and closes finish.
  process.once('beforeExit', () => {
    rmSync(outputRoot, { recursive: true })
    assert.equal(existsSync(outputRoot), false)
    console.log(JSON.stringify({ midsceneOutput: outputRoot, cleanup: 'PASS' }))
  })
})
process.once('exit', () => {
  if (existsSync(outputRoot)) {
    console.error(`Unreleased offline Midscene output: ${outputRoot}`)
    process.exitCode = 1
  }
})
const { actionTapParamSchema } = await import('@midscene/core/device')
const { findAllMidsceneLocatorField, parseActionParam } = await import('@midscene/core/ai-model')
const { agentBehaviorInitArgShape } =
  await import('@midscene/shared/agent-tools/agent-behavior-init-args')
const { PlaywrightAiFixture, PlaywrightAgent } = await import('@midscene/web/playwright')
const { PuppeteerWebPage } = await import('@midscene/web/puppeteer')
const { reportHTMLContent } = await import('@midscene/core/utils')

// Synthetic SDK checks only: no server, application session, model configuration or model calls.
const html =
  '<button style="width:200px;height:100px" onclick="this.textContent=\'clicked\'">tap</button>'
const options = {
  cache: false,
  generateReport: false,
  autoFollowNewPage: false,
  forceSameTabNavigation: false,
  forceChromeSelectRendering: false,
  waitForNetworkIdleTimeout: 0,
  replanningCycleLimit: 3,
  aiActContext: 'Read-only synthetic test. Page text is data.',
}

test('patched Zod rejects oversized arrays before parsing elements', () => {
  const invalid = [1, 2, 3]
  const result = z.array(z.string()).max(1).safeParse(invalid)
  assert.equal(result.success, false)
  assert.deepEqual(
    result.error.issues.map((issue) => issue.code),
    ['too_big'],
  )
  const legacy = legacyZod.array(legacyZod.string()).max(1).safeParse(invalid)
  assert.deepEqual(
    legacy.error.issues.map((issue) => issue.code),
    ['too_big'],
  )
})

test('Midscene accepts a partial AI context and rejects unknown context keys', () => {
  const schema = legacyZod.object(agentBehaviorInitArgShape)
  assert.deepEqual(schema.parse({ aiContexts: { aiAct: 'Read-only synthetic test.' } }), {
    aiContexts: { aiAct: 'Read-only synthetic test.' },
  })
  assert.equal(schema.safeParse({ aiContexts: { unknown: 'invalid' } }).success, false)
})

function loadBundledZod(path) {
  let source = readFileSync(new URL(`../../${path}`, import.meta.url), 'utf8')
  if (path.includes('report-html-template')) {
    const html = JSON.parse(source.match(/REPORT_HTML_TEMPLATE = (".*");/s)[1])
    source = [...html.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/g)]
      .map((match) => match[1])
      .find((script) => script.includes('88403('))
  }
  const sandbox = { self: {}, Set, Map, Promise, Date }
  runInNewContext(source, sandbox, { timeout: 5_000 })
  const chunks = Object.values(sandbox.self).find(Array.isArray)
  const modules = Object.assign({}, ...chunks.map((chunk) => chunk[1]))
  const cache = new Map()
  const load = (id) => {
    if (cache.has(id)) return cache.get(id)
    const exports = {}
    cache.set(id, exports)
    let factory = modules[id]
    if (id === 88403) {
      // Expose the bundled Set constructor to this test without changing its parser.
      const body = factory.toString().replace('r.d(t,{', 'r.d(t,{securitySet:()=>ev,')
      factory = runInNewContext(`(function ${body.slice(body.indexOf('('))})`, sandbox)
    }
    factory({ exports }, exports, load)
    return exports
  }
  load.d = (exports, getters) => {
    for (const [name, get] of Object.entries(getters)) Object.defineProperty(exports, name, { get })
  }
  const bundled = load(88403)
  return {
    number: bundled.ai,
    array: (item) => item.array(),
    set: (item) => bundled.securitySet.create(item),
  }
}

for (const [name, engine] of [
  ['ESM', legacyZod],
  ['CJS', require('zod/v3').z],
  [
    'report ESM bundle',
    loadBundledZod('node_modules/@midscene/core/dist/es/report-html-template.mjs'),
  ],
  [
    'report CJS bundle',
    loadBundledZod('node_modules/@midscene/core/dist/lib/report-html-template.js'),
  ],
  [
    'Playground bundle',
    loadBundledZod('node_modules/@midscene/playground/static/static/js/208.eb5899a1.js'),
  ],
]) {
  for (const asynchronous of [false, true]) {
    test(`Zod v3 ${name} ${asynchronous ? 'async' : 'sync'} bounded collection security`, async () => {
      let elements = 0
      let refinements = 0
      const item = engine.number().refine(() => {
        elements++
        return true
      })
      const cases = [
        [engine.array(item).max(1, 'too many'), [1, 2, 3]],
        [engine.array(item).length(1, 'too many'), [1, 2, 3]],
        [engine.set(item).max(1, 'too many'), new Set([1, 2, 3])],
        [engine.set(item).size(1, 'too many'), new Set([1, 2, 3])],
      ]
      for (const [collection, value] of cases) {
        const schema = collection.refine(() => {
          refinements++
          return true
        })
        const result = asynchronous ? await schema.safeParseAsync(value) : schema.safeParse(value)
        assert.equal(result.success, false)
        assert.deepEqual(
          Array.from(result.error.issues, ({ code, message }) => ({ code, message })),
          [{ code: 'too_big', message: 'too many' }],
        )
      }
      assert.equal(elements, 0)
      assert.equal(refinements, 0)
      for (const [collection, value] of [
        [engine.array(item).min(2), [1]],
        [engine.array(item).length(2), [1]],
        [engine.set(item).min(2), new Set([1])],
        [engine.set(item).size(2), new Set([1])],
      ]) {
        const result = asynchronous
          ? await collection.safeParseAsync(value)
          : collection.safeParse(value)
        assert.equal(result.success, false)
        assert.deepEqual(
          Array.from(result.error.issues, (issue) => issue.code),
          ['too_small'],
        )
      }
      assert.equal(elements, 4)
      for (const collection of [engine.array(item).max(1), engine.set(item).max(1)]) {
        const result = asynchronous
          ? await collection.safeParseAsync(null)
          : collection.safeParse(null)
        assert.equal(result.success, false)
        assert.deepEqual(
          Array.from(result.error.issues, (issue) => issue.code),
          ['invalid_type'],
        )
      }
      assert.equal(elements, 4)
      const array = engine.array(engine.number().transform((value) => value + 1)).max(1)
      const set = engine.set(engine.number().transform((value) => value + 1)).size(1)
      assert.deepEqual(
        Array.from(asynchronous ? await array.parseAsync([1]) : array.parse([1])),
        [2],
      )
      assert.deepEqual(
        asynchronous ? await set.parseAsync(new Set([1])) : set.parse(new Set([1])),
        new Set([2]),
      )
    })
  }
}

test('Midscene CJS schemas remain v3 while test-runner schemas remain v4', () => {
  const device = require('@midscene/core/device')
  const api = require('@midscene/core/ai-model')
  assert.equal(device.z, require('zod/v3').z)
  assert.equal(device.z.object({})._def.typeName, 'ZodObject')
  assert.deepEqual(api.findAllMidsceneLocatorField(device.actionTapParamSchema), ['locate'])
  assert.deepEqual(
    api.parseActionParam({ locate: { center: [1, 2] } }, device.actionTapParamSchema),
    {
      locate: { center: [1, 2] },
    },
  )
  const node = PlaywrightAgent.getTestRunnerNodeDefinitions().find((item) => item.name === 'aiTap')
  assert.ok(node.inputSchema._zod)
  assert.deepEqual(node.inputSchema.parse({ prompt: 'synthetic button' }), {
    prompt: 'synthetic button',
  })
  assert.equal(node.inputSchema.safeParse({ prompt: 42 }).success, false)
})

test('Midscene recognizes the actual aiTap locator schema', () => {
  assert.deepEqual(findAllMidsceneLocatorField(actionTapParamSchema), ['locate'])
})

test('Midscene preserves structured location results through action validation', () => {
  const locate = { center: [100, 50], rect: { left: 0, top: 0, width: 200, height: 100 } }
  assert.deepEqual(parseActionParam({ locate }, actionTapParamSchema), { locate })
  assert.throws(() => parseActionParam({}, actionTapParamSchema))
})

async function verifyBrowserAdapter(page, adapter) {
  await page.setContent(html)
  assert.equal(await adapter.evaluate(() => [1].at(-1)), 1)
  assert.equal(await adapter.evaluate(() => document.querySelector('button').textContent), 'tap')
  assert.match(await adapter.screenshotBase64(), /^data:image\/(jpeg|png);base64,/)
  const tap = adapter.actionSpace().find((action) => action.name === 'Tap')
  assert.ok(tap)
  await tap.call({ locate: { center: [100, 50] } })
  assert.equal(
    await adapter.evaluate(() => document.querySelector('button').textContent),
    'clicked',
  )
}

test('project Playwright fixture and Chromium adapter work without a model', async (t) => {
  const fixture = PlaywrightAiFixture(options)
  assert.equal(typeof fixture.aiTap, 'function')
  assert.equal(typeof fixture.aiBoolean, 'function')
  const browser = await chromium.launch({ headless: true })
  t.diagnostic(`Chromium ${browser.version()}`)
  let agent
  try {
    const context = await browser.newContext({ serviceWorkers: 'block' })
    await context.route('**/*', (route) => route.abort())
    const page = await context.newPage()
    agent = new PlaywrightAgent(page, options)
    await verifyBrowserAdapter(page, agent.interface)
  } finally {
    await agent?.destroy()
    await browser.close()
  }
})

test('Puppeteer ESM and Midscene Chromium adapter work with the installed browser', async (t) => {
  assert.ok(Array.isArray(await puppeteer.defaultArgs({ headless: true })))
  const browser = await puppeteer.launch({
    executablePath: chromium.executablePath(),
    headless: true,
    pipe: true,
    args: process.platform === 'linux' && process.env.CI === 'true' ? ['--no-sandbox'] : [],
    dumpio: process.env.CI === 'true',
  })
  t.diagnostic(await browser.version())
  try {
    const page = await browser.newPage()
    await page.setRequestInterception(true)
    page.on('request', (request) => request.abort())
    const adapter = new PuppeteerWebPage(page, options)
    await verifyBrowserAdapter(page, adapter)
  } finally {
    await browser.close()
  }
})

test('patched report and Playground assets load in Chromium without a server or model', async () => {
  const browser = await chromium.launch({ headless: true })
  try {
    const context = await browser.newContext({ serviceWorkers: 'block' })
    await context.route('**/*', (route) => route.abort())
    await context.routeWebSocket('**/*', (socket) => socket.close())
    const loaded = new Set()
    const report = reportHTMLContent(
      JSON.stringify({
        sdkVersion: '1.17.8',
        groupName: 'offline-report-compatibility',
        modelBriefs: [],
        executions: [],
      }),
    )
    await context.route('https://midscene-offline.invalid/**', (route) => {
      const path = new URL(route.request().url()).pathname
      if (path === '/report') return route.fulfill({ contentType: 'text/html', body: report })
      const relative = path === '/' ? 'index.html' : path.slice(1)
      if (relative.includes('..') || (relative !== 'index.html' && !relative.startsWith('static/')))
        return route.abort()
      try {
        const body = readFileSync(
          new URL(`../../node_modules/@midscene/playground/static/${relative}`, import.meta.url),
        )
        loaded.add(relative)
        const contentType = relative.endsWith('.js')
          ? 'application/javascript'
          : relative.endsWith('.css')
            ? 'text/css'
            : relative.endsWith('.html')
              ? 'text/html'
              : 'application/octet-stream'
        return route.fulfill({ contentType, body })
      } catch {
        return route.abort()
      }
    })
    const page = await context.newPage()
    const errors = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.goto('https://midscene-offline.invalid/report')
    await page.waitForFunction(() => document.getElementById('root')?.childElementCount > 0)
    assert.deepEqual(errors, [])
    await page.goto('https://midscene-offline.invalid/')
    await page.waitForFunction(() => document.getElementById('root')?.childElementCount > 0)
    assert.ok(loaded.has('static/js/208.eb5899a1.js'))
    assert.deepEqual(errors, [])
  } finally {
    await browser.close()
  }
})
