#!/usr/bin/env node

import { existsSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs'
import { dirname, join, posix, relative, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import { format, resolveConfig } from 'prettier'
import ts from 'typescript'

const VERSION = 3
const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const projectRoot = resolve(scriptDirectory, '..')
const defaultBaseline = resolve(scriptDirectory, 'baselines/route-baseline.json')

function parseArguments(argv) {
  const options = {
    baseline: defaultBaseline,
    check: false,
    json: false,
    writeBaseline: undefined,
  }

  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index]
    if (argument === '--json') options.json = true
    else if (argument === '--check') options.check = true
    else if (argument === '--baseline' || argument === '--write-baseline') {
      const value = argv[index + 1]
      if (!value || value.startsWith('--')) throw new Error(`${argument} requires a file`)
      index += 1
      if (argument === '--baseline') options.baseline = resolve(process.cwd(), value)
      else options.writeBaseline = resolve(process.cwd(), value)
    } else if (argument === '--help' || argument === '-h') {
      console.log(`Usage: node scripts/analyze-route-baseline.mjs [options]

Options:
  --json                    Print the complete analysis as JSON.
  --check                   Compare the analysis with the saved baseline.
  --baseline <file>         Select a baseline file.
  --write-baseline <file>   Write the current analysis as a baseline.
  --help                    Show this help.`)
      process.exit(0)
    } else throw new Error(`Unknown option: ${argument}`)
  }
  return options
}

function projectPath(filePath) {
  return relative(projectRoot, filePath).split(sep).join('/')
}

function parseSource(filePath) {
  return ts.createSourceFile(
    projectPath(filePath),
    readFileSync(filePath, 'utf8'),
    ts.ScriptTarget.Latest,
    true,
    filePath.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  )
}

function collectFiles(directory, predicate) {
  if (!existsSync(directory)) return []
  const files = []
  for (const entry of readdirSync(directory)) {
    const filePath = join(directory, entry)
    const stats = statSync(filePath)
    if (stats.isDirectory()) files.push(...collectFiles(filePath, predicate))
    else if (predicate(filePath)) files.push(filePath)
  }
  return files
}

function propertyName(name) {
  if (ts.isIdentifier(name) || ts.isStringLiteralLike(name)) return name.text
  return undefined
}

function jsxTagName(name) {
  return ts.isIdentifier(name) ? name.text : name.getText()
}

function collectRouterPaths(sourceFile) {
  const paths = []
  function visit(node) {
    if (
      (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) &&
      jsxTagName(node.tagName) === 'Route'
    ) {
      const pathAttribute = node.attributes.properties.find(
        (attribute) => ts.isJsxAttribute(attribute) && attribute.name.text === 'path',
      )
      if (pathAttribute && ts.isJsxAttribute(pathAttribute) && pathAttribute.initializer) {
        if (ts.isStringLiteral(pathAttribute.initializer))
          paths.push(pathAttribute.initializer.text)
        else if (
          ts.isJsxExpression(pathAttribute.initializer) &&
          pathAttribute.initializer.expression &&
          ts.isStringLiteralLike(pathAttribute.initializer.expression)
        ) {
          paths.push(pathAttribute.initializer.expression.text)
        }
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(sourceFile)
  return [...new Set(paths)].sort()
}

function dynamicImportSource(node) {
  let source
  function visit(current) {
    if (
      ts.isCallExpression(current) &&
      current.expression.kind === ts.SyntaxKind.ImportKeyword &&
      current.arguments.length === 1 &&
      ts.isStringLiteralLike(current.arguments[0])
    ) {
      source = current.arguments[0].text
    }
    if (!source) ts.forEachChild(current, visit)
  }
  visit(node)
  return source
}

function normalizeManifestSource(manifestFile, source) {
  if (!source.startsWith('.')) return source
  const resolvedSource = posix.normalize(
    posix.join(posix.dirname(projectPath(manifestFile)), source),
  )
  return resolvedSource.startsWith('src/') ? `@/${resolvedSource.slice(4)}` : resolvedSource
}

function defaultRouteModuleExport(node) {
  let exportName
  function visit(current) {
    if (
      ts.isPropertyAssignment(current) &&
      propertyName(current.name) === 'default' &&
      ts.isPropertyAccessExpression(current.initializer)
    ) {
      exportName = current.initializer.name.text
    }
    if (!exportName) ts.forEachChild(current, visit)
  }
  visit(node)
  return exportName
}

function collectManifestRoutes(sourceFile, manifestFile) {
  const routes = []

  function unwrapExpression(expression) {
    let current = expression
    while (
      ts.isAsExpression(current) ||
      ts.isSatisfiesExpression(current) ||
      ts.isParenthesizedExpression(current) ||
      ts.isTypeAssertionExpression(current)
    ) {
      current = current.expression
    }
    return current
  }

  function literalProperty(object, name) {
    const property = object.properties.find(
      (candidate) => ts.isPropertyAssignment(candidate) && propertyName(candidate.name) === name,
    )
    return property &&
      ts.isPropertyAssignment(property) &&
      ts.isStringLiteralLike(property.initializer)
      ? property.initializer.text
      : undefined
  }

  function visit(node) {
    const definitions =
      ts.isCallExpression(node) && node.arguments[0]
        ? unwrapExpression(node.arguments[0])
        : undefined
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      node.expression.text === 'defineRoutes' &&
      definitions &&
      ts.isArrayLiteralExpression(definitions)
    ) {
      for (const element of definitions.elements) {
        if (!ts.isObjectLiteralExpression(element)) continue
        const metaProperty = element.properties.find(
          (property) => ts.isPropertyAssignment(property) && propertyName(property.name) === 'meta',
        )
        if (
          !metaProperty ||
          !ts.isPropertyAssignment(metaProperty) ||
          !ts.isObjectLiteralExpression(metaProperty.initializer)
        ) {
          continue
        }

        const path = literalProperty(metaProperty.initializer, 'path')
        if (!path) continue
        const id = literalProperty(metaProperty.initializer, 'id') ?? path
        const aliasesProperty = element.properties.find(
          (property) =>
            ts.isPropertyAssignment(property) && propertyName(property.name) === 'aliases',
        )
        const aliases =
          aliasesProperty &&
          ts.isPropertyAssignment(aliasesProperty) &&
          ts.isArrayLiteralExpression(aliasesProperty.initializer)
            ? aliasesProperty.initializer.elements
                .filter(ts.isStringLiteralLike)
                .map(({ text }) => text)
            : []
        const importSource = dynamicImportSource(element)
        const moduleExport = defaultRouteModuleExport(element) ?? 'default'
        routes.push({
          aliases,
          id,
          path,
          ...(importSource
            ? {
                lazy: {
                  exportName: moduleExport === 'default' ? `${id}:default` : moduleExport,
                  moduleExport,
                  source: normalizeManifestSource(manifestFile, importSource),
                },
              }
            : {}),
        })
      }
      return
    }
    ts.forEachChild(node, visit)
  }

  visit(sourceFile)
  return routes
}

function analyze() {
  const routerFile = resolve(projectRoot, 'src/routes/index.tsx')
  const registryFile = resolve(projectRoot, 'src/routes/registry.ts')
  const fallbackManifestFile = resolve(projectRoot, 'src/routes/fallback-routes.ts')
  const manifestFiles = [
    ...collectFiles(resolve(projectRoot, 'src/features'), (filePath) =>
      posix.basename(filePath).match(/^routes\.tsx?$/),
    ),
    fallbackManifestFile,
  ].sort()
  const manifestRoutes = manifestFiles.flatMap((filePath) =>
    collectManifestRoutes(parseSource(filePath), filePath),
  )
  const inlineJsxPaths = collectRouterPaths(parseSource(routerFile))
  const runtimePaths = [
    ...new Set([
      ...inlineJsxPaths,
      ...manifestRoutes.flatMap(({ aliases, path }) => [path, ...aliases]),
    ]),
  ].sort()
  const metadataPaths = [...new Set(manifestRoutes.map(({ path }) => path))].sort()
  const lazyRoutes = manifestRoutes
    .flatMap(({ lazy }) => (lazy ? [lazy] : []))
    .sort((left, right) => left.exportName.localeCompare(right.exportName))
  const runtimeSet = new Set(runtimePaths)
  const metadataSet = new Set(metadataPaths)
  const lazySources = [...new Set(lazyRoutes.map(({ source }) => source))].sort()
  const sourceGroups = Object.fromEntries(
    lazySources.map((source) => [
      source,
      lazyRoutes.filter((entry) => entry.source === source).map(({ exportName }) => exportName),
    ]),
  )

  return {
    version: VERSION,
    sources: {
      router: projectPath(routerFile),
      registry: projectPath(registryFile),
      manifests: manifestFiles.map(projectPath),
    },
    summary: {
      runtimePaths: runtimePaths.length,
      metadataPaths: metadataPaths.length,
      overlappingPaths: metadataPaths.filter((path) => runtimeSet.has(path)).length,
      runtimeOnlyPaths: runtimePaths.filter((path) => !metadataSet.has(path)).length,
      metadataOnlyPaths: metadataPaths.filter((path) => !runtimeSet.has(path)).length,
      inlineJsxPaths: inlineJsxPaths.length,
      lazyRouteLoads: lazyRoutes.length,
      lazySourceModules: lazySources.length,
    },
    paths: {
      runtime: runtimePaths,
      metadata: metadataPaths,
      overlap: metadataPaths.filter((path) => runtimeSet.has(path)),
      runtimeOnly: runtimePaths.filter((path) => !metadataSet.has(path)),
      metadataOnly: metadataPaths.filter((path) => !runtimeSet.has(path)),
      inlineJsx: inlineJsxPaths,
    },
    lazy: { routes: lazyRoutes, sources: lazySources, sourceGroups },
  }
}

function comparable(analysis) {
  return {
    paths: {
      runtime: analysis.paths.runtime,
      metadata: analysis.paths.metadata,
      inlineJsx: analysis.paths.inlineJsx,
    },
  }
}

function printHuman(analysis, check) {
  console.log('Route architecture baseline')
  console.log(`  Runtime paths:      ${analysis.summary.runtimePaths}`)
  console.log(`  Metadata paths:     ${analysis.summary.metadataPaths}`)
  console.log(`  Shared paths:       ${analysis.summary.overlappingPaths}`)
  console.log(`  Runtime-only paths: ${analysis.summary.runtimeOnlyPaths}`)
  console.log(`  Metadata-only:      ${analysis.summary.metadataOnlyPaths}`)
  console.log(`  Inline JSX paths:   ${analysis.summary.inlineJsxPaths}`)
  console.log(`  Lazy route loads:   ${analysis.summary.lazyRouteLoads}`)
  console.log(`  Lazy source modules:${String(analysis.summary.lazySourceModules).padStart(4)}`)
  if (check) console.log('  Baseline:           matches')
  console.log('')
  console.log('Largest lazy source groups')
  for (const [source, names] of Object.entries(analysis.lazy.sourceGroups)
    .sort((left, right) => right[1].length - left[1].length)
    .slice(0, 10)) {
    console.log(`  ${String(names.length).padStart(3)}  ${source}`)
  }
}

try {
  const options = parseArguments(process.argv.slice(2))
  const analysis = analyze()
  if (options.writeBaseline) {
    writeFileSync(
      options.writeBaseline,
      await format(JSON.stringify({ ...analysis, generatedAt: new Date().toISOString() }), {
        ...(await resolveConfig(defaultBaseline)),
        filepath: options.writeBaseline,
      }),
      'utf8',
    )
  }
  if (options.check) {
    const baseline = JSON.parse(readFileSync(options.baseline, 'utf8'))
    if (JSON.stringify(comparable(analysis)) !== JSON.stringify(comparable(baseline))) {
      console.error(`Route architecture baseline changed: ${projectPath(options.baseline)}`)
      process.exit(1)
    }
  }
  if (options.json) console.log(JSON.stringify(analysis, null, 2))
  else printHuman(analysis, options.check)
} catch (error) {
  console.error(`Route architecture analysis failed: ${error.message}`)
  process.exit(2)
}
