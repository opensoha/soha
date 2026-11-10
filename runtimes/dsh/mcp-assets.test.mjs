import assert from 'node:assert/strict';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { WebSocketServer } from 'ws';
import { prepareMCPAsset } from './mcp-assets.mjs';

const token = 'fixture-mcp-token-01';
const ref = { kind: 'mcp', id: 'fixture-one', version: '1', required: true };
const allowedURI = 'fixture://approved/resource';
const forbiddenURI = 'fixture://outside/resource';
const inputSchema = { type: 'object', properties: { word: { type: 'string' } }, required: ['word'], additionalProperties: false };
const exposed = { name: 'read.note', description: 'Read a fixture note.', inputSchema };
const context = (signal = new AbortController().signal) => ({ signal, deadline: Date.now() + 5_000, profile: 'heya' });
const preDispatchFailure = { message: 'MCP operation failed', definiteFailure: true };
const postDispatchFailure = (error) => error.message === 'MCP operation failed' && error.definiteFailure !== true;

test('personal stdio binds the run and requires a cleanup acknowledgement', async (t) => {
  const server = createServer();
  const wss = new WebSocketServer({ noServer: true });
  let cleanup = 'closed', rejectUpgrade = false;
  const methods = [];
  const definition = {
    id: ref.id, version: ref.version, transport: 'stdio', executionPosition: 'personal-docker',
    allowedTools: ['read_note'], allowedResourceURIs: [],
  };
  const binding = {
    serviceToken: token, toolGrant: 'fixture-tool-grant', runId: 'run:fixture', attempt: 2, fence: 3,
  };
  server.on('upgrade', (req, socket, head) => {
    const target = new URL(req.url, 'http://core');
    assert.equal(target.pathname, '/api/v1/copilot/dsh/personal-stdio');
    assert.deepEqual(Object.fromEntries(target.searchParams), {
      runId: binding.runId, attempt: '2', fence: '3', assetId: ref.id, version: ref.version,
    });
    assert.equal(req.headers.authorization, `Bearer ${binding.serviceToken}`);
    assert.equal(req.headers['x-soha-tool-grant'], binding.toolGrant);
    if (rejectUpgrade) {
      socket.end('HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n');
      return;
    }
    wss.handleUpgrade(req, socket, head, (ws) => {
      ws.on('message', (data, isBinary) => {
        const raw = data.toString();
        if (!isBinary) {
          assert.deepEqual(JSON.parse(raw), { close: true });
          methods.push('close');
          if (cleanup) ws.send(JSON.stringify({ status: cleanup }));
          else ws.close();
          return;
        }
        assert.ok(raw.endsWith('\n'));
        const { id, method, params } = JSON.parse(raw);
        methods.push(method);
        if (id === undefined) return;
        const result = {
          initialize: { protocolVersion: params?.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: 'personal', version: '1' } },
          'tools/list': { tools: [{ ...exposed, name: 'read_note' }] },
        }[method];
        assert.ok(result);
        const response = Buffer.from(JSON.stringify({ jsonrpc: '2.0', id, result }) + '\n');
        ws.send(response.subarray(0, 8));
        ws.send(response.subarray(8));
      });
    });
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(async () => {
    for (const client of wss.clients) client.terminate();
    await new Promise((resolve) => wss.close(resolve));
    await new Promise((resolve) => server.close(resolve));
  });
  const personal = { ...binding, callbackURL: `http://127.0.0.1:${server.address().port}/api/v1/copilot/dsh/tools` };
  const prepare = (asset = definition, profile = 'heya') =>
    prepareMCPAsset(asset, ref, { ...context(), profile, personal });
  await assert.rejects(prepare({ ...definition, program: '/bin/untrusted' }), /asset unavailable/);
  await assert.rejects(prepare(definition, 'chat'), /asset unavailable/);
  await assert.rejects(prepare({ ...definition, allowedTools: ['read.note'] }), /asset unavailable/);
  const lease = await prepare();
  assert.deepEqual(lease.capabilities, ['tools']);
  await lease.close();
  assert.deepEqual(methods, ['initialize', 'notifications/initialized', 'tools/list', 'close']);

  for (const status of ['unknown', undefined]) {
    cleanup = status;
    const lease = await prepare();
    await assert.rejects(lease.close(), { cleanupUnknown: true });
  }
  rejectUpgrade = true;
  await assert.rejects(prepare(), /asset unavailable/);
});

test('managed MCP lease only invokes frozen grants and redacts provider failures', async (t) => {
  const dir = await mkdtemp(join(process.env.OPENSOHA_VERIFY_DIR ?? tmpdir(), 'soha-mcp-'));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const credentialFile = join(dir, 'credential');
  await writeFile(credentialFile, token, { mode: 0o600 });
  let tools = [exposed, { ...exposed, name: 'hidden' }];
  let callCount = 0;
  let reads = 0;
  let resourceLists = 0;
  let toolDiscoveryUnavailable = false;
  let resourceListed = true;
  let responseURI = allowedURI;
  let sessionId = 'fixture-session';
  const deletedSessions = [];
  let deleteStatus = 405;
  let deleteSessionEcho;
  let redirectVisits = 0;
  const server = createServer(async (req, res) => {
    if (req.url === '/redirect') { res.writeHead(307, { location: '/unexpected' }).end(); return; }
    if (req.url === '/unexpected') { redirectVisits++; res.writeHead(500).end(); return; }
    if (req.headers.authorization !== `Bearer ${token}` || req.url !== '/mcp') { res.writeHead(401).end(); return; }
    if (req.method === 'GET') { res.writeHead(405).end(); return; }
    if (req.method === 'DELETE') {
      deletedSessions.push(req.headers['mcp-session-id']);
      if (deleteSessionEcho) res.setHeader('mcp-session-id', deleteSessionEcho);
      res.writeHead(deleteStatus).end();
      return;
    }
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    const { id, method, params } = JSON.parse(Buffer.concat(chunks));
    if (id === undefined) { res.writeHead(202).end(); return; }
    const reply = (result) => {
      if (sessionId) res.setHeader('mcp-session-id', sessionId);
      return res.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify({ jsonrpc: '2.0', id, result }));
    };
    switch (method) {
      case 'initialize': return reply({ protocolVersion: params.protocolVersion, capabilities: { tools: {}, resources: {} }, serverInfo: { name: 'fixture', version: '1' } });
      case 'tools/list': return toolDiscoveryUnavailable ? res.writeHead(503).end() : reply({ tools, ttlMs: 360_000 });
      case 'resources/list': {
        resourceLists++;
        const resources = [{ uri: forbiddenURI, name: 'outside' }];
        if (resourceListed) resources.push({ uri: allowedURI, name: 'approved' });
        return reply({ resources });
      }
      case 'resources/read':
        reads++;
        return reply({ contents: [{ uri: responseURI, text: 'fixture body' }] });
      case 'tools/call': {
        callCount++;
        if (params.arguments.word === 'abort') return;
        const word = params.arguments.word;
        if (word === 'failure') return reply({ isError: true, content: [{ type: 'text', text: `raw failure ${token}` }] });
        return reply({ content: [{ type: 'text', text: word === 'leak' ? token : word }], _meta: { redacted: token } });
      }
      default: return res.writeHead(500).end();
    }
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  t.after(() => { server.closeAllConnections(); return new Promise((resolve) => server.close(resolve)); });
  const allowedOrigin = `http://127.0.0.1:${server.address().port}`;
  const definition = { ...ref, transport: 'http', allowedOrigin, url: `${allowedOrigin}/mcp`, loopbackFixture: true, credentialFile,
    allowedTools: ['read.note'], allowedResourceURIs: [allowedURI] };
  await assert.rejects(prepareMCPAsset({ ...definition, url: `${allowedOrigin}/redirect` }, ref, context()), { cleanupUnknown: true });
  assert.equal(redirectVisits, 0);
  await assert.rejects(prepareMCPAsset({ ...definition, loopbackFixture: false }, ref, context()), /asset unavailable/);
  await assert.rejects(prepareMCPAsset({ ...definition, url: 'https://127.0.0.1/mcp', allowedOrigin: 'https://127.0.0.1', loopbackFixture: false }, ref, context()), /asset unavailable/);

  const run = new AbortController();
  const lease = await prepareMCPAsset(definition, ref, context(run.signal));
  const [tool, list, read] = lease.tools;
  assert.deepEqual(lease.tools.map(({ name }) => name), ['mcp_fixture_one_read_note', 'mcp_fixture_one_list_resources', 'mcp_fixture_one_read_resource']);
  assert.equal(callCount, 0);
  assert.equal(reads, 0);
  const listed = resourceLists;
  const dispatchList = await list.preflight({}, run.signal);
  assert.equal(resourceLists, listed);
  assert.deepEqual(await dispatchList(), { resources: [{ uri: allowedURI }] });
  assert.equal(resourceLists, listed + 1);
  await assert.rejects(read.invoke({ uri: forbiddenURI }, run.signal), preDispatchFailure);
  resourceListed = false;
  await assert.rejects(read.invoke({ uri: allowedURI }, run.signal), preDispatchFailure);
  assert.equal(reads, 0);
  resourceListed = true;
  const readInput = { uri: allowedURI };
  const dispatchRead = await read.preflight(readInput, run.signal);
  assert.equal(reads, 0);
  readInput.uri = forbiddenURI;
  assert.deepEqual(await dispatchRead(), { contents: [{ uri: allowedURI, text: 'fixture body' }] });
  responseURI = forbiddenURI;
  await assert.rejects(read.invoke({ uri: allowedURI }, run.signal), postDispatchFailure);
  assert.equal(reads, 2);
  toolDiscoveryUnavailable = true;
  await assert.rejects(tool.invoke({ word: 'discovery failed' }, run.signal), preDispatchFailure);
  toolDiscoveryUnavailable = false;
  await assert.rejects(tool.invoke({ word: 42 }, run.signal), preDispatchFailure);
  assert.equal(callCount, 0);
  const toolInput = { word: 'hello' };
  const dispatchTool = await tool.preflight(toolInput, run.signal);
  assert.equal(callCount, 0);
  toolInput.word = 'changed after validation';
  assert.deepEqual(await dispatchTool(), { content: [{ type: 'text', text: 'hello' }] });
  await assert.rejects(dispatchTool(), preDispatchFailure);
  assert.deepEqual(await tool.invoke({ word: 'failure' }, run.signal), { isError: true, content: [{ type: 'text', text: 'MCP tool failed' }] });
  await assert.rejects(tool.invoke({ word: 'leak' }, run.signal), postDispatchFailure);
  tools = [{ ...exposed, inputSchema: { ...inputSchema, required: [] } }];
  await assert.rejects(tool.invoke({ word: 'blocked' }, run.signal), preDispatchFailure);
  tools = [exposed];
  await assert.rejects(tool.invoke({ word: 'still blocked' }, run.signal), preDispatchFailure);
  assert.equal(callCount, 3);
  run.abort();
  let retryCleanup;
  await assert.rejects(lease.close(), (error) => {
    retryCleanup = error.retryCleanup;
    return error.cleanupUnknown === true;
  });
  assert.deepEqual(deletedSessions, [sessionId]);
  deleteStatus = 202;
  await assert.rejects(retryCleanup(), { cleanupUnknown: true });
  assert.deepEqual(deletedSessions, [sessionId, sessionId]);
  deleteStatus = 204;
  await retryCleanup();
  await lease.close();
  assert.deepEqual(deletedSessions, [sessionId, sessionId, sessionId]);

  const cancellable = await prepareMCPAsset(definition, ref, context());
  const abort = new AbortController();
  const pending = cancellable.tools[0].invoke({ word: 'abort' }, abort.signal);
  const seen = AbortSignal.timeout(1_000);
  while (callCount !== 4) {
    seen.throwIfAborted();
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  abort.abort();
  await assert.rejects(pending, postDispatchFailure);
  await cancellable.close();
  assert.equal(deletedSessions.length, 4);

  tools = [exposed, { ...exposed, name: 'read-note' }];
  await assert.rejects(prepareMCPAsset({ ...definition, allowedTools: ['read.note', 'read-note'] }, ref, context()), /asset unavailable/);
  assert.equal(deletedSessions.length, 5);
  tools = [exposed];
  const revoked = await prepareMCPAsset(definition, ref, context());
  const revokedSignal = new AbortController().signal;
  const dispatchRevoked = await revoked.tools[0].preflight({ word: 'denied' }, revokedSignal);
  await writeFile(credentialFile, 'fixture-mcp-token-02');
  await assert.rejects(revoked.tools[0].preflight({ word: 'denied' }, revokedSignal), preDispatchFailure);
  await assert.rejects(dispatchRevoked(), preDispatchFailure);
  assert.equal(callCount, 4);
  deleteStatus = 401;
  await assert.rejects(revoked.close(), { cleanupUnknown: true });
  assert.equal(deletedSessions.length, 6);
  await rm(credentialFile);
  deleteStatus = 404;
  await assert.rejects(revoked.close(), { cleanupUnknown: true });
  deleteSessionEcho = 'different-session';
  await assert.rejects(revoked.close(), { cleanupUnknown: true });
  deleteSessionEcho = sessionId;
  await revoked.close();
  assert.equal(deletedSessions.length, 9);
  assert.deepEqual(deletedSessions.slice(-4), [sessionId, sessionId, sessionId, sessionId]);

  await writeFile(credentialFile, token, { mode: 0o600 });
  sessionId = undefined;
  const stateless = await prepareMCPAsset(definition, ref, context());
  const canceledBeforeDispatch = new AbortController();
  const dispatchCanceled = await stateless.tools[0].preflight({ word: 'canceled' }, canceledBeforeDispatch.signal);
  canceledBeforeDispatch.abort();
  await assert.rejects(dispatchCanceled(), preDispatchFailure);
  await stateless.close();
  assert.equal(callCount, 4);
  assert.equal(deletedSessions.length, 9);
});
