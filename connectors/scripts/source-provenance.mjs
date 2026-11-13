import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, lstatSync, readFileSync, readlinkSync, realpathSync } from "node:fs";
import path from "node:path";

export const normalizeSourcePath = (relative, separator = path.sep) => relative.split(separator).join("/");

// Bind local packaging to its real source, without consulting credential-bearing remotes.
export function sourceProvenance(componentRoot, outputPaths = []) {
  componentRoot = realpathSync(componentRoot);
  const git = (...args) => execFileSync("git", ["-C", componentRoot, ...args], {
    encoding: "utf8", env: { ...process.env, GIT_OPTIONAL_LOCKS: "0" }
  }).trimEnd();
  const top = git("rev-parse", "--show-toplevel");
  const sourceDirectory = normalizeSourcePath(path.relative(top, componentRoot)) || ".";
  if (JSON.parse(readFileSync(path.join(componentRoot, "package.json"), "utf8")).name !== "@opensoha/connectors") {
    throw new Error("invalid connector package identity");
  }
  let sourceRepository;
  if (sourceDirectory === "." && !existsSync(path.join(top, "go.mod"))) {
    sourceRepository = "github.com/opensoha/soha-connectors";
  } else if (sourceDirectory === "connectors" &&
      /^module github\.com\/opensoha\/soha$/m.test(readFileSync(path.join(top, "go.mod"), "utf8"))) {
    sourceRepository = "github.com/opensoha/soha";
  } else {
    throw new Error("unrecognized connector source layout");
  }
  const sourceCommit = git("rev-parse", "HEAD");
  if (!/^[a-f0-9]{40}$/.test(sourceCommit)) throw new Error("invalid source commit");
  const excluded = new Set(outputPaths.map(file => normalizeSourcePath(path.relative(componentRoot, path.join(realpathSync(path.dirname(path.resolve(file))), path.basename(file))))));
  const names = [...new Set(git("ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", ".")
    .split("\0").filter(name => name && !excluded.has(name)))].sort();
  const hash = createHash("sha256");
  for (const name of names) {
    const file = path.join(componentRoot, name);
    let stat;
    try { stat = lstatSync(file); } catch (error) {
      if (error.code !== "ENOENT") throw error;
      hash.update(JSON.stringify([name, "deleted"]) + "\n");
      continue;
    }
    const mode = stat.isSymbolicLink() ? "120000" : stat.isFile() ? (stat.mode & 0o111 ? "100755" : "100644") : null;
    if (!mode) throw new Error(`unsupported source entry ${name}`);
    const bytes = stat.isSymbolicLink() ? Buffer.from(readlinkSync(file)) : readFileSync(file);
    hash.update(JSON.stringify([name, mode, createHash("sha256").update(bytes).digest("hex")]) + "\n");
  }
  const modified = [
    ...git("ls-files", "-z", "--modified", "--deleted", "--others", "--exclude-standard", "--", ".").split("\0"),
    ...git("diff", "--cached", "--name-only", "-z", "--relative", "--no-renames", "--", ".").split("\0")
  ].some(name => name && !excluded.has(name));
  return { sourceRepository, sourceCommit, sourceDirectory, sourceSha256: hash.digest("hex"), sourceModified: modified };
}
