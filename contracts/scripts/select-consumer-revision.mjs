import { appendFileSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

export function selectConsumerRevision(revisions, consumer, mode = 'pinned') {
  if (!revisions || typeof revisions !== 'object' || Array.isArray(revisions)) {
    throw new Error('Consumer revisions must be an object.')
  }
  if (!['pinned', 'latest'].includes(mode)) throw new Error(`Unsupported consumer mode: ${mode}`)
  for (const [name, ref] of Object.entries(revisions)) {
    if (typeof ref !== 'string' || !/^[0-9a-f]{40}$/.test(ref) || /^0+$/.test(ref)) {
      throw new Error(`Consumer ${name} requires a full nonzero commit SHA.`)
    }
  }
  if (!consumer || !Object.hasOwn(revisions, consumer)) {
    throw new Error(`Missing reviewed revision for consumer: ${consumer ?? '(unset)'}`)
  }
  return mode === 'latest' ? 'main' : revisions[consumer]
}

export function selectConsumerRepository(consumer) {
  if (!['soha', 'soha-web', 'soha-app', 'soha-cli', 'soha-agent', 'soha-skills'].includes(consumer)) {
    throw new Error(`Unknown consumer repository: ${consumer}`)
  }
  return consumer === 'soha-app' ? 'opensoha/soha-app' : 'opensoha/soha'
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const args = process.argv.slice(2)
    const index = args.indexOf('--revisions-file')
    if (args.length && (index !== 0 || args.length !== 2 || !args[1] || args[1].startsWith('--'))) {
      throw new Error('Only --revisions-file <original-source-manifest> is supported.')
    }
    const revisions = JSON.parse(readFileSync(index === 0 ? args[1] : new URL('../compat/consumer-revisions.json', import.meta.url), 'utf8'))
    const consumer = process.env.CONSUMER
    const mode = process.env.CONSUMER_REVISION_MODE || 'pinned'
    const ref = selectConsumerRevision(revisions, consumer, mode)
    const repository = selectConsumerRepository(consumer)
    if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `ref=${ref}\nrepository=${repository}\n`)
    console.log(`${consumer}: ${mode} -> ${ref}`)
  } catch (error) {
    console.error(`Consumer revision selection failed: ${error.message}`)
    process.exitCode = 1
  }
}
