import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const npm = process.platform === "win32" ? "npm.cmd" : "npm";

const { stdout } = await execFileAsync(npm, ["pack", "--dry-run", "--json", "--ignore-scripts"], {
  maxBuffer: 1024 * 1024 * 10
});
const pack = JSON.parse(stdout)[0];
const files = new Set((pack.files ?? []).map((file) => file.path));
const errors = [];

for (const required of [
  "package.json",
  "README.md",
  "LICENSE",
  ".env.example",
  "CHANGELOG.md",
  "docs/soha-core-integration.md",
  "docs/operations/feishu-runtime-runbook.md",
  "dist/index.js",
  "dist/index.d.ts",
  "dist/connectors/feishu/index.js",
  "dist/connectors/feishu/index.d.ts",
  "dist/connectors/feishu/server.js",
  "dist/connectors/feishu/server.d.ts",
  "connectors/index.json",
  "connectors/capability-matrix.json",
  "connectors/feishu/connector.manifest.json",
  "connectors/feishu/README.md",
  "connectors/wechat/README.md",
  "connectors/wecom/README.md",
  "schemas/connector-manifest.schema.json",
  "schemas/connector-event-envelope.schema.json",
  "schemas/connector-action.schema.json",
  "scripts/validate.mjs"
]) {
  if (!files.has(required)) {
    errors.push(`package is missing ${required}`);
  }
}

for (const file of files) {
  if (
    file.startsWith(".github/") ||
    file.startsWith("dist-test/") ||
    file.startsWith("node_modules/") ||
    file.startsWith("src/") ||
    file.startsWith("test/")
  ) {
    errors.push(`package must not include ${file}`);
  }
}

if (!pack.filename?.endsWith(".tgz")) {
  errors.push(`package filename should be a .tgz archive, got ${pack.filename ?? "<empty>"}`);
}

if (errors.length > 0) {
  for (const error of errors) {
    console.error(`package:check: ${error}`);
  }
  process.exit(1);
}

console.log(`package:check validated ${files.size} files in ${pack.filename}`);
