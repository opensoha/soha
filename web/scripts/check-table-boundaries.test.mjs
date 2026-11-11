import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'vitest'
import { checkRepository, newViolations, scanTableImports } from './check-table-boundaries.mjs'

const page = 'src/features/example/list-page.tsx'
const scan = (text, file = page) => scanTableImports(text, file)

for (const source of [
  "import { Table } from 'antd'",
  "import { Table as DataGrid } from 'antd'",
  "import { Table } from 'antd/es'",
  "export { Table } from 'antd/lib/index.js'",
  "import Table from 'antd/es/table'",
  "import Grid from 'antd/lib/table/index'",
  "import Grid, { type TableProps } from 'antd/es/table'",
  "export { Table as Grid } from 'antd'",
  "export { default } from 'antd/es/table'",
  "import * as Antd from 'antd'",
  "import Antd from 'antd'",
  "export * from 'antd'",
  "const library = await import('antd')",
  "const library = await import('antd', {})",
  "export * from 'antd/lib'",
  "const library = require('antd/lib/table')",
  "import Antd = require('antd')",
]) {
  test(`rejects runtime bypass: ${source}`, () => assert.ok(scan(source).length > 0))
}
for (const source of [
  "import type { TableProps } from 'antd'",
  "import { type TableProps, type Table } from 'antd'",
  "import type Table from 'antd/es/table'",
  "export type { TableProps } from 'antd'",
  "export { type TableProps } from 'antd/es/table'",
  "import { Button, type TableProps } from 'antd'",
  "import { AdminTable } from '@/components/admin-table'",
  "const text = \"import { Table } from 'antd'\"",
  "// import { Table } from 'antd'",
  "import { Table } from './local-component'",
]) {
  test(`allows non-bypass: ${source}`, () => assert.equal(scan(source).length, 0))
}
test('only the exact shared implementation may own raw Table', () => {
  const source = "import { Table } from 'antd'"
  assert.equal(scan(source, 'src/components/admin-table.tsx').length, 0)
  assert.equal(scan(source, 'src/components/new-wrapper.tsx').length, 1)
  assert.equal(scan(source, 'src/features/example/list-page.test.tsx').length, 0)
  assert.equal(scan(source, 'src/features/example/list-page.stories.tsx').length, 0)
})
test('baseline allowances retain multiplicity and ignore line shifts', () => {
  const source = "import { Table } from 'antd'"
  const baseline = scan(source)
  assert.equal(newViolations(scan('\n\n' + source), baseline).length, 0)
  assert.equal(newViolations([...baseline, ...baseline], baseline).length, 1)
  assert.equal(newViolations([], baseline).length, 0)
  assert.equal(newViolations(scan(source, 'src/features/new/page.tsx'), baseline).length, 1)
})
test('real git comparison detects new, including untracked, source and rejects invalid baselines', () => {
  const root = mkdtempSync(join(tmpdir(), 'soha-table-boundary-'))
  const git = (...args) => execFileSync('git', args, { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim()
  const write = (file, text) => {
    const path = join(root, file)
    mkdirSync(join(path, '..'), { recursive: true })
    writeFileSync(path, text)
  }
  try {
    git('init', '-b', 'main')
    git('config', 'user.name', 'Boundary Test')
    git('config', 'user.email', 'boundary-test@example.invalid')
    write(page, "import { Table } from 'antd'\n")
    git('add', '.')
    git('commit', '-m', 'reviewed baseline')
    const base = git('rev-parse', 'HEAD')
    git('commit', '--allow-empty', '-m', 'candidate')
    assert.equal(checkRepository(root, base).newViolations.length, 0)
    write('src/features/new/page.tsx', "import { Table as Grid } from 'antd'\n")
    assert.equal(checkRepository(root, base).newViolations.length, 1)
    write('src/features/new/page.tsx', "import { AdminTable } from '@/components/admin-table'\n")
    assert.equal(checkRepository(root, base).newViolations.length, 0)
    assert.throws(() => checkRepository(root, 'HEAD'), /Baseline resolves to HEAD/)
    assert.throws(() => checkRepository(root, ''), /Supply --base/)
    assert.throws(() => checkRepository(root, '0000000000'), /Supply --base/)
    assert.throws(() => checkRepository(root, 'nonexistent-reviewed-ref'))
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})
