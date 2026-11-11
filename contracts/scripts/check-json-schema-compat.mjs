import { readFile } from "node:fs/promises";

const packageJson = JSON.parse(await readFile("package.json", "utf8"));
const baselinePath = "compat/json-schema-ecosystem-0.1.0.json";
const baseline = JSON.parse(await readFile(baselinePath, "utf8"));
const errors = [];

if (baseline.version !== packageJson.version) {
  errors.push(`${baselinePath} version (${baseline.version}) must match package.json version (${packageJson.version})`);
}

for (const [schemaFile, expected] of Object.entries(baseline.schemas ?? {})) {
  const schema = JSON.parse(await readFile(schemaFile, "utf8"));
  checkRootProperties(schemaFile, schema, expected);
  checkRootRequired(schemaFile, schema, expected);
  checkEnums(schemaFile, schema, expected);
}

if (errors.length > 0) {
  for (const error of errors) {
    console.error(`error: ${error}`);
  }
  throw new Error(`JSON Schema compatibility check failed with ${errors.length} error(s)`);
}

console.log(`checked ${Object.keys(baseline.schemas ?? {}).length} ecosystem JSON schema compatibility baselines`);

function checkRootProperties(schemaFile, schema, expected) {
  const current = new Set(Object.keys(schema.properties ?? {}));
  for (const property of expected.rootProperties ?? []) {
    if (!current.has(property)) {
      errors.push(`${schemaFile} removed root property ${property}`);
    }
  }
}

function checkRootRequired(schemaFile, schema, expected) {
  const expectedRequired = new Set(expected.rootRequired ?? []);
  for (const property of schema.required ?? []) {
    if (!expectedRequired.has(property)) {
      errors.push(`${schemaFile} added root required property ${property}`);
    }
  }
}

function checkEnums(schemaFile, schema, expected) {
  for (const enumBaseline of expected.enums ?? []) {
    const current = getByPath(schema, enumBaseline.path);
    if (!Array.isArray(current?.enum)) {
      errors.push(`${schemaFile} removed enum at ${enumBaseline.path}`);
      continue;
    }
    const values = new Set(current.enum);
    for (const value of enumBaseline.values ?? []) {
      if (!values.has(value)) {
        errors.push(`${schemaFile} removed enum value ${value} at ${enumBaseline.path}`);
      }
    }
  }
}

function getByPath(value, path) {
  return path.split("/").reduce((current, segment) => current?.[segment], value);
}
