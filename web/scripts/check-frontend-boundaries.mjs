#!/usr/bin/env node

import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs'
import { dirname, extname, join, posix, relative, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

const BASELINE_VERSION = 1
const SOURCE_EXTENSIONS = new Set(['.ts', '.tsx'])
const TEST_FILE_MARKERS = ['.test.', '.spec.', '.stories.']
const RULES = {
  PAGE_API_CLIENT: 'page-direct-api-client',
  BARE_QUERY_KEY: 'bare-query-key',
  CROSS_FEATURE_IMPORT: 'cross-feature-deep-import',
  AGGREGATE_ROUTE_MODULE: 'aggregate-route-module',
  ROUTE_PAGE_BARREL: 'route-page-barrel',
  SHARED_PAGE_DEPENDENCY: 'shared-capability-page-dependency',
}

const COMPUTE_ROUTE_LEAF_IMPORTS = new Set([
  'src/features/virtualization/virtual-machines/list-page.tsx',
  'src/features/virtualization/virtual-machines/detail-page.tsx',
  'src/features/virtualization/clusters/list-page.tsx',
  'src/features/virtualization/images/list-page.tsx',
  'src/features/virtualization/storage/list-page.tsx',
  'src/features/virtualization/flavors/list-page.tsx',
  'src/features/docker/hosts/page.tsx',
  'src/features/docker/projects/list-page.tsx',
  'src/features/docker/projects/detail-page.tsx',
  'src/features/docker/templates/page.tsx',
])

function isComputeRouteLeafImport(sourcePath, targetPath, referenceKind) {
  return (
    sourcePath === 'src/features/compute/routes.ts' &&
    referenceKind === 'dynamic-import' &&
    COMPUTE_ROUTE_LEAF_IMPORTS.has(targetPath)
  )
}

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const projectRoot = resolve(scriptDirectory, '..')

function printHelp() {
  console.log(`Usage: node scripts/check-frontend-boundaries.mjs [options]

Reports frontend architecture boundary violations using the TypeScript AST.
Report mode always exits successfully. Enforce mode fails only for violations
that are new relative to a baseline.

Options:
  --json                    Print the complete report as JSON.
  --verbose                 Print every violation in the human report.
  --enforce                 Exit 1 when violations exceed the baseline.
  --base <git-ref>          Compare with the merge-base of HEAD and <git-ref>.
  --baseline <file>         Compare with a JSON baseline written by this script.
  --write-baseline <file>   Write the current inventory as a JSON baseline.
  --help                    Show this help.

When --enforce has no explicit baseline, the script tries
FRONTEND_BOUNDARY_BASE_REF, origin/main, then main. Existing debt at that
merge-base remains visible but does not fail the check.`)
}

function parseArguments(argv) {
  const options = {
    baseRef: undefined,
    baselineFile: undefined,
    enforce: false,
    json: false,
    verbose: false,
    writeBaselineFile: undefined,
  }

  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index]

    if (argument === '--help' || argument === '-h') {
      printHelp()
      process.exit(0)
    }

    if (argument === '--json') {
      options.json = true
      continue
    }

    if (argument === '--verbose') {
      options.verbose = true
      continue
    }

    if (argument === '--enforce') {
      options.enforce = true
      continue
    }

    if (argument === '--base' || argument === '--baseline' || argument === '--write-baseline') {
      const value = argv[index + 1]
      if (!value || value.startsWith('--')) {
        throw new Error(`${argument} requires a value`)
      }
      index += 1

      if (argument === '--base') options.baseRef = value
      if (argument === '--baseline') options.baselineFile = resolve(process.cwd(), value)
      if (argument === '--write-baseline') {
        options.writeBaselineFile = resolve(process.cwd(), value)
      }
      continue
    }

    throw new Error(`Unknown option: ${argument}`)
  }

  if (options.baseRef && options.baselineFile) {
    throw new Error('--base and --baseline are mutually exclusive')
  }

  return options
}

function toProjectPath(filePath) {
  return relative(projectRoot, filePath).split(sep).join(posix.sep)
}

function isProductionSource(filePath) {
  if (!SOURCE_EXTENSIONS.has(extname(filePath))) return false
  if (filePath.endsWith('.d.ts')) return false
  return !TEST_FILE_MARKERS.some((marker) => filePath.includes(marker))
}

function collectFiles(directory) {
  if (!existsSync(directory)) return []

  const files = []
  for (const entry of readdirSync(directory)) {
    const path = join(directory, entry)
    const stats = statSync(path)
    if (stats.isDirectory()) files.push(...collectFiles(path))
    else if (isProductionSource(path)) files.push(path)
  }
  return files
}

function loadWorkingTreeSources() {
  const files = [
    ...collectFiles(resolve(projectRoot, 'src/features')),
    ...collectFiles(resolve(projectRoot, 'src/routes')),
  ].sort()

  return new Map(files.map((filePath) => [toProjectPath(filePath), readFileSync(filePath, 'utf8')]))
}

function git(args, options = {}) {
  return execFileSync('git', args, {
    cwd: projectRoot,
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
    stdio: ['ignore', 'pipe', 'pipe'],
    ...options,
  }).trim()
}

function tryMergeBase(baseRef) {
  try {
    return git(['merge-base', 'HEAD', baseRef])
  } catch {
    return undefined
  }
}

function resolveAutomaticBase() {
  const candidates = [process.env.FRONTEND_BOUNDARY_BASE_REF, 'origin/main', 'main'].filter(Boolean)

  for (const candidate of candidates) {
    const commit = tryMergeBase(candidate)
    if (commit) return { commit, ref: candidate }
  }

  throw new Error('Unable to find a git baseline. Pass --base <git-ref> or --baseline <file>.')
}

function loadGitSources(commit) {
  const output = git(['ls-tree', '-r', '--name-only', commit, '--', 'src/features', 'src/routes'])
  const files = output ? output.split('\n').filter(isProductionSource) : []
  const sources = new Map()

  for (const filePath of files) {
    sources.set(filePath, git(['show', `${commit}:${filePath}`]))
  }

  return sources
}

function sourceKind(filePath) {
  return filePath.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS
}

function createSourceFiles(sources) {
  return new Map(
    [...sources].map(([filePath, source]) => [
      filePath,
      ts.createSourceFile(filePath, source, ts.ScriptTarget.Latest, true, sourceKind(filePath)),
    ]),
  )
}

function fileStem(filePath) {
  const base = posix.basename(filePath)
  return base.slice(0, base.length - extname(base).length)
}

function isPageFile(filePath) {
  const stem = fileStem(filePath)
  return stem === 'page' || stem === 'pages' || stem.endsWith('-page') || stem.endsWith('-pages')
}

function isRouteFile(filePath) {
  if (filePath.startsWith('src/routes/')) return true
  const stem = fileStem(filePath)
  return (
    stem === 'route' || stem === 'routes' || stem.endsWith('-route') || stem.endsWith('-routes')
  )
}

function isKeysFile(filePath) {
  const stem = fileStem(filePath)
  return stem === 'keys' || stem.endsWith('-keys') || stem.endsWith('.keys')
}

function featureSegments(filePath) {
  const segments = filePath.split(posix.sep)
  if (segments[0] !== 'src' || segments[1] !== 'features' || !segments[2]) return undefined
  return { domain: segments[2], rest: segments.slice(3) }
}

function stripKnownExtension(modulePath) {
  for (const extension of ['.tsx', '.ts', '.jsx', '.js']) {
    if (modulePath.endsWith(extension)) return modulePath.slice(0, -extension.length)
  }
  return modulePath
}

function modulePathWithoutQuery(modulePath) {
  const queryIndex = modulePath.indexOf('?')
  return queryIndex === -1 ? modulePath : modulePath.slice(0, queryIndex)
}

function moduleToProjectPath(sourcePath, moduleSpecifier) {
  const cleanSpecifier = modulePathWithoutQuery(moduleSpecifier)
  if (cleanSpecifier.startsWith('@/')) return `src/${cleanSpecifier.slice(2)}`
  if (!cleanSpecifier.startsWith('.')) return undefined
  return posix.normalize(posix.join(posix.dirname(sourcePath), cleanSpecifier))
}

function resolveModulePath(sourcePath, moduleSpecifier, sourceFiles) {
  const unresolved = moduleToProjectPath(sourcePath, moduleSpecifier)
  if (!unresolved) return undefined
  const withoutExtension = stripKnownExtension(unresolved)
  const candidates = [
    unresolved,
    `${withoutExtension}.ts`,
    `${withoutExtension}.tsx`,
    `${withoutExtension}/index.ts`,
    `${withoutExtension}/index.tsx`,
  ]
  return candidates.find((candidate) => sourceFiles.has(candidate)) ?? withoutExtension
}

function moduleReferences(sourceFile) {
  const references = []

  function add(node, value, kind) {
    if (ts.isStringLiteralLike(value)) {
      references.push({ kind, node: value, specifier: value.text })
    }
  }

  function visit(node) {
    if (ts.isImportDeclaration(node)) add(node, node.moduleSpecifier, 'import')
    else if (ts.isExportDeclaration(node) && node.moduleSpecifier) {
      add(node, node.moduleSpecifier, 'export')
    } else if (
      ts.isImportEqualsDeclaration(node) &&
      ts.isExternalModuleReference(node.moduleReference) &&
      node.moduleReference.expression
    ) {
      add(node, node.moduleReference.expression, 'import-equals')
    } else if (
      ts.isCallExpression(node) &&
      node.expression.kind === ts.SyntaxKind.ImportKeyword &&
      node.arguments.length === 1
    ) {
      add(node, node.arguments[0], 'dynamic-import')
    }

    ts.forEachChild(node, visit)
  }

  visit(sourceFile)
  return references
}

function isApiClientModule(sourcePath, moduleSpecifier) {
  if (stripKnownExtension(modulePathWithoutQuery(moduleSpecifier)) === '@/services/api-client') {
    return true
  }
  const projectPath = moduleToProjectPath(sourcePath, moduleSpecifier)
  return stripKnownExtension(projectPath ?? '') === 'src/services/api-client'
}

function propertyName(node) {
  if (ts.isIdentifier(node) || ts.isStringLiteralLike(node)) return node.text
  return undefined
}

function printedNode(node, sourceFile) {
  return ts
    .createPrinter({ removeComments: true })
    .printNode(ts.EmitHint.Unspecified, node, sourceFile)
}

function isAggregatePagesModule(moduleSpecifier) {
  const cleanPath = stripKnownExtension(modulePathWithoutQuery(moduleSpecifier))
  const basename = posix.basename(cleanPath)
  return basename === 'pages' || basename.endsWith('-pages')
}

function isPageModulePath(modulePath) {
  const stem = fileStem(stripKnownExtension(modulePath))
  return stem === 'page' || stem === 'pages' || stem.endsWith('-page') || stem.endsWith('-pages')
}

function countRoutableExports(sourceFile) {
  const names = new Set()
  let exportsPageModule = false

  function addName(name) {
    if (name?.endsWith('Page')) names.add(name)
  }

  for (const statement of sourceFile.statements) {
    const modifiers = ts.canHaveModifiers(statement) ? ts.getModifiers(statement) : undefined
    const exported = modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword)

    if (exported && 'name' in statement && statement.name && ts.isIdentifier(statement.name)) {
      addName(statement.name.text)
    }

    if (exported && ts.isVariableStatement(statement)) {
      for (const declaration of statement.declarationList.declarations) {
        if (ts.isIdentifier(declaration.name)) addName(declaration.name.text)
      }
    }

    if (ts.isExportDeclaration(statement)) {
      if (statement.exportClause && ts.isNamedExports(statement.exportClause)) {
        for (const element of statement.exportClause.elements) addName(element.name.text)
      } else if (
        !statement.exportClause &&
        statement.moduleSpecifier &&
        ts.isStringLiteralLike(statement.moduleSpecifier) &&
        isPageModulePath(statement.moduleSpecifier.text)
      ) {
        exportsPageModule = true
      }
    }
  }

  return names.size + (exportsPageModule ? 2 : 0)
}

function violationLocation(sourceFile, node) {
  const position = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile))
  return { column: position.character + 1, line: position.line + 1 }
}

function makeViolation({ sourceFile, node, rule, message, moduleSpecifier, signature }) {
  const location = violationLocation(sourceFile, node)
  const file = sourceFile.fileName
  return {
    rule,
    file,
    ...location,
    message,
    ...(moduleSpecifier ? { moduleSpecifier } : {}),
    fingerprint: [rule, file, signature].join('|'),
  }
}

function scanSources(sources) {
  const sourceFiles = createSourceFiles(sources)
  const violations = []

  for (const [filePath, sourceFile] of sourceFiles) {
    const references = moduleReferences(sourceFile)
    const sourceFeature = featureSegments(filePath)

    if (isPageFile(filePath) || isRouteFile(filePath)) {
      for (const reference of references) {
        if (isApiClientModule(filePath, reference.specifier)) {
          violations.push(
            makeViolation({
              sourceFile,
              node: reference.node,
              rule: RULES.PAGE_API_CLIENT,
              message:
                'Route and page modules must use a feature API instead of api-client directly.',
              moduleSpecifier: reference.specifier,
              signature: reference.specifier,
            }),
          )
        }
      }
    }

    if (!isKeysFile(filePath)) {
      function findBareQueryKeys(node) {
        let array

        if (
          ts.isPropertyAssignment(node) &&
          propertyName(node.name) === 'queryKey' &&
          ts.isArrayLiteralExpression(node.initializer)
        ) {
          array = node.initializer
        } else if (
          ts.isVariableDeclaration(node) &&
          ts.isIdentifier(node.name) &&
          node.name.text === 'queryKey' &&
          node.initializer &&
          ts.isArrayLiteralExpression(node.initializer)
        ) {
          array = node.initializer
        } else if (
          ts.isBinaryExpression(node) &&
          node.operatorToken.kind === ts.SyntaxKind.EqualsToken &&
          ts.isIdentifier(node.left) &&
          node.left.text === 'queryKey' &&
          ts.isArrayLiteralExpression(node.right)
        ) {
          array = node.right
        }

        if (array) {
          const printed = printedNode(array, sourceFile)
          violations.push(
            makeViolation({
              sourceFile,
              node: array,
              rule: RULES.BARE_QUERY_KEY,
              message: 'Query keys must come from a keys.ts factory instead of an inline array.',
              signature: printed,
            }),
          )
        }

        ts.forEachChild(node, findBareQueryKeys)
      }

      findBareQueryKeys(sourceFile)
    }

    if (sourceFeature) {
      for (const reference of references) {
        const targetPath = resolveModulePath(filePath, reference.specifier, sourceFiles)
        if (!targetPath) continue
        const targetFeature = featureSegments(targetPath)
        if (!targetFeature) continue

        if (
          sourceFeature.domain !== targetFeature.domain &&
          targetFeature.rest.length > 0 &&
          !(targetFeature.rest.length === 1 && fileStem(targetFeature.rest[0]) === 'index') &&
          !isComputeRouteLeafImport(filePath, targetPath, reference.kind)
        ) {
          violations.push(
            makeViolation({
              sourceFile,
              node: reference.node,
              rule: RULES.CROSS_FEATURE_IMPORT,
              message: `Feature ${sourceFeature.domain} must use the public entry of feature ${targetFeature.domain}.`,
              moduleSpecifier: reference.specifier,
              signature: `${sourceFeature.domain}->${targetFeature.domain}:${reference.specifier}`,
            }),
          )
        }

        if (
          sourceFeature.rest[0] === 'shared' &&
          sourceFeature.domain === targetFeature.domain &&
          targetFeature.rest[0] !== 'shared' &&
          isPageModulePath(targetPath)
        ) {
          violations.push(
            makeViolation({
              sourceFile,
              node: reference.node,
              rule: RULES.SHARED_PAGE_DEPENDENCY,
              message: 'Domain shared code must not depend on a concrete capability page.',
              moduleSpecifier: reference.specifier,
              signature: reference.specifier,
            }),
          )
        }
      }
    }

    if (isRouteFile(filePath)) {
      for (const reference of references.filter(({ kind }) => kind === 'dynamic-import')) {
        if (isAggregatePagesModule(reference.specifier)) {
          violations.push(
            makeViolation({
              sourceFile,
              node: reference.node,
              rule: RULES.AGGREGATE_ROUTE_MODULE,
              message:
                'Route loaders must import one leaf page module, not an aggregate *-pages module.',
              moduleSpecifier: reference.specifier,
              signature: reference.specifier,
            }),
          )
        }

        const resolvedPath = resolveModulePath(filePath, reference.specifier, sourceFiles)
        const targetSource = resolvedPath ? sourceFiles.get(resolvedPath) : undefined
        if (
          resolvedPath &&
          (fileStem(resolvedPath) === 'index' ||
            posix.basename(stripKnownExtension(reference.specifier)) === 'index') &&
          targetSource &&
          countRoutableExports(targetSource) > 1
        ) {
          violations.push(
            makeViolation({
              sourceFile,
              node: reference.node,
              rule: RULES.ROUTE_PAGE_BARREL,
              message:
                'Route loaders must import a leaf page directly, not a barrel exporting multiple pages.',
              moduleSpecifier: reference.specifier,
              signature: reference.specifier,
            }),
          )
        }
      }
    }
  }

  violations.sort(
    (left, right) =>
      left.rule.localeCompare(right.rule) ||
      left.file.localeCompare(right.file) ||
      left.line - right.line ||
      left.column - right.column,
  )

  return { filesScanned: sourceFiles.size, violations }
}

function countsByFingerprint(violations) {
  const counts = {}
  for (const violation of violations) {
    counts[violation.fingerprint] = (counts[violation.fingerprint] ?? 0) + 1
  }
  return counts
}

function countsByRule(violations) {
  const initialCounts = Object.fromEntries(Object.values(RULES).map((rule) => [rule, 0]))
  return violations.reduce((counts, violation) => {
    counts[violation.rule] = (counts[violation.rule] ?? 0) + 1
    return counts
  }, initialCounts)
}

function baselineFromFile(filePath) {
  const data = JSON.parse(readFileSync(filePath, 'utf8'))
  if (data.version !== BASELINE_VERSION || !data.counts || typeof data.counts !== 'object') {
    throw new Error(`Unsupported frontend boundary baseline: ${filePath}`)
  }
  return { counts: data.counts, description: toProjectPath(filePath) }
}

function compareWithBaseline(violations, baselineCounts) {
  const currentCounts = countsByFingerprint(violations)
  const remaining = { ...baselineCounts }
  const newViolations = []

  for (const violation of violations) {
    const allowance = remaining[violation.fingerprint] ?? 0
    if (allowance > 0) remaining[violation.fingerprint] = allowance - 1
    else newViolations.push(violation)
  }

  const resolvedCount = Object.entries(baselineCounts).reduce(
    (total, [fingerprint, count]) => total + Math.max(0, count - (currentCounts[fingerprint] ?? 0)),
    0,
  )

  return { newViolations, resolvedCount }
}

function createBaseline(scan) {
  return {
    version: BASELINE_VERSION,
    generatedAt: new Date().toISOString(),
    filesScanned: scan.filesScanned,
    violations: scan.violations.length,
    byRule: countsByRule(scan.violations),
    counts: countsByFingerprint(scan.violations),
  }
}

function formatCounts(counts) {
  const ruleNames = Object.values(RULES)
  return ruleNames.map((rule) => `  ${rule.padEnd(36)} ${String(counts[rule] ?? 0).padStart(5)}`)
}

function printHumanReport(report, verbose) {
  console.log('Frontend boundary report')
  console.log(`  Mode:       ${report.mode}`)
  console.log(`  Files:      ${report.summary.filesScanned}`)
  console.log(`  Violations: ${report.summary.violations}`)
  console.log('')
  console.log('Violations by rule')
  console.log(formatCounts(report.summary.byRule).join('\n'))

  if (report.baseline) {
    console.log('')
    console.log(`Baseline: ${report.baseline.description}`)
    console.log(`  Existing allowed: ${report.baseline.existingViolations}`)
    console.log(`  Resolved:         ${report.baseline.resolvedViolations}`)
    console.log(`  New:              ${report.baseline.newViolations}`)
  }

  const details = report.baseline ? report.newViolations : report.violations
  if (details.length === 0) return

  const limit = verbose ? details.length : Math.min(details.length, 40)
  console.log('')
  console.log(report.baseline ? 'New violations' : 'Violation sample')
  for (const violation of details.slice(0, limit)) {
    console.log(
      `  ${violation.file}:${violation.line}:${violation.column} [${violation.rule}] ${violation.message}`,
    )
  }
  if (limit < details.length) {
    console.log(`  ... ${details.length - limit} more; use --verbose or --json for all details.`)
  }
}

function main() {
  let options
  try {
    options = parseArguments(process.argv.slice(2))
  } catch (error) {
    console.error(`Frontend boundary check configuration error: ${error.message}`)
    process.exit(2)
  }

  const currentScan = scanSources(loadWorkingTreeSources())

  if (options.writeBaselineFile) {
    writeFileSync(
      options.writeBaselineFile,
      `${JSON.stringify(createBaseline(currentScan), null, 2)}\n`,
      'utf8',
    )
  }

  let baseline
  if (options.baselineFile) {
    baseline = baselineFromFile(options.baselineFile)
  } else if (options.baseRef || options.enforce) {
    const resolvedBase = options.baseRef
      ? { commit: tryMergeBase(options.baseRef), ref: options.baseRef }
      : resolveAutomaticBase()
    if (!resolvedBase.commit)
      throw new Error(`Unable to resolve merge-base for ${resolvedBase.ref}`)
    const baseScan = scanSources(loadGitSources(resolvedBase.commit))
    baseline = {
      counts: countsByFingerprint(baseScan.violations),
      description: `${resolvedBase.ref} (${resolvedBase.commit.slice(0, 12)})`,
    }
  }

  const comparison = baseline
    ? compareWithBaseline(currentScan.violations, baseline.counts)
    : undefined
  const report = {
    version: BASELINE_VERSION,
    mode: options.enforce ? 'enforce' : 'report',
    summary: {
      filesScanned: currentScan.filesScanned,
      violations: currentScan.violations.length,
      byRule: countsByRule(currentScan.violations),
    },
    ...(baseline
      ? {
          baseline: {
            description: baseline.description,
            existingViolations: Object.values(baseline.counts).reduce(
              (total, count) => total + count,
              0,
            ),
            resolvedViolations: comparison.resolvedCount,
            newViolations: comparison.newViolations.length,
          },
          newViolations: comparison.newViolations,
        }
      : {}),
    violations: currentScan.violations,
  }

  if (options.json) console.log(JSON.stringify(report, null, 2))
  else printHumanReport(report, options.verbose)

  if (options.enforce && comparison.newViolations.length > 0) process.exit(1)
}

try {
  main()
} catch (error) {
  console.error(`Frontend boundary check failed: ${error.message}`)
  process.exit(2)
}
