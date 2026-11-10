import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer, request } from 'node:http';
import { mkdir, mkdtemp, realpath, rename, rm, readFile, writeFile, readdir, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createRuntime, configFromEnv, effectiveRunDeadline } from './service.mjs';
import { VERSION, sha, stable } from './protocol.mjs';
import { createOrderedToolBridge } from './worker.mjs';
import { writeHookOutcome } from './assembly-plugin.mjs';
const token = 'service-secret-'.repeat(4),
  pat = 'gateway-secret-'.repeat(4),
  grant = 'grant-secret-'.repeat(4);
const tool = {
  name: 'knowledge.search',
  description: 'Search authorized knowledge',
  inputSchema: {
    type: 'object',
    properties: { query: { type: 'string' } },
    required: ['query'],
    additionalProperties: false,
  },
  readOnly: true,
};
const controlTool = {
  name: 'harness_request_input',
  description: 'Request missing user input',
  readOnly: true,
  inputSchema: {
    type: 'object',
    required: ['question'],
    properties: { question: { type: 'string', minLength: 1, maxLength: 2048 } },
    additionalProperties: false,
  },
};
const discoveryTool = {
  name: 'harness_discover_tasks',
  description: 'Discover authorized tasks',
  readOnly: true,
  inputSchema: {
    type: 'object',
    properties: {
      query: { type: 'string', maxLength: 256 },
      offset: { type: 'integer', minimum: 0, maximum: 512 },
    },
    additionalProperties: false,
  },
};
const start = (runId = 'run1', extra = {}) => ({
  schemaVersion: VERSION,
  kind: 'start',
  runId,
  attempt: 1,
  fence: 10,
  ownerKey: 'opaque-owner',
  sessionId: 'session1',
  requestId: 'request1',
  inputHash: 'a'.repeat(64),
  contextRevision: '1',
  profile: 'chat',
  model: 'loopback-model',
  prompt: 'Find an answer',
  toolCatalogRevision: 'catalog1',
  deadline: new Date(Date.now() + 60000).toISOString(),
  maxOutputTokens: 256,
  ...extra,
});
async function listen(server) {
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  return `http://127.0.0.1:${server.address().port}`;
}
async function config() {
  return {
    host: '127.0.0.1',
    port: 0,
    token,
    pat,
    gatewayURL: 'http://127.0.0.1:1',
    callbackURL: 'http://127.0.0.1:1',
    catalog: { revision: 'catalog1', tools: [tool] },
    models: ['loopback-model'],
    dataDir: await mkdtemp(
      join(process.env.OPENSOHA_VERIFY_DIR || tmpdir(), 'task123-dsh-service-'),
    ),
    schemaPath: resolve('../../../soha-contracts/agent/dsh-runtime.schema.json'),
    capacity: 1,
    maxRunMs: 25000,
    maxTokens: 512,
  };
}
async function api(base, path, body, auth = token) {
  const r = await fetch(base + path, {
    method: body ? 'POST' : 'GET',
    headers: { authorization: `Bearer ${auth}`, 'content-type': 'application/json' },
    ...(body ? { body: JSON.stringify(body) } : {}),
  });
  return { code: r.status, body: await r.json() };
}
test('asset inventory is redacted and MCP probe only discovers and closes', async (t) => {
  let closeStatus = 204, loseInitialize = false;
  const methods = [];
  const upstream = createServer(async (req, res) => {
    if (req.method === 'GET') return res.writeHead(405).end();
    if (req.method === 'DELETE') {
      methods.push('DELETE');
      assert.equal(req.headers['mcp-session-id'], 'probe-session');
      return res.writeHead(closeStatus).end();
    }
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    const { method, id, params } = JSON.parse(Buffer.concat(chunks));
    methods.push(method);
    if (method === 'initialize' && req.url === '/gateway-error') return res.writeHead(502).end();
    if (method === 'initialize' && req.url === '/gateway-error-session')
      return res.writeHead(502, { 'mcp-session-id': 'probe-session' }).end();
    if (method === 'initialize' && loseInitialize) return res.destroy();
    if (id === undefined) return res.writeHead(202).end();
    const result = {
      initialize: { protocolVersion: params?.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: 'internal', version: '1' } },
      'tools/list': { tools: [{ name: 'private-tool', inputSchema: { type: 'object' } }] },
    }[method];
    assert.ok(result);
    res.writeHead(200, { 'content-type': 'application/json', 'mcp-session-id': 'probe-session' });
    res.end(JSON.stringify({ jsonrpc: '2.0', id, result }));
  });
  let c, runtime;
  t.after(async () => {
    try { await runtime?.close(); }
    finally {
      upstream.closeAllConnections();
      await new Promise((resolve) => upstream.close(resolve));
      if (c) await rm(c.dataDir, { recursive: true, force: true });
    }
  });
  const origin = await listen(upstream);
  c = await config();
  const runtimeConfig = { ...c, pluginAssets: {
    mcp: [
      { id: 'personal', version: '1', transport: 'stdio', executionPosition: 'personal-docker', allowedTools: ['private-tool'], allowedResourceURIs: [] },
      { id: 'fixture', version: '2', transport: 'http', url: origin + '/internal', allowedOrigin: origin,
        loopbackFixture: true, allowedTools: ['private-tool'], allowedResourceURIs: [] },
    ],
    skill: [{ id: 'private-skill', version: '3', packageRoot: '/private/root' }],
  } };
  runtimeConfig.pluginAssets.mcp.push(
    { ...runtimeConfig.pluginAssets.mcp[1], id: 'lost-response' },
    ...['gateway-error', 'gateway-error-session'].map((id) => ({ ...runtimeConfig.pluginAssets.mcp[1], id, url: `${origin}/${id}` })),
  );
  runtime = await createRuntime(runtimeConfig);
  const base = await listen(runtime.server);
  assert.equal((await api(base, '/v1/assets', undefined, 'wrong')).code, 401);
  assert.deepEqual(await api(base, '/v1/assets'), { code: 200, body: { items: [
    { kind: 'hook', id: 'tool-audit', version: '1' },
    { kind: 'hook', id: 'tool-limit', version: '1' },
    { kind: 'mcp', id: 'fixture', version: '2', transport: 'http' },
    { kind: 'mcp', id: 'gateway-error-session', version: '2', transport: 'http' },
    { kind: 'mcp', id: 'gateway-error', version: '2', transport: 'http' },
    { kind: 'mcp', id: 'lost-response', version: '2', transport: 'http' },
    { kind: 'mcp', id: 'personal', version: '1', transport: 'stdio', executionPosition: 'personal-docker' },
    { kind: 'skill', id: 'private-skill', version: '3' },
  ] } });
  const probe = (id, version, scope) => api(base, '/v1/assets/mcp/probe', { id, version, scope });
  assert.equal((await probe('fixture', '2', 'invalid')).code, 400);
  assert.deepEqual((await probe('personal', '1', 'heya')).body, { status: 'unavailable', capabilities: [] });
  assert.deepEqual((await probe('fixture', '2', 'chat')).body, { status: 'ready', capabilities: ['tools'] });
  const discovery = ['initialize', 'notifications/initialized', 'tools/list', 'DELETE'];
  assert.deepEqual(methods, discovery);
  closeStatus = 405;
  assert.deepEqual((await probe('fixture', '2', 'chat')).body, { status: 'cleanup_unknown', capabilities: [] });
  assert.deepEqual((await probe('fixture', '2', 'chat')).body, { status: 'cleanup_unknown', capabilities: [] });
  assert.deepEqual(methods, [...discovery, ...discovery]);
  loseInitialize = true;
  assert.deepEqual((await probe('lost-response', '2', 'chat')).body, { status: 'cleanup_unknown', capabilities: [] });
  assert.deepEqual(methods, [...discovery, ...discovery, 'initialize']);
  closeStatus = 204;
  assert.deepEqual((await probe('gateway-error-session', '2', 'chat')).body, { status: 'unavailable', capabilities: [] });
  assert.deepEqual((await probe('gateway-error', '2', 'chat')).body, { status: 'cleanup_unknown', capabilities: [] });
  assert.deepEqual((await probe('gateway-error', '2', 'chat')).body, { status: 'cleanup_unknown', capabilities: [] });
  assert.deepEqual(methods, [...discovery, ...discovery, 'initialize', 'initialize', 'DELETE', 'initialize']);
  await runtime.close();
  runtime = await createRuntime(runtimeConfig);
  const restarted = await listen(runtime.server);
  const retry = await api(restarted, '/v1/assets/mcp/probe', { id: 'lost-response', version: '2', scope: 'chat' });
  assert.deepEqual(retry.body, { status: 'cleanup_unknown', capabilities: [] });
  assert.deepEqual((await api(restarted, '/v1/assets/mcp/probe', { id: 'gateway-error', version: '2', scope: 'chat' })).body,
    { status: 'cleanup_unknown', capabilities: [] });
  assert.deepEqual(methods, [...discovery, ...discovery, 'initialize', 'initialize', 'DELETE', 'initialize']);
});
async function done(base, id) {
  let r;
  for (let i = 0; i < 500; i++) {
    r = await api(base, `/v1/runs/${id}/attempts/1`);
    if (['completed', 'failed', 'canceled', 'unknown'].includes(r.body.status)) return r.body;
    await new Promise((r) => setTimeout(r, 20));
  }
  throw Error('run did not terminate ' + JSON.stringify(r));
}

test('hook spool read failure preserves terminal result and replays once after restart', async (t) => {
  const c = await config();
  const outcome = { callId: 'hook-call', toolName: tool.name, outcome: 'completed' };
  const path = '/v1/runs/hook-replay/attempts/1';
  let directory, runtime;
  t.after(async () => {
    await runtime?.close();
    await rm(c.dataDir, { recursive: true, force: true });
  });
  runtime = await createRuntime(c, async ({ auditDir }) => {
    directory = auditDir;
    writeHookOutcome(directory, 1, outcome);
    await rename(directory, `${directory}.held`);
    await writeFile(directory, 'temporarily unreadable');
    return { assistantText: 'Finished', usageKnown: false };
  });
  const base = await listen(runtime.server);
  assert.equal((await api(base, '/v1/runs', start('hook-replay'))).code, 202);
  const original = await done(base, 'hook-replay');
  assert.equal(original.status, 'completed');
  assert.equal(original.assistantText, 'Finished');
  assert.deepEqual((await api(base, `${path}/events`)).body.items.filter(({ type }) => type === 'hook_observed'), []);
  await runtime.close();
  await rm(directory);
  await rename(`${directory}.held`, directory);
  runtime = await createRuntime(c);
  const restarted = await listen(runtime.server);
  assert.deepEqual((await api(restarted, path)).body, { ...original, sequence: original.sequence + 1 });
  writeHookOutcome(directory, 1, outcome);
  const { items } = (await api(restarted, `${path}/events`)).body;
  assert.deepEqual(items.filter(({ type }) => type === 'hook_observed').map(({ data }) => data), [outcome]);
  await assert.rejects(readdir(directory), { code: 'ENOENT' });
});

test('terminal journal retry commits the original completed result and pending hook together', async (t) => {
  const c = await config();
  const outcome = { callId: 'fallback-call', toolName: tool.name, outcome: 'completed' };
  const usage = { inputTokens: 2, outputTokens: 1, totalTokens: 3 };
  const id = 'terminal-retry';
  const path = `/v1/runs/${id}/attempts/1`;
  const obstacle = join(c.dataDir, `${sha(`${id}/1`)}.json.tmp`);
  const finished = Promise.withResolvers();
  const runtime = await createRuntime(c, async ({ onHookOutcome }) => {
    await mkdir(obstacle);
    await assert.rejects(onHookOutcome(outcome), { code: 'EISDIR' });
    finished.resolve();
    return { assistantText: 'Finished', usageKnown: true, usage };
  });
  t.after(async () => {
    await rm(obstacle, { recursive: true, force: true });
    try { await runtime.close(); }
    finally { await rm(c.dataDir, { recursive: true, force: true }); }
  });
  const base = await listen(runtime.server);
  assert.equal((await api(base, '/v1/runs', start(id))).code, 202);
  await finished.promise;
  const before = (await api(base, path)).body;
  assert.equal(before.status, 'running');
  assert.equal(before.usageKnown, false);
  assert.equal(before.assistantText, undefined);
  assert.equal((await api(base, `${path}/events`)).body.items.some(({ type }) => type === 'hook_observed'), false);
  await rm(obstacle, { recursive: true });
  const { items } = (await api(base, `${path}/events?after=${before.sequence}`)).body;
  assert.deepEqual(items.map(({ type }) => type), ['hook_observed', 'completed']);
  assert.deepEqual(items[0].data, outcome);
  const final = (await api(base, path)).body;
  assert.equal(final.status, 'completed');
  assert.equal(final.assistantText, 'Finished');
  assert.deepEqual(final.usage, usage);
  assert.equal(final.sequence, before.sequence + 2);
});

test('personal MCP assembly attempts the required connection before an earlier optional connection', async (t) => {
  const c = await config();
  const attempted = [];
  const core = createServer((req, res) => res.writeHead(403).end());
  core.on('upgrade', (req, socket) => {
    attempted.push(new URL(req.url, 'http://fixture').searchParams.get('assetId'));
    socket.end('HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n');
  });
  let runtime;
  t.after(async () => {
    try { await runtime?.close(); }
    finally {
      core.closeAllConnections();
      await new Promise((resolve) => core.close(resolve));
      await rm(c.dataDir, { recursive: true, force: true });
    }
  });
  const assets = ['optional', 'required'].map((id) => ({ kind: 'mcp', id, version: '1', required: id === 'required' }));
  runtime = await createRuntime({ ...c, modelAuthMode: 'core', callbackURL: `${await listen(core)}/dsh/tools`,
    pluginAssets: { mcp: assets.map(({ id, version }) => ({ id, version, transport: 'stdio',
      executionPosition: 'personal-docker', allowedTools: ['read_note'], allowedResourceURIs: [] })) },
  });
  const base = await listen(runtime.server);
  assert.equal((await api(base, '/v1/runs', start('personal-required', {
    profile: 'heya', toolGrant: grant, assembly: { profileId: 'ops', revision: 1, assets },
  }))).code, 202);
  assert.equal((await done(base, 'personal-required')).status, 'failed');
  assert.deepEqual(attempted, ['required']);
});
test('HTTP auth, binding, capacity, durable cancel, restart unknown and no native tool admission', async () => {
  const c = await config();
  let rt;
  const executor = async ({ signal }) => {
    await new Promise((r) => signal.addEventListener('abort', r, { once: true }));
    throw Error('aborted');
  };
  try {
    rt = await createRuntime(c, executor);
    let base = await listen(rt.server);
    assert.equal((await api(base, '/v1/health', null, 'bad')).code, 401);
    const duplicateAuth = await new Promise((resolve) => {
      const req = request(
        base + '/v1/health',
        { headers: { authorization: [`Bearer ${token}`, `Bearer ${token}`] } },
        (res) => {
          res.resume();
          resolve(res.statusCode);
        },
      );
      req.end();
    });
    assert.equal(duplicateAuth, 401);
    assert.equal((await api(base, '/v1/runs', { kind: 'start' })).code, 400);
    assert.equal((await api(base, '/v1/runs', { padding: 'x'.repeat(1048576) })).code, 413);
    assert.equal(
      (
        await api(
          base,
          '/v1/runs',
          start('native', { tools: [{ ...tool, name: 'bash' }], toolGrant: grant }),
        )
      ).code,
      403,
    );
    assert.equal(
      (
        await api(
          base,
          '/v1/runs',
          start('native', { tools: [{ ...tool, readOnly: false }], toolGrant: grant }),
        )
      ).code,
      400,
    );
    const s = start();
    const first = await api(base, '/v1/runs', s);
    assert.equal(first.code, 202);
    assert.deepEqual((await api(base, '/v1/runs', { ...s, toolGrant: grant })).body, first.body);
    assert.equal((await api(base, '/v1/runs', { ...s, prompt: 'collision' })).code, 409);
    assert.equal((await api(base, '/v1/runs', { ...s, fence: 11 })).code, 409);
    assert.equal((await api(base, '/v1/runs', start('capacity'))).code, 429);
    assert.equal((await api(base, '/v1/runs/run1/attempts/2')).code, 404);
    assert.equal((await api(base, '/v1/runs/run1/attempts/1/events?after=999')).code, 400);
    assert.equal(
      (
        await api(base, '/v1/runs/run1/attempts/1/cancel', {
          schemaVersion: VERSION,
          kind: 'cancel',
          runId: 'run1',
          attempt: 1,
          fence: 9,
        })
      ).code,
      409,
    );
    const canceled = await api(base, '/v1/runs/run1/attempts/1/cancel', {
      schemaVersion: VERSION,
      kind: 'cancel',
      runId: 'run1',
      attempt: 1,
      fence: 10,
    });
    assert.equal(canceled.body.status, 'canceled');
    assert.equal((await api(base, '/v1/runs', start('run1', { attempt: 2, fence: 9 }))).code, 409);
    await rt.close();
    rt = undefined;
    const file = (await readdir(c.dataDir)).find((f) => f.endsWith('.json'));
    const record = JSON.parse(await readFile(join(c.dataDir, file), 'utf8'));
    record.status = 'running';
    await writeFile(join(c.dataDir, file), JSON.stringify(record));
    rt = await createRuntime(c, () => {
      throw Error('must not replay');
    });
    base = await listen(rt.server);
    assert.equal((await api(base, '/v1/runs/run1/attempts/1')).body.status, 'unknown');
    const persisted = await readFile(join(c.dataDir, file), 'utf8');
    assert.ok(!persisted.includes(grant) && !persisted.includes(token) && !persisted.includes(pat));
    assert.equal((await stat(join(c.dataDir, file))).mode & 0o777, 0o600);
  } finally {
    await rt?.close();
    await rm(c.dataDir, { recursive: true, force: true });
  }
});
function chunk(res, obj) {
  res.write('data: ' + JSON.stringify(obj) + '\n\n');
}
for (const { wrong, missing, coreMode, twoTools, schemaCase, assemblyCase } of [
  { wrong: false, missing: false },
  { wrong: true, missing: false },
  { wrong: false, missing: true },
  { wrong: false, missing: false, coreMode: true },
  { wrong: false, missing: false, coreMode: true, twoTools: true },
  { wrong: false, missing: false, coreMode: true, schemaCase: 'control lengths' },
  { wrong: false, missing: false, coreMode: true, schemaCase: 'discovery bounds' },
  ...['degraded', 'audit fallback', 'required', 'rejected', 'cancel', 'mismatch'].map((assemblyCase) => ({
    coreMode: true,
    assemblyCase,
  })),
])
  test(
    `real fixed SDK → HTTP model → Core tool callback (${assemblyCase ? 'assembly ' + assemblyCase : wrong ? 'wrong binding rejected' : missing ? 'partial usage unknown' : schemaCase ? schemaCase : coreMode ? (twoTools ? 'Core ordered dual tools' : 'Core model authorization') : 'success'})`,
    { timeout: 60000 },
    async () => {
      const c = await config();
      const selectedTool =
        schemaCase === 'control lengths'
          ? controlTool
          : schemaCase === 'discovery bounds'
            ? discoveryTool
            : tool;
      let rt;
      let models = 0,
        calls = 0,
        reports = 0;
      const required = assemblyCase === 'required';
      const assembly = {
        profileId: 'ops',
        revision: 2,
        assets: [
          { kind: 'hook', id: 'tool-audit', version: '1', required: true },
          { kind: 'hook', id: 'tool-limit', version: '1', required: true },
          { kind: 'mcp', id: 'external', version: '1', required },
        ],
      };
      const reportArrived = Promise.withResolvers(),
        releaseConfirmation = Promise.withResolvers();
      let assertionFailure;
      const modelCallIds = new Set();
      const toolCallIds = [];
      const core = createServer(async (req, res) => {
        try {
          assert.equal(req.headers.authorization, `Bearer ${token}`);
          assert.equal(req.headers['x-soha-tool-grant'], grant);
          let raw = '';
          for await (const b of req) raw += b;
          const v = JSON.parse(raw);
          if (req.url === '/api/v1/copilot/dsh/assembly') {
            assert.ok(assemblyCase);
            reports++;
            const { kind, ...report } = v;
            assert.equal(kind, 'assembly_report');
            assert.deepEqual(report, {
              schemaVersion: VERSION,
              runId: 'sdk',
              attempt: 1,
              fence: 10,
              profileId: assembly.profileId,
              revision: assembly.revision,
              assets: [
                { kind: 'hook', id: 'tool-audit', version: '1', status: 'loaded', capabilities: ['tool-audit'] },
                { kind: 'hook', id: 'tool-limit', version: '1', status: 'loaded', capabilities: ['tool-limit'] },
                { kind: 'mcp', id: 'external', version: '1', status: 'unavailable', code: 'asset_unavailable' },
              ],
            });
            assert.equal(models, 0);
            reportArrived.resolve();
            await releaseConfirmation.promise;
            if (!res.destroyed) {
              res.writeHead(200, { 'content-type': 'application/json' });
              res.end(JSON.stringify({
                ...report,
                kind: 'assembly_decision',
                fence: assemblyCase === 'mismatch' ? 11 : report.fence,
                status: assemblyCase === 'rejected' ? 'rejected' : 'degraded',
              }));
            }
            return;
          }
          assert.equal(req.url, '/api/v1/copilot/dsh/tools');
          assert.equal(v.toolName, selectedTool.name);
          assert.equal(v.inputHash, sha(raw.match(/,"input":(.*)}$/s)[1]));
          calls++;
          toolCallIds.push(v.callId);
          if (twoTools) assert.deepEqual(toolCallIds, ['call1', 'call2'].slice(0, calls));
          res.writeHead(200, { 'content-type': 'application/json' });
          res.end(
            JSON.stringify({
              schemaVersion: VERSION,
              kind: 'tool_result',
              runId: wrong ? 'other' : v.runId,
              attempt: v.attempt,
              fence: v.fence,
              callId: v.callId,
              inputHash: v.inputHash,
              status: 'completed',
              output: { answer: 'safe result' },
            }),
          );
        } catch (e) {
          assertionFailure = e;
          reportArrived.resolve();
          res.writeHead(500);
          res.end();
        }
      });
      const model = createServer(async (req, res) => {
        try {
          assert.equal(req.headers.authorization, `Bearer ${coreMode ? token : pat}`);
          let raw = '';
          for await (const b of req) raw += b;
          assert.ok(!raw.includes(grant) && !raw.includes(token));
          const body = JSON.parse(raw);
          assert.deepEqual(body.tools[0].function.parameters, selectedTool.inputSchema);
          assert.equal(body.tools[0].function.description, selectedTool.description);
          if (coreMode) {
            assert.equal(req.url, '/api/v1/copilot/dsh/model/v1/chat/completions');
            assert.equal(req.headers['x-soha-tool-grant'], grant);
            for (const [header, value] of Object.entries({
              'x-soha-run-id': 'sdk',
              'x-soha-run-attempt': '1',
              'x-soha-run-fence': '10',
              'x-soha-context-revision': '1',
              'x-soha-tool-catalog-revision': 'catalog1',
            }))
              assert.equal(req.headers[header], value);
            assert.equal(req.headers['x-soha-input-hash'], sha(Buffer.from(raw)));
            const id = req.headers['x-soha-model-call-id'];
            assert.match(id, /^[a-f0-9-]{36}$/);
            assert.ok(!modelCallIds.has(id));
            modelCallIds.add(id);
          }
          assert.deepEqual(
            body.tools.map((t) => t.function.name),
            [coreMode ? selectedTool.name : 'soha_tool_0'],
          );
          if (coreMode) {
            assert.equal(body.model, 'soha-workbench');
            for (const message of body.messages || [])
              for (const call of message.tool_calls || [])
                assert.equal(call.function.name, selectedTool.name);
          }
          models++;
          res.writeHead(200, { 'content-type': 'text/event-stream' });
          const common = {
            id: 'completion1',
            object: 'chat.completion.chunk',
            created: 1,
            model: coreMode ? 'soha-workbench' : 'loopback-model',
          };
          chunk(res, {
            ...common,
            choices: [{ index: 0, delta: { role: 'assistant' }, finish_reason: null }],
          });
          if (models === 1) {
            chunk(res, {
              ...common,
              choices: [
                {
                  index: 0,
                  delta: {
                    tool_calls: [
                      {
                        index: 0,
                        id: 'call1',
                        type: 'function',
                        function: {
                          name: coreMode ? selectedTool.name : 'soha_tool_0',
                          arguments: JSON.stringify(
                            schemaCase === 'control lengths'
                              ? { question: 'Which namespace?' }
                              : schemaCase === 'discovery bounds'
                                ? { query: 'hello', offset: 0 }
                                : { query: 'hello' },
                          ),
                        },
                      },
                      ...(twoTools
                        ? [
                            {
                              index: 1,
                              id: 'call2',
                              type: 'function',
                              function: { name: selectedTool.name, arguments: '{"query":"world"}' },
                            },
                          ]
                        : []),
                    ],
                  },
                  finish_reason: null,
                },
              ],
            });
            chunk(res, {
              ...common,
              choices: [{ index: 0, delta: {}, finish_reason: 'tool_calls' }],
            });
          } else {
            chunk(res, {
              ...common,
              choices: [{ index: 0, delta: { content: 'OK' }, finish_reason: null }],
            });
            chunk(res, { ...common, choices: [{ index: 0, delta: {}, finish_reason: 'stop' }] });
          }
          if (!missing || models === 1)
            chunk(res, {
              ...common,
              choices: [],
              usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
            });
          res.end('data: [DONE]\n\n');
        } catch (e) {
          assertionFailure = e;
          res.writeHead(500);
          res.end();
        }
      });
      try {
        c.callbackURL = (await listen(core)) + '/api/v1/copilot/dsh/tools';
        const modelOrigin = await listen(model);
        c.gatewayURL = modelOrigin + (coreMode ? '/api/v1/copilot/dsh/model/v1' : '/v1');
        if (coreMode) {
          c.models = ['soha-workbench'];
          const tokenFile = join(c.dataDir, 'service-token');
          await writeFile(tokenFile, token);
          const envConfig = await configFromEnv({
            SOHA_DSH_SERVICE_TOKEN_FILE: tokenFile,
            SOHA_DSH_TOOL_CATALOG_FILE: join(c.dataDir, 'must-not-read-catalog'),
            SOHA_DSH_GATEWAY_PAT_FILE: join(c.dataDir, 'must-not-be-read'),
            SOHA_DSH_GATEWAY_URL: c.gatewayURL,
            SOHA_DSH_CORE_CALLBACK_URL: c.callbackURL,
            SOHA_DSH_MODELS: c.models.join(','),
            SOHA_DSH_DATA_DIR: c.dataDir,
          });
          assert.equal(envConfig.modelAuthMode, 'core');
          assert.equal(Object.hasOwn(envConfig, 'pat'), false);
          assert.equal(Object.hasOwn(envConfig, 'catalog'), false);
          c.modelAuthMode = envConfig.modelAuthMode;
          delete c.pat;
          delete c.catalog;
        }
        rt = await createRuntime(c);
        const base = await listen(rt.server);
        if (coreMode)
          assert.equal(
            (await api(base, '/v1/runs', start('missing-grant', { model: 'soha-workbench' }))).code,
            403,
          );
        const run = start('sdk', {
          tools: [selectedTool],
          toolGrant: grant,
          ...(coreMode ? { model: 'soha-workbench' } : {}),
          ...(assemblyCase ? { assembly } : {}),
        });
        const receipt = await api(base, '/v1/runs', run);
        assert.equal(receipt.code, 202);
        if (assemblyCase) {
          await reportArrived.promise;
          if (assertionFailure) throw assertionFailure;
          assert.deepEqual(await api(base, '/v1/runs', run), { code: 200, body: receipt.body });
          assert.equal(models, 0);
          if (assemblyCase === 'audit fallback') {
            const spool = join(c.dataDir, `${sha(`${run.runId}/${run.attempt}/${run.fence}`)}.hooks`);
            await writeFile(spool, 'primary unavailable');
            await writeFile(`${spool}.retry`, 'secondary unavailable');
          }
          if (assemblyCase === 'cancel') {
            const stop = await api(base, '/v1/runs/sdk/attempts/1/cancel', {
              schemaVersion: VERSION,
              kind: 'cancel',
              runId: run.runId,
              attempt: run.attempt,
              fence: run.fence,
            });
            assert.equal(stop.body.status, 'canceled');
          }
          releaseConfirmation.resolve();
        }
        const result = await done(base, 'sdk');
        if (assertionFailure) throw assertionFailure;
        assert.equal(reports, assemblyCase ? 1 : 0);
        if (assemblyCase && !['degraded', 'audit fallback'].includes(assemblyCase)) {
          assert.equal(result.status, assemblyCase === 'cancel' ? 'canceled' : 'failed');
          assert.equal(calls, 0);
          assert.equal(models, 0);
          return;
        }
        assert.equal(result.status, wrong ? 'unknown' : 'completed', JSON.stringify(result));
        assert.equal(calls, twoTools ? 2 : 1);
        if (!wrong) {
          assert.equal(result.assistantText, 'OK');
          assert.equal(models, 2);
          if (coreMode) assert.equal(modelCallIds.size, 2);
          assert.equal(result.usageKnown, !missing);
          if (missing) assert.equal(Object.hasOwn(result, 'usage'), false);
          else
            assert.deepEqual(result.usage, { inputTokens: 20, outputTokens: 10, totalTokens: 30 });
        }
        const events = await api(base, '/v1/runs/sdk/attempts/1/events');
        assert.ok(events.body.items.some((e) => e.type === 'tool_started'));
        assert.ok(!JSON.stringify(events).includes(grant));
        const audits = events.body.items.filter((e) => e.type === 'hook_observed');
        assert.deepEqual(
          audits.map((e) => e.data),
          assemblyCase ? [{ callId: 'call1', toolName: selectedTool.name, outcome: 'completed' }] : [],
        );
        assert.ok(!JSON.stringify(audits).includes('safe result'));
        assert.ok(!JSON.stringify(audits).includes('hello'));
        const terminalEvent = events.body.items.at(-1);
        assert.equal(terminalEvent.data.usageKnown, !wrong && !missing);
        if (!wrong && !missing) {
          assert.deepEqual(terminalEvent.data.usage, result.usage);
          await rt.close();
          rt = await createRuntime(c, () => {
            throw Error('terminal run must not replay');
          });
          const restarted = await listen(rt.server);
          assert.deepEqual((await api(restarted, '/v1/runs/sdk/attempts/1')).body, result);
          assert.deepEqual(
            (await api(restarted, '/v1/runs/sdk/attempts/1/events')).body,
            events.body,
          );
        } else assert.equal(Object.hasOwn(terminalEvent.data, 'usage'), false);
      } finally {
        releaseConfirmation.resolve();
        await rt?.close();
        core.closeAllConnections();
        model.closeAllConnections();
        await Promise.all([new Promise((r) => core.close(r)), new Promise((r) => model.close(r))]);
        await rm(c.dataDir, { recursive: true, force: true });
      }
    },
  );

test('fixed SDK activates managed MCP and Skill before ordered execution', { timeout: 60000 }, async (t) => {
  for (const outcome of ['completed', 'denied', 'unknown']) {
    await t.test(outcome, async () => {
      const c = await config();
      const mcp = { kind: 'mcp', id: 'diagnostics', version: '2' };
      const skill = { kind: 'skill', id: 'runbook', version: '1' };
      const missing = { ...skill, id: 'missing-dependency' };
      const audit = { kind: 'hook', id: 'tool-audit', version: '1' };
      const instructions = '# Read scoped evidence before searching\n';
      const skillRoot = join(await realpath(c.dataDir), 'managed-skill');
      const skillManifest = ({ id, version }, requires) => {
        const snapshot = { id, version, description: 'Read approved guidance',
          files: { 'SKILL.md': sha(instructions) }, requires };
        return { ...snapshot, root: skillRoot, digest: sha(stable(snapshot)) };
      };
      const skillDefinition = skillManifest(skill, [mcp]);
      const skillOutput = {
        id: skill.id, version: skill.version, digest: skillDefinition.digest,
        path: 'SKILL.md', content: instructions,
      };
      const mcpOutput = { content: [{ type: 'text', text: 'scoped-note' }] };
      const baseOutput = { answer: 'known-result' };
      const remoteTool = {
        name: 'read.note',
        description: 'Read the scoped note',
        inputSchema: {
          type: 'object', properties: { word: { type: 'string' } },
          required: ['word'], additionalProperties: false,
        },
      };
      const call = [
        { id: 'skill1', name: 'skill_runbook_load', input: {}, output: skillOutput },
        { id: 'mcp1', name: 'mcp_diagnostics_read_note', input: { word: 'probe' }, output: mcpOutput },
        { id: 'base1', name: tool.name, input: { query: 'probe' }, output: baseOutput },
      ];
      const [readSkill, readMCP, readBase] = call;
      const identity = { schemaVersion: VERSION, runId: 'managed', attempt: 1, fence: 10 };
      const timeline = [],
        errors = [],
        claimed = new Map(),
        receipts = new Map();
      let rt, nativeTools, mcpLists = 0, mcpClosed = 0, models = 0;
      const json = (res, value, headers) =>
        res.writeHead(200, { 'content-type': 'application/json', ...headers }).end(JSON.stringify(value));
      const checkedServer = (handle) => createServer(async (req, res) => {
        try {
          let raw = '';
          for await (const chunk of req) raw += chunk;
          await handle(req, res, raw ? JSON.parse(raw) : null, raw);
        } catch (error) {
          errors.push(error);
          if (res.headersSent) res.destroy();
          else res.writeHead(500).end();
        }
      });
      const provider = checkedServer((req, res, body) => {
        assert.equal(req.url, '/mcp');
        if (req.method === 'GET') return res.writeHead(405).end();
        if (req.method === 'DELETE') {
          mcpClosed++;
          return res.writeHead(204).end();
        }
        const { method, id, params } = body;
        const reply = (result) => json(res, { jsonrpc: '2.0', id, result }, { 'mcp-session-id': 'fixture' });
        if (id === undefined) return res.writeHead(202).end();
        if (method === 'initialize') return reply({
          protocolVersion: params.protocolVersion, capabilities: { tools: {} },
          serverInfo: { name: 'fixture', version: '1' },
        });
        if (method === 'tools/list') {
          mcpLists++;
          return reply({ tools: [remoteTool] });
        }
        assert.equal(method, 'tools/call');
        assert.ok(claimed.has(readMCP.id));
        assert.deepEqual(params.arguments, readMCP.input);
        assert.equal(params.name, remoteTool.name);
        timeline.push('mcp:call');
        return reply(mcpOutput);
      });
      const toolResult = (id, inputHash, output) => ({
        ...identity, kind: 'tool_result', callId: id, inputHash, status: 'completed', output,
      });
      const core = checkedServer((req, res, body, raw) => {
        assert.equal(req.headers.authorization, `Bearer ${token}`);
        assert.equal(req.headers['x-soha-tool-grant'], grant);
        const { schemaVersion, runId, attempt, fence } = body;
        assert.deepEqual({ schemaVersion, runId, attempt, fence }, identity);
        if (req.url === '/api/v1/copilot/dsh/assembly') {
          assert.equal(models, 0);
          assert.ok(mcpLists >= 1);
          assert.equal(body.kind, 'assembly_report');
          assert.equal(body.profileId, 'ops');
          assert.equal(body.revision, 4);
          assert.ok(!raw.includes('scoped evidence'));
          assert.deepEqual(body.assets, [
            { ...skill, status: 'loaded', capabilities: [readSkill.name] },
            { ...mcp, status: 'loaded', capabilities: [readMCP.name] },
            { ...missing, status: 'unavailable', code: 'dependency_unavailable' },
            { ...audit, status: 'loaded', capabilities: [audit.id] },
          ]);
          nativeTools = body.nativeTools;
          assert.deepEqual(nativeTools.map(({ name }) => name), [readMCP.name, readSkill.name]);
          assert.deepEqual(nativeTools[0], {
            kind: mcp.kind, assetId: mcp.id, version: mcp.version, name: readMCP.name,
            description: remoteTool.description, inputSchema: remoteTool.inputSchema,
          });
          timeline.push('assembly');
          return json(res, { ...body, kind: 'assembly_decision', status: 'degraded' });
        }
        const { callId, inputHash } = body;
        const expected = call.find(({ id }) => id === callId);
        assert.ok(expected);
        if (req.url === '/api/v1/copilot/dsh/plugin-tools/settle') {
          assert.equal(inputHash, claimed.get(callId));
          assert.ok(!receipts.has(callId));
          assert.deepEqual(body, toolResult(callId, inputHash, expected.output));
          timeline.push(`settle:${callId}`);
          receipts.set(callId, body);
          if (outcome === 'unknown' && callId === readMCP.id) return res.destroy();
          return json(res, body);
        }
        assert.equal(body.kind, 'tool_call');
        assert.equal(body.toolName, expected.name);
        assert.equal(body.toolCatalogRevision, 'catalog1');
        assert.deepEqual(body.input, expected.input);
        assert.equal(inputHash, sha(raw.match(/,"input":(.*)}$/s)[1]));
        if (req.url === '/api/v1/copilot/dsh/plugin-tools/authorize') {
          assert.ok(!claimed.has(callId));
          timeline.push(`authorize:${callId}`);
          if (outcome === 'denied' && callId === readMCP.id) return res.writeHead(403).end();
          claimed.set(callId, inputHash);
          return res.writeHead(204).end();
        }
        assert.equal(req.url, '/api/v1/copilot/dsh/tools');
        assert.equal(callId, readBase.id);
        assert.ok(receipts.has(readMCP.id));
        timeline.push(`tools:${callId}`);
        const receipt = toolResult(callId, inputHash, baseOutput);
        receipts.set(callId, receipt);
        return json(res, receipt);
      });
      const model = checkedServer((req, res, body, raw) => {
        assert.equal(req.url, '/api/v1/copilot/dsh/model/v1/chat/completions');
        assert.equal(req.headers.authorization, `Bearer ${token}`);
        assert.equal(req.headers['x-soha-tool-grant'], grant);
        assert.equal(req.headers['x-soha-input-hash'], sha(raw));
        assert.equal(timeline[0], 'assembly');
        assert.deepEqual(body.tools.map(({ function: { name, description, parameters } }) => ({
          name, description, parameters,
        })), [tool, ...nativeTools].map(({ name, description, inputSchema }) => ({
          name, description, parameters: inputSchema,
        })));
        const previous = body.messages.filter((message) => message.role === 'tool');
        assert.deepEqual(previous.map(({ tool_call_id, content }) => ({
          id: tool_call_id, result: JSON.parse(content),
        })), call.slice(0, [0, 1, 3][models]).map(({ id, output }) => ({
          id, result: { status: 'completed', output },
        })));
        assert.ok(previous.every(({ tool_call_id }) => receipts.has(tool_call_id)));
        timeline.push(`model:${++models}`);
        const next = [[readSkill], [readMCP, readBase], []][models - 1];
        const common = { id: `completion${models}`, object: 'chat.completion.chunk', created: 1, model: body.model };
        const choice = (delta, finish_reason = null) => chunk(res, {
          ...common, choices: [{ index: 0, delta, finish_reason }],
        });
        res.writeHead(200, { 'content-type': 'text/event-stream' });
        choice({ role: 'assistant' });
        if (next.length) {
          choice({ tool_calls: next.map(({ id, name, input }, index) => ({
            index, id, type: 'function', function: { name, arguments: JSON.stringify(input) },
          })) });
          choice({}, 'tool_calls');
        } else {
          choice({ content: 'Verified' });
          choice({}, 'stop');
        }
        chunk(res, { ...common, choices: [],
          usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 } });
        res.end('data: [DONE]\n\n');
      });
      try {
        await mkdir(skillRoot);
        await writeFile(join(skillRoot, 'SKILL.md'), instructions);
        const mcpOrigin = await listen(provider);
        const manifestPath = join(c.dataDir, 'managed-assets.json');
        await writeFile(manifestPath, JSON.stringify({
          mcp: [{ id: mcp.id, version: mcp.version, transport: 'http', allowedOrigin: mcpOrigin,
            url: mcpOrigin + '/mcp', allowedTools: [remoteTool.name], allowedResourceURIs: [], loopbackFixture: true }],
          skill: [skillDefinition, skillManifest(missing, [{ ...mcp, id: 'inactive' }])],
        }));
        const tokenPath = join(c.dataDir, 'service-token');
        await writeFile(tokenPath, token);
        rt = await createRuntime(await configFromEnv({
          SOHA_DSH_SERVICE_TOKEN_FILE: tokenPath,
          SOHA_DSH_PLUGIN_ASSETS_FILE: manifestPath,
          SOHA_DSH_DATA_DIR: c.dataDir,
          SOHA_DSH_SCHEMA_PATH: c.schemaPath,
          SOHA_DSH_MODELS: 'loopback-model',
          SOHA_DSH_GATEWAY_URL: (await listen(model)) + '/api/v1/copilot/dsh/model/v1',
          SOHA_DSH_CORE_CALLBACK_URL: (await listen(core)) + '/api/v1/copilot/dsh/tools',
        }));
        const base = await listen(rt.server);
        assert.equal((await api(base, '/v1/runs', start(identity.runId, {
          toolGrant: grant, tools: [tool],
          assembly: { profileId: 'ops', revision: 4,
            assets: [skill, mcp, missing, audit].map((ref) => ({ ...ref, required: ref !== missing })) },
        }))).code, 202);
        const final = await done(base, identity.runId);
        if (errors.length) throw errors[0];
        assert.equal(final.status, outcome === 'denied' ? 'failed' : outcome, JSON.stringify(final));
        assert.deepEqual(timeline, [
          'assembly', 'model:1', 'authorize:skill1', 'settle:skill1', 'model:2',
          'authorize:mcp1',
          ...(outcome !== 'denied' ? ['mcp:call', 'settle:mcp1'] : []),
          ...(outcome === 'completed' ? ['tools:base1', 'model:3'] : []),
        ]);
        assert.equal(mcpClosed, 1);
        const { body: { items } } = await api(base, '/v1/runs/managed/attempts/1/events');
        const observed = items.filter(({ type }) => type === 'hook_observed');
        if (outcome !== 'completed') {
          assert.equal(items.at(-1).data.errorCode, outcome === 'unknown'
            ? 'tool_outcome_unknown' : 'execution_stopped');
          assert.equal(final.usageKnown, false);
          assert.deepEqual(observed.filter(({ data }) => data.callId === readMCP.id).map(({ data }) => data), [
            { callId: readMCP.id, toolName: readMCP.name, outcome },
          ]);
          assert.ok(!observed.some(({ data }) => data.callId === readBase.id && data.outcome === outcome));
        } else {
          assert.equal(final.assistantText, 'Verified');
          assert.deepEqual(observed.map(({ data }) => data), call.map(({ id, name }) => ({
            callId: id, toolName: name, outcome: 'completed',
          })));
        }
        assert.ok(!JSON.stringify(items).includes(instructions.trim()));
        assert.ok(!JSON.stringify(items).includes('scoped-note'));
      } finally {
        try {
          await rt?.close();
        } finally {
          for (const server of [provider, core, model]) {
            server.closeAllConnections();
            if (server.listening) await new Promise((resolve) => server.close(resolve));
          }
          await rm(c.dataDir, { recursive: true, force: true });
        }
      }
    });
  }
});

for (const earlyEof of [false, true])
  test(
    `UTF-8 split stream (${earlyEof ? 'early EOF rejected' : 'durable Chinese delta'})`,
    { timeout: 60000 },
    async () => {
      const c = await config();
      let rt,
        requests = 0;
      const model = createServer(async (req, res) => {
        for await (const _ of req) {
        }
        requests++;
        res.writeHead(200, { 'content-type': 'text/event-stream' });
        const common = {
          id: 'chinese1',
          object: 'chat.completion.chunk',
          created: 1,
          model: 'loopback-model',
        };
        const raw = Buffer.from(
          'data: ' +
            JSON.stringify({
              ...common,
              choices: [
                { index: 0, delta: { role: 'assistant', content: '你好' }, finish_reason: null },
              ],
            }) +
            '\n\n',
        );
        const split = raw.indexOf(Buffer.from('你')) + 1;
        res.write(raw.subarray(0, split));
        await new Promise((resolve) => setTimeout(resolve, 50));
        if (res.destroyed) return;
        res.write(raw.subarray(split));
        chunk(res, { ...common, choices: [{ index: 0, delta: {}, finish_reason: 'stop' }] });
        chunk(res, {
          ...common,
          choices: [],
          usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
        });
        res.end(earlyEof ? '' : 'data: [DONE]\n\n');
      });
      try {
        c.gatewayURL = (await listen(model)) + '/v1';
        rt = await createRuntime(c);
        const base = await listen(rt.server);
        assert.equal((await api(base, '/v1/runs', start('chinese'))).code, 202);
        const result = await done(base, 'chinese');
        assert.equal(requests, 1);
        const events = (await api(base, '/v1/runs/chinese/attempts/1/events')).body.items;
        assert.equal(
          events
            .filter((e) => e.type === 'message_delta')
            .map((e) => e.data.delta)
            .join(''),
          '你好',
        );
        if (earlyEof) {
          assert.equal(result.status, 'failed');
          assert.equal(result.usageKnown, false);
          assert.equal(Object.hasOwn(result, 'usage'), false);
          assert.ok(!events.some((e) => e.type === 'completed'));
        } else {
          assert.equal(result.status, 'completed');
          assert.equal(result.assistantText, '你好');
          assert.equal(result.usageKnown, true);
          assert.deepEqual(result.usage, { inputTokens: 10, outputTokens: 5, totalTokens: 15 });
        }
      } finally {
        await rt?.close();
        model.closeAllConnections();
        await new Promise((resolve) => model.close(resolve));
        await rm(c.dataDir, { recursive: true, force: true });
      }
    },
  );

test('Core per-run catalogs and schemas remain isolated without a startup catalog', async () => {
  const c = await config();
  c.modelAuthMode = 'core';
  c.capacity = 2;
  delete c.catalog;
  delete c.pat;
  const firstTool = {
    ...tool,
    inputSchema: {
      type: 'object',
      properties: { value: { type: 'string' } },
      required: ['value'],
      additionalProperties: false,
    },
  };
  const secondTool = {
    ...firstTool,
    name: 'resource.update',
    readOnly: false,
    inputSchema: {
      type: 'object',
      properties: { value: { type: 'integer' } },
      required: ['value'],
      additionalProperties: false,
    },
  };
  const starts = [
    start('catalog-a', {
      toolGrant: grant + '-a',
      toolCatalogRevision: 'revision-a',
      tools: [firstTool],
    }),
    start('catalog-b', {
      profile: 'heya',
      toolGrant: grant + '-b',
      toolCatalogRevision: 'revision-b',
      tools: [secondTool],
    }),
  ];
  let rt,
    ready,
    arrived = 0;
  const barrier = new Promise((resolve) => {
    ready = resolve;
  });
  const observed = [];
  const core = createServer(async (req, res) => {
    let raw = '';
    for await (const chunk of req) raw += chunk;
    const call = JSON.parse(raw);
    observed.push(call);
    const source = starts.find((s) => s.runId === call.runId);
    assert.equal(req.headers['x-soha-tool-grant'], source.toolGrant);
    assert.equal(call.toolName, source.tools[0].name);
    assert.equal(call.toolCatalogRevision, source.toolCatalogRevision);
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(
      JSON.stringify({
        schemaVersion: VERSION,
        kind: 'tool_result',
        runId: call.runId,
        attempt: call.attempt,
        fence: call.fence,
        callId: call.callId,
        inputHash: call.inputHash,
        status: 'completed',
        output: 'OK',
      }),
    );
  });
  const executor = async ({ start: s, bridge }) => {
    if (++arrived === 2) ready();
    await barrier;
    const value = s.runId === 'catalog-a' ? 'text' : 42;
    await assert.rejects(
      bridge({
        alias: 'soha_tool_0',
        callId: 'invalid',
        input: { value: typeof value === 'string' ? 42 : 'text' },
      }),
      (e) => e.status === 403,
    );
    await assert.rejects(
      bridge({ alias: 'soha_tool_1', callId: 'foreign', input: { value } }),
      (e) => e.status === 403,
    );
    const result = await bridge({ alias: 'soha_tool_0', callId: 'valid', input: { value } });
    assert.equal(result.status, 'completed');
    return { assistantText: 'OK', usageKnown: false };
  };
  try {
    c.callbackURL = await listen(core);
    rt = await createRuntime(c, executor);
    const base = await listen(rt.server);
    assert.equal(
      (await api(base, '/v1/runs', start('chat-write', { toolGrant: grant, tools: [secondTool] })))
        .code,
      400,
    );
    assert.equal(
      (
        await api(
          base,
          '/v1/runs',
          start('unsupported', {
            toolGrant: grant,
            tools: [
              {
                ...firstTool,
                inputSchema: {
                  type: 'object',
                  properties: { value: { $ref: 'https://example.invalid/schema' } },
                },
              },
            ],
          }),
        )
      ).code,
      400,
    );
    const accepted = await Promise.all(starts.map((s) => api(base, '/v1/runs', s)));
    assert.deepEqual(
      accepted.map((r) => r.code),
      [202, 202],
    );
    const results = await Promise.all(starts.map((s) => done(base, s.runId)));
    assert.deepEqual(
      results.map((r) => r.status),
      ['completed', 'completed'],
    );
    assert.equal(observed.length, 2);
    assert.deepEqual(
      new Set(observed.map((c) => c.toolCatalogRevision)),
      new Set(['revision-a', 'revision-b']),
    );
  } finally {
    await rt?.close();
    core.closeAllConnections();
    await new Promise((resolve) => core.close(resolve));
    await rm(c.dataDir, { recursive: true, force: true });
  }
});

for (const outcome of ['completed', 'failed', 'canceled'])
  test(`parent tool gate orders reverse/concurrent callbacks (${outcome})`, async () => {
    const controller = new AbortController();
    const received = [];
    let gate, firstArrived;
    const arrived = new Promise((resolve) => {
      firstArrived = resolve;
    });
    const modelResponse = {
      choices: [
        {
          delta: {
            tool_calls: [
              { index: 0, id: 'first', function: { name: 'soha_tool_0' } },
              { index: 1, id: 'second', function: { name: 'soha_tool_0' } },
            ],
          },
        },
      ],
    };
    const core = createServer(async (req, res) => {
      let raw = '';
      for await (const chunk of req) raw += chunk;
      const call = JSON.parse(raw);
      received.push(call.callId);
      firstArrived();
      if (call.callId === 'first') await new Promise((resolve) => setTimeout(resolve, 30));
      if (res.destroyed) return;
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ status: outcome === 'failed' ? 'failed' : 'completed' }));
    });
    try {
      const endpoint = await listen(core);
      gate = createOrderedToolBridge(async (call) => {
        const response = await fetch(endpoint, {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify(call),
          signal: controller.signal,
        });
        return response.json();
      }, controller.signal);
      const calls = modelResponse.choices[0].delta.tool_calls.map((c) => ({
        callId: c.id,
        alias: c.function.name,
        input: { query: c.id },
      }));
      gate.freezeRound(calls);
      const second = gate.invoke(calls[1]);
      const first = gate.invoke(calls[0]);
      const settled = Promise.allSettled([first, second]);
      if (outcome === 'canceled') {
        await arrived;
        controller.abort();
      }
      const results = await settled;
      if (outcome === 'completed') {
        assert.deepEqual(received, ['first', 'second']);
        assert.deepEqual(
          results.map((r) => r.status),
          ['fulfilled', 'fulfilled'],
        );
        gate.assertIdle();
      } else {
        assert.deepEqual(received, ['first']);
        assert.deepEqual(
          results.map((r) => r.status),
          ['rejected', 'rejected'],
        );
        assert.throws(() => gate.assertIdle());
        await assert.rejects(gate.invoke(calls[1]));
      }
    } finally {
      gate?.close();
      core.closeAllConnections();
      await new Promise((resolve) => core.close(resolve));
    }
  });

test('tool callback deadline uses the earlier Start or whole-Run budget', () => {
  const acceptedAt = Date.now();
  assert.equal(
    effectiveRunDeadline(new Date(acceptedAt + 20000).toISOString(), 35000, acceptedAt),
    acceptedAt + 20000,
  );
  assert.equal(
    effectiveRunDeadline(new Date(acceptedAt + 60000).toISOString(), 35000, acceptedAt),
    acceptedAt + 35000,
  );
});
for (const outcome of ['completed', 'canceled', 'expired'])
  test(`tool callback waits with remaining Run budget (${outcome})`, async () => {
    const c = await config();
    c.maxRunMs = 35000;
    let clock = Date.now(),
      rt,
      arrived;
    const received = new Promise((resolve) => {
      arrived = resolve;
    });
    const budgets = [];
    let calls = 0;
    const core = createServer(async (req, res) => {
      let raw = '';
      for await (const chunk of req) raw += chunk;
      const call = JSON.parse(raw);
      calls++;
      arrived();
      await new Promise((resolve) => setTimeout(resolve, 30));
      if (res.destroyed) return;
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(
        JSON.stringify({
          schemaVersion: VERSION,
          kind: 'tool_result',
          runId: call.runId,
          attempt: call.attempt,
          fence: call.fence,
          callId: call.callId,
          inputHash: call.inputHash,
          status: 'completed',
          output: 'OK',
        }),
      );
    });
    const executor = async ({ bridge }) => {
      clock += outcome === 'expired' ? 36000 : 1000;
      const result = await bridge({
        alias: 'soha_tool_0',
        callId: 'approval',
        input: { query: 'read' },
      });
      assert.equal(result.status, 'completed');
      return { assistantText: 'OK', usageKnown: false };
    };
    try {
      c.callbackURL = await listen(core);
      rt = await createRuntime(c, executor, {
        now: () => clock,
        timeoutSignal: (ms) => {
          budgets.push(ms);
          return AbortSignal.timeout(1000);
        },
      });
      const base = await listen(rt.server);
      const s = start('approval-wait', {
        tools: [tool],
        toolGrant: grant,
        deadline: new Date(clock + (outcome === 'completed' ? 25000 : 60000)).toISOString(),
      });
      assert.equal((await api(base, '/v1/runs', s)).code, 202);
      if (outcome === 'canceled') {
        await received;
        await api(base, '/v1/runs/approval-wait/attempts/1/cancel', {
          schemaVersion: VERSION,
          kind: 'cancel',
          runId: s.runId,
          attempt: 1,
          fence: 10,
        });
      }
      const status = await done(base, s.runId);
      assert.equal(status.status, outcome === 'completed' ? 'completed' : 'unknown');
      assert.equal(calls, outcome === 'expired' ? 0 : 1);
      assert.deepEqual(
        budgets,
        outcome === 'expired' ? [] : [outcome === 'completed' ? 24000 : 34000],
      );
    } finally {
      await rt?.close();
      core.closeAllConnections();
      await new Promise((resolve) => core.close(resolve));
      await rm(c.dataDir, { recursive: true, force: true });
    }
  });

test('full control schemas remain authoritative at parent callback bounds', async () => {
  const c = await config();
  c.modelAuthMode = 'core';
  delete c.catalog;
  delete c.pat;
  let rt,
    calls = 0;
  const core = createServer(async (req, res) => {
    let raw = '';
    for await (const chunk of req) raw += chunk;
    const call = JSON.parse(raw);
    calls++;
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(
      JSON.stringify({
        schemaVersion: VERSION,
        kind: 'tool_result',
        runId: call.runId,
        attempt: call.attempt,
        fence: call.fence,
        callId: call.callId,
        inputHash: call.inputHash,
        status: 'completed',
        output: 'OK',
      }),
    );
  });
  const executor = async ({ tools, bridge }) => {
    assert.deepEqual(tools[0].sdkInputSchema, { type: 'object', additionalProperties: true });
    const invalid =
      tools[0].name === controlTool.name
        ? [{ question: '' }, { question: 'x'.repeat(2049) }]
        : [
            { query: 'q', offset: -1 },
            { query: 'q', offset: 513 },
            { query: 'q', offset: 0.5 },
            { query: 'q'.repeat(257), offset: 0 },
          ];
    for (const input of invalid)
      await assert.rejects(
        bridge({ alias: 'soha_tool_0', callId: 'invalid', input }),
        (e) => e.status === 403,
      );
    const input =
      tools[0].name === controlTool.name
        ? { question: 'Which namespace?' }
        : { query: 'q', offset: 512 };
    await bridge({ alias: 'soha_tool_0', callId: 'valid', input });
    return { assistantText: 'OK', usageKnown: false };
  };
  try {
    c.callbackURL = await listen(core);
    rt = await createRuntime(c, executor);
    const base = await listen(rt.server);
    for (const [index, selectedTool] of [controlTool, discoveryTool].entries()) {
      const s = start('full-schema-' + index, {
        tools: [selectedTool],
        toolGrant: grant,
        toolCatalogRevision: 'full-' + index,
      });
      assert.equal((await api(base, '/v1/runs', s)).code, 202);
      assert.equal((await done(base, s.runId)).status, 'completed');
    }
    assert.equal(calls, 2);
    const invalid = await api(
      base,
      '/v1/runs',
      start('unknown-schema', {
        tools: [{ ...controlTool, inputSchema: { type: 'object', inventedValidation: true } }],
        toolGrant: grant,
      }),
    );
    assert.equal(invalid.code, 400);
    assert.equal(invalid.body.error, 'invalid_tool_schema');
    const remote = await api(
      base,
      '/v1/runs',
      start('remote-schema', {
        tools: [
          {
            ...controlTool,
            inputSchema: {
              type: 'object',
              properties: { question: { $ref: 'https://example.invalid/schema' } },
            },
          },
        ],
        toolGrant: grant,
      }),
    );
    assert.equal(remote.code, 400);
    assert.equal(remote.body.error, 'unsupported_tool_schema_reference');
  } finally {
    await rt?.close();
    core.closeAllConnections();
    await new Promise((resolve) => core.close(resolve));
    await rm(c.dataDir, { recursive: true, force: true });
  }
});
