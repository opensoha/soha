import { createHash } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";

const root = new URL("..", import.meta.url);
const catalogURL = new URL("auth/permission-catalog.json", root);
const goURL = new URL("auth/catalog_generated.go", root);
const checkOnly = process.argv.includes("--check");

const catalog = JSON.parse(await readFile(catalogURL, "utf8"));
const keys = catalog.permissions.map((permission) => permission.key);
if (new Set(keys).size !== keys.length) {
  throw new Error("permission catalog contains duplicate keys");
}
if ([...keys].sort().some((key, index) => key !== keys[index])) {
  throw new Error("permission catalog keys must be sorted");
}

const contentHash = `sha256:${sha256(JSON.stringify(catalog.permissions))}`;
const nextCatalog = { ...catalog, contentHash };
const catalogText = `${JSON.stringify(nextCatalog, null, 2)}\n`;
const goText = `// Code generated from OpenSoha permission contracts. DO NOT EDIT.

package auth

import _ "embed"

const (
	PermissionCatalogVersion = ${JSON.stringify(catalog.catalogVersion)}
	PermissionCatalogContentSHA256 = ${JSON.stringify(contentHash.slice("sha256:".length))}
	PermissionCatalogSHA256 = ${JSON.stringify(sha256(catalogText))}
)

//go:embed permission-catalog.json
var permissionCatalogJSON []byte

// PermissionCatalogJSON returns a copy of the canonical permission catalog.
func PermissionCatalogJSON() []byte { return append([]byte(nil), permissionCatalogJSON...) }
`;

if (checkOnly) {
  const currentCatalog = await readFile(catalogURL, "utf8");
  const currentGo = await readFile(goURL, "utf8").catch(() => "");
  if (currentCatalog !== catalogText || currentGo !== goText) {
    throw new Error("permission catalog artifacts are out of date; run npm run generate:permissions");
  }
} else {
  await writeFile(catalogURL, catalogText);
  await writeFile(goURL, goText);
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}
