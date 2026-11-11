#!/usr/bin/env node
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

const tableModule = /^antd\/(?:es|lib)\/table(?:\/|$)/
const antdBarrel = /^antd(?:\/(?:es|lib)(?:\/index(?:\.js)?)?)?$/
const implementation = 'src/components/admin-table.tsx'

export function isProductionSource(file) {
  return /^src\/.*\.tsx?$/.test(file) && !file.endsWith('.d.ts') &&
    !/\.(?:test|spec|stories)\./.test(file)
}

// Check module origins, not the local JSX name: aliases must not bypass ownership.
export function scanTableImports(text, file) {
  if (!isProductionSource(file) || file === implementation) return []
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true,
    file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const violations = []
  const add = (node, module, rule) => {
    const { line, character } = source.getLineAndCharacterOfPosition(node.getStart(source))
    violations.push({ file, line: line + 1, column: character + 1, rule,
      fingerprint: JSON.stringify([file, module, rule]) })
  }
  const inspectBindings = (node, module, bindings, opaque) => {
    if (tableModule.test(module)) {
      if (!opaque && bindings && bindings.every((item) => item.isTypeOnly)) return
      add(node, module, 'raw-antd-table')
    } else if (antdBarrel.test(module)) {
      if (opaque) add(node, module, 'opaque-antd-runtime')
      for (const item of bindings ?? []) {
        const name = (item.propertyName ?? item.name).text
        if (!item.isTypeOnly && (name === 'Table' || name === 'default')) {
          add(item, module, name === 'Table' ? 'raw-antd-table' : 'opaque-antd-runtime')
        }
      }
    }
  }
  const visit = (node) => {
    if (ts.isImportDeclaration(node) && ts.isStringLiteralLike(node.moduleSpecifier)) {
      const clause = node.importClause
      if (!clause?.isTypeOnly) {
        const bindings = clause?.namedBindings && ts.isNamedImports(clause.namedBindings)
          ? [...clause.namedBindings.elements] : undefined
        inspectBindings(node, node.moduleSpecifier.text, bindings,
          Boolean(clause?.name || (clause?.namedBindings && ts.isNamespaceImport(clause.namedBindings))))
      }
    } else if (ts.isExportDeclaration(node) && node.moduleSpecifier &&
      ts.isStringLiteralLike(node.moduleSpecifier) && !node.isTypeOnly) {
      const bindings = node.exportClause && ts.isNamedExports(node.exportClause)
        ? [...node.exportClause.elements] : undefined
      inspectBindings(node, node.moduleSpecifier.text, bindings, !bindings)
    } else if (ts.isImportEqualsDeclaration(node) && !node.isTypeOnly &&
      ts.isExternalModuleReference(node.moduleReference)) {
      const value = node.moduleReference.expression
      if (value && ts.isStringLiteralLike(value)) inspectBindings(node, value.text, undefined, true)
    } else if (ts.isCallExpression(node) &&
      (node.expression.kind === ts.SyntaxKind.ImportKeyword ||
        (ts.isIdentifier(node.expression) && node.expression.text === 'require'))) {
      const value = node.arguments[0]
      if (value && ts.isStringLiteralLike(value)) inspectBindings(node, value.text, undefined, true)
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
  return violations
}

export function newViolations(current, baseline) {
  const remaining = new Map()
  for (const item of baseline) remaining.set(item.fingerprint, (remaining.get(item.fingerprint) ?? 0) + 1)
  return current.filter((item) => {
    const count = remaining.get(item.fingerprint) ?? 0
    if (!count) return true
    remaining.set(item.fingerprint, count - 1)
    return false
  })
}

export function checkRepository(root, baseRef) {
  if (!baseRef || baseRef.startsWith('-') || /^0+$/.test(baseRef)) {
    throw new Error('Supply --base <reviewed-ref> or TABLE_BOUNDARY_BASE_REF; an empty/self baseline is not acceptance.')
  }
  const git = (...args) => execFileSync('git', args, {
    cwd: root, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  const head = git('rev-parse', '--verify', 'HEAD').trim()
  const resolved = git('rev-parse', '--verify', `${baseRef}^{commit}`).trim()
  const base = git('merge-base', head, resolved).trim()
  if (base === head) throw new Error('Baseline resolves to HEAD. Use the PR base or previous reviewed commit, not the current commit.')
  const baselineFiles = git('ls-tree', '-r', '--name-only', '-z', base, '--', 'src')
    .split('\0').filter(isProductionSource)
  const currentFiles = [...new Set(git('ls-files', '-z', '--cached', '--others', '--exclude-standard', '--', 'src')
    .split('\0').filter(isProductionSource))].filter((file) => existsSync(resolve(root, file)))
  const previous = baselineFiles.flatMap((file) => scanTableImports(git('show', `${base}:${file}`), file))
  const current = currentFiles.flatMap((file) => scanTableImports(readFileSync(resolve(root, file), 'utf8'), file))
  return { base, head, filesScanned: currentFiles.length, totalViolations: current.length,
    newViolations: newViolations(current, previous) }
}

function main() {
  const args = process.argv.slice(2)
  if (args.length && !(args.length === 2 && args[0] === '--base')) {
    throw new Error('Usage: node scripts/check-table-boundaries.mjs [--base <reviewed-ref>]')
  }
  const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
  const report = checkRepository(root, args[1] ?? process.env.TABLE_BOUNDARY_BASE_REF)
  console.log(JSON.stringify(report, null, 2))
  if (report.newViolations.length) {
    console.error('Use AdminTable and named Antd imports. Historical debt is reported, not a new-page template; do not reset the baseline to hide new violations.')
    process.exitCode = 1
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main() } catch (error) {
    console.error(`Table boundary check failed: ${error.message}`)
    process.exitCode = 2
  }
}
