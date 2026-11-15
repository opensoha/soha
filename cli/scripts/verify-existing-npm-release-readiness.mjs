#!/usr/bin/env node
// Retry only public-registry propagation 404s; all identity/signature checks remain downstream.
import assert from 'node:assert/strict';
import {setTimeout as sleep} from 'node:timers/promises';
import {pathToFileURL} from 'node:url';

export async function waitForPublicPackage(version, {fetcher = fetch, pause = sleep, attempts = 12} = {}) {
  assert.match(version, /^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$/);
  assert.ok(Number.isInteger(attempts) && attempts >= 1 && attempts <= 12);
  const metadataURL = `https://registry.npmjs.org/@opensoha%2fcli/${version}`;
  for (let attempt = 1; attempt <= attempts; attempt++) {
    const metadata = await fetcher(metadataURL, {signal: AbortSignal.timeout(10_000), redirect: 'error'});
    let pending = metadata.status === 404;
    if (pending) await metadata.body?.cancel();
    if (!pending) {
      assert.equal(metadata.status, 200, `public registry failed: ${metadata.status}`);
      const body = await metadata.json();
      assert.equal(body.name, '@opensoha/cli');
      assert.equal(body.version, version);
      assert.ok(body.dist?.attestations?.url, 'public package has no signed provenance URL');
      for (const url of [body.dist.tarball, body.dist.attestations.url]) {
        assert.equal(new URL(url).origin, 'https://registry.npmjs.org', 'unexpected public package origin');
        const response = await fetcher(url, {signal: AbortSignal.timeout(10_000), redirect: 'error'});
        await response.body?.cancel();
        if (response.status === 404) { pending = true; break; }
        assert.equal(response.status, 200, `public package retrieval failed: ${response.status}`);
      }
      if (!pending) return;
    }
    assert.ok(attempt < attempts, 'public npm package remained unavailable after bounded 404 wait');
    await pause(5000);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try { assert.equal(process.argv.length, 3); await waitForPublicPackage(process.argv[2]); }
  catch (error) { console.error(error.message); process.exitCode = 1; }
}
