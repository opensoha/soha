import { closeSync, fsyncSync, mkdirSync, openSync, renameSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { toolOutcomeMessage } from './bridge-plugin.mjs';
import { sha } from './protocol.mjs';

export const name = 'soha-assembly';
export const inject = ['tools'];
const runStopped = 'Soha run is stopped';

function syncDirectory(directory) {
  const fd = openSync(directory, 'r');
  try { fsyncSync(fd); } finally { closeSync(fd); }
}

export function writeHookOutcome(directory, order, observation) {
  if (mkdirSync(directory, { recursive: true, mode: 0o700 })) syncDirectory(dirname(directory));
  const path = join(directory, `${String(order).padStart(6, '0')}-${sha(observation.callId)}.json`);
  writeFileSync(`${path}.tmp`, JSON.stringify(observation), { mode: 0o600, flush: true });
  renameSync(`${path}.tmp`, path);
  syncDirectory(directory);
}

export async function apply(ctx, config) {
  const loaded = [];
  if (config.hooks.includes('tool-limit')) {
    const deadline = performance.now() + Math.max(0, config.deadline - Date.now());
    let disposed = false;
    ctx.on('dispose', () => {
      disposed = true;
    });
    ctx.tools.guard((exec) => {
      if (disposed || exec.signal.aborted || performance.now() >= deadline) return runStopped;
    });
    loaded.push('tool-limit');
  }
  if (config.hooks.includes('tool-audit')) {
    const names = new Map(config.auditTools);
    const pending = new Map();
    let order = 0;
    const flush = () => Promise.all([...pending].map(([position, entry]) => {
      if (entry.delivery) return entry.delivery;
      const { alias, observation } = entry;
      try {
        try {
          writeHookOutcome(config.auditDir, position, observation);
        } catch {
          writeHookOutcome(`${config.auditDir}.retry`, position, observation);
        }
        pending.delete(position);
      } catch {
        const { callId, outcome } = observation;
        entry.delivery = fetch(config.auditURL, {
          method: 'POST', redirect: 'error',
          headers: {
            authorization: `Bearer ${process.env.SOHA_DSH_SYNTHETIC_KEY}`,
            'content-type': 'application/json',
          },
          body: JSON.stringify({ alias, callId, outcome }),
          signal: AbortSignal.timeout(400),
        }).then(async (response) => {
          if (response.status === 204) pending.delete(position);
          else {
            await response.body?.cancel();
            throw new Error('Soha hook outcome delivery rejected');
          }
        }).catch(() => {
          console.warn('dsh hook outcome awaiting delivery');
        }).finally(() => { entry.delivery = undefined; });
        return entry.delivery;
      }
    }));
    ctx.on('dispose', async () => {
      await flush();
      if (pending.size) await flush();
    });
    ctx.on('tools/result', (exec, result) => {
      const toolName = names.get(exec.name);
      if (!toolName || !/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(exec.callId))
        return;
      let outcome = 'completed';
      if (result.isError) {
        const code = result.error.info?.code;
        if (code === 'ABORTED' || code === 'ABORTED_BEFORE_DISPATCH') outcome = 'canceled';
        else if ([runStopped, toolOutcomeMessage.denied].includes(result.error.message)) outcome = 'denied';
        else if (result.error.message === toolOutcomeMessage.unknown) outcome = 'unknown';
        else outcome = 'failed';
      }
      // Result observers cannot change the tool result. Keep failed writes queued until disposal.
      pending.set(++order, { alias: exec.name, observation: { callId: exec.callId, toolName, outcome } });
      return flush();
    });
    loaded.push('tool-audit');
  }
  const response = await fetch(config.registrationURL, {
    method: 'POST',
    redirect: 'error',
    headers: {
      authorization: `Bearer ${process.env.SOHA_DSH_SYNTHETIC_KEY}`,
      'content-type': 'application/json',
    },
    body: JSON.stringify(loaded),
    signal: AbortSignal.timeout(2000),
  });
  if (response.status !== 204) throw new Error('Soha assembly registration rejected');
}
