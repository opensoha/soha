import { createServer, validateHeaderValue } from 'node:http';
import { timingSafeEqual, randomUUID } from 'node:crypto';
import { constants, createReadStream } from 'node:fs';
import {
  mkdir,
  readFile,
  writeFile,
  rename,
  readdir,
  chmod,
  open,
  rm,
  rmdir,
  mkdtemp,
} from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  VERSION,
  sha,
  stable,
  fail,
  validators,
  compileTool,
  boundedJSON,
  readJSON,
} from './protocol.mjs';
import { builtinHooks, executeDsh } from './worker.mjs';
import { prepareMCPAsset } from './mcp-assets.mjs';
const here = dirname(fileURLToPath(import.meta.url));
const assetID = /^[a-z0-9][a-z0-9-]{0,47}$/;
const assetVersion = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const probeMs = 5_000;
const terminal = (s) => ['completed', 'failed', 'canceled', 'unknown'].includes(s);
const equal = (a, b) => {
  if (typeof a !== 'string' || typeof b !== 'string') return false;
  const x = Buffer.from(a),
    y = Buffer.from(b);
  return x.length === y.length && timingSafeEqual(x, y);
};
function url(value) {
  const parsed = new URL(value);
  if (
    !['http:', 'https:'].includes(parsed.protocol) ||
    parsed.username ||
    parsed.password ||
    parsed.search ||
    parsed.hash
  )
    throw fail();
  return parsed.href.replace(/\/$/, '');
}
export async function configFromEnv(env = process.env) {
  const secret = async (name) => {
    const value = (await readFile(env[name], 'utf8')).trim();
    if (value.length < 32 || /[\r\n]/.test(value)) throw fail();
    return value;
  };
  const integer = (name, value, max) => {
    const n = Number(env[name] ?? value);
    if (!Number.isSafeInteger(n) || n < 1 || n > max) throw fail();
    return n;
  };
  const modelAuthMode = env.SOHA_DSH_MODEL_AUTH_MODE || 'core';
  if (!['core', 'gateway'].includes(modelAuthMode)) throw fail();
  return {
    modelAuthMode,
    host: env.SOHA_DSH_HOST || '127.0.0.1',
    port: Number(env.SOHA_DSH_PORT || 8097),
    token: await secret('SOHA_DSH_SERVICE_TOKEN_FILE'),
    ...(modelAuthMode === 'gateway' ? { pat: await secret('SOHA_DSH_GATEWAY_PAT_FILE') } : {}),
    gatewayURL: url(env.SOHA_DSH_GATEWAY_URL),
    callbackURL: url(env.SOHA_DSH_CORE_CALLBACK_URL),
    ...(env.SOHA_DSH_PLUGIN_ASSETS_FILE
      ? { pluginAssets: await readJSON(createReadStream(env.SOHA_DSH_PLUGIN_ASSETS_FILE), 262144) }
      : {}),
    ...(modelAuthMode === 'gateway'
      ? { catalog: JSON.parse(await readFile(env.SOHA_DSH_TOOL_CATALOG_FILE, 'utf8')) }
      : {}),
    models: (env.SOHA_DSH_MODELS || '').split(',').filter(Boolean),
    dataDir: resolve(env.SOHA_DSH_DATA_DIR || '/var/lib/soha-dsh'),
    schemaPath:
      env.SOHA_DSH_SCHEMA_PATH ||
      resolve(here, '../../../contracts/agent/dsh-runtime.schema.json'),
    capacity: integer('SOHA_DSH_MAX_CONCURRENCY', 4, 128),
    maxRunMs: integer('SOHA_DSH_MAX_RUN_MS', 120000, 3600000),
    maxTokens: integer('SOHA_DSH_MAX_OUTPUT_TOKENS', 4096, 131072),
  };
}
export function effectiveRunDeadline(startDeadline, maxRunMs, acceptedAt = Date.now()) {
  return Math.min(Date.parse(startDeadline), acceptedAt + maxRunMs);
}
function assetRegistry(manifest) {
  const assets = new Map();
  if (!manifest || typeof manifest !== 'object' || Array.isArray(manifest)) throw fail();
  for (const [kind, definitions] of Object.entries(manifest)) {
    if (!['mcp', 'skill'].includes(kind) || !Array.isArray(definitions) || definitions.length > 256)
      throw fail();
    for (const definition of definitions) {
      const { id, version } = definition ?? {};
      const key = `${kind}:${id}:${version}`;
      if (
        typeof id !== 'string' ||
        !assetID.test(id) ||
        typeof version !== 'string' ||
        !assetVersion.test(version) ||
        assets.has(key)
      )
        throw fail();
      assets.set(key, definition);
    }
  }
  return assets;
}
function assetInventory(assets) {
  const items = builtinHooks.map((id) => ({ kind: 'hook', id, version: '1' }));
  for (const key of [...assets.keys()].sort()) {
    const [kind, id, version] = key.split(':');
    const item = { kind, id, version };
    const { transport, executionPosition } = assets.get(key);
    if (kind === 'mcp' && ['http', 'stdio'].includes(transport)) item.transport = transport;
    if (item.transport === 'stdio' && typeof executionPosition === 'string' && assetID.test(executionPosition))
      item.executionPosition = executionPosition;
    items.push(item);
  }
  return { items };
}
export async function createRuntime(
  config,
  executor = executeDsh,
  { now = Date.now, timeoutSignal = (ms) => AbortSignal.timeout(ms) } = {},
) {
  process.umask(0o077);
  config = {
    ...config,
    modelAuthMode: config.modelAuthMode ?? 'gateway',
    pluginAssets: assetRegistry(config.pluginAssets === undefined ? {} : config.pluginAssets),
  };
  const inventory = assetInventory(config.pluginAssets);
  if (!['core', 'gateway'].includes(config.modelAuthMode)) throw fail();
  const validate = await validators(config.schemaPath);
  if (!config.models.length) throw fail();
  const catalog = new Map();
  if (config.modelAuthMode === 'gateway') {
    if (
      !config.catalog?.revision ||
      !Array.isArray(config.catalog.tools) ||
      config.catalog.tools.length > 64
    )
      throw fail();
    for (const tool of config.catalog.tools) {
      if (
        catalog.has(tool.name) ||
        !/^[A-Za-z][A-Za-z0-9_.-]{0,127}$/.test(tool.name) ||
        typeof tool.readOnly !== 'boolean'
      )
        throw fail();
      catalog.set(tool.name, { tool, check: compileTool(tool) });
    }
  }
  await mkdir(config.dataDir, { recursive: true, mode: 0o700 });
  await chmod(config.dataDir, 0o700);
  // A single writer owns this journal directory. An unclean exit deliberately
  // requires operator removal of the lock after proving the prior process dead.
  const lockPath = join(config.dataDir, 'writer.lock');
  try {
    const pid = Number(await readFile(lockPath, 'utf8'));
    if (!Number.isSafeInteger(pid) || pid < 1) throw fail(503);
    try {
      process.kill(pid, 0);
      throw fail(503);
    } catch (e) {
      if (e.code !== 'ESRCH') throw e;
    }
    await rm(lockPath);
  } catch (e) {
    if (e.code !== 'ENOENT') throw e;
  }
  const lock = await open(lockPath, 'wx', 0o600);
  await lock.writeFile(String(process.pid));
  await lock.sync();
  const records = new Map(),
    active = new Map();
  const probes = new Map();
  const stopProbes = new AbortController();
  let ready = true,
    serial = Promise.resolve();
  const exclusive = (fn) => {
    const p = serial.then(fn);
    serial = p.catch(() => {});
    return p;
  };
  const key = (id, attempt) => `${id}/${attempt}`;
  const hookDirectory = (r) => join(config.dataDir, `${sha(`${key(r.runId, r.attempt)}/${r.fence}`)}.hooks`);
  const syncDataDir = async () => {
    const dir = await open(config.dataDir, 'r');
    try {
      await dir.sync();
    } finally {
      await dir.close();
    }
  };
  const persist = async (r) => {
    const path = join(config.dataDir, `${sha(key(r.runId, r.attempt))}.json`),
      tmp = `${path}.tmp`;
    const fd = await open(tmp, 'w', 0o600);
    try {
      await fd.writeFile(JSON.stringify(r));
      await fd.sync();
    } finally {
      await fd.close();
    }
    await rename(tmp, path);
    await syncDataDir();
  };
  const identity = (r) => ({
    schemaVersion: VERSION,
    runId: r.runId,
    attempt: r.attempt,
    fence: r.fence,
  });
  const event = (r, type, data) => {
    if (r.events.length >= (terminal(type) || terminal(r.status) ? 4096 : 4095)) throw fail(413);
    const e = validate('Event', {
      ...identity(r),
      kind: 'event',
      sequence: r.sequence + 1,
      type,
      data,
    });
    r.events.push(e);
    r.sequence = e.sequence;
  };
  const containsSecret = (value, grant) => {
    const raw = JSON.stringify(value);
    return [config.token, config.pat, grant]
      .filter(Boolean)
      .some((s) => raw.includes(s) || raw.includes(JSON.stringify(s).slice(1, -1)));
  };
  const collectHookOutcomes = async (r, grant) => {
    const directory = hookDirectory(r);
    const directories = [directory, `${directory}.retry`];
    const files = (await Promise.all(directories.map(async (dir) => {
      try {
        return (await readdir(dir)).map((file) => ({ file, path: join(dir, file) }));
      } catch (error) {
        if (error.code !== 'ENOENT') console.warn('dsh hook outcome spool unavailable');
        return [];
      }
    }))).flat();
    if (!files.length) return [];
    const seen = new Set(r.events.filter(({ type }) => type === 'hook_observed').map(({ data }) => data.callId));
    const acknowledged = [];
    for (const { file, path } of files.sort((a, b) => a.file.localeCompare(b.file))) {
      const match = /^[0-9]{6}-([a-f0-9]{64})\.json$/.exec(file);
      if (!match) continue;
      try {
        const fd = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
        let data;
        try {
          const info = await fd.stat();
          if (!info.isFile() || info.size > 1024) throw fail(413);
          data = await readJSON(fd.createReadStream({ autoClose: false }), 1024);
        } finally {
          await fd.close();
        }
        if (Object.keys(data).length !== 3 || sha(data.callId) !== match[1] || containsSecret(data, grant)) throw fail(403);
        if (!seen.has(data.callId)) {
          event(r, 'hook_observed', data);
          seen.add(data.callId);
        }
        acknowledged.push(path);
      } catch {
        console.warn('dsh hook outcome awaiting replay');
      }
    }
    return acknowledged;
  };
  const acknowledgeHookOutcomes = async (r, acknowledged) => {
    if (!acknowledged.length) return;
    const directory = hookDirectory(r);
    await Promise.allSettled(acknowledged.map((path) => rm(path, { force: true })));
    await Promise.allSettled([rmdir(directory), rmdir(`${directory}.retry`)]);
  };
  const replayHookOutcomes = (r) => exclusive(async () => {
    const length = r.events.length, sequence = r.sequence;
    const acknowledged = await collectHookOutcomes(r);
    try {
      if (r.sequence !== sequence) await persist(r);
      await acknowledgeHookOutcomes(r, acknowledged);
    } catch {
      r.events.length = length;
      r.sequence = sequence;
      console.warn('dsh hook outcome awaiting replay');
    }
  });
  const snapshot = (r) =>
    validate('Status', {
      ...identity(r),
      kind: 'status',
      submissionId: r.submissionId,
      inputHash: r.inputHash,
      status: r.status,
      sequence: r.sequence,
      usageKnown: r.usageKnown === true,
      ...(r.usageKnown === true ? { usage: r.usage } : {}),
      ...(r.assistantText ? { assistantText: r.assistantText } : {}),
    });
  const transition = (r, status, data = {}, grant) =>
    exclusive(async () => {
      if (terminal(r.status)) return;
      const final = terminal(status);
      const length = r.events.length;
      const previous = { status: r.status, sequence: r.sequence, assistantText: r.assistantText,
        usageKnown: r.usageKnown, usage: r.usage };
      const a = active.get(key(r.runId, r.attempt));
      const acknowledged = final ? await collectHookOutcomes(r, grant) : [];
      try {
        if (final && a?.pendingHookOutcomes.size) {
          const seen = new Set(r.events.filter(({ type }) => type === 'hook_observed').map(({ data }) => data.callId));
          for (const [callId, observation] of a.pendingHookOutcomes) {
            if (r.events.length >= 4095) break;
            if (!seen.has(callId)) event(r, 'hook_observed', observation);
          }
        }
        r.status = status;
        if (final) {
          if (Object.hasOwn(data, 'assistantText')) r.assistantText = data.assistantText;
          r.usageKnown = data.usageKnown === true;
          r.usage = r.usageKnown ? data.usage : undefined;
        }
        event(r, status === 'running' ? 'started' : status, { status, ...data });
        await persist(r);
      } catch (error) {
        Object.assign(r, previous);
        r.events.length = length;
        throw error;
      }
      await acknowledgeHookOutcomes(r, acknowledged);
      if (final && a) {
        a.pendingHookOutcomes.clear();
        a.abort.abort();
      }
    });
  const commitTerminal = async (r, a) => {
    const intent = a?.terminalIntent;
    if (!intent) return;
    try {
      await transition(r, intent.status, intent.data, intent.grant);
    } catch {
      console.warn('dsh terminal result awaiting persistence');
      return;
    }
    a.terminalIntent = undefined;
    if (!a.exitUnknown) active.delete(key(r.runId, r.attempt));
  };
  try {
    for (const file of await readdir(config.dataDir))
      if (/^[a-f0-9]{64}\.json$/.test(file)) {
        const r = JSON.parse(await readFile(join(config.dataDir, file), 'utf8'));
        records.set(key(r.runId, r.attempt), r);
        if (terminal(r.status)) await replayHookOutcomes(r);
        else await transition(r, 'unknown', { errorCode: 'runtime_restarted' });
      }
  } catch (err) {
    await lock.close();
    await rm(lockPath);
    throw err;
  }
  const callback = async (a, start, url, body, signal = a.abort.signal) => {
    const remainingMs = Math.floor(a.deadline - now());
    if (remainingMs < 1) throw fail(503);
    return fetch(url, {
      method: 'POST',
      redirect: 'error',
      headers: {
        authorization: `Bearer ${config.token}`,
        'X-Soha-Tool-Grant': start.toolGrant,
        'content-type': 'application/json',
      },
      body,
      signal: AbortSignal.any([signal, a.abort.signal, timeoutSignal(remainingMs)]),
    });
  };
  const callbackJSON = async (response) => {
    if (!response.ok) throw fail();
    return readJSON(response.body);
  };
  async function confirmAssembly(r, a, start, assets, nativeTools) {
    const { profileId, revision } = start.assembly;
    const binding = {
      ...identity(r),
      profileId,
      revision,
      assets,
      ...(nativeTools.length ? { nativeTools: nativeTools.map((tool) => tool.descriptor) } : {}),
    };
    const report = validate('AssemblyReport', { ...binding, kind: 'assembly_report' });
    if (containsSecret(report, start.toolGrant)) throw fail(403);
    const response = await callbackJSON(
      await callback(a, start, new URL('assembly', config.callbackURL), JSON.stringify(report)),
    );
    const { kind, status, ...confirmed } = validate('AssemblyDecision', response);
    const expected = assets.every((asset) => asset.status === 'loaded') ? 'confirmed' : 'degraded';
    if (
      kind !== 'assembly_decision' ||
      status !== expected ||
      stable(binding) !== stable(confirmed) ||
      assets.some((asset, i) => asset.status !== 'loaded' && start.assembly.assets[i].required) ||
      terminal(r.status) ||
      r.cancelRequested ||
      a.abort.signal.aborted ||
      now() >= a.deadline
    )
      throw fail(409);
    a.nativeTools = new Map(nativeTools.map((tool) => [tool.alias, tool]));
  }
  async function bridge(r, start, tools, call, signal) {
    const a = active.get(key(r.runId, r.attempt));
    if (!a || a.toolUnknown || terminal(r.status) || r.cancelRequested || signal.aborted)
      throw fail(409);
    const native = a.nativeTools?.get(call.alias);
    const tool = native ?? tools.find((t) => t.alias === call.alias);
    if (!tool || !start.toolGrant) throw fail(403);
    boundedJSON(call.input);
    if (!tool.checkInput(call.input)) throw fail(403);
    if (containsSecret(call.input, start.toolGrant)) throw fail(403);
    const inputRaw = JSON.stringify(call.input),
      inputHash = sha(inputRaw);
    if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(call.callId)) throw fail();
    if (a.calls.has(call.callId)) throw fail(409);
    a.calls.add(call.callId);
    await exclusive(async () => {
      if (terminal(r.status) || r.cancelRequested || signal.aborted) throw fail(409);
      r.status = 'waiting_tool';
      event(r, 'tool_started', {
        callId: call.callId,
        toolName: tool.name,
        status: 'waiting_tool',
      });
      await persist(r);
    });
    let dispatched = false;
    let result;
    try {
      const message = validate('ToolCall', {
        ...identity(r),
        kind: 'tool_call',
        callId: call.callId,
        toolCatalogRevision: start.toolCatalogRevision,
        toolName: tool.name,
        inputHash,
        input: call.input,
      });
      // inputRaw is embedded unchanged; the hash covers exactly this byte slice.
      const { input, ...head } = message;
      const raw = JSON.stringify(head).slice(0, -1) + ',"input":' + inputRaw + '}';
      if (native) {
        const assertPluginCallActive = () => {
          if (
            r.cancelRequested || terminal(r.status) || signal.aborted ||
            a.abort.signal.aborted || now() >= a.deadline
          )
            throw fail(409);
        };
        assertPluginCallActive();
        let invoke = () => native.invoke(call.input, signal);
        if (native.preflight) {
          try {
            invoke = await native.preflight(call.input, signal);
          } catch {
            invoke = undefined;
          }
        }
        assertPluginCallActive();
        const authorize = await callback(
          a, start, new URL('plugin-tools/authorize', config.callbackURL), raw, signal,
        );
        if (authorize.status === 200) {
          result = validate('ToolResult', await callbackJSON(authorize));
          if (result.status !== 'completed') throw fail(409);
        } else if (authorize.status === 204) {
          assertPluginCallActive();
          a.pendingTools++;
          dispatched = true;
          let outcome = { status: 'failed', errorCode: 'plugin_tool_failed' };
          if (typeof invoke === 'function') {
            try {
              const output = await invoke();
              if (native.descriptor.kind !== 'mcp' || output?.isError !== true) {
                boundedJSON(output);
                if (containsSecret(output, start.toolGrant)) throw fail(403);
                outcome = { status: 'completed', output };
              }
            } catch (error) {
              if (error?.definiteFailure !== true &&
                !(native.descriptor.kind === 'skill' && !signal.aborted))
                outcome = { status: 'unknown', errorCode: 'plugin_outcome_unknown' };
            }
          }
          const settlement = validate('ToolResult', {
            ...identity(r),
            kind: 'tool_result',
            callId: message.callId,
            inputHash,
            ...outcome,
          });
          const settled = await callback(
            a, start, new URL('plugin-tools/settle', config.callbackURL), JSON.stringify(settlement), signal,
          );
          if (settled.status !== 200) {
            await settled.body?.cancel();
            throw fail(409);
          }
          result = validate('ToolResult', await callbackJSON(settled));
          if (stable(result) !== stable(settlement)) throw fail(409);
        } else {
          await authorize.body?.cancel();
          if (authorize.status < 400 || authorize.status >= 500) throw fail(503);
          result = validate('ToolResult', {
            ...identity(r),
            kind: 'tool_result',
            callId: message.callId,
            inputHash,
            status: 'denied',
            errorCode: 'plugin_tool_denied',
          });
        }
      } else {
        a.pendingTools++;
        dispatched = true;
        result = validate(
          'ToolResult',
          await callbackJSON(await callback(a, start, config.callbackURL, raw, signal)),
        );
      }
      for (const field of ['schemaVersion', 'runId', 'attempt', 'fence', 'callId', 'inputHash'])
        if (result[field] !== message[field]) throw fail();
      if ('output' in result) boundedJSON(result.output);
      if (containsSecret(result, start.toolGrant)) throw fail();
      if (result.status === 'unknown') throw fail();
    } catch {
      if (dispatched) {
        a.toolUnknown = true;
        // The ordered Core gate stops every later dispatch while the SDK observes this result.
        if (config.modelAuthMode === 'core') return { status: 'unknown' };
      }
      a.abort.abort();
      throw fail(503);
    } finally {
      if (dispatched) a.pendingTools--;
    }
    if (terminal(r.status) || r.cancelRequested) throw fail(409);
    await exclusive(async () => {
      if (terminal(r.status) || r.cancelRequested) throw fail(409);
      r.status = 'running';
      event(r, 'tool_completed', { callId: call.callId, toolName: tool.name, status: 'running' });
      await persist(r);
    });
    return {
      status: result.status,
      ...('output' in result ? { output: result.output } : {}),
      ...(result.errorCode ? { errorCode: result.errorCode } : {}),
    };
  }
  async function run(r, start, tools) {
    const a = active.get(key(r.runId, r.attempt));
    try {
      await transition(r, 'running');
      const result = await executor({
        config,
        start,
        tools,
        deadline: a.deadline,
        signal: a.abort.signal,
        setClient: (c) => {
          a.client = c;
        },
        bridge: (call, signal = a.abort.signal) => bridge(r, start, tools, call, signal),
        confirmAssembly: (assets, nativeTools = []) => confirmAssembly(r, a, start, assets, nativeTools),
        auditDir: hookDirectory(r),
        onHookOutcome: (observation) => exclusive(async () => {
          if (containsSecret(observation, start.toolGrant)) throw fail(403);
          const { callId } = observation;
          const observed = r.events.find(({ type, data }) => type === 'hook_observed' && data.callId === callId);
          const previous = observed?.data ?? a.pendingHookOutcomes.get(callId);
          if (previous && stable(previous) !== stable(observation)) throw fail(409);
          if (observed) return;
          if (terminal(r.status) || (!previous && r.events.length + a.pendingHookOutcomes.size >= 4095)) throw fail(413);
          const length = r.events.length, sequence = r.sequence;
          event(r, 'hook_observed', observation);
          a.pendingHookOutcomes.set(callId, observation);
          try {
            await persist(r);
            a.pendingHookOutcomes.delete(callId);
          } catch (error) {
            r.events.length = length;
            r.sequence = sequence;
            throw error;
          }
        }),
        onDelta: (delta) =>
          exclusive(async () => {
            if (terminal(r.status) || r.cancelRequested) throw fail(409);
            const text = (r.assistantText || '') + delta;
            if (
              text.length > 131072 ||
              Buffer.byteLength(text) > 131072 ||
              containsSecret(text, start.toolGrant)
            )
              throw fail(413);
            r.assistantText = text;
            let part = '';
            for (const char of delta) {
              if (Buffer.byteLength(part + char) > 16384) {
                event(r, 'message_delta', { delta: part });
                part = '';
              }
              part += char;
            }
            if (part) event(r, 'message_delta', { delta: part });
            await persist(r);
          }),
      });
      const text = result.assistantText;
      if (a.abort.signal.aborted) throw fail();
      if (containsSecret(text, start.toolGrant)) throw fail();
      if (Buffer.byteLength(text) > 131072 || text.length > 131072) throw fail(413);
      const usageKnown = !a.toolUnknown && result.usageKnown === true;
      a.terminalIntent = {
        status: a.toolUnknown ? 'unknown' : 'completed',
        data: { assistantText: text, usageKnown, ...(usageKnown ? { usage: validate('Usage', result.usage) } : {}) },
        grant: start.toolGrant,
      };
    } catch (error) {
      if (error?.exitUnknown && !error.scopedExitUnknown) {
        a.exitUnknown = true;
        a.retryCleanup = error.retryCleanup;
        ready = false;
      }
      if (a.pendingTools) {
        a.toolUnknown = true;
        a.abort.abort();
      }
      a.terminalIntent = {
        status: a.toolUnknown || error?.exitUnknown ? 'unknown' : r.cancelRequested ? 'canceled' : 'failed',
        data: {
          usageKnown: false,
          errorCode: a.toolUnknown ? 'tool_outcome_unknown' : 'execution_stopped',
        },
        grant: start.toolGrant,
      };
    }
    await commitTerminal(r, a);
  }
  async function accept(start) {
    validate('Start', start);
    if (config.modelAuthMode === 'core' || start.assembly) {
      if (!start.toolGrant) throw fail(403);
      try {
        for (const field of [
          'runId',
          'attempt',
          'fence',
          'contextRevision',
          'toolCatalogRevision',
          'toolGrant',
        ])
          validateHeaderValue('X-Soha-Binding', String(start[field]));
      } catch {
        throw fail();
      }
    }
    if (start.assembly) {
      const assets = start.assembly.assets;
      if (new Set(assets.map(({ kind, id }) => `${kind}:${id}`)).size !== assets.length) throw fail();
    }
    const { toolGrant, ...frozen } = start,
      fingerprint = sha(stable(frozen));
    return exclusive(async () => {
      const existing = records.get(key(start.runId, start.attempt));
      if (existing) {
        if (existing.fingerprint !== fingerprint || existing.inputHash !== start.inputHash)
          throw fail(409);
        return { code: 200, body: existing.receipt };
      }
      const previous = [...records.values()].filter((r) => r.runId === start.runId);
      if (
        previous.some(
          (r) =>
            start.attempt <= r.attempt ||
            start.fence <= r.fence ||
            !terminal(r.status) ||
            r.ownerHash !== sha(start.ownerKey),
        )
      )
        throw fail(409);
      if (!ready) throw fail(503);
      if (active.size >= config.capacity) throw fail(429);
      if (
        !config.models.includes(start.model) ||
        (config.modelAuthMode === 'gateway' &&
          start.toolCatalogRevision !== config.catalog.revision)
      )
        throw fail(403);
      if (Date.parse(start.deadline) <= Date.now() || start.maxOutputTokens > config.maxTokens)
        throw fail();
      const names = new Set(),
        tools = [];
      for (const t of start.tools || []) {
        const trusted = config.modelAuthMode === 'core' ? t : catalog.get(t.name)?.tool;
        if (
          names.has(t.name) ||
          !trusted ||
          stable(t) !== stable(trusted) ||
          (start.profile === 'chat' && !trusted.readOnly)
        )
          throw fail(403);
        names.add(t.name);
        boundedJSON(t.inputSchema);
        let checkInput;
        try {
          checkInput =
            config.modelAuthMode === 'core' ? compileTool(trusted) : catalog.get(t.name).check;
        } catch (error) {
          throw error.status ? error : fail();
        }
        tools.push({
          ...trusted,
          alias: `soha_tool_${tools.length}`,
          checkInput,
          sdkInputSchema: checkInput.sdkInputSchema,
        });
      }
      if (tools.length && !toolGrant) throw fail(403);
      const r = {
        ...identity(start),
        submissionId: randomUUID(),
        inputHash: start.inputHash,
        fingerprint,
        ownerHash: sha(start.ownerKey),
        status: 'accepted',
        sequence: 0,
        events: [],
      };
      event(r, 'accepted', { status: 'accepted' });
      r.receipt = validate('Receipt', {
        ...identity(r),
        kind: 'receipt',
        submissionId: r.submissionId,
        inputHash: r.inputHash,
        status: 'accepted',
        sequence: r.sequence,
      });
      await persist(r);
      records.set(key(r.runId, r.attempt), r);
      const a = {
        deadline: effectiveRunDeadline(start.deadline, config.maxRunMs, now()),
        abort: new AbortController(),
        calls: new Set(),
        pendingTools: 0,
        pendingHookOutcomes: new Map(),
        toolUnknown: false,
      };
      active.set(key(r.runId, r.attempt), a);
      // Execution starts only after durable receipt exists, with no hidden queue.
      setImmediate(() => run(r, start, tools));
      return { code: 202, body: r.receipt };
    });
  }
  async function probeMCP(body, res) {
    const { id, version, scope } = body ?? {};
    if (typeof id !== 'string' || !assetID.test(id) || typeof version !== 'string' ||
        !assetVersion.test(version) || !['chat', 'heya'].includes(scope) || Object.keys(body).length !== 3)
      throw fail();
    const kind = 'mcp', key = `${kind}:${id}:${version}`;
    const definition = config.pluginAssets.get(key);
    if (!ready || res.destroyed || !definition || probes.has(key) || probes.size >= Math.min(config.capacity, 4))
      return { status: 'unavailable', capabilities: [] };

    const client = new AbortController();
    const disconnect = () => client.abort();
    res.once('close', disconnect);
    const marker = join(config.dataDir, `mcp-probe-${sha(key)}.pending`);
    const probe = (async () => {
      try {
        try {
          const fd = await open(marker, 'wx', 0o600);
          await fd.close();
          await syncDataDir();
        } catch (error) {
          return { status: error.code === 'EEXIST' ? 'cleanup_unknown' : 'unavailable', capabilities: [] };
        }
        let result;
        try {
          const lease = await prepareMCPAsset(definition, { kind, id, version }, {
            signal: AbortSignal.any([client.signal, stopProbes.signal]),
            deadline: Date.now() + probeMs,
            workspace: config.dataDir,
            profile: scope,
          });
          await lease.close();
          result = { status: 'ready', capabilities: lease.capabilities };
        } catch (error) {
          if (error?.cleanupUnknown) return { status: 'cleanup_unknown', capabilities: [] };
          result = { status: 'unavailable', capabilities: [] };
        }
        try {
          await rm(marker);
          await syncDataDir();
          return result;
        } catch {
          return { status: 'cleanup_unknown', capabilities: [] };
        }
      } finally {
        res.off('close', disconnect);
        probes.delete(key);
      }
    })();
    probes.set(key, probe);
    return probe;
  }
  const server = createServer(async (req, res) => {
    const send = (code, body) => {
      const raw = JSON.stringify(body);
      if (Buffer.byteLength(raw) > 1048576) throw fail(413);
      res.writeHead(code, { 'content-type': 'application/json', 'cache-control': 'no-store' });
      res.end(raw);
    };
    try {
      if (
        req.rawHeaders.filter((v, i) => i % 2 === 0 && v.toLowerCase() === 'authorization')
          .length !== 1 ||
        !equal(req.headers.authorization, `Bearer ${config.token}`)
      )
        throw fail(401);
      const parsed = new URL(req.url, 'http://runtime');
      if (req.method === 'GET' && parsed.pathname === '/v1/health')
        return send(
          200,
          validate('Health', {
            schemaVersion: VERSION,
            kind: 'health',
            ready,
            protocolVersion: VERSION,
            runtimeVersion: '0.2.2-rc.1',
            capabilities: ['isolated-run', 'durable-receipt', 'soha-tool-bridge', 'runtime-assembly-v1'],
          }),
        );
      if (req.method === 'POST' && parsed.pathname === '/v1/runs') {
        const result = await accept(await readJSON(req));
        return send(result.code, result.body);
      }
      if (req.method === 'GET' && parsed.pathname === '/v1/assets') return send(200, inventory);
      if (req.method === 'POST' && parsed.pathname === '/v1/assets/mcp/probe') {
        const result = await probeMCP(await readJSON(req, 2048), res);
        if (!res.destroyed) return send(200, result);
        return;
      }
      const m = parsed.pathname.match(
        /^\/v1\/runs\/([A-Za-z0-9][A-Za-z0-9._:-]{0,127})\/attempts\/([1-9][0-9]*)(\/(events|cancel))?$/,
      );
      if (!m || !Number.isSafeInteger(Number(m[2]))) throw fail(404);
      const r = records.get(key(m[1], Number(m[2])));
      if (!r) throw fail(404);
      await serial;
      if (req.method === 'GET' && (!m[4] || m[4] === 'events')) {
        await commitTerminal(r, active.get(key(r.runId, r.attempt)));
        if (terminal(r.status)) await replayHookOutcomes(r);
      }
      if (req.method === 'GET' && !m[4]) return send(200, await exclusive(() => snapshot(r)));
      if (req.method === 'GET' && m[4] === 'events') {
        const page = await exclusive(() => {
          const raw = parsed.searchParams.get('after') ?? '0', after = Number(raw);
          if (!/^\d+$/.test(raw) || !Number.isSafeInteger(after) || after > r.sequence) throw fail();
          const candidates = r.events.filter((e) => e.sequence > after), items = [];
          for (const e of candidates) {
            if (items.length === 256 ||
                Buffer.byteLength(JSON.stringify(items)) + Buffer.byteLength(JSON.stringify(e)) > 1000000)
              break;
            items.push(e);
          }
          return validate('EventPage', {
            items,
            nextSequence: items.at(-1)?.sequence ?? after,
            hasMore: items.length < candidates.length,
          });
        });
        return send(200, page);
      }
      if (req.method === 'POST' && m[4] === 'cancel') {
        const control = validate('Control', await readJSON(req));
        if (control.runId !== r.runId || control.attempt !== r.attempt || control.fence !== r.fence)
          throw fail(409);
        const id = key(r.runId, r.attempt);
        await commitTerminal(r, active.get(id));
        await exclusive(async () => {
          if (!terminal(r.status) && !active.get(id)?.terminalIntent) {
            r.cancelRequested = true;
            await persist(r);
          }
        });
        const a = active.get(id);
        if (a && !a.terminalIntent) {
          a.abort.abort();
          await a.client?.close();
          // The worker closes and reaps before its promise settles and changes state.
          const until = Date.now() + 10000;
          while (active.has(id) && !a.terminalIntent && Date.now() < until)
            await new Promise((resolve) => setTimeout(resolve, 10));
          await commitTerminal(r, a);
        }
        return send(200, await exclusive(() => snapshot(r)));
      }
      throw fail(404);
    } catch (error) {
      if (!res.headersSent)
        send(error.status || 503, {
          error: ['invalid_tool_schema', 'unsupported_tool_schema_reference'].includes(error.code)
            ? error.code
            : 'runtime_request_rejected',
        });
      else res.destroy();
    }
  });
  server.requestTimeout = 15000;
  server.headersTimeout = 10000;
  return {
    server,
    async close() {
      ready = false;
      stopProbes.abort();
      await Promise.allSettled(probes.values());
      for (const a of active.values()) {
        a.abort.abort();
        await a.client?.close().catch(() => {});
      }
      for (const [id, a] of active) {
        await commitTerminal(records.get(id), a);
        if (a.exitUnknown) {
          try {
            await a.client?.close();
            await a.retryCleanup?.();
            a.exitUnknown = false;
            if (!a.terminalIntent) active.delete(id);
          } catch {}
        }
      }
      const until = Date.now() + 10000;
      while (active.size && Date.now() < until) await new Promise((r) => setTimeout(r, 10));
      await serial;
      for (const r of records.values()) {
        const a = active.get(key(r.runId, r.attempt));
        if (a?.terminalIntent) await commitTerminal(r, a);
        else if (!terminal(r.status)) await transition(r, 'unknown', { errorCode: 'service_shutdown' });
      }
      server.closeAllConnections();
      if (server.listening) await new Promise((r) => server.close(r));
      if (active.size) throw fail(503);
      await lock.close();
      await rm(lockPath);
    },
  };
}
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    const config = await configFromEnv();
    const runtime = await createRuntime(config);
    runtime.server.listen(config.port, config.host);
    let closing = false;
    for (const sig of ['SIGINT', 'SIGTERM'])
      process.on(sig, async () => {
        if (closing) return;
        closing = true;
        await runtime.close();
      });
  } catch {
    console.error('dsh runtime startup failed');
    process.exitCode = 1;
  }
}
