import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { readPackCandidate, verifyPublicNpmRelease } from './verify-public-npm-release.mjs';

async function fixture(t) {
  const directory = await mkdtemp(path.join(tmpdir(), 'cli-npm-release-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const bytes = Buffer.from('exact candidate tar fixture');
  const descriptor = [{ name: '@opensoha/cli', version: '0.2.0', filename: 'opensoha-cli-0.2.0.tgz',
    integrity: `sha512-${createHash('sha512').update(bytes).digest('base64')}`, size: bytes.length }];
  const descriptorPath = path.join(directory, 'pack.json');
  await writeFile(descriptorPath, JSON.stringify(descriptor));
  await writeFile(path.join(directory, descriptor[0].filename), bytes);
  return { descriptor, options: { descriptorPath, packageDirectory: directory, tag: 'cli/v0.2.0', sha: 'a'.repeat(40) } };
}

test('candidate hashes use the actual packed bytes and exact CLI release identity', async (t) => {
  const { descriptor, options } = await fixture(t);
  const candidate = await readPackCandidate(options);
  assert.equal(candidate.integrity, descriptor[0].integrity);
  assert.equal(candidate.sha256, createHash('sha256').update('exact candidate tar fixture').digest('hex'));
  let observed;
  const receipt = await verifyPublicNpmRelease(options, async (expected) => {
    observed = expected;
    return { integrity: expected.integrity, sha256: expected.sha256, provenance: 'npm audit signatures passed' };
  });
  const { file, ...expected } = candidate;
  assert.deepEqual(observed, expected);
  assert.equal(observed.workflow, '.github/workflows/cli-release.yml');
  assert.equal(receipt.file, file);
});

test('npm12 exact package-key map retains candidate bytes and public provenance verification', async (t) => {
  const { descriptor, options } = await fixture(t);
  const entry = { ...descriptor[0], id: '@opensoha/cli@0.2.0', entryCount: 4, bundled: [] };
  await writeFile(options.descriptorPath, JSON.stringify({ '@opensoha/cli': entry }));
  const candidate = await readPackCandidate(options);
  assert.equal(candidate.integrity, entry.integrity);
  assert.equal(candidate.version, '0.2.0');
  const receipt = await verifyPublicNpmRelease(options, async (expected) => {
    assert.equal(expected.tag, 'cli/v0.2.0');
    assert.equal(expected.sha, options.sha);
    assert.equal(expected.workflow, '.github/workflows/cli-release.yml');
    return { integrity: expected.integrity, sha256: expected.sha256, provenance: 'npm audit signatures passed' };
  });
  assert.equal(receipt.sha256, candidate.sha256);
});

test('npm12 maps reject unknown or extra package keys and malformed entries', async (t) => {
  const { descriptor, options } = await fixture(t);
  const entry = descriptor[0];
  for (const value of [null, true, 'entry', {}, [], [entry, entry],
    { '@opensoha/contracts': entry }, { '@opensoha/cli': entry, extra: entry },
    { '@opensoha/cli': null }, { '@opensoha/cli': [] }, { '@opensoha/cli': 'entry' }]) {
    await writeFile(options.descriptorPath, JSON.stringify(value));
    await assert.rejects(readPackCandidate(options), /exactly one CLI pack descriptor|invalid CLI pack descriptor entry/u);
  }
});

test('npm12 maps preserve strict name, version, filename, size and SRI validation', async (t) => {
  const { descriptor, options } = await fixture(t);
  for (const change of [{ name: '@opensoha/contracts' }, { version: '0.2.1' }, { filename: '../escape.tgz' },
    { size: descriptor[0].size + 1 }, { size: String(descriptor[0].size) }, { integrity: 'sha512-wrong' }]) {
    await writeFile(options.descriptorPath, JSON.stringify({ '@opensoha/cli': { ...descriptor[0], ...change } }));
    await assert.rejects(readPackCandidate(options), /identity differs|bytes differ/u);
  }
});

test('wrong existing package SRI and signed source rejection cannot become successful recovery', async (t) => {
  const { options } = await fixture(t);
  await assert.rejects(verifyPublicNpmRelease(options, async (candidate) => ({
    integrity: 'sha512-wrong-package', sha256: candidate.sha256, provenance: 'npm audit signatures passed',
  })), /receipt differs/u);
  await assert.rejects(verifyPublicNpmRelease(options, async (candidate) => {
    assert.equal(candidate.tag, 'cli/v0.2.0');
    assert.equal(candidate.sha, 'a'.repeat(40));
    assert.equal(candidate.workflow, '.github/workflows/cli-release.yml');
    throw new Error('signed provenance source SHA differs');
  }), /signed provenance source SHA differs/u);
  await assert.rejects(verifyPublicNpmRelease(options, async (candidate) => ({
    integrity: candidate.integrity, sha256: candidate.sha256, provenance: 'metadata only',
  })), /receipt differs/u);
});

test('tampered tar, descriptor identity, unsafe filename and wrong tag fail before public verification', async (t) => {
  const { descriptor, options } = await fixture(t);
  await writeFile(path.join(options.packageDirectory, descriptor[0].filename), 'changed tar');
  await assert.rejects(readPackCandidate(options), /bytes differ/u);
  for (const change of [{ filename: '../escape.tgz' }, { version: '0.2.1' }, { name: '@opensoha/contracts' }]) {
    await writeFile(options.descriptorPath, JSON.stringify([{ ...descriptor[0], ...change }]));
    await assert.rejects(readPackCandidate(options), /identity differs/u);
  }
  await assert.rejects(readPackCandidate({ ...options, tag: 'v0.2.0' }), /exact stable CLI tag/u);
});

test('workflow packs and publishes the exact tar and verifies it before npx; only image gate checks unused images', async () => {
  const workflow = await readFile(new URL('../../.github/workflows/cli-release.yml', import.meta.url), 'utf8');
  const npm = workflow.slice(workflow.indexOf('  npm-publish:'), workflow.indexOf('  image:'));
  const image = workflow.slice(workflow.indexOf('  image:'));
  assert.ok(npm.includes('npm pack --workspace @opensoha/cli --pack-destination "$RUNNER_TEMP/cli-package" --json'));
  assert.ok(npm.includes('npm publish "$package_file" --access public --provenance'));
  assert.ok(npm.indexOf('verify-public-npm-release.mjs verify') < npm.indexOf('npx -y'));
  assert.ok(npm.includes('.error.code == "E404"'));
  assert.ok(!npm.includes('--dry-run'));
  assert.ok(!npm.includes('--images-unused'));
  assert.ok(image.includes('--images-unused'));
  assert.ok(image.indexOf('--images-unused') < image.indexOf('docker/login-action@'));
});


test('actual npm pack descriptor and tar pass candidate identity verification', async (t) => {
  const directory = await mkdtemp(path.join(tmpdir(), 'cli-npm-actual-pack-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const root = fileURLToPath(new URL('../../', import.meta.url));
  const packed = spawnSync('npm', ['pack', '--workspace', '@opensoha/cli', '--pack-destination', directory, '--json'], {
    cwd: root, encoding: 'utf8', env: { ...process.env, npm_config_update_notifier: 'false' },
  });
  assert.equal(packed.status, 0, packed.stderr);
  const descriptorPath = path.join(directory, 'pack.json');
  await writeFile(descriptorPath, packed.stdout);
  const { version } = JSON.parse(await readFile(new URL('../npm/cli/package.json', import.meta.url), 'utf8'));
  const candidate = await readPackCandidate({ descriptorPath, packageDirectory: directory, tag: `cli/v${version}`, sha: 'a'.repeat(40) });
  assert.equal(candidate.version, version);
  assert.equal(candidate.sha256, createHash('sha256').update(await readFile(candidate.file)).digest('hex'));
});
