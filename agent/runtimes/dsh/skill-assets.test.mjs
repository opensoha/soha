import assert from 'node:assert/strict';
import { chmod, link, mkdir, mkdtemp, realpath, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { sha, stable } from './protocol.mjs';
import { prepareSkillAsset } from './skill-assets.mjs';

const ref = { kind: 'skill', id: 'read-only-diagnosis', version: '1.0.0' };
const dependency = { kind: 'mcp', id: 'diagnostics', version: '2' };
const instructions = '# Diagnose from scoped evidence\n';
const resource = 'Query only the named namespace.\n';

async function fixture(t) {
  const dir = await mkdtemp(join(await realpath(tmpdir()), 'soha-dsh-skill-'));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const root = join(dir, 'managed');
  const workspace = join(dir, 'workspace');
  await mkdir(join(root, 'references'), { recursive: true });
  await mkdir(workspace);
  const options = { workspace, assets: [dependency], deadline: Date.now() + 15_000 };
  const manifest = (entries, overrides = {}) => {
    const snapshot = {
      id: ref.id,
      version: ref.version,
      description: 'Diagnose using approved evidence',
      requires: [dependency],
      files: Object.fromEntries(Object.entries(entries).map(([path, content]) => [path, sha(content)])),
      ...overrides,
    };
    return { root, digest: sha(stable(snapshot)), ...snapshot };
  };
  return { root, workspace, options, manifest };
}

test('lists only pinned summaries and reads authorized bodies and resources on demand', async (t) => {
  const { root, options, manifest } = await fixture(t);
  const bodyPath = join(root, 'SKILL.md');
  const path = 'references/scope.md';
  const definition = manifest({ 'SKILL.md': instructions, [path]: resource });

  // No content files exist at prepare time.
  const asset = await prepareSkillAsset(definition, ref, options);
  t.after(() => asset.close());
  const [load, read] = asset.tools;
  assert.deepEqual(asset.tools.map(({ name }) => name), [
    'skill_read_only_diagnosis_load',
    'skill_read_only_diagnosis_resource',
  ]);
  assert.ok(load.description.includes(definition.digest));
  assert.ok(!JSON.stringify(asset.tools).includes(instructions));
  assert.deepEqual(read.inputSchema.properties.path.enum, [path]);
  await assert.rejects(load.invoke({}), { code: 'skill_content_rejected' });
  await writeFile(bodyPath, instructions);
  await writeFile(join(root, path), resource);
  assert.deepEqual(await load.invoke({}), {
    id: ref.id,
    version: ref.version,
    digest: definition.digest,
    path: 'SKILL.md',
    content: instructions,
  });
  assert.equal((await read.invoke({ path })).content, resource);
  await assert.rejects(read.invoke({ path: '../workspace/private.md' }), { code: 'skill_input_invalid' });
  await assert.rejects(load.invoke({ path }), { code: 'skill_input_invalid' });
  await writeFile(join(root, path), 'changed after prepare');
  await assert.rejects(read.invoke({ path }), { code: 'skill_content_rejected' });
  await assert.rejects(load.invoke({}, AbortSignal.abort()), { code: 'skill_asset_stopped' });
});

test('requires exact enabled MCP references and a valid pinned manifest', async (t) => {
  const { root, workspace, options, manifest } = await fixture(t);
  const files = { 'SKILL.md': instructions };
  const definition = manifest(files);
  for (const assets of [[], [{ ...dependency, version: '3' }], [{ ...dependency, kind: 'skill' }]]) {
    await assert.rejects(prepareSkillAsset(definition, ref, { ...options, assets }), {
      code: 'skill_dependency_missing',
    });
  }
  await assert.rejects(prepareSkillAsset(definition, { ...ref, version: 'other' }, options), {
    code: 'skill_asset_invalid',
  });
  await assert.rejects(prepareSkillAsset({ ...definition, description: 'edited' }, ref, options), {
    code: 'skill_asset_invalid',
  });
  await assert.rejects(prepareSkillAsset(manifest({ ...files, '../escaped.md': resource }), ref, options), {
    code: 'skill_asset_invalid',
  });
  await assert.rejects(prepareSkillAsset({ ...definition, root: workspace }, ref, options), {
    code: 'skill_asset_invalid',
  });
  const alias = join(workspace, 'alias');
  await symlink(root, alias);
  await assert.rejects(prepareSkillAsset({ ...definition, root: alias }, ref, options), {
    code: 'skill_asset_invalid',
  });
  await assert.rejects(prepareSkillAsset(definition, ref, { ...options, deadline: Date.now() - 1 }), {
    code: 'skill_asset_stopped',
  });
});

test('rejects unsafe content even when its byte digest matches', async (t) => {
  const { root, workspace, options, manifest } = await fixture(t);
  const files = {
    'SKILL.md': instructions,
    'binary.md': Buffer.from([0xff, 0xfe, 0x00]),
    'oversized.txt': 'x'.repeat(16 * 1024 + 1),
    'executable.txt': resource,
    'script.txt': '#!/bin/sh\necho run\n',
    'symlink.md': resource,
    'symlink-dir/file.md': resource,
    'hardlink.md': resource,
  };
  for (const [path, content] of Object.entries(files).slice(0, 5)) {
    await writeFile(join(root, path), content);
  }
  await chmod(join(root, 'executable.txt'), 0o755);
  const outside = join(workspace, 'file.md');
  await writeFile(outside, resource);
  await symlink(outside, join(root, 'symlink.md'));
  await symlink(workspace, join(root, 'symlink-dir'));
  await link(outside, join(root, 'hardlink.md'));

  const asset = await prepareSkillAsset(manifest(files), ref, options);
  const [load, read] = asset.tools;
  try {
    for (const path of Object.keys(files).slice(1)) {
      await assert.rejects(read.invoke({ path }), { code: 'skill_content_rejected' });
    }
  } finally {
    await asset.close();
  }
  await assert.rejects(load.invoke({}), { code: 'skill_asset_stopped' });
});
