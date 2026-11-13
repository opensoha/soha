// A03: normal API-issued temporary relay PAT and official OpenAI adapter.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { createHash, randomUUID } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { mkdtemp, mkdir, readFile, writeFile, rm } from 'node:fs/promises';
import { homedir, tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import { HarnessClient } from '@deepseek-ai/dsh-sdk-client';

const require = createRequire(import.meta.url);
const version = '0.2.2-rc.1';
for (const pkg of ['dsh', 'dsh-sdk-client', 'dsh-llm-pi-ai']) assert.equal(require(`@deepseek-ai/${pkg}/package.json`).version, version);
function bindDownstreamClose(response, controller, completed) {
  response.on('close', () => { if (!completed()) controller.abort(); });
}
async function checkCompletedEof() {
  const observed = [];
  const connections = new Set();
  const peer = createServer((req, res) => {
    const controller = new AbortController();
    const complete = req.url === '/complete';
    bindDownstreamClose(res, controller, () => complete);
    res.on('close', () => observed.push({ complete, aborted: controller.signal.aborted }));
    res.writeHead(200, { 'content-type': 'text/event-stream' });
    res.write(complete ? 'data: [DONE]\n\n' : 'data: {"choices":[]}\n\n');
    // Keep the body open: reader cancellation must exercise the close branch.
  });
  peer.on('connection', socket => { connections.add(socket); socket.on('close', () => connections.delete(socket)); });
  try {
    await new Promise(resolve => peer.listen(0, '127.0.0.1', resolve));
    for (const path of ['/complete', '/incomplete']) {
      const response = await fetch(`http://127.0.0.1:${peer.address().port}${path}`, { signal: AbortSignal.timeout(5000) });
      const reader = response.body.getReader();
      const chunk = await reader.read();
      assert.equal(new TextDecoder().decode(chunk.value).includes('[DONE]'), path === '/complete');
      await reader.cancel();
    }
    const end = Date.now() + 5000;
    while (observed.length < 2) {
      assert.ok(Date.now() < end, 'local close notifications bounded');
      await new Promise(resolve => setTimeout(resolve, 10));
    }
    assert.deepEqual(observed, [{ complete: true, aborted: false }, { complete: false, aborted: true }]);
    console.log(JSON.stringify({ check: 'completed-and-incomplete-local-stream-close', status: 'PASS', observed }));
  } finally {
    for (const socket of connections) socket.destroy();
    await new Promise(resolve => peer.close(resolve));
  }
}
if (process.argv.includes('--check-completed-eof')) {
  await checkCompletedEof();
  process.exit(0);
}
const model = 'gpt-6-luna';
const endpoint = '/api/v1/ai-gateway/llm/openai/v1/chat/completions';
const result = { attempt: 'Task123/B1-U1a/A03', version, node: process.version, model, endpoint,
  adapter: '@deepseek-ai/dsh-llm-pi-ai', protocol: 'openai-completions', stage: 'local-profile', status: 'FAIL', sdkRequests: 0 };
const root = await mkdtemp(join(process.env.OPENSOHA_VERIFY_DIR || tmpdir(), 'task123-dsh-gateway-'));
const sockets = new Set(), controllers = new Set();
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const numericUsage = usage => Object.fromEntries(Object.entries(usage ?? {}).filter(([, value]) => typeof value === 'number'));
const codeOnly = value => typeof value === 'string' && /^[A-Za-z][A-Za-z_0-9.-]{0,60}$/.test(value) ? value : 'redacted';
let client, server, origin, jwt, pat, patID, proxyFailure;
let creationAttempted = false;
const tokenName = `task123-dsh-a03-${randomUUID()}`;
let sseBuffer = '';
const sse = [];
function capture(text) {
  sseBuffer += text;
  assert.ok(sseBuffer.length < 1024 * 1024, 'SSE frame bound');
  const frames = sseBuffer.split(/\r?\n\r?\n/);
  sseBuffer = frames.pop();
  for (const frame of frames) {
    const data = frame.split(/\r?\n/).filter(line => line.startsWith('data:')).map(line => line.slice(5).trim()).join('\n');
    if (data === '[DONE]') { sse.push({ done: true }); continue; }
    if (!data) continue;
    const event = JSON.parse(data);
    sse.push({ ...(typeof event.model === 'string' ? { model: event.model } : {}),
      finishReasons: (event.choices ?? []).map(choice => choice.finish_reason).filter(Boolean),
      ...(event.usage ? { usage: numericUsage(event.usage) } : {}) });
  }
}
async function jsonResponse(response) {
  let raw = '';
  for await (const chunk of response.body ?? []) {
    raw += Buffer.from(chunk).toString('utf8');
    assert.ok(raw.length < 1024 * 1024, 'JSON response bound');
  }
  return raw ? JSON.parse(raw) : {};
}
async function api(method, path, body) {
  const response = await fetch(new URL(path, origin), { method, redirect: 'error',
    headers: { authorization: `Bearer ${jwt}`, 'content-type': 'application/json' },
    ...(body ? { body: JSON.stringify(body) } : {}), signal: AbortSignal.timeout(20000) });
  const data = await jsonResponse(response);
  if (!response.ok) {
    result.apiFailure = { path, httpStatus: response.status, code: codeOnly(data.error?.code) };
    throw new Error('Normal API rejected request');
  }
  return { status: response.status, data };
}

try {
  // Reuse the CLI's supported refresh-and-save flow; never rotate a refresh
  // token in memory while leaving the user's persisted session stale.
  result.stage = 'local-cli-session';
  const session = spawnSync('soha', ['token', 'list', '--profile', 'local'],
    { timeout: 45000, maxBuffer: 1024 * 1024, stdio: ['ignore', 'pipe', 'pipe'] });
  result.localSessionCheck = { command: 'soha token list --profile local', exitCode: session.status };
  assert.equal(session.status, 0, 'normal CLI session check/refresh must succeed');
  const config = JSON.parse(await readFile(join(homedir(), '.soha/config.json'), 'utf8'));
  const profile = config.profiles?.local;
  assert.ok(profile?.accessToken, 'local JWT required for token lifecycle');
  jwt = profile.accessToken;
  origin = new URL(profile.serverUrl);
  assert.ok(['localhost', '127.0.0.1', '[::1]'].includes(origin.hostname), 'local loopback only');
  assert.ok(!origin.username && !origin.password && !origin.search && !origin.hash);
  result.gatewayOrigin = origin.origin;
  result.stage = 'pat-create';
  const expiresAt = new Date(Date.now() + 15 * 60 * 1000).toISOString();
  creationAttempted = true;
  const created = await api('POST', '/api/v1/ai-gateway/personal-access-tokens', {
    name: tokenName, permissionKeys: ['ai.gateway.relay.invoke'], scopes: ['llm-relay'], expiresAt,
    metadata: { purpose: 'llm-relay', allowedModels: [model], allowedProviderKinds: ['openai'] },
  });
  patID = created.data.data?.token?.id;
  pat = created.data.data?.value;
  result.credential = { id: patID, expiresAt, createHttpStatus: created.status,
    permissionKeys: ['ai.gateway.relay.invoke'], purpose: 'llm-relay', allowedModels: [model], allowedProviderKinds: ['openai'] };
  assert.ok(patID && pat, 'created token response requires ID and one-time value');
  assert.deepEqual(created.data.data.token.permissionKeys, ['ai.gateway.relay.invoke']);
  console.log(JSON.stringify({ stage: 'pat-created', credentialId: patID, expiresAt, httpStatus: created.status }));

  result.stage = 'native-openai-preflight';
  const preflightID = `task123-dsh-preflight-${randomUUID()}`;
  const preflight = await fetch(new URL(endpoint, origin), { method: 'POST', redirect: 'error',
    headers: { authorization: `Bearer ${pat}`, 'content-type': 'application/json', 'x-request-id': preflightID },
    body: JSON.stringify({ model, messages: [{ role: 'user', content: 'Reply exactly OK.' }], max_completion_tokens: 256, stream: false }),
    signal: AbortSignal.timeout(75000) });
  const preflightBody = await jsonResponse(preflight);
  result.preflight = { httpStatus: preflight.status, requestId: preflightID,
    returnedModel: preflightBody.model, finish: preflightBody.choices?.[0]?.finish_reason,
    assistantTextPresent: !!preflightBody.choices?.[0]?.message?.content?.trim(), usage: numericUsage(preflightBody.usage),
    ...(preflightBody.error ? { errorCode: codeOnly(preflightBody.error.code ?? preflightBody.error.type) } : {}) };
  console.log(JSON.stringify({ stage: 'native-openai-preflight', ...result.preflight }));
  assert.equal(preflight.status, 200);
  assert.equal(result.preflight.finish, 'stop');
  assert.ok(result.preflight.assistantTextPresent && Object.keys(result.preflight.usage).length);

  const workspace = join(root, 'workspace');
  await mkdir(workspace);
  server = createServer(async (req, res) => {
    const controller = new AbortController();
    controllers.add(controller);
    bindDownstreamClose(res, controller, () => sse.some(event => event.done));
    try {
      result.sdkRequests++;
      assert.equal(result.sdkRequests, 1, 'one SDK upstream request per attempt; retries prohibited');
      assert.equal(req.method, 'POST');
      assert.equal(req.url, '/v1/chat/completions');
      assert.equal(req.headers.authorization, 'Bearer task123-gateway-synthetic');
      let raw = Buffer.alloc(0);
      for await (const chunk of req) { raw = Buffer.concat([raw, chunk]); assert.ok(raw.length < 1024 * 1024); }
      const body = JSON.parse(raw);
      assert.equal(body.model, model);
      assert.equal(body.max_completion_tokens, 512);
      assert.equal(body.stream, true);
      assert.deepEqual(body.tools ?? [], []);
      assert.ok(!Object.hasOwn(body, 'dsh_session_log') && !Object.hasOwn(body, 'dsh_plugin_packages'));
      result.requestBodySha256 = hash(raw);
      result.forwardedBodySha256 = hash(raw);
      result.requestId = `task123-dsh-sdk-${randomUUID()}`;
      result.stage = 'sdk-gateway-http';
      const headers = {};
      for (const [name, value] of Object.entries(req.headers)) if (typeof value === 'string'
        && !['host', 'connection', 'content-length', 'transfer-encoding', 'authorization'].includes(name)) headers[name] = value;
      headers.authorization = `Bearer ${pat}`;
      headers['x-request-id'] = result.requestId;
      const response = await fetch(new URL(endpoint, origin), { method: 'POST', headers, body: raw, redirect: 'error',
        signal: AbortSignal.any([controller.signal, AbortSignal.timeout(75000)]) });
      result.httpStatus = response.status;
      result.responseContentType = response.headers.get('content-type');
      res.writeHead(response.status, { 'content-type': result.responseContentType || 'application/json' });
      if (!response.ok) {
        const error = await jsonResponse(response);
        result.gatewayErrorCode = codeOnly(error.error?.code ?? error.error?.type);
        res.end(JSON.stringify({ error: { type: 'gateway_error', message: `Gateway HTTP ${response.status}` } }));
        return;
      }
      assert.match(result.responseContentType || '', /text\/event-stream/);
      result.stage = 'sdk-gateway-sse';
      const decoder = new TextDecoder();
      let bytes = 0;
      for await (const chunk of response.body) {
        bytes += chunk.length;
        assert.ok(bytes < 1024 * 1024);
        capture(decoder.decode(chunk, { stream: true }));
        res.write(chunk);
        if (sse.some(event => event.done)) break;
      }
      capture(decoder.decode());
      res.end();
    } catch (error) { proxyFailure = error; res.destroy(); }
    finally { controllers.delete(controller); }
  });
  server.on('connection', socket => { sockets.add(socket); socket.on('close', () => sockets.delete(socket)); });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const patch = join(root, 'gateway.patch.yml');
  await writeFile(patch, JSON.stringify([
    { id: 'llm-deepseek', disabled: true }, { id: 'deepseek-llm-api-extensions', disabled: true },
    { insert: [{ id: 'llm-pi-ai', name: '@deepseek-ai/dsh-llm-pi-ai', config: { providers: {
      'soha-gateway': { apiKeyEnv: 'SOHA_QUALIFICATION_KEY', api: 'openai-completions',
        baseURL: `http://127.0.0.1:${server.address().port}/v1`, models: [{ id: model, contextWindow: 128000, maxTokens: 512 }],
        compat: { supportsUsageInStreaming: true, maxTokensField: 'max_completion_tokens' },
        retryPolicy: { mode: 'normal', maxRetries: 0 } },
    } } }] },
  ]));
  result.stage = 'sdk-initialize';
  client = new HarnessClient({ profile: 'sdk-minimal',
    patches: [join(dirname(fileURLToPath(import.meta.url)), 'chat-restricted.patch.yml'), patch],
    dshHome: join(root, 'dsh'), processCwd: workspace,
    env: { PATH: dirname(process.execPath), HOME: root, TMPDIR: root, SOHA_QUALIFICATION_KEY: 'task123-gateway-synthetic' },
    initializeTimeoutMs: 20000, requestTimeoutMs: 90000, shutdownTimeoutMs: 500, disposeEofGraceMs: 1000, disposeGraceMs: 1000 });
  result.identity = await client.initialize({ cwd: workspace, provider: 'soha-gateway', model, maxTokens: 512 });
  const sub = client.subscribeSessionTree('task123-gateway');
  result.stage = 'sdk-prompt';
  const messageId = await client.prompt('task123-gateway', [{ type: 'text', text: 'Reply with exactly OK. Do not use tools.' }]);
  await client.request('session/wait', { sessionId: 'task123-gateway' });
  const events = [];
  for (let next; (next = sub.tryNext());) if (next.method === 'session.event') events.push(next.params.event);
  sub.close();
  result.eventTypes = [...new Set(events.map(event => event.type))];
  result.promptReceiptBound = events.some(event => event.type === 'agent/inbox/spliced' && JSON.stringify(event.data).includes(messageId));
  result.finish = events.filter(event => event.type === 'turn/end').map(event => event.data.reason.kind);
  result.runtimeErrorCodes = events.filter(event => event.type === 'turn/end' && event.data.reason.kind === 'error').map(event => codeOnly(event.data.reason.error?.code));
  result.runtimeUsage = events.filter(event => event.type === 'assistant/message').map(event => numericUsage(event.data.usage));
  const texts = events.filter(event => event.type === 'assistant/message').flatMap(event => event.data.message.content.filter(block => block.type === 'text').map(block => block.text));
  result.assistantTextPresent = texts.some(text => text.trim());
  result.assistantTextEqualsOK = texts.join('').trim() === 'OK';
  result.stage = 'assertions';
  assert.ok(!proxyFailure);
  assert.equal(result.sdkRequests, 1);
  assert.equal(result.httpStatus, 200);
  assert.ok(result.promptReceiptBound && result.assistantTextPresent);
  assert.deepEqual(result.finish, ['completed']);
  assert.ok(sse.some(event => event.done) && sse.some(event => event.finishReasons?.includes('stop')));
  assert.ok(sse.some(event => Object.keys(event.usage ?? {}).length));
  assert.ok(result.runtimeUsage.some(usage => Object.keys(usage).length));
  result.status = 'PASS'; result.stage = 'completed';
} catch (error) {
  result.errorClass = (proxyFailure ?? error)?.name || 'Error';
  if (error instanceof assert.AssertionError) result.failedAssertion = String(error.message).slice(0, 200);
  process.exitCode = 1;
} finally {
  try { await client?.close(); }
  catch { result.status = 'FAIL'; result.cleanup = 'child-exit-unconfirmed'; result.retainedRoot = root; process.exitCode = 1; }
  for (const controller of controllers) controller.abort();
  for (const socket of sockets) socket.destroy();
  if (server?.listening) await new Promise(resolve => server.close(resolve));
  if (!result.cleanup) { await rm(root, { recursive: true, force: true }); result.cleanup = 'children-proxy-home-removed'; }
  // Reconcile only this unique name if creation's response was lost; never replay creation.
  try {
    if (!patID && creationAttempted && !result.apiFailure) {
      const listed = await api('GET', '/api/v1/ai-gateway/personal-access-tokens');
      const matches = listed.data.items.filter(item => item.name === tokenName);
      assert.ok(matches.length <= 1);
      patID = matches[0]?.id;
    }
    if (patID) {
      const revoked = await api('POST', `/api/v1/ai-gateway/personal-access-tokens/${encodeURIComponent(patID)}/revoke`);
      const listed = await api('GET', '/api/v1/ai-gateway/personal-access-tokens');
      const item = listed.data.items.find(item => item.id === patID);
      result.revocation = { id: patID, httpStatus: revoked.status, status: revoked.data.status, revokedAt: item?.revokedAt, confirmed: !!item?.revokedAt };
      assert.equal(revoked.data.status, 'ok');
      assert.ok(result.revocation.confirmed, 'normal API must confirm revokedAt');
    }
  } catch { result.status = 'FAIL'; result.revocation = { id: patID, confirmed: false, requiresFollowup: true }; process.exitCode = 1; }
  pat = undefined; jwt = undefined;
  result.sse = sse;
  console.log(JSON.stringify(result, null, 2));
}
