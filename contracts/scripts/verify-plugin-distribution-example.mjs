import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";

const contractsRoot = new URL("../", import.meta.url);
const exampleRoot = new URL("examples/plugin-distribution/", contractsRoot);
const catalogLocation = new URL("marketplace/index.json", exampleRoot);
const readJSON = async (location) => JSON.parse(await readFile(location, "utf8"));
const loadSchema = (name) => readJSON(new URL(`plugins/${name}.schema.json`, contractsRoot));

const validator = new Ajv2020({ allErrors: true, strict: false });
addFormats(validator);
validator.addSchema(await loadSchema("companion-pack"));
const validateManifest = validator.compile(await loadSchema("plugin-manifest"));
const validateCatalog = validator.compile(await loadSchema("marketplace-catalog"));

function assertValid(validate, document, label) {
  assert.ok(validate(document), `${label}: ${validator.errorsText(validate.errors)}`);
}

const rootManifestBytes = await readFile(new URL("plugin.manifest.json", exampleRoot));
const rootManifest = JSON.parse(rootManifestBytes);
assertValid(validateManifest, rootManifest, "repository root manifest");
assert.equal(rootManifest.runtime.mode, "manifest-only");
assert.equal(rootManifest.type, "skill-pack");
assert.equal(rootManifest.assets, undefined, "sample must not require assets");
assert.deepEqual(Object.keys(rootManifest.extensionPoints), ["ai"]);
assert.deepEqual(Object.keys(rootManifest.extensionPoints.ai), ["skillPacks"]);
assert.equal(rootManifest.extensionPoints.ai.skillPacks.length, 1);

const catalog = await readJSON(catalogLocation);
assertValid(validateCatalog, catalog, "catalog");
assert.equal(catalog.sourceId, "opensoha-local", "sample must not claim the official source ID");
assert.equal(catalog.plugins.length, 1);
const [plugin] = catalog.plugins;
assert.equal(plugin.versions.length, 1);
const [version] = plugin.versions;
assert.match(rootManifest.id, /^[a-z0-9]+(?:[.-][a-z0-9]+)+$/);
assert.match(rootManifest.version, /^[0-9]+\.[0-9]+\.[0-9]+$/);
assert.equal(version.manifestUrl, `plugins/${rootManifest.id}/${rootManifest.version}/plugin.manifest.json`);
assert.equal(plugin.latestVersion, rootManifest.version);
assert.equal(version.version, rootManifest.version);
for (const key of ["id", "name", "version", "publisher", "type"]) {
  assert.equal(plugin[key], rootManifest[key], `catalog ${key} must match the manifest`);
}
assert.deepEqual(plugin.manifest, rootManifest, "inline catalog manifest must match the repository manifest");

const snapshotBytes = await readFile(new URL(version.manifestUrl, catalogLocation));
const checksum = `sha256:${createHash("sha256").update(snapshotBytes).digest("hex")}`;
assert.equal(version.checksum, checksum, "catalog checksum must cover raw snapshot bytes");
assert.ok(snapshotBytes.equals(rootManifestBytes), "version snapshot must match the root manifest byte for byte");

console.log(`Verified ${rootManifest.id}@${version.version} (${checksum})`);
