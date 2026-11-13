import { readFile } from "node:fs/promises";
import { parse } from "yaml";

const packagePath = "package.json";
const packageLockPath = "../package-lock.json";
const openapiPath = "openapi/soha-api.yaml";
const openapiJsonPath = "openapi/soha-api.json";
const goModPath = "go.mod";
const goClientPath = "gen/go/sohaapi/client.go";
const modulePath = "github.com/opensoha/soha/contracts";

const packageJson = JSON.parse(await readFile(packagePath, "utf8"));
const packageLock = JSON.parse(await readFile(packageLockPath, "utf8"));
const openapi = parse(await readFile(openapiPath, "utf8"));
const openapiJson = JSON.parse(await readFile(openapiJsonPath, "utf8"));
const goMod = await readFile(goModPath, "utf8");
const goClient = await readFile(goClientPath, "utf8");

const version = packageJson.version;
const errors = [];

expect(/^0\.1\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(version), `${packagePath} version must stay on the 0.1.x stream: ${version}`);
expect(openapi?.info?.version === version, `${openapiPath} info.version (${openapi?.info?.version}) must match ${packagePath} version (${version})`);
expect(openapiJson?.info?.version === version, `${openapiJsonPath} info.version (${openapiJson?.info?.version}) must match ${packagePath} version (${version})`);
expect(packageJson.name === "@opensoha/contracts", `${packagePath} must name @opensoha/contracts`);
expect(
  packageLock.packages?.contracts?.name === packageJson.name && packageLock.packages?.contracts?.version === version,
  `${packageLockPath} packages["contracts"] must match SDK name ${packageJson.name} and version ${version}`,
);
expect(packageJson.repository?.type === "git" && packageJson.repository?.url === "https://github.com/opensoha/soha" && packageJson.repository?.directory === "contracts", `${packagePath} repository must identify opensoha/soha contracts/`);
expect(goMod.startsWith(`module ${modulePath}\n`), `${goModPath} must declare module ${modulePath}`);
expect(
  goClient.includes(`const defaultUserAgent = "opensoha-contracts/${version}"`),
  `${goClientPath} defaultUserAgent must include the exact contract version ${version}`,
);

const releaseTag = currentReleaseTag();
if (releaseTag) {
  expect(releaseTag === `contracts/v${version}`, `release tag ${releaseTag} must match version contracts/v${version}`);
}

if (errors.length > 0) {
  for (const error of errors) {
    console.error(`error: ${error}`);
  }
  throw new Error(`version synchronization failed with ${errors.length} error(s)`);
}

const tagText = releaseTag ? ` and release tag ${releaseTag}` : "";
console.log(`version sync ok: npm/OpenAPI YAML+JSON/root workspace lock/Go module/Go client all use ${version}${tagText}`);

function currentReleaseTag() {
  const refName = process.env.GITHUB_REF_NAME?.trim();
  if (process.env.GITHUB_REF_TYPE === "tag" && refName) {
    return refName.startsWith("contracts/v") ? refName : "";
  }

  const ref = process.env.GITHUB_REF?.trim();
  if (ref?.startsWith("refs/tags/")) {
    const tag = ref.slice("refs/tags/".length);
    return tag.startsWith("contracts/v") ? tag : "";
  }

  return "";
}

function expect(condition, message) {
  if (!condition) {
    errors.push(message);
  }
}
