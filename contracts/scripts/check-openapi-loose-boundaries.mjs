import { readFile } from "node:fs/promises";
import { parse } from "yaml";
import { isObject } from "./openapi-contract-utils.mjs";

const openapiPath = "openapi/soha-api.yaml";
const baselinePath = "compat/openapi-loose-boundaries-0.1.0.json";
const looseSchemaRefs = new Set(["#/components/schemas/AnyValue", "#/components/schemas/GenericObject"]);

const openapi = parse(await readFile(openapiPath, "utf8"));
const baseline = JSON.parse(await readFile(baselinePath, "utf8"));

const current = collectLooseBoundaries(openapi);
const expected = normalizeEntries(baseline.entries ?? []);
const added = current.filter((entry) => !expected.has(entryKey(entry)));
const removed = [...expected.values()].filter((entry) => !current.some((item) => entryKey(item) === entryKey(entry)));

if (baseline.version !== openapi.info?.version) {
  throw new Error(`${baselinePath} version (${baseline.version}) must match ${openapiPath} info.version (${openapi.info?.version})`);
}
if (added.length > 0 || removed.length > 0) {
  for (const entry of added) {
    console.error(`new loose OpenAPI boundary: ${formatEntry(entry)}`);
  }
  for (const entry of removed) {
    console.error(`loose OpenAPI boundary removed from spec but still in baseline: ${formatEntry(entry)}`);
  }
  throw new Error(
    `OpenAPI loose boundary baseline mismatch: ${added.length} added, ${removed.length} removed. Review the schema boundary and update ${baselinePath} intentionally.`,
  );
}

console.log(`checked ${current.length} OpenAPI loose schema boundaries against ${baselinePath}`);

function collectLooseBoundaries(document) {
  const entries = [];
  walk(document, [], (value, path) => {
    if (!isObject(value)) {
      return;
    }
    if (typeof value.$ref === "string" && looseSchemaRefs.has(value.$ref)) {
      entries.push({ kind: "schemaRef", path: path.join("/"), target: value.$ref });
    }
    if (value.additionalProperties === true) {
      entries.push({ kind: "additionalPropertiesTrue", path: [...path, "additionalProperties"].join("/") });
    }
  });
  return sortEntries(entries);
}

function walk(value, path, visitor) {
  visitor(value, path);
  if (Array.isArray(value)) {
    for (const [index, item] of value.entries()) {
      walk(item, [...path, String(index)], visitor);
    }
    return;
  }
  if (isObject(value)) {
    for (const [key, nested] of Object.entries(value)) {
      walk(nested, [...path, key], visitor);
    }
  }
}

function normalizeEntries(entries) {
  return new Map(sortEntries(entries).map((entry) => [entryKey(entry), entry]));
}

function sortEntries(entries) {
  return [...entries].sort((a, b) => entryKey(a).localeCompare(entryKey(b)));
}

function entryKey(entry) {
  return `${entry.kind}:${entry.path}:${entry.target ?? ""}`;
}

function formatEntry(entry) {
  return entry.target ? `${entry.kind} ${entry.path} -> ${entry.target}` : `${entry.kind} ${entry.path}`;
}
