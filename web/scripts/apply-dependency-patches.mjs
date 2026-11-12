import { createHash } from 'node:crypto'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

export const dependencyPatchPlan = JSON.parse(
  readFileSync(new URL('./dependency-patches.json', import.meta.url), 'utf8'),
)
const sha256 = (content) => createHash('sha256').update(content).digest('hex')
const fail = (message) => {
  throw new Error(`Dependency patch refused: ${message}`)
}

export function applyDependencyPatches(root) {
  const plan = dependencyPatchPlan
  const patch = readFileSync(new URL('./dependency-patches.patch', import.meta.url))
  if (sha256(patch) !== plan.patchSha256) fail('review patch digest changed')
  const lock = JSON.parse(readFileSync(join(root, 'package-lock.json'), 'utf8'))
  const midscene = plan.packages.filter((pkg) => pkg.path.includes('@midscene/'))
  const installed = midscene.filter((pkg) => existsSync(join(root, pkg.path)))
  if (installed.length !== 0 && installed.length !== midscene.length)
    fail('partial Midscene installation')
  const omitMidscene = installed.length === 0
  const nwsapi = plan.packages.find((pkg) => pkg.path === 'node_modules/nwsapi')
  const omitDev =
    process.env.npm_config_omit?.split(/[\s,]+/).includes('dev') ||
    process.env.NODE_ENV === 'production'
  const omitNwsapi =
    !existsSync(join(root, nwsapi.path)) &&
    omitDev &&
    lock.packages?.[nwsapi.path]?.dev === true &&
    lock.packages?.['node_modules/jsdom']?.dev === true &&
    !existsSync(join(root, 'node_modules/jsdom'))
  if (
    lock.packages?.[nwsapi.path]?.version !== nwsapi.version ||
    lock.packages?.[nwsapi.path]?.integrity !== nwsapi.integrity
  )
    fail(`upstream package changed: ${nwsapi.path}`)
  for (const pkg of plan.packages) {
    if (omitMidscene && pkg.path.includes('@midscene/')) continue
    if (omitNwsapi && pkg.path === nwsapi.path) continue
    const manifest = readFileSync(join(root, pkg.path, 'package.json'), 'utf8')
    if (
      JSON.parse(manifest).version !== pkg.version ||
      sha256(manifest) !== pkg.manifestSha256 ||
      lock.packages?.[pkg.path]?.integrity !== pkg.integrity
    )
      fail(`upstream package changed: ${pkg.path}`)
  }
  const writes = []
  let verified = 0
  for (const file of plan.files) {
    if (omitMidscene && file.path.includes('@midscene/')) continue
    if (omitNwsapi && file.path.startsWith(`${nwsapi.path}/`)) continue
    const path = join(root, file.path)
    const before = readFileSync(path, 'utf8')
    const digest = sha256(before)
    if (digest === file.after) {
      verified++
      continue
    }
    if (digest !== file.before) fail(`unknown file contents: ${file.path}`)
    let after = before
    for (const key of file.edits) {
      const edit = plan.edits[key]
      if (after.split(edit.before).length !== 2) fail(`patch context changed: ${file.path}`)
      after = after.replace(edit.before, edit.after)
    }
    if (sha256(after) !== file.after) fail(`patched digest mismatch: ${file.path}`)
    writes.push({ path, after })
  }
  // Validate every input before changing any file; pre/post hashes also permit interrupted installs.
  for (const { path, after } of writes) writeFileSync(path, after)
  return { patched: writes.length, verified, midscene: omitMidscene ? 'omitted' : 'patched' }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  console.log(
    JSON.stringify(applyDependencyPatches(fileURLToPath(new URL('../..', import.meta.url)))),
  )
}
