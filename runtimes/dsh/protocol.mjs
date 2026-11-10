import Ajv from 'ajv/dist/2020.js';
import formats from 'ajv-formats';
import { readFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { assertObjectJsonSchema } from '@deepseek-ai/dsh-tools';
export const VERSION = 'opensoha.dev/dsh-runtime/v1';
export const sha = (value) => createHash('sha256').update(value).digest('hex');
export const stable = (value) =>
  JSON.stringify(value, (_, v) =>
    v && !Array.isArray(v) && typeof v === 'object'
      ? Object.fromEntries(
          Object.keys(v)
            .sort()
            .map((k) => [k, v[k]]),
        )
      : v,
  );
export const fail = (status = 400) =>
  Object.assign(new Error('runtime_request_rejected'), { status });
export function boundedJSON(value, depth = 0) {
  if (depth > 16 || Buffer.byteLength(JSON.stringify(value)) > 131072) throw fail(413);
  if (typeof value === 'string' && value.length > 131072) throw fail(413);
  if (Array.isArray(value)) {
    if (value.length > 256) throw fail(413);
    value.forEach((v) => boundedJSON(v, depth + 1));
  } else if (value && typeof value === 'object') {
    if (Object.keys(value).length > 64 || Object.keys(value).some((k) => k.length > 128))
      throw fail(413);
    Object.values(value).forEach((v) => boundedJSON(v, depth + 1));
  }
}
export async function readJSON(stream, max = 1048576) {
  let size = 0;
  const chunks = [];
  const iterable = stream.iterator ? stream.iterator({ destroyOnReturn: false }) : stream;
  for await (const chunk of iterable) {
    size += chunk.length;
    if (size > max) {
      stream.resume?.();
      throw fail(413);
    }
    chunks.push(chunk);
  }
  try {
    return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(Buffer.concat(chunks)));
  } catch {
    throw fail();
  }
}
export async function validators(path) {
  const schema = JSON.parse(await readFile(path, 'utf8'));
  const ajv = new Ajv({ strict: true });
  formats(ajv);
  ajv.addSchema(schema);
  const checks = Object.fromEntries(
    [
      'Start',
      'Receipt',
      'Status',
      'Usage',
      'Event',
      'EventPage',
      'ToolCall',
      'ToolResult',
      'AssemblyReport',
      'AssemblyDecision',
      'Control',
      'Health',
    ].map((name) => [name, ajv.compile({ $ref: `${schema.$id}#/$defs/${name}` })]),
  );
  return (name, value) => {
    if (!checks[name](value)) throw fail();
    return value;
  };
}
export function compileTool(tool) {
  boundedJSON(tool.inputSchema);
  const reject = (code) => Object.assign(fail(), { code });
  const rejectReferences = (value) => {
    if (!value || typeof value !== 'object') return;
    for (const [key, child] of Object.entries(value)) {
      if (
        ['$ref', '$dynamicRef', '$recursiveRef'].includes(key) &&
        typeof child === 'string' &&
        !child.startsWith('#')
      )
        throw reject('unsupported_tool_schema_reference');
      rejectReferences(child);
    }
  };
  rejectReferences(tool.inputSchema);
  const ajv = new Ajv({ strict: true });
  formats(ajv);
  let check;
  try {
    check = ajv.compile(tool.inputSchema);
  } catch {
    throw reject('invalid_tool_schema');
  }
  try {
    assertObjectJsonSchema(tool.inputSchema);
    check.sdkInputSchema = tool.inputSchema;
  } catch {
    // The SDK is not a second schema authority. Its registration shape admits
    // every object; the parent and Core retain the complete execution contract.
    check.sdkInputSchema = { type: 'object', additionalProperties: true };
  }
  return check;
}
