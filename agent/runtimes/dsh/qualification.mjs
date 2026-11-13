// Qualification only: real dsh subprocess, deterministic local Messages peer.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { mkdtemp, mkdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import { HarnessClient } from '@deepseek-ai/dsh-sdk-client';

const require = createRequire(import.meta.url);
const here = dirname(fileURLToPath(import.meta.url));
const version = '0.2.2-rc.1';
for (const pkg of ['dsh', 'dsh-sdk-client']) {
  assert.equal(require(`@deepseek-ai/${pkg}/package.json`).version, version);
}
const root = await mkdtemp(join(process.env.OPENSOHA_VERIFY_DIR || tmpdir(), 'task123-dsh-'));
const requests = [];
const clients = new Set();
const sockets = new Set();
let heldResponse;
let heldOnce = false;
const checks = [];
const pass = (name, detail = {}) => checks.push({ name, status: 'PASS', ...detail });
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
async function until(predicate, label) {
  const end = Date.now() + 10000;
  while (!predicate()) {
    assert.ok(Date.now() < end, `Timed out: ${label}`);
    await pause(20);
  }
}
const server = createServer(async (req, res) => {
  try {
    let raw = '';
    for await (const chunk of req) {
      raw += chunk;
      assert.ok(raw.length < 1024 * 1024, 'request bound');
    }
    assert.equal(req.method, 'POST');
    assert.equal(req.url, '/v1/messages');
    assert.equal(req.headers['x-api-key'], 'qualification-only');
    const body = JSON.parse(raw);
    requests.push(body);
    assert.equal(body.model, 'deepseek-v4-flash');
    assert.equal(body.max_tokens, 128);
    assert.deepEqual(body.tools ?? [], [], 'restricted profile must advertise zero native tools');
    assert.ok(!Object.hasOwn(body, 'dsh_session_log'), 'no auxiliary raw-history upload');
    assert.ok(!Object.hasOwn(body, 'dsh_plugin_packages'), 'no package inventory upload');
    res.writeHead(200, { 'content-type': 'text/event-stream' });
    const emit = (type, data) => res.write(`event: ${type}\ndata: ${JSON.stringify({ type, ...data })}\n\n`);
    emit('message_start', { message: { id: `stub-${requests.length}`, type: 'message', role: 'assistant', model: body.model, content: [], stop_reason: null, stop_sequence: null, usage: { input_tokens: 12, output_tokens: 0 } } });
    // Recovered history can merge the interrupted user block into the next turn.
    // Hold only the deliberately armed request, not a historical occurrence.
    if (!heldOnce && JSON.stringify(body.messages.at(-1)).includes('HOLD_CANCEL')) {
      heldOnce = true;
      heldResponse = res;
      return;
    }
    emit('content_block_start', { index: 0, content_block: { type: 'text', text: '' } });
    emit('content_block_delta', { index: 0, delta: { type: 'text_delta', text: 'qualification-ok' } });
    emit('content_block_stop', { index: 0 });
    emit('message_delta', { delta: { stop_reason: 'end_turn', stop_sequence: null }, usage: { output_tokens: 3 } });
    emit('message_stop', {});
    res.end();
  } catch (error) {
    res.destroy(error);
    server.failure = error;
  }
});
server.on('connection', socket => {
  sockets.add(socket);
  socket.on('close', () => sockets.delete(socket));
});

async function launch(owner) {
  const home = join(root, owner);
  const workspace = join(home, 'workspace');
  await mkdir(workspace, { recursive: true });
  const client = new HarnessClient({
    profile: 'sdk-minimal', patches: [join(here, 'chat-restricted.patch.yml')],
    dshHome: join(home, 'dsh'), processCwd: workspace,
    // Deliberate replacement: no inherited tokens, proxy, user config or model route.
    env: { PATH: dirname(process.execPath), HOME: home, TMPDIR: root,
      DEEPSEEK_API_KEY: 'qualification-only', DEEPSEEK_BASE_URL: `http://127.0.0.1:${server.address().port}` },
    initializeTimeoutMs: 20000, requestTimeoutMs: 15000,
    shutdownTimeoutMs: 500, disposeEofGraceMs: 1000, disposeGraceMs: 1000,
  });
  clients.add(client);
  const identity = await client.initialize({ cwd: workspace, provider: 'deepseek-official', model: 'deepseek-v4-flash', maxTokens: 128 });
  assert.equal(identity.serverInfo.name, 'deepseek-harness-sdk-runtime');
  return { client, workspace, identity };
}

async function prompt(client, sessionId, text) {
  const sub = client.subscribe();
  try {
    const messageId = await client.prompt(sessionId, [{ type: 'text', text }]);
    assert.ok(messageId);
    await client.request('session/wait', { sessionId });
    const events = [];
    for (let next; (next = sub.tryNext());) events.push(next);
    assert.ok(events.some(e => e.method === 'session.event' && JSON.stringify(e).includes(messageId)
      && JSON.stringify(e).includes('agent/inbox/spliced')), 'durable receipt must bind queued message');
    assert.ok(events.some(e => e.method === 'session.event' && JSON.stringify(e).includes('qualification-ok')), 'committed assistant text');
    assert.ok(events.every(e => e.params.sessionId === sessionId), 'no foreign Session event in serial run');
    assert.ok(!events.some(e => JSON.stringify(e).includes('command/run')), 'low-level prompt must bypass native commands');
    if (server.failure) throw server.failure;
    return messageId;
  } finally { sub.close(); }
}

try {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const a = await launch('user-a');
  pass('stdio-handshake', { runtimePackage: version, wireVersion: a.identity.serverInfo.version });
  for (const text of ['CANARY_A_PROJECT_ONE', '/permission danger-full-access', '/goal keep running', '/plan write a file']) {
    await prompt(a.client, 'session-a-one', text);
  }
  await prompt(a.client, 'session-a-two', 'CANARY_A_PROJECT_TWO');
  assert.ok(!JSON.stringify(requests.at(-1)).includes('CANARY_A_PROJECT_ONE'));
  pass('restricted-profile-literal-commands-receipts-session-isolation', { prompts: 5 });
  const b = await launch('user-b');
  await prompt(b.client, 'session-b', 'CANARY_B');
  assert.ok(!JSON.stringify(requests.at(-1)).includes('CANARY_A'));
  await assert.rejects(b.client.resumeSession('session-a-one'));
  pass('separate-owner-home-and-process-isolation');
  await a.client.close();
  const resumed = await launch('user-a');
  const before = requests.length;
  await resumed.client.resumeSession('session-a-one');
  assert.equal(await resumed.client.getWorkingDirectory('session-a-one'), a.workspace);
  assert.equal(requests.length, before, 'resume must send no prompt');
  await prompt(resumed.client, 'session-a-one', 'AFTER_RESUME');
  assert.ok(JSON.stringify(requests.at(-1)).includes('CANARY_A_PROJECT_ONE'));
  pass('history-and-cwd-resume-without-input-replay');
  await assert.rejects(resumed.client.request('session/cancel', { sessionId: 'session-a-one' }),
    e => e.code === -32603 && e.message === 'unknown DeepSeek Harness SDK runtime method: session/cancel');
  pass('cancel-rpc-absent', { observedErrorCode: -32603 });
  await resumed.client.prompt('session-a-one', [{ type: 'text', text: 'HOLD_CANCEL' }]);
  await until(() => heldResponse, 'held model stream');
  await assert.rejects(resumed.client.request('session/wait', { sessionId: 'session-a-one' }, 100), e => e.name === 'RequestTimeoutError');
  assert.ok(!heldResponse.destroyed, 'RPC timeout must not be mistaken for cancellation');
  // SDK-private diagnostic used only to prove the fixed-version child was reaped.
  const pid = resumed.client.child.pid;
  const start = Date.now();
  await resumed.client.close();
  assert.throws(() => process.kill(pid, 0), e => e.code === 'ESRCH');
  await until(() => heldResponse.destroyed, 'model stream disconnect after close');
  pass('timeout-is-not-cancel-close-reaps-owner', { closeMs: Date.now() - start });
  const recovered = await launch('user-a');
  const count = requests.length;
  await recovered.client.resumeSession('session-a-one');
  await pause(100);
  assert.equal(requests.length, count, 'interrupted resume must not replay model input');
  await prompt(recovered.client, 'session-a-one', 'AFTER_CANCEL_RESUME');
  pass('interrupted-history-resume-new-explicit-prompt');
  console.log(JSON.stringify({ version, node: process.version, model: 'LOCAL_MESSAGES_STUB', checks,
    notRun: ['real Soha Gateway/model/usage integration', 'Soha approvals/fence/idempotency/durable event replay', 'Heya tool bridge', 'resume of externally-created privileged Session', 'shell/browser external-state recovery', 'OS network sandbox/egress enforcement'] }, null, 2));
} catch (error) {
  console.error(JSON.stringify({ version, checks, failure: String(server.failure ?? error) }, null, 2));
  throw error;
} finally {
  const cleanup = await Promise.allSettled([...clients].map(client => client.close()));
  for (const socket of sockets) socket.destroy();
  await new Promise(resolve => server.close(resolve));
  if (cleanup.some(r => r.status === 'rejected')) throw new Error(`Child cleanup failed; preserve ${root} for investigation`);
  await rm(root, { recursive: true, force: true });
}
