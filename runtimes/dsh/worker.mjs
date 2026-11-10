import { createServer } from 'node:http';
import { randomUUID } from 'node:crypto';
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';
import { HarnessClient } from '@deepseek-ai/dsh-sdk-client';
import { boundedJSON, compileTool, fail, readJSON, sha, stable } from './protocol.mjs';
import { prepareMCPAsset } from './mcp-assets.mjs';
import { prepareSkillAsset } from './skill-assets.mjs';
import { toolOutcomeHeader, toolOutcomeMessage } from './bridge-plugin.mjs';
const here = dirname(fileURLToPath(import.meta.url));
export const builtinHooks = Object.freeze(['tool-audit', 'tool-limit']);
// This gate owns dispatch order; SDK scheduling is not an authorization boundary.
export function createOrderedToolBridge(bridge, signal) {
  let entries = [],
    cursor = 0,
    closed;
  const used = new Set();
  const stop = (error = fail(409)) => {
    if (closed) return;
    closed = error;
    for (const entry of entries) entry.reject?.(error);
  };
  const assertIdle = () => {
    if (closed) throw closed;
    if (cursor !== entries.length) throw fail(409);
  };
  const drain = () => {
    const entry = entries[cursor];
    if (closed || !entry?.request || entry.started) return;
    entry.started = true;
    void (async () => {
      try {
        const result = await bridge(entry.request, entry.signal ?? signal);
        if (closed) throw closed;
        if (result.status !== 'completed') {
          if (Object.hasOwn(toolOutcomeMessage, result.status))
            entry.reject(Object.assign(fail(409), { toolOutcome: result.status }));
          stop();
          return;
        }
        cursor++;
        entry.resolve(result);
        drain();
      } catch (error) {
        stop(error);
      }
    })();
  };
  const aborted = () => stop(fail(409));
  signal.addEventListener('abort', aborted, { once: true });
  if (signal.aborted) aborted();
  return {
    assertIdle,
    freezeRound(calls) {
      assertIdle();
      const next = [];
      for (const call of calls) {
        if (
          typeof call.callId !== 'string' ||
          !/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(call.callId) ||
          used.has(call.callId) ||
          !call.alias
        ) {
          stop(fail(403));
          throw closed;
        }
        used.add(call.callId);
        next.push({ ...call });
      }
      entries = next;
      cursor = 0;
    },
    invoke(request, callSignal) {
      if (closed) return Promise.reject(closed);
      const entry = entries.find((e) => e.callId === request.callId);
      if (!entry || entry.alias !== request.alias || entry.request) {
        stop(fail(403));
        return Promise.reject(closed);
      }
      return new Promise((resolve, reject) => {
        Object.assign(entry, { request, signal: callSignal, resolve, reject });
        drain();
      });
    },
    close() {
      signal.removeEventListener('abort', aborted);
      stop();
    },
  };
}
export async function executeDsh({
  config, start, tools: platformTools, deadline, signal: serviceSignal, setClient, bridge,
  confirmAssembly, auditDir, onHookOutcome, onDelta,
}) {
  const home = await mkdtemp(join(config.dataDir, 'run-')),
    key = randomUUID();
  const hooks = (start.assembly?.assets ?? [])
    .filter(({ kind, id, version }) => kind === 'hook' && version === '1' && builtinHooks.includes(id))
    .map(({ id }) => id);
  const registration = Promise.withResolvers();
  const nativeRegistration = Promise.withResolvers();
  const tools = [...platformTools],
    nativeTools = [],
    connections = new Set(),
    preparedAssets = new Map(),
    assetFailures = new Map();
  const personalConnections = new WeakSet();
  let pendingCleanup, pendingCleanupPersonal;
  const assetKey = ({ kind, id, version }) => `${kind}:${id}:${version}`;
  let registrationOpen = hooks.length > 0,
    nativeRegistrationOpen = false,
    auditOpen = false,
    activated = !start.assembly;
  const lifetime = new AbortController(),
    signal = lifetime.signal;
  const assertLive = () => {
    if (signal.aborted || serviceSignal.aborted || Date.now() >= deadline) throw fail(409);
  };
  let client,
    output = '',
    bound = false,
    messageId,
    turnEnd,
    streamBytes = 0,
    requests = 0,
    outputTokens = 0;
  const modelCalls = [];
  const toolQueue = createOrderedToolBridge(bridge, signal);
  const pendingEvents = [],
    controllers = new Set();
  const abort = () => {
    lifetime.abort();
    toolQueue.close();
    for (const c of controllers) c.abort();
    void client?.close().catch(() => {});
  };
  serviceSignal.addEventListener('abort', abort, { once: true });
  const timer = setTimeout(abort, Math.max(1, deadline - Date.now()));
  const proxy = createServer(async (req, res) => {
    const controller = new AbortController();
    controllers.add(controller);
    let streamComplete = false;
    let call;
    res.on('close', () => {
      if (!streamComplete) controller.abort();
    });
    try {
      if (req.headers.authorization !== `Bearer ${key}`) throw fail(403);
      if (req.method === 'POST' && req.url === '/assembly/audit' && auditOpen) {
        const data = await readJSON(req, 1024);
        const { alias, callId, outcome } = data ?? {};
        const tool = tools.find((candidate) => candidate.alias === alias);
        if (!auditOpen || !tool || typeof callId !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(callId) ||
          !['completed', 'failed', 'denied', 'canceled', 'unknown'].includes(outcome) || Object.keys(data).length !== 3)
          throw fail(403);
        await onHookOutcome({ callId, toolName: tool.name, outcome });
        res.writeHead(204);
        res.end();
        return;
      }
      assertLive();
      if (req.method === 'POST' && req.url === '/assembly/registered' && registrationOpen) {
        const loaded = await readJSON(req, 4096);
        assertLive();
        if (
          !registrationOpen ||
          !Array.isArray(loaded) ||
          new Set(loaded).size !== loaded.length ||
          loaded.some((id) => !hooks.includes(id))
        )
          throw fail(403);
        registrationOpen = false;
        registration.resolve(loaded);
        res.writeHead(204);
        res.end();
        return;
      }
      if (req.method === 'POST' && req.url === '/assembly/tools' && nativeRegistrationOpen) {
        const aliases = await readJSON(req, 4096);
        assertLive();
        if (!nativeRegistrationOpen || stable(aliases) !== stable(tools.map((tool) => tool.alias)))
          throw fail(403);
        nativeRegistrationOpen = false;
        nativeRegistration.resolve(true);
        res.writeHead(204);
        res.end();
        return;
      }
      if (!activated) throw fail(403);
      if (req.method === 'POST' && req.url === '/tool') {
        const request = await readJSON(req);
        assertLive();
        const callSignal = AbortSignal.any([controller.signal, signal]);
        const result = await (config.modelAuthMode === 'core'
          ? toolQueue.invoke(request, callSignal)
          : bridge(request, callSignal));
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(JSON.stringify(result));
        return;
      }
      if (req.method !== 'POST' || req.url !== '/v1/chat/completions' || ++requests > 16)
        throw fail(403);
      call = { complete: false };
      call.done = new Promise((resolve) => {
        call.resolve = resolve;
      });
      modelCalls.push(call);
      const body = await readJSON(req);
      assertLive();
      if (body.model !== start.model || body.stream !== true || outputTokens >= start.maxOutputTokens)
        throw fail(403);
      const expected = new Map(tools.map((t) => [t.alias, t]));
      if (
        (body.tools || []).length !== tools.length ||
        new Set((body.tools || []).map((t) => t.function?.name)).size !== tools.length ||
        (body.tools || []).some((t) => !expected.has(t.function?.name))
      )
        throw fail(403);
      let outputLimit = start.maxOutputTokens - outputTokens;
      for (const field of ['max_completion_tokens', 'max_tokens']) {
        if (!Object.hasOwn(body, field)) continue;
        if (!Number.isSafeInteger(body[field]) || body[field] < 1) throw fail(403);
        outputLimit = Math.min(outputLimit, body[field]);
      }
      body.max_completion_tokens = outputLimit;
      delete body.max_tokens;
      for (const definition of body.tools || []) {
        const source = expected.get(definition.function.name);
        definition.function.parameters = source.inputSchema;
        definition.function.description = source.description;
      }
      const coreMode = config.modelAuthMode === 'core';
      if (coreMode) toolQueue.assertIdle();
      const originalNames = new Map(tools.map((t) => [t.alias, t.name]));
      const aliasNames = new Map(tools.map((t) => [t.name, t.alias]));
      const mapName = (fn, names) => {
        if (!fn || !Object.hasOwn(fn, 'name')) return;
        const mapped = names.get(fn.name);
        if (!mapped) throw fail(403);
        fn.name = mapped;
      };
      if (coreMode) {
        for (const tool of body.tools || []) mapName(tool.function, originalNames);
        for (const message of body.messages || [])
          for (const call of message.tool_calls || []) mapName(call.function, originalNames);
        if (typeof body.tool_choice === 'object') mapName(body.tool_choice.function, originalNames);
      }
      const reserved = body.max_completion_tokens;
      outputTokens += reserved;
      let reportedUsage;
      const requestBody = JSON.stringify(body);
      if (Buffer.byteLength(requestBody) > 1048576) throw fail(413);
      const headers = {
        authorization: `Bearer ${coreMode ? config.token : config.pat}`,
        'content-type': 'application/json',
        ...(coreMode
          ? {
              'X-Soha-Tool-Grant': start.toolGrant,
              'X-Soha-Run-ID': start.runId,
              'X-Soha-Run-Attempt': String(start.attempt),
              'X-Soha-Run-Fence': String(start.fence),
              'X-Soha-Context-Revision': start.contextRevision,
              'X-Soha-Tool-Catalog-Revision': start.toolCatalogRevision,
              'X-Soha-Model-Call-ID': randomUUID(),
              'X-Soha-Input-Hash': sha(requestBody),
            }
          : {}),
      };
      const upstream = await fetch(`${config.gatewayURL}/chat/completions`, {
        method: 'POST',
        redirect: 'error',
        headers,
        body: requestBody,
        signal: AbortSignal.any([controller.signal, signal, AbortSignal.timeout(config.maxRunMs)]),
      });
      if (!upstream.ok) throw fail(503);
      res.writeHead(200, { 'content-type': 'text/event-stream' });
      let frames = '',
        sawDone = false,
        sawFinish = false,
        invalidUsage = false;
      const decoder = new TextDecoder('utf-8', { fatal: true });
      const roundCalls = new Map();
      const consume = async (text) => {
        frames += text;
        if (Buffer.byteLength(frames) > 1048576) throw fail(413);
        const split = frames.split(/\r?\n\r?\n/);
        frames = split.pop();
        let forwarded = '';
        for (const frame of split) {
          const lines = frame.split('\n');
          for (let index = 0; index < lines.length; index++) {
            const line = lines[index];
            if (!line.startsWith('data:')) continue;
            const data = line.slice(5).trim();
            if (data === '[DONE]') {
              sawDone = true;
              continue;
            }
            if (!data || sawDone) throw fail();
            const e = JSON.parse(data);
            if (e.usage != null) {
              const u = e.usage;
              const valid =
                ['prompt_tokens', 'completion_tokens', 'total_tokens'].every(
                  (k) => Number.isSafeInteger(u[k]) && u[k] >= 0,
                ) &&
                Number.isSafeInteger(u.prompt_tokens + u.completion_tokens) &&
                u.total_tokens === u.prompt_tokens + u.completion_tokens;
              const usage = valid
                ? {
                    inputTokens: u.prompt_tokens,
                    outputTokens: u.completion_tokens,
                    totalTokens: u.total_tokens,
                  }
                : undefined;
              if (!usage || (call.usage && JSON.stringify(call.usage) !== JSON.stringify(usage)))
                invalidUsage = true;
              if (usage && !invalidUsage) call.usage = usage;
              if (Number.isSafeInteger(u.completion_tokens) && u.completion_tokens >= 0) {
                reportedUsage = u.completion_tokens;
                if (reportedUsage > reserved) throw fail(413);
              }
            }
            for (const choice of e.choices || []) {
              if (coreMode) {
                for (const toolCall of choice.delta?.tool_calls || []) {
                  mapName(toolCall.function, aliasNames);
                  if (
                    choice.index !== 0 ||
                    !Number.isSafeInteger(toolCall.index) ||
                    toolCall.index < 0 ||
                    toolCall.index >= 64
                  )
                    throw fail();
                  const previous = roundCalls.get(toolCall.index) || {};
                  if (
                    (toolCall.id && previous.callId && toolCall.id !== previous.callId) ||
                    (toolCall.function?.name &&
                      previous.alias &&
                      toolCall.function.name !== previous.alias)
                  )
                    throw fail();
                  roundCalls.set(toolCall.index, {
                    callId: toolCall.id || previous.callId,
                    alias: toolCall.function?.name || previous.alias,
                  });
                }
              }
              if (choice.finish_reason != null) sawFinish = true;
              if (typeof choice.delta?.content === 'string') await onDelta(choice.delta.content);
            }
            if (coreMode) lines[index] = 'data: ' + JSON.stringify(e);
          }
          forwarded += lines.join('\n') + '\n\n';
        }
        // A downstream reader may close immediately upon DONE. That is a
        // complete stream, not an instruction to abort the upstream EOF read.
        streamComplete = sawDone && sawFinish && !frames.trim();
        if (coreMode && streamComplete && !call.roundFrozen) {
          const ordered = [...roundCalls.entries()].sort(([a], [b]) => a - b);
          if (ordered.some(([index], position) => index !== position)) throw fail();
          toolQueue.freezeRound(ordered.map(([, entry]) => entry));
          call.roundFrozen = true;
        }
        return forwarded;
      };
      for await (const chunk of upstream.body) {
        streamBytes += chunk.length;
        if (streamBytes > 4194304) throw fail(413);
        const forwarded = await consume(decoder.decode(chunk, { stream: true }));
        const wireChunk = coreMode ? forwarded : chunk;
        if (wireChunk.length && !res.write(wireChunk))
          await new Promise((resolve, reject) => {
            res.once('drain', resolve);
            res.once('close', () => reject(fail()));
          });
      }
      await consume(decoder.decode());
      if (!streamComplete) throw fail();
      call.complete = true;
      if (invalidUsage) delete call.usage;
      if (reportedUsage !== undefined) outputTokens -= reserved - reportedUsage;
      res.end();
    } catch (error) {
      if (!res.headersSent) {
        if (req.url === '/tool' && Object.hasOwn(toolOutcomeMessage, error?.toolOutcome))
          res.writeHead(409, { [toolOutcomeHeader]: error.toolOutcome });
        else res.writeHead(503);
        res.end('{"error":"runtime_upstream_rejected"}');
      } else res.destroy();
    } finally {
      call?.resolve();
      controllers.delete(controller);
    }
  });
  const closeAssets = async () => {
    const leases = [...connections];
    const closed = await Promise.allSettled(leases.map(async (connection) => {
      await connection.close();
      connections.delete(connection);
    }));
    const unknown = closed.flatMap((result, index) => result.status === 'rejected' ? [personalConnections.has(leases[index])] : []);
    if (pendingCleanup) {
      try {
        await pendingCleanup();
        pendingCleanup = undefined;
      } catch {
        unknown.push(pendingCleanupPersonal);
      }
    }
    if (unknown.length) throw Object.assign(fail(503), { personalOnly: unknown.every(Boolean) });
  };
  try {
    assertLive();
    await new Promise((resolve) => proxy.listen(0, '127.0.0.1', resolve));
    const workspace = join(home, 'workspace');
    await mkdir(workspace, { mode: 0o700 });
    if (start.assembly && config.modelAuthMode === 'core') {
      const usedNames = new Set(tools.map((tool) => tool.name));
      const mcpRefs = [];
      let personalStdio = false;
      const requiredPersonal = start.assembly.assets.filter((ref) => {
        if (ref.kind !== 'mcp' || !ref.required) return false;
        const definition = config.pluginAssets.get(assetKey(ref));
        return definition?.transport === 'stdio' && definition.executionPosition === 'personal-docker';
      });
      if (requiredPersonal.length > 1) throw fail(403);
      const personal = {
        callbackURL: config.callbackURL,
        serviceToken: config.token,
        runId: start.runId,
        attempt: start.attempt,
        fence: start.fence,
        toolGrant: start.toolGrant,
      };
      for (const kind of ['mcp', 'skill']) {
        for (const ref of start.assembly.assets.filter((asset) => asset.kind === kind)) {
          assertLive();
          const definition = config.pluginAssets.get(assetKey(ref));
          if (!definition || (definition.transport === 'stdio' && start.profile !== 'heya'))
            continue;
          const isPersonal = kind === 'mcp' && definition.transport === 'stdio' && definition.executionPosition === 'personal-docker';
          if (isPersonal && (personalStdio || (requiredPersonal.length && ref !== requiredPersonal[0]))) continue;
          const prepare = kind === 'mcp' ? prepareMCPAsset : prepareSkillAsset;
          let connection;
          try {
            connection = await prepare(definition, ref, {
              deadline, signal, workspace, profile: start.profile, assets: mcpRefs, personal,
            });
            if (typeof connection?.close !== 'function') throw fail();
            connections.add(connection);
            if (isPersonal) personalConnections.add(connection);
            assertLive();
            if (!Array.isArray(connection.tools) || !connection.tools.length ||
              connection.tools.length + tools.length > 64)
              throw fail(413);
            const names = new Set(usedNames);
            const prefix = `${kind}_${ref.id.replaceAll('-', '_')}_`;
            const staged = connection.tools.map((tool, index) => {
              const { name, description, inputSchema, invoke, preflight } = tool;
              if (
                typeof name !== 'string' ||
                !name.startsWith(prefix) ||
                !/^[A-Za-z0-9_-]{1,64}$/.test(name) ||
                names.has(name) ||
                typeof description !== 'string' ||
                [...description].length > 512 ||
                inputSchema?.type !== 'object' ||
                typeof invoke !== 'function' ||
                (preflight !== undefined && typeof preflight !== 'function')
              )
                throw fail(403);
              names.add(name);
              const descriptor = { kind, assetId: ref.id, version: ref.version, name, description, inputSchema };
              const checkInput = compileTool(tool);
              return {
                ...descriptor,
                descriptor,
                invoke: invoke.bind(tool),
                ...(preflight ? { preflight: preflight.bind(tool) } : {}),
                alias: `soha_tool_${tools.length + index}`,
                checkInput,
                sdkInputSchema: checkInput.sdkInputSchema,
              };
            });
            boundedJSON([...nativeTools, ...staged].map((tool) => tool.descriptor));
            for (const tool of staged) usedNames.add(tool.name);
            tools.push(...staged);
            nativeTools.push(...staged);
            preparedAssets.set(assetKey(ref), staged.map((tool) => tool.name));
            if (kind === 'mcp') mcpRefs.push(ref);
            if (isPersonal) personalStdio = true;
          } catch (error) {
            if (connections.has(connection)) {
              await connection.close();
              connections.delete(connection);
            }
            if (error?.cleanupUnknown) {
              pendingCleanup = error.retryCleanup ?? (() => { throw fail(503); });
              pendingCleanupPersonal = isPersonal;
              throw fail(503);
            }
            if (kind === 'skill' && error?.code === 'skill_dependency_missing')
              assetFailures.set(assetKey(ref), 'dependency_unavailable');
            assertLive();
          }
        }
      }
    }
    nativeRegistrationOpen = nativeTools.length > 0;
    const patch = join(home, 'runtime.patch.json'),
      base = `http://127.0.0.1:${proxy.address().port}`;
    await writeFile(
      patch,
      JSON.stringify([
        { id: 'llm-deepseek', disabled: true },
        { id: 'deepseek-llm-api-extensions', disabled: true },
        {
          insert: [
            {
              id: 'llm-pi-ai',
              name: '@deepseek-ai/dsh-llm-pi-ai',
              config: {
                providers: {
                  'soha-gateway': {
                    apiKeyEnv: 'SOHA_DSH_SYNTHETIC_KEY',
                    api: 'openai-completions',
                    baseURL: `${base}/v1`,
                    models: [
                      { id: start.model, contextWindow: 128000, maxTokens: start.maxOutputTokens },
                    ],
                    compat: {
                      supportsUsageInStreaming: true,
                      maxTokensField: 'max_completion_tokens',
                    },
                    retryPolicy: { mode: 'normal', maxRetries: 0 },
                  },
                },
              },
            },
            {
              id: 'soha-tool-bridge',
              name: join(here, 'bridge-plugin.mjs'),
              config: {
                tools: tools.map(({ alias, description, inputSchema, sdkInputSchema }) => ({
                  alias, description, inputSchema, sdkInputSchema,
                })),
                url: `${base}/tool`,
                ...(nativeRegistrationOpen ? { registrationURL: `${base}/assembly/tools` } : {}),
              },
            },
            ...(hooks.length
              ? [{
                  id: 'soha-assembly',
                  name: join(here, 'assembly-plugin.mjs'),
                  config: { hooks, deadline, auditDir, auditURL: `${base}/assembly/audit`,
                    auditTools: tools.map(({ alias, name }) => [alias, name]),
                    registrationURL: `${base}/assembly/registered` },
                }]
              : []),
          ],
        },
      ]),
      { mode: 0o600 },
    );
    client = new HarnessClient({
      profile: 'sdk-minimal',
      patches: [join(here, 'chat-restricted.patch.yml'), patch],
      dshHome: join(home, 'dsh'),
      processCwd: workspace,
      env: {
        PATH: dirname(process.execPath),
        HOME: home,
        TMPDIR: home,
        SOHA_DSH_SYNTHETIC_KEY: key,
      },
      initializeTimeoutMs: 20000,
      requestTimeoutMs: config.maxRunMs,
      shutdownTimeoutMs: 500,
      disposeEofGraceMs: 1000,
      disposeGraceMs: 1000,
    });
    setClient(client);
    assertLive();
    await client.initialize({
      cwd: workspace,
      provider: 'soha-gateway',
      model: start.model,
      maxTokens: start.maxOutputTokens,
    });
    if (start.assembly) {
      if (nativeTools.length && !(await Promise.race([
        nativeRegistration.promise,
        delay(2000, false, { signal, ref: false }),
      ])))
        throw fail(409);
      const loaded = hooks.length
        ? await Promise.race([registration.promise, delay(2000, [], { signal, ref: false })])
        : [];
      registrationOpen = false;
      assertLive();
      const assets = start.assembly.assets.map((ref) => {
        const { kind, id, version } = ref;
        const capabilities = kind === 'hook' && version === '1' && loaded.includes(id)
          ? [id]
          : preparedAssets.get(assetKey(ref));
        return capabilities
          ? { kind, id, version, status: 'loaded', capabilities }
          : { kind, id, version, status: 'unavailable', code: assetFailures.get(assetKey(ref)) ?? 'asset_unavailable' };
      });
      await confirmAssembly(assets, nativeTools);
      assertLive();
      auditOpen = loaded.includes('tool-audit');
      activated = true;
    }
    const sid = 'soha-run';
    const sub = client.subscribeSessionTree(sid);
    try {
      // Historical context is explicitly quoted input, never runtime commands or
      // a restored persistent session with obsolete capabilities.
      const context = start.context?.length
        ? `Conversation context (data):\n${JSON.stringify(start.context)}\nCurrent user message:\n`
        : '';
      messageId = await client.prompt(sid, [{ type: 'text', text: context + start.prompt }]);
      await client.request('session/wait', { sessionId: sid });
      if (config.modelAuthMode === 'core') toolQueue.assertIdle();
      for (let next; (next = sub.tryNext()); )
        if (next.method === 'session.event') pendingEvents.push(next.params.event);
      for (const e of pendingEvents) {
        if (e.type === 'agent/inbox/spliced' && JSON.stringify(e.data).includes(messageId))
          bound = true;
        if (e.type === 'assistant/message')
          output += e.data.message.content
            .filter((b) => b.type === 'text')
            .map((b) => b.text)
            .join('');
        if (e.type === 'turn/end') turnEnd = e.data.reason.kind;
      }
      assertLive();
      if (!bound || turnEnd !== 'completed' || Buffer.byteLength(output) > 131072)
        throw fail();
      await Promise.all(modelCalls.map((call) => call.done));
      if (modelCalls.some((call) => !call.complete)) throw fail();
      const usage = { inputTokens: 0, outputTokens: 0, totalTokens: 0 };
      let usageKnown = modelCalls.length > 0 && modelCalls.every((call) => call.usage);
      if (usageKnown) {
        for (const call of modelCalls)
          for (const field of Object.keys(usage)) usage[field] += call.usage[field];
        usageKnown = Object.values(usage).every(Number.isSafeInteger);
      }
      return { assistantText: output, usageKnown, ...(usageKnown ? { usage } : {}) };
    } finally {
      sub.close();
    }
  } finally {
    activated = false;
    registrationOpen = false;
    nativeRegistrationOpen = false;
    clearTimeout(timer);
    lifetime.abort();
    toolQueue.close();
    serviceSignal.removeEventListener('abort', abort);
    for (const c of controllers) c.abort();
    const [child, assets] = await Promise.allSettled([client?.close(), closeAssets()]);
    auditOpen = false;
    proxy.closeAllConnections();
    if (proxy.listening) await new Promise((resolve) => proxy.close(resolve));
    if (child.status === 'fulfilled' && assets.status === 'rejected' && assets.reason.personalOnly) {
      // Agent owns the unresolved lease fence; other users' Runs can continue.
      await rm(home, { recursive: true, force: true });
      throw Object.assign(fail(503), { exitUnknown: true, scopedExitUnknown: true });
    }
    if (child.status === 'rejected' || assets.status === 'rejected') {
      throw Object.assign(fail(503), {
        exitUnknown: true,
        retryCleanup: async () => {
          await closeAssets();
          await rm(home, { recursive: true, force: true });
        },
      });
    }
    await rm(home, { recursive: true, force: true });
  }
}
