#!/usr/bin/env node
// Read-only qualification of an immutable CLI release; target source is data only.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {readFile, writeFile} from 'node:fs/promises';
import {pathToFileURL} from 'node:url';
import {verifyNpmRelease} from '../../contracts/scripts/verify-npm-release.mjs';

const repository = 'opensoha/soha';
const workflow = '.github/workflows/cli-release.yml';
const name = '@opensoha/cli';
const shaPattern = /^[a-f0-9]{40}$/;
const prerequisites = [
  'Verify release identity and declared public SDK',
  ...['linux', 'darwin', 'windows'].flatMap(os => ['amd64', 'arm64'].map(arch => `Build ${os}/${arch}`)),
  'Checksums', 'Release binary smoke', 'GitHub Release', 'Docker image', 'public-download',
];

export function context(env = process.env) {
  assert.equal(env.RELEASE_MODE, 'verify-existing', 'expected verify-existing mode');
  assert.equal(env.GITHUB_EVENT_NAME, 'workflow_dispatch', 'expected workflow_dispatch');
  assert.equal(env.GITHUB_REPOSITORY, repository, 'expected owning Root repository');
  assert.equal(env.GITHUB_REF, 'refs/heads/main', 'verification must run from main');
  assert.equal(env.GITHUB_WORKFLOW_REF, `${repository}/${workflow}@refs/heads/main`, 'expected trusted main workflow');
  assert.match(env.GITHUB_SHA ?? '', shaPattern, 'invalid verifier code SHA');
  assert.match(env.GITHUB_RUN_ID ?? '', /^[1-9][0-9]*$/, 'invalid verification run');
  assert.match(env.GITHUB_RUN_ATTEMPT ?? '', /^[1-9][0-9]*$/, 'invalid verification attempt');
  const match = /^cli\/v((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))$/.exec(env.RELEASE_TAG ?? '');
  assert.ok(match, 'expected stable cli/vX.Y.Z tag');
  assert.match(env.RELEASE_SHA ?? '', shaPattern, 'invalid original source SHA');
  assert.match(env.ORIGINAL_RUN ?? '', /^[1-9][0-9]*$/, 'invalid original release run');
  assert.match(env.EXPECTED_SHA256 ?? '', /^[a-f0-9]{64}$/, 'invalid original SHA-256');
  const integrity = env.EXPECTED_INTEGRITY ?? '';
  assert.match(integrity, /^sha512-[A-Za-z0-9+/]{86}==$/, 'invalid original SHA-512 SRI');
  assert.equal(`sha512-${Buffer.from(integrity.slice(7), 'base64').toString('base64')}`, integrity, 'noncanonical SRI');
  return {name, version: match[1], tag: env.RELEASE_TAG, sha: env.RELEASE_SHA, workflow,
    integrity, sha256: env.EXPECTED_SHA256, originalRun: env.ORIGINAL_RUN,
    verifierSha: env.GITHUB_SHA, verificationRun: env.GITHUB_RUN_ID, verificationAttempt: env.GITHUB_RUN_ATTEMPT};
}

export function githubArgs(endpoint, paginate = false) {
  const args = ['api', '--method', 'GET'];
  if (paginate) args.push('--paginate', '--slurp');
  args.push(endpoint ? `repos/${repository}/${endpoint}` : `repos/${repository}`);
  return args;
}

export async function github(endpoint, paginate = false) {
  const result = spawnSync('gh', githubArgs(endpoint, paginate), {encoding: 'utf8', timeout: 60_000, maxBuffer: 8 * 1024 * 1024});
  assert.equal(result.status, 0, `GitHub read failed: ${endpoint}: ${result.stderr ?? ''}`);
  return JSON.parse(result.stdout);
}

function pages(value, key) {
  assert.ok(Array.isArray(value) && value.length > 0, `missing ${key} pages`);
  return value.flatMap(page => {
    assert.ok(Array.isArray(page[key]), `invalid ${key} response`);
    return page[key];
  });
}

async function successfulMainCI(sha, api) {
  const runs = pages(await api(`actions/workflows/ci.yml/runs?head_sha=${sha}&per_page=100`, true), 'workflow_runs');
  const matching = runs.filter(run => run.head_sha === sha && run.head_branch === 'main' && run.event === 'push');
  matching.sort((a, b) => b.id - a.id || b.run_attempt - a.run_attempt);
  const run = matching[0];
  assert.ok(run && run.repository?.full_name === repository && run.head_repository?.full_name === repository,
    'missing exact-source owning Root main/push CI');
  assert.equal(run.status, 'completed', 'exact-source CI still running');
  assert.equal(run.conclusion, 'success', 'exact-source CI did not succeed');
  return {id: run.id, attempt: run.run_attempt, sha};
}

export async function authorize(env = process.env, api = github) {
  const input = context(env);
  const repo = await api('');
  assert.equal(repo.full_name, repository);
  assert.equal(repo.default_branch, 'main', 'Root default branch must be main');
  const main = await api('branches/main');
  assert.match(main.commit?.sha ?? '', shaPattern);
  for (const sha of new Set([input.sha, input.verifierSha])) {
    const comparison = await api(`compare/${sha}...${main.commit.sha}`);
    assert.ok(['ahead', 'identical'].includes(comparison.status) && comparison.merge_base_commit?.sha === sha,
      'source or verifier is not an ancestor of Root main');
  }
  const ref = await api(`git/ref/tags/${encodeURIComponent(input.tag)}`);
  assert.equal(ref.ref, `refs/tags/${input.tag}`);
  let object = ref.object;
  const tagObject = object?.sha;
  for (let depth = 0; object?.type === 'tag' && depth < 4; depth++) {
    assert.match(object.sha ?? '', shaPattern);
    object = (await api(`git/tags/${object.sha}`)).object;
  }
  assert.equal(object?.type, 'commit', 'tag did not resolve to a commit');
  assert.equal(object.sha, input.sha, 'immutable tag source mismatch');
  const file = await api(`contents/cli/npm/cli/package.json?ref=${input.sha}`);
  assert.equal(file.type, 'file');
  assert.equal(file.encoding, 'base64');
  assert.ok(Number.isInteger(file.size) && file.size > 0 && file.size <= 65536, 'invalid source manifest size');
  const bytes = Buffer.from(file.content ?? '', 'base64');
  assert.equal(bytes.length, file.size, 'source manifest size mismatch');
  const manifest = JSON.parse(bytes.toString('utf8'));
  assert.equal(manifest.name, name);
  assert.equal(manifest.version, input.version, 'tag/source package version mismatch');
  const sourceCI = await successfulMainCI(input.sha, api);
  const verifierCI = input.sha === input.verifierSha ? sourceCI : await successfulMainCI(input.verifierSha, api);
  const definition = await api('actions/workflows/cli-release.yml');
  assert.equal(definition.path, workflow);
  const run = await api(`actions/runs/${input.originalRun}/attempts/1`);
  assert.equal(String(run.id), input.originalRun);
  assert.equal(run.run_attempt, 1);
  assert.equal(run.repository?.full_name, repository);
  assert.equal(run.head_repository?.full_name, repository);
  assert.equal(run.workflow_id, definition.id, 'original run has wrong workflow');
  assert.equal(run.path, workflow);
  assert.equal(run.head_sha, input.sha, 'original run source mismatch');
  assert.equal(run.head_branch, input.tag, 'original run tag mismatch');
  assert.ok(['push', 'workflow_dispatch'].includes(run.event), 'unexpected original release event');
  assert.equal(run.status, 'completed', 'original run still running');
  const allJobs = pages(await api(`actions/runs/${input.originalRun}/attempts/1/jobs?per_page=100`, true), 'jobs');
  assert.equal(new Set(allJobs.map(job => job.id)).size, allJobs.length, 'duplicate original jobs');
  const readOnlyJobs = allJobs.filter(job => job.name === 'Read-only existing npm qualification');
  assert.ok(readOnlyJobs.length <= 1, 'duplicate read-only qualification job');
  for (const job of readOnlyJobs) {
    assert.equal(job.status, 'completed');
    assert.equal(job.conclusion, 'skipped', 'original release unexpectedly executed read-only mode');
  }
  const jobs = allJobs.filter(job => job.name !== 'Read-only existing npm qualification');
  // The original release has twelve qualified prerequisites and one npm publisher.
  assert.equal(jobs.length, prerequisites.length + 1, 'unexpected original release job set');
  const qualified = prerequisites.map(jobName => {
    const matches = jobs.filter(job => job.name === jobName);
    assert.equal(matches.length, 1, `missing or duplicate prerequisite: ${jobName}`);
    const job = matches[0];
    assert.equal(job.status, 'completed', `unfinished prerequisite: ${jobName}`);
    assert.equal(job.conclusion, 'success', `unsuccessful prerequisite: ${jobName}`);
    return {id: job.id, name: job.name};
  });
  const npm = jobs.filter(job => job.name === 'npm package');
  assert.equal(npm.length, 1);
  assert.equal(npm[0].status, 'completed');
  assert.ok(['success', 'failure'].includes(npm[0].conclusion), 'original npm job was not executed');
  return {...input, repository, mainSha: main.commit.sha, tagObject, sourceCI, verifierCI,
    originalAttempt: 1, prerequisites: qualified, originalNpmJob: npm[0].id};
}

function boundProof(proof, env) {
  const input = context(env);
  for (const [key, value] of Object.entries(input)) assert.equal(proof[key], value, `qualification binding mismatch: ${key}`);
  assert.equal(proof.repository, repository);
  assert.equal(proof.originalAttempt, 1);
  assert.equal(proof.prerequisites?.length, prerequisites.length);
  return input;
}

export async function verifyPublic(proof, env = process.env, verifier = verifyNpmRelease) {
  const input = boundProof(proof, env);
  // GitHub credentials are confined to the preceding metadata read step.
  for (const key of ['GH_TOKEN', 'GITHUB_TOKEN', 'NODE_AUTH_TOKEN', 'NPM_TOKEN', 'ACTIONS_ID_TOKEN_REQUEST_TOKEN']) {
    assert.ok(!env[key], `${key} must not reach public npm verification`);
  }
  const receipt = await verifier(input);
  assert.equal(receipt.integrity, input.integrity);
  assert.equal(receipt.sha256, input.sha256);
  assert.equal(receipt.provenance, 'npm audit signatures passed');
  return receipt;
}

export function receipt(proof, publicReceipt, binary, env = process.env) {
  const input = boundProof(proof, env);
  assert.equal(publicReceipt.integrity, input.integrity);
  assert.equal(publicReceipt.sha256, input.sha256);
  assert.equal(publicReceipt.provenance, 'npm audit signatures passed');
  assert.equal(binary.version, input.version, 'fresh-cache CLI version mismatch');
  assert.equal(binary.commit, input.sha, 'fresh-cache CLI source mismatch');
  return {...proof, publicPackage: publicReceipt, binary: {version: binary.version, commit: binary.commit},
    qualification: 'read-only existing release verified',
    provenanceBinding: 'cryptographic repository/workflow/tag/source; original run checked separately through GitHub Actions API',
    verifiedAt: new Date().toISOString()};
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const [mode, ...paths] = process.argv.slice(2);
    const load = async path => JSON.parse(await readFile(path, 'utf8'));
    let result;
    if (mode === 'authorize' && paths.length === 1) result = await authorize();
    else if (mode === 'verify' && paths.length === 2) result = await verifyPublic(await load(paths[0]));
    else if (mode === 'receipt' && paths.length === 4) result = receipt(await load(paths[0]), await load(paths[1]), await load(paths[2]));
    else throw new Error('usage: authorize OUTPUT | verify PROOF OUTPUT | receipt PROOF PUBLIC BINARY OUTPUT');
    await writeFile(paths.at(-1), JSON.stringify(result, null, 2) + '\n', {mode: 0o600, flag: 'wx'});
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
