import { readdir, readFile } from "node:fs/promises";

export async function fixtureSchemaNames(root, required) {
  const entries = await readdir(root, { withFileTypes: true });
  for (const entry of entries) {
    if (!entry.isDirectory()) throw new Error(`${new URL(entry.name, root)} must be a schema directory`);
  }
  return [...new Set([...required, ...entries.map((entry) => entry.name)])].sort();
}

export async function validateFixtureDirectory(root, validate) {
  const entries = await readdir(root, { withFileTypes: true });
  for (const entry of entries) {
    if (!entry.isDirectory() || !["valid", "invalid"].includes(entry.name))
      throw new Error(`${new URL(entry.name, root)} must be inside valid/ or invalid/`);
  }
  let count = 0;
  for (const kind of ["valid", "invalid"]) {
    const directory = new URL(`${kind}/`, root);
    const files = await readdir(directory, { withFileTypes: true });
    if (files.length === 0) throw new Error(`${directory} must contain at least one fixture`);
    for (const file of files) {
      const path = new URL(file.name, directory);
      if (!file.isFile() || !file.name.endsWith(".json")) throw new Error(`${path} must be a JSON fixture`);
      const passed = validate(JSON.parse(await readFile(path, "utf8")));
      if (passed !== (kind === "valid"))
        throw new Error(`${path} should be ${kind}:\n${formatAjvErrors(validate.errors)}`);
      count++;
    }
  }
  return count;
}

export function formatAjvErrors(errors) {
  return (errors ?? []).map((error) => `- ${error.instancePath || "/"} ${error.message}`).join("\n");
}
