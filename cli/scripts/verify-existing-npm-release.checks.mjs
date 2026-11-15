import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import {authorize, context, verifyPublic, receipt} from './verify-existing-npm-release.mjs';
import {waitForPublicPackage} from './verify-existing-npm-release-readiness.mjs';

const source = '7'.repeat(40), verifier = '8'.repeat(40), main = '9'.repeat(40);
const env = {RELEASE_MODE: 'verify-existing', GITHUB_EVENT_NAME: 'workflow_dispatch',
  GITHUB_REPOSITORY: 'opensoha/soha', GITHUB_REF: 'refs/heads/main',
  GITHUB_WORKFLOW_REF: 'opensoha/soha/.github/workflows/cli-release.yml@refs/heads/main',
  GITHUB_SHA: verifier, GITHUB_RUN_ID: '99', GITHUB_RUN_ATTEMPT: '1',
  RELEASE_TAG: 'cli/v0.2.1', RELEASE_SHA: source, ORIGINAL_RUN: '42',
  EXPECTED_INTEGRITY: `sha512-${Buffer.alloc(64, 1).toString('base64')}`, EXPECTED_SHA256: 'a'.repeat(64)};
const jobNames = ['Verify release identity and declared public SDK',
  ...['linux', 'darwin', 'windows'].flatMap(os => ['amd64', 'arm64'].map(arch => `Build ${os}/${arch}`)),
  'Checksums', 'Release binary smoke', 'GitHub Release', 'Docker image', 'public-download', 'npm package'];
function fixture(change = () => {}) {
  const manifest = Buffer.from(JSON.stringify({name: '@opensoha/cli', version: '0.2.1'}));
  const data = {
    '': {full_name: 'opensoha/soha', default_branch: 'main'},
    'branches/main': {commit: {sha: main}, protected: false},
    [`compare/${source}...${main}`]: {status: 'ahead', merge_base_commit: {sha: source}},
    [`compare/${verifier}...${main}`]: {status: 'ahead', merge_base_commit: {sha: verifier}},
    'git/ref/tags/cli%2Fv0.2.1': {ref: 'refs/tags/cli/v0.2.1', object: {type: 'tag', sha: 'b'.repeat(40)}},
    [`git/tags/${'b'.repeat(40)}`]: {object: {type: 'commit', sha: source}},
    [`contents/cli/npm/cli/package.json?ref=${source}`]: {type: 'file', encoding: 'base64', size: manifest.length, content: manifest.toString('base64')},
    'actions/workflows/cli-release.yml': {id: 12, path: '.github/workflows/cli-release.yml'},
    'actions/runs/42/attempts/1': {id: 42, run_attempt: 1, repository: {full_name: 'opensoha/soha'},
      head_repository: {full_name: 'opensoha/soha'}, workflow_id: 12, path: '.github/workflows/cli-release.yml',
      head_sha: source, head_branch: 'cli/v0.2.1', event: 'push', status: 'completed', conclusion: 'failure'},
    'actions/runs/42/attempts/1/jobs?per_page=100': [{jobs: jobNames.map((name, i) => ({id: i + 1, name, status: 'completed', conclusion: name === 'npm package' ? 'failure' : 'success'}))}],
  };
  for (const sha of [source, verifier]) data[`actions/workflows/ci.yml/runs?head_sha=${sha}&per_page=100`] = [{workflow_runs: [{id: sha === source ? 40 : 41, run_attempt: 1,
    repository: {full_name: 'opensoha/soha'}, head_repository: {full_name: 'opensoha/soha'}, head_sha: sha,
    head_branch: 'main', event: 'push', status: 'completed', conclusion: 'success'}]}];
  change(data);
  return async endpoint => { assert.ok(Object.hasOwn(data, endpoint), `unexpected API read ${endpoint}`); return structuredClone(data[endpoint]); };
}

test('qualifies source/main CI and the original failed npm run without branch-protection assumptions', async () => {
  const proof = await authorize(env, fixture());
  assert.equal(proof.sha, source);
  assert.equal(proof.verifierSha, verifier);
  assert.equal(proof.prerequisites.length, 12);
  const publicReceipt = await verifyPublic(proof, env, async input => {
    assert.equal(input.sha, source);
    assert.equal(input.workflow, '.github/workflows/cli-release.yml');
    return {integrity: input.integrity, sha256: input.sha256, provenance: 'npm audit signatures passed'};
  });
  const result = receipt(proof, publicReceipt, {version: '0.2.1', commit: source}, env);
  assert.equal(result.qualification, 'read-only existing release verified');
  await authorize(env, fixture(data => data['actions/runs/42/attempts/1/jobs?per_page=100'][0].jobs.push(
    {id: 14, name: 'Read-only existing npm qualification', status: 'completed', conclusion: 'skipped'})));
  assert.match(result.provenanceBinding, /original run checked separately/);
});

test('fails bad mode, ref, repository, workflow, tag, SHA, run and malformed digest before API reads', async () => {
  for (const [key, value] of [
    ['RELEASE_MODE', 'publish'], ['RELEASE_MODE', 'unknown'], ['GITHUB_EVENT_NAME', 'push'],
    ['GITHUB_REF', 'refs/tags/cli/v0.2.1'], ['GITHUB_REPOSITORY', 'fork/soha'],
    ['GITHUB_WORKFLOW_REF', 'opensoha/soha/.github/workflows/other.yml@refs/heads/main'],
    ['RELEASE_TAG', 'v0.2.1'], ['RELEASE_TAG', 'agent/v0.2.1'], ['RELEASE_TAG', 'cli/v0.2.1-rc.1'],
    ['RELEASE_SHA', 'short'], ['ORIGINAL_RUN', '../42'], ['EXPECTED_SHA256', 'a'.repeat(63)],
    ['EXPECTED_INTEGRITY', 'sha512-wrong'], ['EXPECTED_INTEGRITY', `sha512-${'B'.repeat(86)}==`],
  ]) await assert.rejects(authorize({...env, [key]: value}, () => assert.fail('must reject before API read')));
});

test('rejects wrong immutable source, non-main ancestry, manifest, original run identity and prerequisite results', async () => {
  const mutations = [
    d => { d[''].default_branch = 'develop'; },
    d => { d[`git/tags/${'b'.repeat(40)}`].object.sha = verifier; },
    d => { d[`compare/${source}...${main}`].merge_base_commit.sha = verifier; },
    d => { d[`contents/cli/npm/cli/package.json?ref=${source}`].size++; },
    d => {
      const file = d[`contents/cli/npm/cli/package.json?ref=${source}`];
      file.content = Buffer.from(JSON.stringify({name: '@opensoha/cli', version: '0.2.0'})).toString('base64');
    },
    d => { d['actions/runs/42/attempts/1'].id = 43; },
    d => { d['actions/runs/42/attempts/1'].workflow_id = 13; },
    d => { d['actions/runs/42/attempts/1'].head_sha = verifier; },
    d => { d['actions/runs/42/attempts/1'].head_branch = 'cli/v0.2.0'; },
    d => { d['actions/runs/42/attempts/1'].repository.full_name = 'fork/soha'; },
    d => { d['actions/runs/42/attempts/1'].run_attempt = 2; },
    d => { d['actions/runs/42/attempts/1/jobs?per_page=100'][0].jobs[0].conclusion = 'failure'; },
    d => { d['actions/runs/42/attempts/1/jobs?per_page=100'][0].jobs.pop(); },
    d => { d['actions/runs/42/attempts/1/jobs?per_page=100'][0].jobs.push({id: 14, name: 'unexpected'}); },
    d => { d['actions/runs/42/attempts/1/jobs?per_page=100'][0].jobs.push({id: 14, name: 'Read-only existing npm qualification', status: 'completed', conclusion: 'failure'}); },
    d => { d[`actions/workflows/ci.yml/runs?head_sha=${source}&per_page=100`][0].workflow_runs[0].conclusion = 'failure'; },
    d => { d[`actions/workflows/ci.yml/runs?head_sha=${verifier}&per_page=100`][0].workflow_runs[0].head_branch = 'feature'; },
  ];
  for (const mutate of mutations) await assert.rejects(authorize(env, fixture(mutate)));
});

test('keeps strict public digest, source, signature marker and credential isolation gates', async () => {
  const proof = await authorize(env, fixture());
  const valid = {integrity: env.EXPECTED_INTEGRITY, sha256: env.EXPECTED_SHA256, provenance: 'npm audit signatures passed'};
  for (const changed of [{...valid, integrity: `sha512-${Buffer.alloc(64, 2).toString('base64')}`},
    {...valid, sha256: 'b'.repeat(64)}, {...valid, provenance: 'unchecked'}]) {
    await assert.rejects(verifyPublic(proof, env, async () => changed));
  }
  await assert.rejects(verifyPublic(proof, {...env, EXPECTED_INTEGRITY: `sha512-${Buffer.alloc(64, 3).toString('base64')}`}, async () => valid));
  for (const token of ['GH_TOKEN', 'GITHUB_TOKEN', 'NODE_AUTH_TOKEN', 'NPM_TOKEN', 'ACTIONS_ID_TOKEN_REQUEST_TOKEN']) {
    await assert.rejects(verifyPublic(proof, {...env, [token]: 'test'}, () => assert.fail('credential reached npm')));
  }
  assert.throws(() => receipt(proof, valid, {version: '0.2.1', commit: verifier}, env));
  assert.throws(() => receipt(proof, valid, {version: '0.2.0', commit: source}, env));
  assert.throws(() => context({...env, EXPECTED_SHA256: ''}));
});

test('normal npm view accepts only the old string or exact singleton npm12 array', () => {
  for (const [value, accepted] of [['0.2.1', true], [['0.2.1'], true], [[], false], [['0.2.1', '0.2.1'], false],
    ['0.2.0', false], [['0.2.0'], false], [{version: '0.2.1'}, false], [null, false]]) {
    const result = spawnSync('jq', ['-e', '--arg', 'version', '0.2.1', '. == $version or . == [$version]'], {input: JSON.stringify(value)});
    assert.equal(result.status === 0, accepted);
  }
});

const metadata = {name: '@opensoha/cli', version: '0.2.1', dist: {tarball: 'https://registry.npmjs.org/cli.tgz', attestations: {url: 'https://registry.npmjs.org/attestations/cli'}}};
const response = (status, body = metadata) => ({status, json: async () => body, body: {cancel: async () => {}}});
test('bounded readiness retries only metadata/tar/provenance 404 reads before strict verification', async () => {
  const sequence = [response(404), response(200), response(404), response(200), response(200), response(404), response(200), response(200), response(200)];
  let pauses = 0;
  await waitForPublicPackage('0.2.1', {fetcher: async () => sequence.shift(), pause: async ms => {assert.equal(ms, 5000); pauses++;}});
  assert.equal(pauses, 3);
  assert.equal(sequence.length, 0);
  let calls = 0;
  await assert.rejects(waitForPublicPackage('0.2.1', {attempts: 2, fetcher: async () => {calls++; return response(404);}, pause: async () => {}}), /bounded 404 wait/);
  assert.equal(calls, 2);
  for (const invalid of [response(500), response(403), response(200, {...metadata, version: '0.2.0'}),
    response(200, {...metadata, dist: {...metadata.dist, tarball: 'https://other.example/cli.tgz'}})]) {
    await assert.rejects(waitForPublicPackage('0.2.1', {fetcher: async () => invalid, pause: () => assert.fail('must not retry non-404')}));
  }
});

test('workflow keeps all normal jobs gated and isolates read-only authority before trusted checkout', async () => {
  const source = await readFile(new URL('../../.github/workflows/cli-release.yml', import.meta.url), 'utf8');
  const jobs = [...source.matchAll(/^  ([a-z-]+):\n([\s\S]*?)(?=^  [a-z-]+:|$(?![\s\S]))/gm)];
  const normal = ['verify', 'build', 'checksums', 'release-smoke', 'github-release', 'npm-publish', 'image', 'public-download'];
  for (const name of normal) {
    const body = jobs.find(job => job[1] === name)?.[2];
    assert.ok(body, `missing ${name}`);
    assert.match(body.split('\n').find(line => line.trim().startsWith('if:')), /inputs.mode == 'publish'/);
  }
  const readonly = source.slice(source.indexOf('  verify-existing:'));
  assert.match(readonly, /inputs.mode != 'publish'/);
  assert.match(readonly, /permissions:\n      contents: read\n      actions: read/);
  assert.ok(readonly.indexOf('test "$RELEASE_MODE" = verify-existing') < readonly.indexOf('actions/checkout'));
  assert.match(readonly, /ref: \$\{\{ github.sha \}\}/);
  assert.doesNotMatch(readonly, /environment:|id-token:|packages:|contents: write|npm pack|npm publish|release-control|publish-release-assets|docker\//);
  assert.match(readonly, /env -u GH_TOKEN -u GITHUB_TOKEN -u NODE_AUTH_TOKEN/);
  assert.match(source, /default: publish/);
  const publish = source.slice(source.indexOf('  npm-publish:'), source.indexOf('  image:'));
  assert.match(publish, /'\. == \$version or \. == \[\$version\]'/);
  assert.ok(publish.indexOf('npm publish "$package_file"') < publish.indexOf('verify-existing-npm-release-readiness.mjs'));
  assert.ok(publish.indexOf('verify-existing-npm-release-readiness.mjs') < publish.indexOf('verify-public-npm-release.mjs verify'));
  assert.match(publish, /release-control.py verify --component cli/);
  assert.match(source.slice(source.indexOf('  image:'), source.indexOf('  public-download:')), /--images-unused/);
});
