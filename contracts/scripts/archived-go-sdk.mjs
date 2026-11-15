import { createHash } from "node:crypto";
import { lstatSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

// Preserve the fixed external consumer's exact module bytes and checksum identity.
export function archivedGoProxy(root, module, version, fallback = process.env.GOPROXY || "https://proxy.golang.org,direct") {
  const manifest = join(root, ".github/legacy-go-sdk.json");
  if (!lstatSync(manifest).isFile() || lstatSync(manifest).isSymbolicLink()) {
    throw new Error("Archived SDK manifest must be a regular source file");
  }
  const metadata = JSON.parse(readFileSync(manifest, "utf8"));
  if (metadata.schemaVersion !== 1 || module !== "github.com/opensoha/soha-contracts"
      || metadata.module !== module || metadata.version !== version
      || !/^v\d+\.\d+\.\d+-0\.\d{14}-[a-f0-9]{12}$/.test(version)
      || !/^h1:[A-Za-z0-9+/]{43}=$/.test(metadata.sum)
      || !/^h1:[A-Za-z0-9+/]{43}=$/.test(metadata.goModSum)
      || typeof fallback !== "string" || /[\r\n]/.test(fallback)) {
    throw new Error("Declared SDK does not match the archived module identity");
  }
  const base = join(root, ".github/legacy-goproxy");
  const directory = join(base, module, "@v");
  for (const name of [".github", ".github/legacy-goproxy", ".github/legacy-goproxy/github.com",
    ".github/legacy-goproxy/github.com/opensoha", ".github/legacy-goproxy/github.com/opensoha/soha-contracts",
    ".github/legacy-goproxy/github.com/opensoha/soha-contracts/@v"]) {
    if (!lstatSync(join(root, name)).isDirectory() || lstatSync(join(root, name)).isSymbolicLink()) {
      throw new Error("Archived SDK directory must be local regular source");
    }
  }
  for (const extension of ["zip", "mod", "info"]) {
    const name = `${version}.${extension}`;
    const file = join(directory, name);
    const expected = metadata.files?.[name];
    if (!/^[a-f0-9]{64}$/.test(expected || "") || !lstatSync(file).isFile() || lstatSync(file).isSymbolicLink()
        || createHash("sha256").update(readFileSync(file)).digest("hex") !== expected) {
      throw new Error("Archived SDK file is missing or its checksum differs");
    }
  }
  return `${pathToFileURL(base).href},${fallback}`;
}
