import { createRequire } from 'node:module'
import assert from 'node:assert/strict'
import { execFileSync, spawnSync } from 'node:child_process'
import {
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'vitest'
import { changedFiles } from './format-changed.mjs'

for (const nested of [false, true]) {
  test(`format guard preserves selection and uses hoisted tooling (nested Web=${nested})`, () => {
    const root = mkdtempSync(join(tmpdir(), 'soha-format-path-'))
    const project = nested ? join(root, 'web') : root
    const git = (...args) =>
      execFileSync('git', args, {
        cwd: root,
        encoding: 'utf8',
        stdio: ['ignore', 'pipe', 'pipe'],
      }).trim()
    const write = (file, content) => {
      mkdirSync(dirname(join(project, file)), { recursive: true })
      writeFileSync(join(project, file), content)
    }
    try {
      mkdirSync(join(project, 'scripts'), { recursive: true })
      copyFileSync(
        fileURLToPath(new URL('./format-changed.mjs', import.meta.url)),
        join(project, 'scripts/format-changed.mjs'),
      )
      const dependencies = dirname(
        dirname(createRequire(import.meta.url).resolve('prettier/package.json')),
      )
      symlinkSync(dependencies, join(root, 'node_modules'), 'dir')
      git('init', '-b', 'main')
      git('config', 'user.name', 'Format Test')
      git('config', 'user.email', 'format-test@example.invalid')
      writeFileSync(join(root, '.gitignore'), 'node_modules/\n')
      write('README.md', '# Baseline\n')
      write('historical.js', 'const historical = 1\n')
      write('deleted.json', '{}\n')
      git('add', '.')
      git('commit', '-m', 'baseline')
      write('README.md', '# Candidate\n')
      write('historical.js', 'const historical=2\n')
      rmSync(join(project, 'deleted.json'))
      write('new.json', '{"new":true}')
      write('dist/generated.json', '{"generated":true}')
      if (nested) writeFileSync(join(root, 'unrelated.json'), '{"outside":true}')
      assert.deepEqual(changedFiles(project), ['README.md', 'new.json'])
      const invoke = (mode) =>
        spawnSync(process.execPath, [join(project, 'scripts/format-changed.mjs'), mode], {
          cwd: project,
          encoding: 'utf8',
        })
      assert.notEqual(invoke('--check').status, 0, 'bad new JSON must fail formatting')
      const written = invoke('--write')
      assert.equal(written.status, 0, written.stderr)
      assert.equal(invoke('--check').status, 0)
      assert.equal(readFileSync(join(project, 'historical.js'), 'utf8'), 'const historical=2\n')
      assert.equal(readFileSync(join(project, 'dist/generated.json'), 'utf8'), '{"generated":true}')
      if (nested)
        assert.equal(readFileSync(join(root, 'unrelated.json'), 'utf8'), '{"outside":true}')
    } finally {
      rmSync(root, { recursive: true, force: true })
    }
  })
}
