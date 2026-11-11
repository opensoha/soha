import { readFile, writeFile } from "node:fs/promises";
import { parse, stringify } from "yaml";

export const httpMethods = ["get", "post", "put", "patch", "delete"];

export async function readOpenapi(path) {
  const text = await readFile(path, "utf8");
  return parse(text);
}

export async function writeJson(path, value) {
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`);
}

export function buildSnapshot(openapi) {
  const schemas = {};
  for (const [name, schema] of Object.entries(openapi.components?.schemas ?? {})) {
    schemas[name] = normalizeSchema(schema);
  }

  const operations = {};
  for (const [path, pathItem] of Object.entries(openapi.paths ?? {})) {
    for (const method of httpMethods) {
      const operation = pathItem?.[method];
      if (!operation) {
        continue;
      }
      operations[`${method.toUpperCase()} ${path}`] = normalizeOperation(operation);
    }
  }

  return {
    openapi: openapi.openapi,
    info: {
      title: openapi.info?.title,
      version: openapi.info?.version,
    },
    operations: sortObject(operations),
    schemas: sortObject(schemas),
  };
}

export function canonicalJson(value) {
  return JSON.stringify(sortValue(value), null, 2);
}

export function normalizeSchema(schema) {
  if (!isObject(schema)) {
    return {};
  }
  const normalized = {};
  if (schema.$ref) {
    normalized.$ref = schema.$ref;
  }
  if (schema.type !== undefined) {
    normalized.type = schema.type;
  }
  if (schema.format !== undefined) {
    normalized.format = schema.format;
  }
  if (schema.additionalProperties !== undefined) {
    normalized.additionalProperties =
      schema.additionalProperties === true || schema.additionalProperties === false
        ? schema.additionalProperties
        : normalizeSchema(schema.additionalProperties);
  }
  if (Array.isArray(schema.required)) {
    normalized.required = [...schema.required].sort();
  }
  if (Array.isArray(schema.enum)) {
    normalized.enum = [...schema.enum].sort();
  }
  if (schema.items !== undefined) {
    normalized.items = normalizeSchema(schema.items);
  }
  if (isObject(schema.properties)) {
    const properties = {};
    for (const [name, property] of Object.entries(schema.properties)) {
      properties[name] = normalizeSchema(property);
    }
    normalized.properties = sortObject(properties);
  }
  for (const composition of ["allOf", "anyOf", "oneOf"]) {
    if (Array.isArray(schema[composition])) {
      normalized[composition] = schema[composition].map((entry) => normalizeSchema(entry));
    }
  }
  return sortObject(normalized);
}

export function normalizeOperation(operation) {
  const responses = {};
  for (const [status, response] of Object.entries(operation.responses ?? {})) {
    responses[status] = normalizeResponse(response);
  }
  return {
    operationId: operation.operationId,
    parameters: normalizeParameters(operation.parameters ?? []),
    requestBody: normalizeRequestBody(operation.requestBody),
    responses: sortObject(responses),
  };
}

export function isHttpMethod(method) {
  return httpMethods.includes(method);
}

export function isObject(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

export function getRefName(ref) {
  return typeof ref === "string" ? ref.split("/").at(-1) : undefined;
}

export function sortObject(value) {
  if (!isObject(value)) {
    return value;
  }
  return Object.fromEntries(Object.entries(value).sort(([left], [right]) => left.localeCompare(right)));
}

function normalizeParameters(parameters) {
  return parameters.map((parameter) => {
    if (parameter.$ref) {
      return { $ref: parameter.$ref };
    }
    return sortObject({
      name: parameter.name,
      in: parameter.in,
      required: parameter.required === true,
      schema: normalizeSchema(parameter.schema),
    });
  });
}

function normalizeRequestBody(requestBody) {
  if (!requestBody) {
    return undefined;
  }
  if (requestBody.$ref) {
    return { $ref: requestBody.$ref };
  }
  return {
    required: requestBody.required === true,
    content: normalizeContent(requestBody.content),
  };
}

function normalizeResponse(response) {
  if (response?.$ref) {
    return { $ref: response.$ref };
  }
  return {
    content: normalizeContent(response?.content),
  };
}

function normalizeContent(content) {
  const normalized = {};
  for (const [mediaType, media] of Object.entries(content ?? {})) {
    normalized[mediaType] = { schema: normalizeSchema(media.schema) };
  }
  return sortObject(normalized);
}

function sortValue(value) {
  if (Array.isArray(value)) {
    return value.map((item) => sortValue(item));
  }
  if (!isObject(value)) {
    return value;
  }
  const sorted = {};
  for (const [key, nested] of Object.entries(value).sort(([left], [right]) => left.localeCompare(right))) {
    sorted[key] = sortValue(nested);
  }
  return sorted;
}

export function toYaml(value) {
  return stringify(value);
}
