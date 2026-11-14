import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

export async function readPackCandidate({ descriptorPath, packageDirectory, tag, sha }) {
  const match = /^cli\/v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u.exec(tag || '');
  if (!match || !/^[a-f0-9]{40}$/u.test(sha || '')) throw new Error('exact stable CLI tag and source SHA required');
  const version = match.slice(1).join('.');
  const descriptor = JSON.parse(await readFile(descriptorPath, 'utf8'));
  let entry;
  if (Array.isArray(descriptor) && descriptor.length === 1) {
    entry = descriptor[0];
  } else if (descriptor && typeof descriptor === 'object' && Object.keys(descriptor).length === 1 && Object.hasOwn(descriptor, '@opensoha/cli')) {
    entry = descriptor['@opensoha/cli'];
  } else {
    throw new Error('expected exactly one CLI pack descriptor');
  }
  if (!entry || typeof entry !== 'object' || Array.isArray(entry)) throw new Error('invalid CLI pack descriptor entry');
  if (entry.name !== '@opensoha/cli' || entry.version !== version || entry.filename !== `opensoha-cli-${version}.tgz`) {
    throw new Error('CLI pack identity differs from the release candidate');
  }
  const file = path.resolve(packageDirectory, entry.filename);
  const bytes = await readFile(file);
  const integrity = `sha512-${createHash('sha512').update(bytes).digest('base64')}`;
  if (entry.integrity !== integrity || entry.size !== bytes.length) throw new Error('CLI pack bytes differ from descriptor integrity or size');
  return { name: '@opensoha/cli', version, tag, sha, workflow: '.github/workflows/cli-release.yml',
    integrity, sha256: createHash('sha256').update(bytes).digest('hex'), file };
}

export async function verifyPublicNpmRelease(options, verifier) {
  const { file, ...candidate } = await readPackCandidate(options);
  const verify = verifier || (await import('../../contracts/scripts/verify-npm-release.mjs')).verifyNpmRelease;
  const receipt = await verify(candidate);
  if (receipt.integrity !== candidate.integrity || receipt.sha256 !== candidate.sha256 || receipt.provenance !== 'npm audit signatures passed') {
    throw new Error('public npm verification receipt differs from the packed CLI candidate');
  }
  return { ...receipt, file };
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  const [mode, descriptorPath, packageDirectory] = process.argv.slice(2);
  if (!['identity', 'verify'].includes(mode) || !descriptorPath || !packageDirectory) {
    throw new Error('usage: verify-public-npm-release.mjs identity|verify <pack.json> <package-directory>');
  }
  const options = { descriptorPath, packageDirectory, tag: process.env.RELEASE_TAG, sha: process.env.RELEASE_SHA };
  console.log(JSON.stringify(mode === 'identity' ? await readPackCandidate(options) : await verifyPublicNpmRelease(options)));
}
