#!/usr/bin/env node

import { gzipSync } from 'node:zlib'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { basename, dirname, relative, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const VERSION = 1
const KiB = 1024
const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const projectRoot = resolve(scriptDirectory, '..')
const defaultManifest = resolve(projectRoot, 'dist/.vite/manifest.json')
const heavyRuntimePatterns = {
  flow: /(?:^|[/_-])flow(?:[/_.-]|$)|@xyflow|dagre/i,
  vchart: /vchart|@visactor/i,
  monaco: /monaco/i,
  xterm: /xterm/i,
  novnc: /novnc|vm-console/i,
}
const budgets = {
  p0: { initialGzipKiB: 450, routeGzipKiB: 150, pageChunkGzipKiB: 50 },
  p1: { initialGzipKiB: 350, routeGzipKiB: 100, pageChunkGzipKiB: 50 },
}

function parseArguments(argv) {
  const options = {
    baseline: undefined,
    json: false,
    manifest: defaultManifest,
    mode: 'report',
    scope: 'all',
    writeBaseline: undefined,
  }
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index]
    if (argument === '--json') options.json = true
    else if (
      argument === '--manifest' ||
      argument === '--mode' ||
      argument === '--scope' ||
      argument === '--baseline' ||
      argument === '--write-baseline'
    ) {
      const value = argv[index + 1]
      if (!value || value.startsWith('--')) throw new Error(`${argument} requires a value`)
      index += 1
      if (argument === '--manifest') options.manifest = resolve(process.cwd(), value)
      if (argument === '--mode') options.mode = value
      if (argument === '--scope') options.scope = value
      if (argument === '--baseline') options.baseline = resolve(process.cwd(), value)
      if (argument === '--write-baseline') options.writeBaseline = resolve(process.cwd(), value)
    } else if (argument === '--help' || argument === '-h') {
      console.log(`Usage: node scripts/check-bundle-budget.mjs [options]

Options:
  --manifest <file>         Vite manifest (default: dist/.vite/manifest.json).
  --mode report|p0|p1       Report metrics or enforce a budget.
  --scope initial|all       Limit enforcement to the initial closure.
  --baseline <file>         Show size deltas from a saved report.
  --write-baseline <file>   Save the current report.
  --json                    Print JSON.
  --help                    Show this help.`)
      process.exit(0)
    } else throw new Error(`Unknown option: ${argument}`)
  }
  if (!['report', 'p0', 'p1'].includes(options.mode)) {
    throw new Error(`Unsupported mode: ${options.mode}`)
  }
  if (!['initial', 'all'].includes(options.scope)) {
    throw new Error(`Unsupported scope: ${options.scope}`)
  }
  return options
}

function projectPath(filePath) {
  return relative(projectRoot, filePath).split(sep).join('/')
}

function outputRoot(manifestPath) {
  const manifestDirectory = dirname(manifestPath)
  return basename(manifestDirectory) === '.vite' ? dirname(manifestDirectory) : manifestDirectory
}

function collectStaticClosure(manifest, roots) {
  const visited = new Set()
  function visit(key) {
    if (!key || visited.has(key) || !manifest[key]) return
    visited.add(key)
    for (const dependency of manifest[key].imports ?? []) visit(dependency)
  }
  for (const root of roots) visit(root)
  return visited
}

function closureFiles(manifest, keys) {
  const files = new Set()
  for (const key of keys) {
    const chunk = manifest[key]
    if (!chunk) continue
    if (chunk.file) files.add(chunk.file)
    for (const css of chunk.css ?? []) files.add(css)
    for (const asset of chunk.assets ?? []) files.add(asset)
  }
  return files
}

function measureFiles(root, files) {
  let raw = 0
  let gzip = 0
  const measured = []
  for (const file of [...files].sort()) {
    const path = resolve(root, file)
    if (!existsSync(path)) throw new Error(`Manifest output is missing: ${path}`)
    const contents = readFileSync(path)
    const entry = { file, raw: contents.length, gzip: gzipSync(contents).length }
    raw += entry.raw
    gzip += entry.gzip
    measured.push(entry)
  }
  return { raw, gzip, files: measured }
}

function difference(left, right) {
  return new Set([...left].filter((item) => !right.has(item)))
}

function findCycles(manifest) {
  let index = 0
  const indices = new Map()
  const lowLinks = new Map()
  const stack = []
  const onStack = new Set()
  const components = []

  function connect(key) {
    indices.set(key, index)
    lowLinks.set(key, index)
    index += 1
    stack.push(key)
    onStack.add(key)
    for (const target of manifest[key]?.imports ?? []) {
      if (!manifest[target]) continue
      if (!indices.has(target)) {
        connect(target)
        lowLinks.set(key, Math.min(lowLinks.get(key), lowLinks.get(target)))
      } else if (onStack.has(target)) {
        lowLinks.set(key, Math.min(lowLinks.get(key), indices.get(target)))
      }
    }
    if (lowLinks.get(key) !== indices.get(key)) return
    const component = []
    let current
    do {
      current = stack.pop()
      onStack.delete(current)
      component.push(current)
    } while (current !== key)
    if (
      component.length > 1 ||
      (component.length === 1 && (manifest[component[0]].imports ?? []).includes(component[0]))
    ) {
      components.push(component.sort())
    }
  }

  for (const key of Object.keys(manifest)) if (!indices.has(key)) connect(key)
  return components.sort((left, right) => right.length - left.length)
}

function runtimeMatches(manifest, keys) {
  const haystacks = [...keys].flatMap((key) => [
    key,
    manifest[key]?.file ?? '',
    manifest[key]?.name ?? '',
  ])
  return Object.fromEntries(
    Object.entries(heavyRuntimePatterns).map(([name, pattern]) => [
      name,
      [...new Set(haystacks.filter((value) => pattern.test(value)))].sort(),
    ]),
  )
}

function kib(bytes) {
  return Number((bytes / KiB).toFixed(2))
}

function metric(measurement) {
  return {
    rawBytes: measurement.raw,
    gzipBytes: measurement.gzip,
    rawKiB: kib(measurement.raw),
    gzipKiB: kib(measurement.gzip),
  }
}

function analyze(manifestPath) {
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'))
  const root = outputRoot(manifestPath)
  const entries = Object.entries(manifest).filter(([, chunk]) => chunk.isEntry)
  if (entries.length !== 1) throw new Error(`Expected one Vite entry, found ${entries.length}`)
  const [entryKey] = entries[0]
  const initialKeys = collectStaticClosure(manifest, [entryKey])
  const initialFiles = closureFiles(manifest, initialKeys)
  const initialMeasurement = measureFiles(root, initialFiles)
  const routeSourceKeys = new Set(
    (manifest[entryKey].dynamicImports ?? []).filter(
      (key) => key.startsWith('src/features/') && /\/(?:[^/]+-)?pages?\.tsx$/.test(key),
    ),
  )
  const routeEntries = Object.entries(manifest)
    .filter(([key, chunk]) => routeSourceKeys.has(key) && chunk.isDynamicEntry)
    .map(([key, chunk]) => {
      const routeKeys = collectStaticClosure(manifest, [key])
      const incrementalKeys = difference(routeKeys, initialKeys)
      const incrementalFiles = difference(closureFiles(manifest, routeKeys), initialFiles)
      const incrementalMeasurement = measureFiles(root, incrementalFiles)
      const ownMeasurement = measureFiles(root, closureFiles(manifest, new Set([key])))
      return {
        source: key,
        file: chunk.file,
        incremental: metric(incrementalMeasurement),
        ownChunk: metric(ownMeasurement),
        heavyRuntimes: runtimeMatches(manifest, incrementalKeys),
      }
    })
    .sort((left, right) => right.incremental.gzipBytes - left.incremental.gzipBytes)
  const cycles = findCycles(manifest)

  return {
    version: VERSION,
    manifest: projectPath(manifestPath),
    entry: entryKey,
    summary: {
      chunks: Object.keys(manifest).length,
      routeSourceModules: routeEntries.length,
      initial: metric(initialMeasurement),
      initialHeavyRuntimes: runtimeMatches(manifest, initialKeys),
      circularStaticImportGroups: cycles,
      largestRouteIncrement: routeEntries[0]
        ? { source: routeEntries[0].source, ...routeEntries[0].incremental }
        : undefined,
      largestPageChunk: [...routeEntries]
        .sort((left, right) => right.ownChunk.gzipBytes - left.ownChunk.gzipBytes)
        .map(({ source, ownChunk }) => ({ source, ...ownChunk }))[0],
    },
    initialFiles: initialMeasurement.files,
    routes: routeEntries,
  }
}

function enforce(report, mode, scope) {
  if (mode === 'report') return []
  const budget = budgets[mode]
  const errors = []
  if (report.summary.initial.gzipKiB > budget.initialGzipKiB) {
    errors.push(
      `initial gzip ${report.summary.initial.gzipKiB} KiB exceeds ${budget.initialGzipKiB} KiB`,
    )
  }
  for (const [runtime, matches] of Object.entries(report.summary.initialHeavyRuntimes)) {
    if (matches.length > 0)
      errors.push(`initial closure contains ${runtime}: ${matches.join(', ')}`)
  }
  for (const cycle of report.summary.circularStaticImportGroups) {
    errors.push(`circular static chunk imports: ${cycle.join(' -> ')}`)
  }
  if (scope === 'all') {
    for (const route of report.routes) {
      if (route.incremental.gzipKiB > budget.routeGzipKiB) {
        errors.push(
          `${route.source} increment ${route.incremental.gzipKiB} KiB exceeds ${budget.routeGzipKiB} KiB`,
        )
      }
      if (route.ownChunk.gzipKiB > budget.pageChunkGzipKiB) {
        errors.push(
          `${route.source} own chunk ${route.ownChunk.gzipKiB} KiB exceeds ${budget.pageChunkGzipKiB} KiB`,
        )
      }
    }
  }
  return errors
}

function printHuman(report, mode, scope, errors, baseline) {
  console.log('Frontend bundle report')
  console.log(`  Mode:                 ${mode}`)
  console.log(`  Enforcement scope:    ${scope}`)
  console.log(`  Manifest chunks:      ${report.summary.chunks}`)
  console.log(`  Route source modules: ${report.summary.routeSourceModules}`)
  console.log(
    `  Initial raw/gzip:     ${report.summary.initial.rawKiB} / ${report.summary.initial.gzipKiB} KiB`,
  )
  console.log(`  Circular groups:      ${report.summary.circularStaticImportGroups.length}`)
  const heavy = Object.entries(report.summary.initialHeavyRuntimes)
    .filter(([, matches]) => matches.length > 0)
    .map(([name]) => name)
  console.log(`  Initial heavy runtime:${heavy.length > 0 ? ` ${heavy.join(', ')}` : ' none'}`)
  if (baseline) {
    console.log(
      `  Baseline gzip delta:  ${kib(report.summary.initial.gzipBytes - baseline.summary.initial.gzipBytes)} KiB`,
    )
  }
  console.log('')
  console.log('Largest dynamic route closures')
  for (const route of report.routes.slice(0, 10)) {
    console.log(
      `  ${String(route.incremental.gzipKiB.toFixed(2)).padStart(8)} KiB  ${route.source}`,
    )
  }
  if (errors.length > 0) {
    console.log('')
    console.log(`Budget violations (${errors.length})`)
    for (const error of errors.slice(0, 40)) console.log(`  - ${error}`)
    if (errors.length > 40) console.log(`  ... ${errors.length - 40} more`)
  }
}

try {
  const options = parseArguments(process.argv.slice(2))
  const report = analyze(options.manifest)
  const baseline = options.baseline ? JSON.parse(readFileSync(options.baseline, 'utf8')) : undefined
  const errors = enforce(report, options.mode, options.scope)
  const output = { ...report, mode: options.mode, scope: options.scope, errors }
  if (options.writeBaseline) {
    writeFileSync(
      options.writeBaseline,
      `${JSON.stringify({ ...report, generatedAt: new Date().toISOString() }, null, 2)}\n`,
      'utf8',
    )
  }
  if (options.json) console.log(JSON.stringify(output, null, 2))
  else printHuman(report, options.mode, options.scope, errors, baseline)
  if (errors.length > 0) process.exit(1)
} catch (error) {
  console.error(`Bundle budget check failed: ${error.message}`)
  process.exit(2)
}
