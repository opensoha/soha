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

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const revisions = JSON.parse(readFileSync(new URL('../compat/consumer-revisions.json', import.meta.url), 'utf8'))
    const consumer = process.env.CONSUMER
    const mode = process.env.CONSUMER_REVISION_MODE || 'pinned'
    const ref = selectConsumerRevision(revisions, consumer, mode)
    if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `ref=${ref}\n`)
    console.log(`${consumer}: ${mode} -> ${ref}`)
  } catch (error) {
    console.error(`Consumer revision selection failed: ${error.message}`)
    process.exitCode = 1
  }
}
