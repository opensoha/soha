import { cpSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createRequire } from "node:module";
import { spawnSync } from "node:child_process";

const require = createRequire(import.meta.url);
const directory = mkdtempSync(join(process.env.OPENSOHA_VERIFY_DIR || tmpdir(), "connectors-test-"));
const output = join(directory, "dist-test");
const run = (args) => {
  const result = spawnSync(process.execPath, args, { stdio: "inherit" });
  if (result.error) throw result.error;
  return result.status ?? 1;
};

try {
  cpSync("package.json", join(directory, "package.json"));
  let code = run([require.resolve("typescript/bin/tsc"), "-p", "tsconfig.test.json", "--outDir", output]);
  if (code === 0) {
    cpSync("schemas", join(directory, "schemas"), { recursive: true });
    code = run(["--test", join(output, "**/*.test.js")]);
  }
  process.exitCode = code;
} finally {
  if (process.env.OPENSOHA_KEEP_VERIFY_ARTIFACTS !== "1") rmSync(directory, { recursive: true, force: true });
}
