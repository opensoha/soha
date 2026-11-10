import { constants } from 'node:fs';
import { lstat, open, realpath } from 'node:fs/promises';
import { dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
import { Context } from '@deepseek-ai/cordis';
import { SkillRegistry } from '@deepseek-ai/dsh-skill';
import { fail, sha, stable } from './protocol.mjs';

const bodyPath = 'SKILL.md';
const maxFileBytes = 16 * 1024;
const assetID = /^[a-z0-9][a-z0-9-]{0,47}$/;
const assetVersion = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const sha256 = /^[a-f0-9]{64}$/;
const controls = /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]/;
const textDecoder = new TextDecoder('utf-8', { fatal: true });
const validString = (value, pattern) => typeof value === 'string' && pattern.test(value);
const isRecord = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);
const reject = (code, status = 403) => Object.assign(fail(status), { code });

function contains(parent, child) {
  const offset = relative(parent, child);
  return offset !== '..' && !offset.startsWith('..' + sep) && !isAbsolute(offset);
}

function resourceName(path) {
  return path.length <= 128 &&
    path.split('/').every((segment) => /^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(segment)) &&
    /\.(md|txt|json|ya?ml|csv)$/i.test(path);
}

function validateManifest(definition, ref, enabledAssets) {
  const invalid = () => reject('skill_asset_invalid');
  if (!isRecord(definition) || ref?.kind !== 'skill') throw invalid();
  const { id, version, root, description, digest, files, requires } = definition;
  if (!validString(id, assetID) || id !== ref.id || !validString(version, assetVersion) || version !== ref.version ||
    typeof root !== 'string' || !isAbsolute(root) || resolve(root) !== root || dirname(root) === root ||
    typeof description !== 'string' || !description.trim() || description.length > 256 || controls.test(description) ||
    !validString(digest, sha256) || !isRecord(files) || !Object.hasOwn(files, bodyPath) ||
    !Array.isArray(requires) || requires.length > 16) throw invalid();
  const entries = Object.entries(files);
  if (entries.length > 32 || entries.some(([path, hash]) => !resourceName(path) || !validString(hash, sha256)))
    throw invalid();
  const dependencies = new Set();
  for (const dependency of requires) {
    if (!isRecord(dependency) || Object.keys(dependency).length !== 3 || dependency.kind !== 'mcp' ||
      !validString(dependency.id, assetID) || !validString(dependency.version, assetVersion) ||
      dependencies.has(dependency.id)) throw invalid();
    dependencies.add(dependency.id);
    if (!enabledAssets.some(({ kind, id, version }) =>
      kind === 'mcp' && id === dependency.id && version === dependency.version))
      throw reject('skill_dependency_missing');
  }
  if (sha(stable({ id, version, description, files, requires })) !== digest) throw invalid();
  return { id, version, root, description, digest, files: new Map(entries) };
}

function regularFile(stat) {
  return stat.isFile() && stat.nlink === 1 && (stat.mode & 0o111) === 0;
}

async function readText(root, path, digest, assertLive) {
  const denied = () => reject('skill_content_rejected');
  try {
    assertLive();
    // The operator mounts this tree read-only. Check all components and the opened file before reading.
    if (await realpath(root) !== root) throw denied();
    const parts = path.split('/');
    const basename = parts.pop();
    let directory = root;
    for (const part of parts) {
      assertLive();
      directory = join(directory, part);
      if (!(await lstat(directory)).isDirectory()) throw denied();
    }
    const target = join(directory, basename);
    assertLive();
    const before = await lstat(target);
    if (!regularFile(before) || before.size > maxFileBytes) throw denied();
    const file = await open(target, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
    try {
      const opened = await file.stat();
      if (!regularFile(opened) || opened.dev !== before.dev || opened.ino !== before.ino ||
        opened.size > maxFileBytes || await realpath(target) !== target) throw denied();
      const buffer = Buffer.alloc(maxFileBytes + 1);
      let length = 0;
      while (length < buffer.length) {
        assertLive();
        const { bytesRead } = await file.read(buffer, length, buffer.length - length, length);
        if (!bytesRead) break;
        length += bytesRead;
      }
      assertLive();
      const bytes = buffer.subarray(0, length);
      if (length > maxFileBytes || sha(bytes) !== digest) throw denied();
      const text = textDecoder.decode(bytes);
      if (controls.test(text) || text.trimStart().startsWith('#!') || (path === bodyPath && !text.trim()))
        throw denied();
      return text;
    } finally {
      await file.close();
    }
  } catch {
    assertLive();
    throw denied();
  }
}

export async function prepareSkillAsset(definition, ref, { signal, deadline, workspace, assets = [] }) {
  const lifetime = new AbortController();
  const assertLive = (callSignal) => {
    if (lifetime.signal.aborted || signal?.aborted || callSignal?.aborted || Date.now() >= deadline)
      throw reject('skill_asset_stopped', 409);
  };
  if (!Number.isFinite(deadline)) throw reject('skill_asset_invalid');
  assertLive();
  const { id, version, root, description, digest, files } = validateManifest(definition, ref, assets);
  try {
    if (await realpath(root) !== root || !(await lstat(root)).isDirectory())
      throw reject('skill_asset_invalid');
    if (workspace) {
      const taskRoot = await realpath(workspace);
      if (contains(root, taskRoot) || contains(taskRoot, root)) throw reject('skill_asset_invalid');
    }
  } catch {
    assertLive();
    throw reject('skill_asset_invalid');
  }
  assertLive();
  const context = new Context();
  const registry = new SkillRegistry(context);
  const provider = 'soha-managed-skill';
  const summary = {
    name: id.replace(/-+/g, '-').replace(/-$/, ''),
    description,
    source: 'bundled',
    provider,
    invocation: { modelInvocable: true, userInvocable: false },
  };
  const pending = new Set();
  const read = async (path, callSignal) => {
    assertLive(callSignal);
    const reading = readText(root, path, files.get(path), () => assertLive(callSignal));
    pending.add(reading);
    try {
      return await reading;
    } finally {
      pending.delete(reading);
    }
  };
  registry.registerProvider(() => ({
    name: provider,
    list: async () => [{ ...summary, rank: 0, locator: bodyPath }],
    get: async ({ locator }, { signal }) => ({ ...summary, content: await read(locator, signal) }),
  }));
  const close = async () => {
    lifetime.abort();
    await context.fiber.dispose();
    await Promise.allSettled(pending);
  };
  try {
    const [entry] = await registry.list({ signal });
    assertLive();
    if (!entry?.invocation.modelInvocable) throw reject('skill_asset_invalid');
    const prefix = 'skill_' + id.replaceAll('-', '_');
    const pin = `${id}@${version} (SHA-256 ${digest})`;
    const result = (path, content) => ({ id, version, digest, path, content });
    const tools = [{
      name: prefix + '_load',
      description: `Read the skill instructions for ${pin}. ${entry.description}`,
      inputSchema: { type: 'object', properties: {}, additionalProperties: false },
      async invoke(input, callSignal) {
        assertLive(callSignal);
        if (!isRecord(input) || Object.keys(input).length !== 0) throw reject('skill_input_invalid');
        const activeSignals = [lifetime.signal, signal, callSignal].filter(Boolean);
        const loaded = await registry.get(entry.name, { signal: AbortSignal.any(activeSignals) }).catch(() => {
          assertLive(callSignal);
          throw reject('skill_content_rejected');
        });
        assertLive(callSignal);
        if (!loaded) throw reject('skill_content_rejected');
        return result(bodyPath, loaded.content);
      },
    }];
    const resources = [...files.keys()].filter((path) => path !== bodyPath).sort();
    if (resources.length) {
      const resourceNames = new Set(resources);
      tools.push({
        name: prefix + '_resource',
        description: `Read a named resource for ${pin}.`,
        inputSchema: {
          type: 'object',
          properties: { path: { type: 'string', enum: resources } },
          required: ['path'],
          additionalProperties: false,
        },
        async invoke(input, callSignal) {
          assertLive(callSignal);
          if (!isRecord(input) || Object.keys(input).length !== 1 || !resourceNames.has(input.path))
            throw reject('skill_input_invalid');
          const content = await read(input.path, callSignal);
          assertLive(callSignal);
          return result(input.path, content);
        },
      });
    }
    return { tools, close };
  } catch (error) {
    await close();
    throw error;
  }
}
