import { readFile } from "node:fs/promises";
import { parse } from "yaml";
import { isHttpMethod, isObject } from "./openapi-contract-utils.mjs";

const openapiPath = "openapi/soha-api.yaml";
const packagePath = "package.json";

const openapi = parse(await readFile(openapiPath, "utf8"));
const packageJson = JSON.parse(await readFile(packagePath, "utf8"));
const errors = [];
const warnings = [];

expect(openapi?.openapi === "3.1.0", `${openapiPath} must use OpenAPI 3.1.0`);
expect(openapi?.info?.title, `${openapiPath} must declare info.title`);
expect(openapi?.info?.version, `${openapiPath} must declare info.version`);
expect(
  openapi?.info?.version === packageJson.version,
  `${openapiPath} info.version (${openapi?.info?.version}) must match package.json version (${packageJson.version})`,
);
expect(Array.isArray(openapi?.servers) && openapi.servers.length > 0, `${openapiPath} must declare at least one server`);
expect(isObject(openapi?.paths) && Object.keys(openapi.paths).length > 0, `${openapiPath} must declare paths`);
expect(
  isObject(openapi?.components?.schemas) && Object.keys(openapi.components.schemas).length > 0,
  `${openapiPath} must declare components.schemas`,
);
expect(isObject(openapi?.components?.securitySchemes), `${openapiPath} must declare components.securitySchemes`);

const operationIds = new Map();
const invalidColonParams = Object.keys(openapi.paths ?? {}).filter((path) => /\/:[A-Za-z]/.test(path));
for (const path of invalidColonParams) {
  errors.push(`OpenAPI paths must use {param} syntax, not colon params: ${path}`);
}

for (const [path, pathItem] of Object.entries(openapi.paths ?? {})) {
  const pathParameters = normalizeParameters(pathItem?.parameters ?? [], "path item");
  for (const [method, operation] of Object.entries(pathItem ?? {})) {
    if (!isHttpMethod(method)) {
      continue;
    }
    lintOperation(path, method, operation, pathParameters);
  }
}

lintReferencedSchemas(openapi);

if (warnings.length > 0) {
  for (const warning of warnings) {
    console.warn(`warning: ${warning}`);
  }
}
if (errors.length > 0) {
  for (const error of errors) {
    console.error(`error: ${error}`);
  }
  throw new Error(`OpenAPI lint failed with ${errors.length} error(s)`);
}

console.log(`linted ${operationIds.size} OpenAPI operations and ${Object.keys(openapi.components.schemas).length} schemas`);

function lintOperation(path, method, operation, pathParameters) {
  const label = `${method.toUpperCase()} ${path}`;
  expect(operation.operationId, `${label} is missing operationId`);
  if (operation.operationId) {
    expect(
      /^[a-z][A-Za-z0-9]*$/.test(operation.operationId),
      `${label} operationId must be lower camel case: ${operation.operationId}`,
    );
    const existing = operationIds.get(operation.operationId);
    if (existing) {
      errors.push(`duplicate operationId ${operation.operationId}: ${existing} and ${label}`);
    } else {
      operationIds.set(operation.operationId, label);
    }
  }
  expect(Array.isArray(operation.tags) && operation.tags.length > 0, `${label} must declare at least one tag`);
  expect(isObject(operation.responses) && Object.keys(operation.responses).length > 0, `${label} is missing responses`);

  const declaredParameters = [...pathParameters, ...normalizeParameters(operation.parameters ?? [], label)];
  lintPathParameters(path, label, declaredParameters);
  lintRequestBody(label, operation.requestBody);
  lintResponses(label, operation.responses ?? {});
}

function lintPathParameters(path, label, declaredParameters) {
  const expectedNames = [...path.matchAll(/\{([^}]+)\}/g)].map((match) => match[1]).sort();
  const declaredByName = new Map(
    declaredParameters.filter((parameter) => parameter.in === "path").map((parameter) => [parameter.name, parameter]),
  );
  for (const name of expectedNames) {
    const parameter = declaredByName.get(name);
    expect(parameter, `${label} path parameter {${name}} is not declared`);
    expect(parameter?.required === true, `${label} path parameter {${name}} must be required`);
    expect(parameter?.schema, `${label} path parameter {${name}} must declare a schema`);
  }
  for (const parameter of declaredByName.values()) {
    expect(expectedNames.includes(parameter.name), `${label} declares unused path parameter ${parameter.name}`);
  }
}

function lintRequestBody(label, requestBody) {
  if (!requestBody) {
    return;
  }
  if (requestBody.$ref) {
    return;
  }
  const entries = Object.entries(requestBody.content ?? {});
  expect(entries.length > 0, `${label} requestBody must declare content`);
  for (const [contentType, content] of entries) {
    expect(content?.schema, `${label} ${contentType} requestBody must declare schema`);
  }
}

function lintResponses(label, responses) {
  const statusCodes = Object.keys(responses);
  expect(statusCodes.some((status) => status === "101" || /^[23]/.test(status)), `${label} must declare a 101, 2xx, or 3xx success response`);
  for (const [status, response] of Object.entries(responses)) {
    if (response?.$ref || status === "101" || status === "204" || status === "302") {
      continue;
    }
    const jsonContent = response?.content?.["application/json"];
    if (!jsonContent) {
      warnings.push(`${label} ${status} response has no application/json content`);
      continue;
    }
    expect(jsonContent.schema, `${label} ${status} application/json response must declare schema`);
  }
}

function lintReferencedSchemas(document) {
  const schemas = document.components?.schemas ?? {};
  const missingRefs = [];
  walk(document, (value) => {
    if (!isObject(value) || typeof value.$ref !== "string") {
      return;
    }
    if (!value.$ref.startsWith("#/components/schemas/")) {
      return;
    }
    const schemaName = value.$ref.split("/").at(-1);
    if (!schemas[schemaName]) {
      missingRefs.push(value.$ref);
    }
  });
  for (const ref of [...new Set(missingRefs)].sort()) {
    errors.push(`missing referenced schema ${ref}`);
  }
}

function normalizeParameters(parameters, label) {
  return parameters.map((parameter) => {
    if (!parameter.$ref) {
      return parameter;
    }
    const resolved = resolveLocalRef(parameter.$ref);
    if (!resolved) {
      errors.push(`${label} references missing parameter ${parameter.$ref}`);
      return {};
    }
    return resolved;
  });
}

function resolveLocalRef(ref) {
  if (!ref.startsWith("#/")) {
    return undefined;
  }
  return ref
    .slice(2)
    .split("/")
    .reduce((current, segment) => current?.[segment], openapi);
}

function walk(value, visitor) {
  visitor(value);
  if (Array.isArray(value)) {
    for (const item of value) {
      walk(item, visitor);
    }
    return;
  }
  if (isObject(value)) {
    for (const nested of Object.values(value)) {
      walk(nested, visitor);
    }
  }
}

function expect(condition, message) {
  if (!condition) {
    errors.push(message);
  }
}
