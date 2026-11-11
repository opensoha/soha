import assert from 'node:assert/strict'
import { mkdtempSync, writeFileSync, readdirSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { createRequire } from 'node:module'
import { randomUUID } from 'node:crypto'

{
  const directory = mkdtempSync(join(tmpdir(), 'soha-auth-artifact-'))
  const canary = randomUUID()
  const playwright = createRequire(import.meta.url).resolve('@playwright/test')
  try {
    writeFileSync(
      join(directory, 'privacy.spec.ts'),
      `
      import {test, expect} from ${JSON.stringify(playwright)};
      test('intentional isolated failure', async ({page}) => {
        await page.setContent('<input aria-label="Password" type="password">');
        await page.getByLabel('Password').fill(process.env.SOHA_PRIVACY_CANARY!);
        expect(false).toBe(true);
      });
    `,
    )
    writeFileSync(
      join(directory, 'config.ts'),
      `
      import real from ${JSON.stringify(resolve('playwright.real.config.ts'))};
      export default {...real, testDir: ${JSON.stringify(directory)},
        testMatch: '**/privacy.spec.ts', globalSetup: undefined,
        reporter: [['line']], outputDir: ${JSON.stringify(join(directory, 'output'))}};
    `,
    )
    const result = spawnSync(
      process.execPath,
      [resolve('node_modules/playwright/cli.js'), 'test', '--config', join(directory, 'config.ts')],
      {
        encoding: 'utf8',
        timeout: 30_000,
        env: { ...process.env, SOHA_PRIVACY_CANARY: canary },
      },
    )
    assert.equal(result.error, undefined)
    assert.equal(result.status, 1)
    assert.ok(!(result.stdout + result.stderr).includes(canary))
    const artifacts = readdirSync(join(directory, 'output'), { recursive: true })
      .filter((name) => name.endsWith('.md'))
      .map((name) => readFileSync(join(directory, 'output', name), 'utf8'))
    assert.ok(artifacts.length > 0)
    for (const artifact of artifacts) {
      assert.ok(!artifact.includes(canary))
      assert.ok(!artifact.includes('# Page snapshot'))
    }
  } finally {
    rmSync(directory, { recursive: true, force: true })
  }
}
console.log('PASS: real Playwright failure artifacts exclude password ARIA values')
