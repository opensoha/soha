import { readFile } from "node:fs/promises";

const workflow = await readFile(new URL("../.github/workflows/release.yml", import.meta.url), "utf8");
const errors = [];

for (const required of [
  "contracts_version:",
  "CONTRACTS_VERSION:",
  "npm ci --no-audit --no-fund",
  "process.env.CONTRACTS_VERSION && dep !== process.env.CONTRACTS_VERSION",
  "entry.version !== dep",
  "!entry.integrity",
  "startsWith('https://registry.npmjs.org/')",
  "verify-release-artifact.mjs",
  "gh release download"
]) {
  if (!workflow.includes(required)) {
    errors.push(`release workflow is missing ${required}`);
  }
}

for (const forbidden of [
  "repository: opensoha/soha-contracts",
  "path: soha-contracts",
  "file:../soha-contracts",
  "npm pkg set",
  "npm install",
  "inputs.contracts_version ||"
]) {
  if (workflow.includes(forbidden)) {
    errors.push(`release workflow must not contain ${forbidden}`);
  }
}

if (errors.length > 0) {
  for (const error of errors) {
    console.error(`release-workflow: ${error}`);
  }
  process.exit(1);
}

console.log("release workflow consumes versioned @opensoha/contracts package");
