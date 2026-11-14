import { readFileSync } from 'node:fs'
import { assertTrustedDispatch, assertTrustedWorkflow } from './quality-guards.mjs'

if (process.argv.includes('--dispatch')) {
  assertTrustedDispatch(
    process.env.GITHUB_REF,
    process.env.GITHUB_SHA,
    process.env.SOHA_APPROVED_AI_SHA,
  )
} else {
  const YAML = (await import('yaml')).default
  for (const [name, variable, environment] of [
    ['quality-ai', 'SOHA_APPROVED_AI_SHA', 'quality-ai'],
    ['quality-ocr', 'SOHA_OCR_APPROVED_CODE_SHA', 'quality-ocr'],
  ]) {
    const file = YAML.parse(
      readFileSync(new URL(`../../.github/workflows/${name}.yml`, import.meta.url), 'utf8'),
    )
    assertTrustedWorkflow(file, variable, environment)
  }
  console.log('Enhanced workflow: manual, read-only, protected environment, fixed actions')
}
