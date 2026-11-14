import { mkdir, readFile } from "node:fs/promises";
import { dirname } from "node:path";
import {
  buildSnapshot,
  canonicalJson,
  getRefName,
  isObject,
  readOpenapi,
  writeJson,
} from "./openapi-contract-utils.mjs";

const openapiPath = "openapi/soha-api.yaml";
const baselinePath = "compat/openapi-0.1.0.snapshot.json";
const updateBaseline = process.argv.includes("--update-baseline");

const current = buildSnapshot(await readOpenapi(openapiPath));

if (updateBaseline) {
  await mkdir(dirname(baselinePath), { recursive: true });
  await writeJson(baselinePath, current);
  console.log(`updated ${baselinePath}`);
  process.exit(0);
}

const baseline = JSON.parse(await readFile(baselinePath, "utf8"));
const errors = [];

compareInfo(baseline.info, current.info);
compareOperations(baseline.operations ?? {}, current.operations ?? {});
compareSchemas(baseline.schemas ?? {}, current.schemas ?? {});

if (errors.length > 0) {
  for (const error of errors) {
    console.error(`breaking: ${error}`);
  }
  throw new Error(`OpenAPI breaking-change check failed with ${errors.length} issue(s)`);
}

console.log(
  `checked OpenAPI compatibility against ${baselinePath}: ${Object.keys(current.operations).length} operations, ${Object.keys(current.schemas).length} schemas`,
);

function compareInfo(before, after) {
  if (before?.title !== after?.title) {
    errors.push(`info.title changed from ${before?.title} to ${after?.title}`);
  }
}

function compareOperations(beforeOperations, afterOperations) {
  for (const [key, before] of Object.entries(beforeOperations)) {
    const after = afterOperations[key];
    if (!after) {
      errors.push(`operation removed: ${key}`);
      continue;
    }
    if (before.operationId !== after.operationId) {
      errors.push(`${key} operationId changed from ${before.operationId} to ${after.operationId}`);
    }
    compareParameters(key, before.parameters ?? [], after.parameters ?? []);
    compareRequestBody(key, before.requestBody, after.requestBody);
    compareResponses(key, before.responses ?? {}, after.responses ?? {});
  }
}

function compareParameters(operationKey, beforeParameters, afterParameters) {
  const afterByKey = new Map(afterParameters.map((parameter) => [parameterKey(parameter), parameter]));
  for (const before of beforeParameters) {
    const after = afterByKey.get(parameterKey(before));
    if (!after) {
      errors.push(`${operationKey} parameter removed: ${before.in} ${before.name ?? before.$ref}`);
      continue;
    }
    if (before.required === true && after.required !== true) {
      errors.push(`${operationKey} required parameter became optional: ${before.in} ${before.name}`);
    }
    compareSchema(`${operationKey} parameter ${before.in} ${before.name}`, before.schema, after.schema);
  }

  const beforeKeys = new Set(beforeParameters.map((parameter) => parameterKey(parameter)));
  for (const after of afterParameters) {
    if (after.required === true && !beforeKeys.has(parameterKey(after))) {
      errors.push(`${operationKey} added required parameter: ${after.in} ${after.name ?? after.$ref}`);
    }
  }
}

function compareRequestBody(operationKey, before, after) {
  if (!before && !after) {
    return;
  }
  if (!before && after?.required === true) {
    errors.push(`${operationKey} added required requestBody`);
    return;
  }
  if (before?.required !== true && after?.required === true) {
    errors.push(`${operationKey} requestBody became required`);
  }
  compareContentSchemas(`${operationKey} requestBody`, before?.content ?? {}, after?.content ?? {});
}

function compareResponses(operationKey, beforeResponses, afterResponses) {
  for (const [status, before] of Object.entries(beforeResponses)) {
    const after = afterResponses[status];
    if (!after) {
      errors.push(`${operationKey} response removed: ${status}`);
      continue;
    }
    compareContentSchemas(`${operationKey} response ${status}`, before.content ?? {}, after.content ?? {});
  }
}

function compareContentSchemas(label, beforeContent, afterContent) {
  for (const [mediaType, before] of Object.entries(beforeContent)) {
    const after = afterContent[mediaType];
    if (!after) {
      errors.push(`${label} media type removed: ${mediaType}`);
      continue;
    }
    compareSchema(`${label} ${mediaType}`, before.schema, after.schema);
  }
}

function compareSchemas(beforeSchemas, afterSchemas) {
  for (const [name, before] of Object.entries(beforeSchemas)) {
    const after = afterSchemas[name];
    if (!after) {
      errors.push(`schema removed: ${name}`);
      continue;
    }
    compareSchema(`schema ${name}`, before, after);
  }
}

function compareSchema(label, before, after) {
  if (!before && !after) {
    return;
  }
  if (!before || !after) {
    errors.push(`${label} schema changed from ${canonicalJson(before)} to ${canonicalJson(after)}`);
    return;
  }
  if (before.$ref !== after.$ref) {
    errors.push(`${label} $ref changed from ${before.$ref} to ${after.$ref}`);
  }
  if (canonicalJson(before.type) !== canonicalJson(after.type)) {
    errors.push(`${label} type changed from ${canonicalJson(before.type)} to ${canonicalJson(after.type)}`);
  }
  if (before.format !== after.format) {
    errors.push(`${label} format changed from ${before.format} to ${after.format}`);
  }
  compareEnum(label, before.enum, after.enum);
  compareAdditionalProperties(label, before.additionalProperties, after.additionalProperties);
  compareObjectSchema(label, before, after);
  compareNestedSchema(`${label} items`, before.items, after.items);
  for (const composition of ["allOf", "anyOf", "oneOf"]) {
    compareComposition(label, composition, before[composition], after[composition]);
  }
}

function compareObjectSchema(label, before, after) {
  const beforeProperties = before.properties ?? {};
  const afterProperties = after.properties ?? {};
  const beforeRequired = new Set(before.required ?? []);
  const afterRequired = new Set(after.required ?? []);

  for (const propertyName of Object.keys(beforeProperties)) {
    if (!afterProperties[propertyName]) {
      errors.push(`${label} property removed: ${propertyName}`);
      continue;
    }
    compareSchema(`${label}.${propertyName}`, beforeProperties[propertyName], afterProperties[propertyName]);
  }
  for (const propertyName of afterRequired) {
    if (!beforeRequired.has(propertyName)) {
      errors.push(`${label} added required property: ${propertyName}`);
    }
  }
}

function compareEnum(label, beforeEnum, afterEnum) {
  if (!Array.isArray(beforeEnum)) {
    return;
  }
  if (!Array.isArray(afterEnum)) {
    errors.push(`${label} enum removed`);
    return;
  }
  for (const value of beforeEnum) {
    if (!afterEnum.includes(value)) {
      errors.push(`${label} enum value removed: ${value}`);
    }
  }
}

function compareAdditionalProperties(label, before, after) {
  if (before === true && after === false) {
    errors.push(`${label} additionalProperties changed from true to false`);
  }
  if (isObject(before) && after === false) {
    errors.push(`${label} additionalProperties schema removed`);
  }
  if (isObject(before) && isObject(after)) {
    compareSchema(`${label} additionalProperties`, before, after);
  }
}

function compareNestedSchema(label, before, after) {
  if (before || after) {
    compareSchema(label, before, after);
  }
}

function compareComposition(label, composition, before, after) {
  if (!Array.isArray(before)) {
    return;
  }
  if (!Array.isArray(after)) {
    errors.push(`${label} ${composition} removed`);
    return;
  }
  for (let index = 0; index < before.length; index += 1) {
    if (!after[index]) {
      errors.push(`${label} ${composition}[${index}] removed`);
      continue;
    }
    compareSchema(`${label} ${composition}[${index}]`, before[index], after[index]);
  }
}

function parameterKey(parameter) {
  if (parameter.$ref) {
    return `$ref:${getRefName(parameter.$ref)}`;
  }
  return `${parameter.in}:${parameter.name}`;
}
