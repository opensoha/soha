import { spawn } from 'node:child_process';
import { lookup } from 'node:dns/promises';
import { constants } from 'node:fs';
import { open, realpath, stat } from 'node:fs/promises';
import { isIP } from 'node:net';
import { isAbsolute, normalize, sep } from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';
import { isDeepStrictEqual } from 'node:util';
import { assertObjectJsonSchema, isPlainJsonRecord, validateJsonSchemaValue } from '@deepseek-ai/dsh-tools';
import { Client, ReadBuffer, StreamableHTTPClientTransport, serializeMessage } from '@modelcontextprotocol/client';
import ipaddr from 'ipaddr.js';
import { Agent, WebSocket, fetch } from 'undici';

const maxWireBytes = 1024 * 1024;
const maxValueBytes = 64 * 1024;
const operationMs = 15_000;
const cleanupMs = 3_000;
const personalCleanupMs = 8_000;
const mcpError = (message) => new Error(message);
const isFixedPath = (value) => typeof value === 'string' && isAbsolute(value) && normalize(value) === value && !value.includes('\0');

function exactSet(items, valid) {
  if (!Array.isArray(items) || items.length > 32 || !items.every(valid) || new Set(items).size !== items.length)
    throw mcpError('Invalid MCP allowlist');
  return new Set(items);
}

function boundedJSON(value, token) {
  const json = JSON.stringify(value);
  if (json === undefined || Buffer.byteLength(json) > maxValueBytes || (token && (
    json.includes(token) || json.includes(Buffer.from(token).toString('base64')) || json.includes(encodeURIComponent(token))
  )))
    throw mcpError('MCP data rejected');
  return JSON.parse(json);
}

async function readToken(path) {
  if (!isFixedPath(path)) throw mcpError('MCP credential unavailable');
  const file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
  try {
    const info = await file.stat();
    if (!info.isFile() || info.size > 4096 || (info.mode & 0o077) !== 0)
      throw mcpError('MCP credential unavailable');
    const bytes = Buffer.alloc(4097);
    const { bytesRead } = await file.read(bytes);
    const token = bytes.toString('utf8', 0, bytesRead).trim();
    if (bytesRead > 4096 || !/^[A-Za-z0-9._~+/-]{16,}=*$/.test(token))
      throw mcpError('MCP credential unavailable');
    return token;
  } finally {
    await file.close();
  }
}

function approvedAddress(address, fixture, pinned) {
  if (!isIP(address) || address.includes('%')) return false;
  const ip = ipaddr.parse(address);
  if (fixture) return address === '127.0.0.1' || address === '::1';
  const range = ip.range();
  if (pinned && ['private', 'carrierGradeNat', 'uniqueLocal'].includes(range)) return true;
  return range === 'unicast' && (ip.kind() === 'ipv4' || ip.match(ipaddr.parse('2000::'), 3));
}

function limitedResponse(response) {
  if (!response.body) return response;
  let size = 0;
  const body = response.body.pipeThrough(new TransformStream({
    transform(chunk, controller) {
      size += chunk.byteLength;
      if (size > maxWireBytes) throw mcpError('MCP response limit exceeded');
      controller.enqueue(chunk);
    },
  }));
  return new Response(body, { status: response.status, headers: response.headers });
}

async function httpTransport(definition, runSignal) {
  const url = new URL(definition.url);
  const fixture = definition.loopbackFixture === true;
  const hostname = url.hostname.replace(/^\[|\]$/g, '');
  const pin = definition.pinnedAddress;
  if (url.origin !== definition.allowedOrigin || url.username || url.password || /[?#]/.test(url.href) ||
      (fixture ? url.protocol !== 'http:' || !approvedAddress(hostname, true, false) : url.protocol !== 'https:') ||
      (pin !== undefined && !approvedAddress(pin, fixture, true)) ||
      (isIP(hostname) && (!approvedAddress(hostname, fixture, pin !== undefined) ||
        (pin !== undefined && ipaddr.parse(hostname).toNormalizedString() !== ipaddr.parse(pin).toNormalizedString()))))
    throw mcpError('Invalid MCP endpoint');

  const token = definition.credentialFile === undefined ? undefined : await readToken(definition.credentialFile);
  const checkCredential = async () => {
    if (token && (await readToken(definition.credentialFile)) !== token) throw mcpError('MCP credential changed');
  };
  const createDispatcher = () => new Agent({
    connections: 2,
    connect: {
      rejectUnauthorized: true,
      lookup(host, options, callback) {
        // The checked IP is handed directly to the socket, without a second DNS lookup.
        const addresses = pin ? Promise.resolve([{ address: pin, family: isIP(pin) }]) : lookup(host, { all: true });
        addresses.then((records) => {
          if (host !== hostname || !records.length || records.some(({ address }) => !approvedAddress(address, fixture, pin !== undefined))) {
            callback(mcpError('MCP destination rejected'));
            return;
          }
          if (options.all) callback(null, records);
          else callback(null, records[0].address, records[0].family);
        }, callback);
      },
    },
  });
  const dispatcher = createDispatcher();
  let unconfirmedInitialize = false, sessionId;
  const initializeDispatcher = dispatcher.compose((dispatch) => (options, handler) => dispatch(options, {
    ...handler,
    onRequestSent() { unconfirmedInitialize = true; },
  }));
  const transport = new StreamableHTTPClientTransport(url, {
    reconnectionOptions: { maxRetries: 0 },
    onInsufficientScope: 'throw',
    async fetch(input, init) {
      if (String(input) !== url.href || !['POST', 'GET'].includes(init?.method))
        throw mcpError('MCP destination rejected');
      const signal = init.signal ? AbortSignal.any([runSignal, init.signal]) : runSignal;
      signal.throwIfAborted();
      const headers = new Headers(init.headers);
      if (token) headers.set('authorization', `Bearer ${token}`);
      await checkCredential();
      signal.throwIfAborted();
      const initialize = init.method === 'POST' && JSON.parse(init.body).method === 'initialize';
      const response = await fetch(url, {
        ...init, headers, signal, dispatcher: initialize ? initializeDispatcher : dispatcher, redirect: 'manual',
      });
      if (initialize) sessionId = response.headers.get('mcp-session-id') || undefined;
      if (!response.ok) {
        await response.body?.cancel();
        return new Response(null, { status: response.status });
      }
      return limitedResponse(response);
    },
  });
  return {
    transport, token, checkCredential,
    initialized() { unconfirmedInitialize = false; },
    async close(client) {
      try {
        if (!sessionId) {
          if (unconfirmedInitialize) throw mcpError('MCP cleanup unconfirmed');
          return;
        }
        // Only this endpoint and its returned session can use the original credential for cleanup.
        const headers = new Headers({ 'mcp-session-id': sessionId });
        if (token) headers.set('authorization', `Bearer ${token}`);
        if (transport.protocolVersion) headers.set('mcp-protocol-version', transport.protocolVersion);
        const cleanupDispatcher = createDispatcher();
        try {
          const response = await fetch(url, { method: 'DELETE', headers, signal: AbortSignal.timeout(cleanupMs),
            dispatcher: cleanupDispatcher, redirect: 'error' });
          await response.body?.cancel();
          if (response.status !== 200 && response.status !== 204 &&
              !(response.status === 404 && response.headers.get('mcp-session-id') === sessionId))
            throw mcpError('MCP cleanup unconfirmed');
          sessionId = undefined;
          unconfirmedInitialize = false;
        } finally {
          await cleanupDispatcher.destroy();
        }
      } finally {
        try { await client.close(); } finally { await dispatcher.destroy(); }
      }
    },
  };
}

async function stdioTransport(definition, { profile, workspace }) {
  const { executionPosition, program, argv, packageRoot } = definition;
  if (process.platform !== 'linux' || profile !== 'heya' || !executionPosition ||
      executionPosition !== process.env.SOHA_DSH_MCP_EXECUTION_POSITION || definition.credentialFile !== undefined)
    throw mcpError('MCP execution position unavailable');
  if (!isFixedPath(program) || !/^\/(mcp|usr|bin)\//.test(program) || !isFixedPath(packageRoot) ||
      !Array.isArray(argv) || argv.length > 32 || !argv.every((arg) => typeof arg === 'string' && arg.length <= 4096 && !arg.includes('\0')))
    throw mcpError('Invalid MCP program registration');

  const root = await realpath(packageRoot);
  const work = await realpath(workspace);
  if (root === '/' || root === work || root.startsWith(`${work}${sep}`) || work.startsWith(`${root}${sep}`) || !(await stat(root)).isDirectory())
    throw mcpError('Invalid MCP package root');

  const buffer = new ReadBuffer({ maxBufferSize: maxWireBytes });
  const { promise: sandboxReady, resolve: ready } = Promise.withResolvers();
  let child, exited, reapPromise, sandboxPID, forced;
  const reap = () => reapPromise ??= (async () => {
    ready(false);
    if (!child) return;
    child.stdin.end();
    const wait = (ms) => Promise.race([exited, sleep(ms, false, { ref: false })]);
    if (!await wait(250)) {
      forced = true;
      if (child.exitCode === null && child.signalCode === null) child.kill('SIGKILL');
      if (!await wait(cleanupMs)) throw mcpError('MCP cleanup unconfirmed');
    }
    if (forced && !sandboxPID) throw mcpError('MCP cleanup unconfirmed');
    // The sandbox program is namespace PID 1. Linux reaps its descendants before PID 1 disappears.
    if (!sandboxPID) return;
    const deadline = Date.now() + cleanupMs;
    while (Date.now() < deadline) {
      try { process.kill(sandboxPID, 0); } catch (error) { if (error.code === 'ESRCH') return; }
      await sleep(25);
    }
    throw mcpError('MCP cleanup unconfirmed');
  })();
  const transport = {
    async start() {
      child = spawn('/usr/bin/bwrap', [
        '--unshare-user', '--unshare-ipc', '--unshare-pid', '--unshare-net', '--unshare-uts',
        '--new-session', '--die-with-parent', '--as-pid-1', '--json-status-fd', '3',
        '--clearenv', '--setenv', 'PATH', '/usr/bin:/bin', '--setenv', 'LANG', 'C.UTF-8',
        '--ro-bind', '/usr', '/usr', '--ro-bind-try', '/bin', '/bin',
        '--ro-bind-try', '/lib', '/lib', '--ro-bind-try', '/lib64', '/lib64',
        '--dev', '/dev', '--proc', '/proc', '--tmpfs', '/tmp', '--ro-bind', root, '/mcp',
        '--chdir', '/mcp', '--', program, ...argv,
      ], { env: {}, shell: false, stdio: ['pipe', 'pipe', 'ignore', 'pipe'] });
      const reportError = () => transport.onerror?.(mcpError('MCP subprocess failed'));
      exited = new Promise((resolve) => child.once('close', () => { resolve(true); transport.onclose?.(); }));
      child.stdin.on('error', reportError);
      child.stdout.on('error', reportError);
      child.stdio[3].on('error', reportError);
      child.stdio[3].once('end', () => ready(false));
      child.stdio[3].once('close', () => ready(false));
      let status = '';
      child.stdio[3].on('data', (chunk) => {
        if (sandboxPID || status.length > 4096) return;
        status += chunk.toString();
        const end = status.indexOf('\n');
        if (end < 0) return;
        try {
          const pid = JSON.parse(status.slice(0, end))['child-pid'];
          if (Number.isSafeInteger(pid) && pid > 1 && pid !== child.pid) {
            sandboxPID = pid;
            ready(true);
          } else ready(false);
        } catch { reportError(); }
      });
      child.stdout.on('data', (chunk) => {
        try {
          buffer.append(chunk);
          for (let message; (message = buffer.readMessage()) !== null;) transport.onmessage?.(message);
        } catch {
          reportError();
          void transport.close();
        }
      });
      await new Promise((resolve, reject) => {
        child.once('spawn', resolve);
        child.once('error', () => reject(mcpError('MCP subprocess unavailable')));
      });
      if (!await sandboxReady) throw mcpError('MCP sandbox unavailable');
    },
    send(message) {
      return new Promise((resolve, reject) => child.stdin.write(serializeMessage(message), (error) => {
        if (error) reject(mcpError('MCP subprocess disconnected'));
        else resolve();
      }));
    },
    close() {
      // The SDK may call close without awaiting it. The owner observes the shared reap result.
      return reap().catch(() => {});
    },
  };
  return { transport, async close(client) {
    const [reaped, disconnected] = await Promise.allSettled([reap(), client.close()]);
    if (reaped.status === 'rejected') {
      reapPromise = undefined;
      throw reaped.reason;
    }
    if (disconnected.status === 'rejected') throw disconnected.reason;
  } };
}

function personalStdioTransport(definition, { profile, personal, runSignal, prepareSignal }) {
  if (profile !== 'heya' || !personal?.serviceToken || !personal.toolGrant ||
      Object.keys(definition).some((key) => ![
        'id', 'version', 'transport', 'executionPosition', 'allowedTools', 'allowedResourceURIs',
      ].includes(key)))
    throw mcpError('MCP execution position unavailable');
  const url = new URL(personal.callbackURL);
  if (!['http:', 'https:'].includes(url.protocol) || !url.pathname.endsWith('/tools') ||
      url.username || url.password || url.search || url.hash)
    throw mcpError('MCP execution position unavailable');
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  url.pathname = url.pathname.slice(0, -'/tools'.length) + '/personal-stdio';
  url.search = new URLSearchParams({
    runId: personal.runId,
    attempt: personal.attempt,
    fence: personal.fence,
    assetId: definition.id,
    version: definition.version,
  }).toString();

  const buffer = new ReadBuffer({ maxBufferSize: maxWireBytes });
  const opened = Promise.withResolvers();
  const ack = Promise.withResolvers();
  const agent = new Agent({ connections: 1, webSocket: { maxPayloadSize: maxWireBytes, maxFragments: 256 } });
  let socket, peer, sent, rejected, closing, cleanupStatus;
  const dispatcher = agent.compose((dispatch) => (options, handler) => dispatch(options, {
    ...handler,
    onRequestSent() {
      sent = true;
    },
    onResponseStart(controller, status, ...args) {
      if (status >= 200) rejected = true;
      return handler.onResponseStart.call(this, controller, status, ...args);
    },
    onRequestUpgrade(controller, status, headers, upgradedSocket) {
      peer = upgradedSocket;
      return handler.onRequestUpgrade.call(this, controller, status, headers, upgradedSocket);
    },
  }));
  const closeSocket = () => closing ??= Promise.resolve().then(async () => {
    try {
      if (['closed', 'unavailable'].includes(cleanupStatus)) return;
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send('{"close":true}');
        if (!await Promise.race([ack.promise, sleep(personalCleanupMs, false, { ref: false })]))
          throw mcpError('MCP cleanup unconfirmed');
      } else if (peer || (sent && !rejected)) {
        throw mcpError('MCP cleanup unconfirmed');
      }
    } finally {
      socket?.close();
      peer?.destroy();
      await agent.destroy();
    }
  });
  const transport = {
    async start() {
      prepareSignal.throwIfAborted();
      socket = new WebSocket(url.href, {
        dispatcher,
        headers: { authorization: `Bearer ${personal.serviceToken}`, 'X-Soha-Tool-Grant': personal.toolGrant },
      });
      socket.binaryType = 'arraybuffer';
      socket.addEventListener('open', () => opened.resolve(), { once: true });
      socket.addEventListener('close', () => {
        opened.reject(mcpError('MCP subprocess unavailable'));
        ack.resolve(false);
        transport.onclose?.();
      }, { once: true });
      socket.addEventListener('message', ({ data }) => {
        try {
          if (typeof data === 'string') {
            if (data.length > 64 || cleanupStatus) throw mcpError('Invalid MCP cleanup acknowledgement');
            const message = JSON.parse(data);
            const status = message?.status;
            if (!['closed', 'unknown', 'unavailable'].includes(status) || Object.keys(message).length !== 1)
              throw mcpError('Invalid MCP cleanup acknowledgement');
            cleanupStatus = status;
            ack.resolve(status !== 'unknown');
          } else if (!closing) {
            buffer.append(Buffer.from(data));
            for (let message; (message = buffer.readMessage()) !== null;) transport.onmessage?.(message);
          }
        } catch {
          transport.onerror?.(mcpError('MCP subprocess failed'));
          void transport.close();
        }
      });
      await opened.promise;
    },
    async send(message) {
      runSignal.throwIfAborted();
      const bytes = Buffer.from(serializeMessage(message));
      if (closing || socket?.readyState !== WebSocket.OPEN || socket.bufferedAmount + bytes.length > maxWireBytes)
        throw mcpError('MCP subprocess disconnected');
      socket.send(bytes);
    },
    close() {
      return closeSocket().catch(() => {});
    },
  };
  return { transport, async close(client) {
    try { await closeSocket(); } finally { await client.close(); }
  } };
}

function selected(entries, allowed, field) {
  const found = new Map();
  for (const entry of entries) {
    const key = entry[field];
    if (!allowed.has(key)) continue;
    if (found.has(key)) throw mcpError('Duplicate MCP catalog entry');
    found.set(key, entry);
  }
  return found;
}

function toolOutput(result, token) {
  if (result.isError) return { isError: true, content: [{ type: 'text', text: 'MCP tool failed' }] };
  const content = result.content.filter((item) => item.type === 'text').map(({ type, text }) => ({ type, text }));
  if (!content.length && result.structuredContent === undefined) throw mcpError('MCP tool returned no supported content');
  return boundedJSON({ content, structuredContent: result.structuredContent }, token);
}

export async function prepareMCPAsset(definition, ref, { signal, deadline, workspace, profile, personal }) {
  const lifetime = new AbortController();
  let connection, closePromise;
  const client = new Client({ name: 'soha-dsh', version: '1.0.0' }, {
    versionNegotiation: { mode: 'legacy' }, enforceStrictCapabilities: true, listMaxPages: 4,
  });
  const close = () => closePromise ??= Promise.resolve().then(async () => {
    lifetime.abort();
    await connection?.close(client);
  }).catch(() => {
    closePromise = undefined;
    throw Object.assign(mcpError('MCP cleanup unconfirmed'), { cleanupUnknown: true, retryCleanup: close });
  });
  try {
    if (ref.kind !== 'mcp' || !/^[a-z0-9][a-z0-9-]{0,47}$/.test(ref.id) ||
        ref.id !== definition.id || ref.version !== definition.version || !Number.isFinite(deadline) || deadline <= Date.now())
      throw mcpError('Invalid MCP asset reference');
    const personalDocker = definition.transport === 'stdio' && definition.executionPosition === 'personal-docker';
    const allowedTools = exactSet(definition.allowedTools, (name) => typeof name === 'string' &&
      (personalDocker ? /^[A-Za-z0-9_]{1,128}$/.test(name) && !['list_resources', 'read_resource'].includes(name)
        : /^[^\s\p{C}]{1,128}$/u.test(name)));
    const allowedURIs = exactSet(definition.allowedResourceURIs, (uri) => typeof uri === 'string' &&
      /^[a-z][a-z0-9+.-]*:[^\s\p{C}]{1,1024}$/iu.test(uri));
    if (!allowedTools.size && !allowedURIs.size) throw mcpError('MCP asset has no grants');

    const runSignal = AbortSignal.any([signal, lifetime.signal, AbortSignal.timeout(Math.max(1, deadline - Date.now()))]);
    const prepareSignal = AbortSignal.any([runSignal, AbortSignal.timeout(operationMs)]);
    runSignal.throwIfAborted();
    if (definition.transport === 'http') connection = await httpTransport(definition, runSignal);
    else if (definition.transport === 'stdio' && definition.executionPosition === 'personal-docker')
      connection = personalStdioTransport(definition, { profile, personal, runSignal, prepareSignal });
    else if (definition.transport === 'stdio') connection = await stdioTransport(definition, { profile, workspace });
    else throw mcpError('Invalid MCP transport');
    const { transport, token, checkCredential } = connection;
    runSignal.throwIfAborted();
    // Abort must close the entire lease even if the server ignores request cancellation.
    const closeOnAbort = () => { close().catch(() => {}); };
    runSignal.addEventListener('abort', closeOnAbort, { once: true });
    prepareSignal.throwIfAborted();
    prepareSignal.addEventListener('abort', closeOnAbort, { once: true });
    await client.connect(transport, { signal: prepareSignal, timeout: operationMs });
    connection.initialized?.();

    const listTools = async (options) => selected((await client.listTools(undefined, options)).tools, allowedTools, 'name');
    const listResources = async (options) => selected((await client.listResources(undefined, options)).resources, allowedURIs, 'uri');
    const options = (signal) => ({ signal, timeout: operationMs, cacheMode: 'bypass' });
    const assertLive = (signal) => {
      signal.throwIfAborted();
      if (Date.now() >= deadline) throw mcpError('MCP operation expired');
    };
    const operationFailure = async (signal, dispatched = false) => {
      const failure = mcpError('MCP operation failed');
      if (!dispatched) failure.definiteFailure = true;
      if (signal.aborted) {
        try { await close(); } catch ({ cleanupUnknown, retryCleanup }) {
          Object.assign(failure, { cleanupUnknown, retryCleanup });
        }
      }
      throw failure;
    };
    const managed = (validate) => {
      const preflight = async (input, signal) => {
        const phaseSignal = () => AbortSignal.any([runSignal, signal, AbortSignal.timeout(operationMs)]);
        const checkSignal = phaseSignal();
        let target;
        try {
          assertLive(checkSignal);
          await checkCredential?.();
          assertLive(checkSignal);
          target = await validate(boundedJSON(input, token), options(checkSignal));
          assertLive(checkSignal);
        } catch {
          return operationFailure(checkSignal);
        }

        let used = false;
        return async () => {
          const callSignal = phaseSignal();
          if (used) return operationFailure(callSignal);
          used = true;
          let dispatched = false;
          const dispatch = (request) => {
            assertLive(callSignal);
            dispatched = true;
            return request();
          };
          try {
            assertLive(callSignal);
            await checkCredential?.();
            assertLive(callSignal);
            const result = await target(options(callSignal), dispatch);
            assertLive(callSignal);
            return result;
          } catch {
            return operationFailure(callSignal, dispatched);
          }
        };
      };
      return { preflight, async invoke(input, signal) { return (await preflight(input, signal))(); } };
    };

    const prefix = `mcp_${ref.id.replaceAll('-', '_')}_`;
    const tools = [];
    const names = new Set(allowedURIs.size ? [`${prefix}list_resources`, `${prefix}read_resource`] : []);
    if (allowedTools.size) {
      const discovered = await listTools(options(prepareSignal));
      if (discovered.size !== allowedTools.size) throw mcpError('MCP catalog grant unavailable');
      for (const [raw, definition] of discovered) {
        const name = prefix + raw.replace(/[^a-zA-Z0-9_]/g, '_');
        boundedJSON(raw, token);
        if (name.length > 64 || names.has(name) || definition.annotations?.readOnlyHint === false ||
            definition.execution?.taskSupport === 'required') throw mcpError('Invalid MCP tool grant');
        names.add(name);
        const inputSchema = boundedJSON(definition.inputSchema, token);
        assertObjectJsonSchema(inputSchema);
        if (definition.outputSchema) assertObjectJsonSchema(boundedJSON(definition.outputSchema, token));
        const description = boundedJSON((definition.description ?? '').slice(0, 512), token);
        let drifted = false;
        tools.push({ name, description, inputSchema, ...managed(async (input, options) => {
          if (drifted || validateJsonSchemaValue(inputSchema, input).length) throw mcpError('MCP tool grant rejected');
          const current = (await listTools(options)).get(raw);
          drifted ||= !isDeepStrictEqual(current, definition);
          if (drifted) throw mcpError('MCP tool catalog changed');
          return async (options, dispatch) => {
            if (drifted) throw mcpError('MCP tool catalog changed');
            const result = await dispatch(() => client.callTool({ name: raw, arguments: input }, { ...options, toolDefinition: definition }));
            return toolOutput(result, token);
          };
        }) });
      }
    }
    if (allowedURIs.size) {
      if ((await listResources(options(prepareSignal))).size !== allowedURIs.size)
        throw mcpError('MCP catalog grant unavailable');
      const uris = boundedJSON([...allowedURIs], token);
      const inputSchema = { type: 'object', properties: {}, additionalProperties: false };
      tools.push({ name: `${prefix}list_resources`, description: 'List approved MCP resource URIs.', inputSchema,
        ...managed((input) => {
          if (!isPlainJsonRecord(input) || Object.keys(input).length) throw mcpError('Invalid MCP resource input');
          return async (options, dispatch) => {
            const resources = [...(await dispatch(() => listResources(options))).keys()].map((uri) => ({ uri }));
            return boundedJSON({ resources }, token);
          };
        }) });
      tools.push({ name: `${prefix}read_resource`, description: 'Read an approved MCP resource.',
        inputSchema: { ...inputSchema, properties: { uri: { type: 'string', enum: uris } }, required: ['uri'] },
        ...managed(async (input, options) => {
          if (!isPlainJsonRecord(input) || Object.keys(input).length !== 1 || !allowedURIs.has(input.uri))
            throw mcpError('MCP resource grant rejected');
          const { uri } = input;
          if (!(await listResources(options)).has(uri)) throw mcpError('MCP resource grant unavailable');
          return async (options, dispatch) => {
            const result = await dispatch(() => client.readResource({ uri }, options));
            if (!result.contents.length || !result.contents.every((item) => item.uri === uri && typeof item.text === 'string'))
              throw mcpError('MCP resource grant rejected');
            const contents = result.contents.map(({ uri, mimeType, text }) => ({ uri, mimeType, text }));
            return boundedJSON({ contents }, token);
          };
        }) });
    }
    prepareSignal.throwIfAborted();
    prepareSignal.removeEventListener('abort', closeOnAbort);
    const capabilities = [];
    if (allowedTools.size) capabilities.push('tools');
    if (allowedURIs.size) capabilities.push('resources');
    return { tools, close, capabilities };
  } catch {
    await close();
    throw mcpError('MCP asset unavailable');
  }
}
