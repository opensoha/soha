import { appendFile, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { basename, dirname, join, relative, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const root = fileURLToPath(new URL("..", import.meta.url));
const packageJson = JSON.parse(await readFile(join(root, "package.json"), "utf8"));
const modulePath = "github.com/opensoha/soha/contracts";
const source = { repository: "opensoha/soha", directory: "contracts" };
const releaseTag = `contracts/v${packageJson.version}`;
const releaseWorkflow = ".github/workflows/contracts-release.yml";
verifyPackageSource(packageJson, "package.json");
const openapiArtifacts = await Promise.all(
  ["openapi/soha-api.yaml", "openapi/soha-api.json"].map(async (path) => {
    const body = await readFile(join(root, path));
    return { path, sha256: createHash("sha256").update(body).digest("hex") };
  }),
);
const outDir = resolve(root, valueArg("--out-dir") ?? "dist/release");
const consumerMatrixPath = optionalPathArg("--consumer-matrix");
const npmCache = process.env.SOHA_NPM_CACHE ?? join(root, "dist/npm-cache");
const releaseSource = process.env.RELEASE_SHA ? checkedReleaseSource() : undefined;
const verifyTarballArg = valueArg("--verify");

if (process.argv.includes("--verify") && !verifyTarballArg) {
  throw new Error("--verify requires a release tarball path");
}

if (verifyTarballArg) {
  const verification = await verifyReleaseArtifacts(resolve(root, verifyTarballArg));
  console.log(`release artifact verified: ${verification.tarball}`);
  console.log(`sha256: ${verification.sha256}`);
  console.log(`package files: ${verification.packageFiles}`);
  process.exit(0);
}

await mkdir(outDir, { recursive: true });
const pack = run("npm", ["pack", "--json", "--pack-destination", outDir, "--cache", npmCache]);
const packInfo = parsePackOutput(pack.stdout);
const tarballPath = join(outDir, packInfo.filename);
const tarball = await readFile(tarballPath);
const sha256 = createHash("sha256").update(tarball).digest("hex");
const checksumPath = `${tarballPath}.sha256`;
const manifestPath = join(outDir, "release-manifest.json");
const sbomPath = join(outDir, `opensoha-contracts-${packageJson.version}.cyclonedx.json`);
const sbomChecksumPath = `${sbomPath}.sha256`;
const provenancePolicyPath = join(outDir, `opensoha-contracts-${packageJson.version}.provenance-policy.json`);
const provenancePolicyChecksumPath = `${provenancePolicyPath}.sha256`;
const consumerMatrixAssetPath = consumerMatrixPath ? join(outDir, "consumer-matrix.json") : undefined;
const consumerMatrixText = consumerMatrixPath ? await readFile(consumerMatrixPath, "utf8") : "";
const consumerMatrix = consumerMatrixText ? JSON.parse(consumerMatrixText) : undefined;
const sbom = generateSBOM();
const sbomText = `${JSON.stringify(sbom, null, 2)}\n`;
const sbomSha256 = createHash("sha256").update(sbomText).digest("hex");
const provenancePolicy = buildProvenancePolicy(packInfo.filename);
const provenancePolicyText = `${JSON.stringify(provenancePolicy, null, 2)}\n`;
const provenancePolicySha256 = createHash("sha256").update(provenancePolicyText).digest("hex");

await writeFile(checksumPath, `${sha256}  ${packInfo.filename}\n`);
await writeFile(sbomPath, sbomText);
await writeFile(sbomChecksumPath, `${sbomSha256}  ${basename(sbomPath)}\n`);
await writeFile(provenancePolicyPath, provenancePolicyText);
await writeFile(provenancePolicyChecksumPath, `${provenancePolicySha256}  ${basename(provenancePolicyPath)}\n`);
if (consumerMatrixAssetPath) {
  await writeFile(consumerMatrixAssetPath, consumerMatrixText);
}
await writeFile(
  manifestPath,
  `${JSON.stringify(
    {
      schemaVersion: 1,
      packageName: packageJson.name,
      version: packageJson.version,
      tag: releaseTag,
      goModule: modulePath,
      source,
      releaseSource,
      npmPackage: packageJson.name,
      openapi: {
        version: packageJson.version,
        artifacts: openapiArtifacts,
      },
      consumerMatrix: consumerMatrix
        ? {
            path: relative(root, consumerMatrixAssetPath),
            checked: consumerMatrix.checked,
            skipped: consumerMatrix.skipped,
            results: consumerMatrix.results?.map((item) => ({
              consumer: item.consumer,
              status: item.status,
            })),
          }
        : undefined,
      artifacts: [
        {
          kind: "npm-tarball",
          name: packInfo.filename,
          path: relative(root, tarballPath),
          checksumFile: relative(root, checksumPath),
          sha256,
          npmShasum: packInfo.shasum,
          npmIntegrity: packInfo.integrity,
          sizeBytes: packInfo.size,
          fileCount: packInfo.entryCount,
        },
        {
          kind: "cyclonedx-sbom",
          name: basename(sbomPath),
          path: relative(root, sbomPath),
          checksumFile: relative(root, sbomChecksumPath),
          sha256: sbomSha256,
          format: sbom.bomFormat,
          specVersion: sbom.specVersion,
          componentPurl: sbom.metadata?.component?.purl,
          componentCount: sbom.components?.length ?? 0,
        },
        {
          kind: "provenance-policy",
          name: basename(provenancePolicyPath),
          path: relative(root, provenancePolicyPath),
          checksumFile: relative(root, provenancePolicyChecksumPath),
          sha256: provenancePolicySha256,
        },
      ],
      sbom: {
        name: basename(sbomPath),
        path: relative(root, sbomPath),
        checksumFile: relative(root, sbomChecksumPath),
        sha256: sbomSha256,
        format: sbom.bomFormat,
        specVersion: sbom.specVersion,
        componentPurl: sbom.metadata?.component?.purl,
        componentCount: sbom.components?.length ?? 0,
      },
      provenance: {
        name: basename(provenancePolicyPath),
        path: relative(root, provenancePolicyPath),
        checksumFile: relative(root, provenancePolicyChecksumPath),
        sha256: provenancePolicySha256,
        workflow: provenancePolicy.workflow.path,
        publishCommand: provenancePolicy.npm.publishCommand,
        requiresIdTokenPermission: provenancePolicy.githubActions.permissions["id-token"] === "write",
        expectedAttestation: provenancePolicy.attestation.expected,
      },
    },
    null,
    2,
  )}\n`,
);

await verifyReleaseArtifacts(tarballPath);
await writeGithubOutputs({
  tarball: tarballPath,
  checksum: checksumPath,
  manifest: manifestPath,
  sbom: sbomPath,
  sbom_checksum: sbomChecksumPath,
  provenance_policy: provenancePolicyPath,
  provenance_policy_checksum: provenancePolicyChecksumPath,
  version: packageJson.version,
});

console.log(`release artifact: ${relative(root, tarballPath)}`);
console.log(`sha256: ${sha256}`);
console.log(`checksum file: ${relative(root, checksumPath)}`);
console.log(`sbom: ${relative(root, sbomPath)}`);
console.log(`sbom sha256: ${sbomSha256}`);
console.log(`provenance policy: ${relative(root, provenancePolicyPath)}`);
console.log(`provenance policy sha256: ${provenancePolicySha256}`);
console.log(`manifest: ${relative(root, manifestPath)}`);

function valueArg(name) {
  const prefix = `${name}=`;
  const value = process.argv.find((arg) => arg.startsWith(prefix));
  if (value) {
    return value.slice(prefix.length);
  }
  const index = process.argv.indexOf(name);
  return index >= 0 ? process.argv[index + 1] : undefined;
}

function parsePackOutput(stdout) {
  let parsed;
  try {
    parsed = JSON.parse(stdout);
  } catch (error) {
    throw new Error(`npm pack did not return JSON output: ${error.message}\n${stdout}`);
  }
  const packInfo = Array.isArray(parsed) ? parsed[0] : parsed;
  if (!packInfo?.filename || !packInfo?.integrity || !packInfo?.shasum) {
    throw new Error(`npm pack JSON is missing expected metadata: ${JSON.stringify(parsed)}`);
  }
  return packInfo;
}

async function verifyReleaseArtifacts(tarballPath) {
  const checksumPath = `${tarballPath}.sha256`;
  const manifestPath = join(dirname(tarballPath), "release-manifest.json");
  const [tarball, checksumText, manifestText] = await Promise.all([
    readFile(tarballPath),
    readFile(checksumPath, "utf8"),
    readFile(manifestPath, "utf8"),
  ]);

  const actualSha256 = createHash("sha256").update(tarball).digest("hex");
  const checksumMatch = checksumText.trim().match(/^([a-f0-9]{64})\s+\*?(.+)$/);
  if (!checksumMatch) {
    throw new Error(`${relative(root, checksumPath)} must contain '<sha256>  <tarball>'`);
  }
  const [, expectedSha256, checksumFilename] = checksumMatch;
  if (expectedSha256 !== actualSha256) {
    throw new Error(`${relative(root, tarballPath)} sha256 mismatch: expected ${expectedSha256}, got ${actualSha256}`);
  }
  if (checksumFilename !== basename(tarballPath)) {
    throw new Error(`${relative(root, checksumPath)} names ${checksumFilename}, expected ${basename(tarballPath)}`);
  }

  const manifest = JSON.parse(manifestText);
  const artifact = manifest.artifacts?.find((item) => item.kind === "npm-tarball") ?? manifest.artifacts?.[0];
  if (manifest.schemaVersion !== 1) {
    throw new Error(`${relative(root, manifestPath)} schemaVersion must be 1`);
  }
  if (manifest.packageName !== packageJson.name || manifest.npmPackage !== packageJson.name) {
    throw new Error(`${relative(root, manifestPath)} package name does not match package.json`);
  }
  if (releaseSource && (manifest.releaseSource?.revision !== releaseSource.revision || manifest.releaseSource?.tree !== releaseSource.tree)) {
    throw new Error("release source revision/tree does not match the checked out candidate");
  }
  if (manifest.version !== packageJson.version || manifest.tag !== releaseTag) {
    throw new Error(`${relative(root, manifestPath)} version/tag does not match package.json`);
  }
  if (manifest.goModule !== modulePath || manifest.source?.repository !== source.repository || manifest.source?.directory !== source.directory) {
    throw new Error(`${relative(root, manifestPath)} SDK source identity does not match the contracts workspace`);
  }
  if (!artifact || artifact.sha256 !== actualSha256 || basename(artifact.name) !== basename(tarballPath)
    || artifact.npmIntegrity !== `sha512-${createHash("sha512").update(tarball).digest("base64")}`) {
    throw new Error(`${relative(root, manifestPath)} artifact metadata does not match tarball`);
  }

  await verifySBOM(manifest, manifestPath, tarballPath);
  await verifyProvenancePolicy(manifest, manifestPath, tarballPath);

  if (manifest.consumerMatrix?.path) {
    const matrixPath = await resolveConsumerMatrixPath(manifest.consumerMatrix.path, tarballPath);
    const matrix = JSON.parse(await readFile(matrixPath, "utf8"));
    if (matrix.contractsVersion !== packageJson.version) {
      throw new Error(`${relative(root, matrixPath)} contractsVersion does not match package.json`);
    }
    if (matrix.checked !== manifest.consumerMatrix.checked || matrix.skipped !== manifest.consumerMatrix.skipped) {
      throw new Error(`${relative(root, manifestPath)} consumer matrix summary does not match ${relative(root, matrixPath)}`);
    }
  }

  const extractDir = await mkdtemp(join(tmpdir(), "soha-contracts-release-"));
  try {
    run("tar", ["-xzf", tarballPath, "-C", extractDir]);
    const packedPackageJson = JSON.parse(await readFile(join(extractDir, "package", "package.json"), "utf8"));
    if (packedPackageJson.name !== packageJson.name || packedPackageJson.version !== packageJson.version) {
      throw new Error(`${relative(root, tarballPath)} packed package.json does not match root package.json`);
    }
    verifyPackageSource(packedPackageJson, "packed package.json");
    const packedGoMod = await readFile(join(extractDir, "package", "go.mod"), "utf8");
    if (!packedGoMod.startsWith(`module ${modulePath}\n`)) {
      throw new Error("packed go.mod does not match the SDK module identity");
    }
    for (const requiredExport of Object.values(packageJson.exports ?? {})) {
      const packedPath = join(extractDir, "package", requiredExport.replace(/^\.\//, ""));
      await readFile(packedPath);
    }
    await verifyPackedOpenAPI(manifest, extractDir);
  } finally {
    await rm(extractDir, { recursive: true, force: true });
  }

  return {
    tarball: relative(root, tarballPath),
    sha256: actualSha256,
    packageFiles: artifact.fileCount,
  };
}

async function verifyPackedOpenAPI(manifest, extractDir) {
  if (manifest.openapi?.version !== packageJson.version || manifest.openapi?.artifacts?.length !== 2) {
    throw new Error("release manifest must include both OpenAPI formats at the package version");
  }
  for (const artifact of manifest.openapi.artifacts) {
    if (!["openapi/soha-api.yaml", "openapi/soha-api.json"].includes(artifact.path)) {
      throw new Error(`release manifest contains unexpected OpenAPI artifact ${artifact.path}`);
    }
    const body = await readFile(join(extractDir, "package", artifact.path));
    const actualSha256 = createHash("sha256").update(body).digest("hex");
    if (actualSha256 !== artifact.sha256) {
      throw new Error(`${artifact.path} sha256 mismatch: expected ${artifact.sha256}, got ${actualSha256}`);
    }
  }
}

async function verifySBOM(manifest, manifestPath, tarballPath) {
  const sbom = manifest.sbom;
  if (!sbom?.path || !sbom?.checksumFile || !sbom?.sha256) {
    throw new Error(`${relative(root, manifestPath)} must include SBOM metadata`);
  }

  const sbomPath = await resolveManifestAssetPath(sbom.path, tarballPath);
  const sbomChecksumPath = await resolveManifestAssetPath(sbom.checksumFile, tarballPath);
  const [sbomText, checksumText] = await Promise.all([
    readFile(sbomPath, "utf8"),
    readFile(sbomChecksumPath, "utf8"),
  ]);
  const actualSha256 = createHash("sha256").update(sbomText).digest("hex");
  if (actualSha256 !== sbom.sha256) {
    throw new Error(`${relative(root, sbomPath)} sha256 mismatch: expected ${sbom.sha256}, got ${actualSha256}`);
  }
  if (checksumText.trim() !== `${actualSha256}  ${basename(sbomPath)}`) {
    throw new Error(`${relative(root, sbomChecksumPath)} does not match SBOM sha256/name`);
  }

  const parsed = JSON.parse(sbomText);
  const component = parsed.metadata?.component;
  if (parsed.bomFormat !== "CycloneDX" || parsed.specVersion !== "1.5") {
    throw new Error(`${relative(root, sbomPath)} must be CycloneDX 1.5`);
  }
  if (component?.version !== packageJson.version || component?.purl !== `pkg:npm/%40opensoha/contracts@${packageJson.version}`) {
    throw new Error(`${relative(root, sbomPath)} component identity does not match package.json`);
  }
  if (!Array.isArray(parsed.components) || parsed.components.length === 0) {
    throw new Error(`${relative(root, sbomPath)} must include dependency components`);
  }
  if (sbom.componentCount !== parsed.components.length) {
    throw new Error(`${relative(root, manifestPath)} SBOM component count does not match ${relative(root, sbomPath)}`);
  }
}

async function verifyProvenancePolicy(manifest, manifestPath, tarballPath) {
  const provenance = manifest.provenance;
  if (!provenance?.path || !provenance?.checksumFile || !provenance?.sha256) {
    throw new Error(`${relative(root, manifestPath)} must include provenance policy metadata`);
  }

  if (provenance.workflow !== releaseWorkflow) {
    throw new Error(`${relative(root, manifestPath)} provenance workflow must identify the Root SDK publisher`);
  }

  const policyPath = await resolveManifestAssetPath(provenance.path, tarballPath);
  const policyChecksumPath = await resolveManifestAssetPath(provenance.checksumFile, tarballPath);
  const [policyText, checksumText] = await Promise.all([
    readFile(policyPath, "utf8"),
    readFile(policyChecksumPath, "utf8"),
  ]);
  const actualSha256 = createHash("sha256").update(policyText).digest("hex");
  if (actualSha256 !== provenance.sha256) {
    throw new Error(`${relative(root, policyPath)} sha256 mismatch: expected ${provenance.sha256}, got ${actualSha256}`);
  }
  if (checksumText.trim() !== `${actualSha256}  ${basename(policyPath)}`) {
    throw new Error(`${relative(root, policyChecksumPath)} does not match provenance policy sha256/name`);
  }

  const policy = JSON.parse(policyText);
  if (policy.schemaVersion !== 1) {
    throw new Error(`${relative(root, policyPath)} schemaVersion must be 1`);
  }
  if (policy.packageName !== packageJson.name || policy.version !== packageJson.version || policy.tag !== releaseTag) {
    throw new Error(`${relative(root, policyPath)} package identity does not match package.json`);
  }
  if (policy.source?.repository !== source.repository || policy.source?.directory !== source.directory || policy.workflow?.path !== releaseWorkflow || policy.workflow?.trigger !== "push tags matching contracts/v*") {
    throw new Error(`${relative(root, policyPath)} source/workflow must identify the Root SDK publisher`);
  }
  if (policy.githubActions?.permissions?.["id-token"] !== "write") {
    throw new Error(`${relative(root, policyPath)} must require GitHub Actions id-token: write`);
  }
  if (policy.npm?.authentication !== "GitHub Actions OIDC trusted publishing" || policy.npm?.environment !== "release-contracts" || policy.npm?.requiresTokenSecret) {
    throw new Error("Root SDK publisher must use protected OIDC trusted publishing without a token fallback");
  }
  if (policy.npm?.publishCommand !== `npm stage publish "${basename(tarballPath)}" --provenance --access public`) {
    throw new Error(`${relative(root, policyPath)} npm publish command does not match the tarball`);
  }
  if (policy.attestation?.expected !== "npm registry provenance generated by npm stage publish --provenance") {
    throw new Error(`${relative(root, policyPath)} must document the expected npm provenance attestation`);
  }
}

async function writeGithubOutputs(outputs) {
  if (!process.env.GITHUB_OUTPUT) {
    return;
  }
  const lines = Object.entries(outputs).map(([key, value]) => `${key}=${value}`);
  await appendFile(process.env.GITHUB_OUTPUT, `${lines.join("\n")}\n`);
}

function run(command, args) {
  const result = spawnSync(command, args, {
    cwd: root,
    env: {
      ...process.env,
      npm_config_cache: npmCache,
    },
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
    stdio: ["ignore", "pipe", "pipe"],
  });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed\n${result.stdout}${result.stderr}`);
  }
  if (result.stderr.trim()) {
    console.error(result.stderr.trim());
  }
  return result;
}

function generateSBOM() {
  const result = run("npm", ["sbom", "--workspace", packageJson.name, "--package-lock-only", "--sbom-format", "cyclonedx", "--sbom-type", "library"]);
  let sbom;
  try {
    sbom = JSON.parse(result.stdout);
  } catch (error) {
    throw new Error(`npm sbom did not return JSON output: ${error.message}\n${result.stdout}`);
  }
  const sdkPurl = `pkg:npm/%40opensoha/contracts@${packageJson.version}`;
  const candidates = sbom.components?.filter((item) => item.purl?.startsWith("pkg:npm/%40opensoha/contracts@") || item["bom-ref"]?.startsWith(`${packageJson.name}@`)) ?? [];
  const sdk = candidates[0];
  const rootRef = sbom.metadata?.component?.["bom-ref"];
  if (candidates.length !== 1 || sdk.purl !== sdkPurl || sdk.version !== packageJson.version || sdk.name !== "contracts" || sdk["bom-ref"] !== `${packageJson.name}@${packageJson.version}` || typeof rootRef !== "string" || rootRef === sdk["bom-ref"]) {
    throw new Error("npm SBOM must contain exactly one SDK workspace component at the package identity");
  }
  if (!Array.isArray(sbom.dependencies)) throw new Error("npm SBOM dependency graph is missing");
  const included = new Set();
  const components = [];
  const dependencies = [];
  const pending = [sdk["bom-ref"]];
  for (let index = 0; index < pending.length; index++) {
    const ref = pending[index];
    if (ref === rootRef) throw new Error("SDK SBOM must not depend on the Root synthetic component");
    if (included.has(ref)) continue;
    const nodes = sbom.components.filter((item) => item["bom-ref"] === ref);
    const edges = sbom.dependencies.filter((item) => item.ref === ref);
    if (!nodes.length || !edges.length) throw new Error(`npm SBOM has missing dependency ${ref}`);
    const identity = (item) => JSON.stringify({ ...item, properties: (item.properties ?? []).filter((property) => property.name !== "cdx:npm:package:path") });
    const expectedIdentity = identity(nodes[0]);
    if (nodes.some((item) => identity(item) !== expectedIdentity)) throw new Error(`npm SBOM has conflicting artifact identity ${ref}`);
    const properties = new Map();
    for (const item of nodes) for (const property of item.properties ?? []) properties.set(JSON.stringify(property), property);
    if (ref !== sdk["bom-ref"]) components.push({ ...nodes[0], properties: [...properties.values()] });
    const dependsOn = [...new Set(edges.flatMap((item) => item.dependsOn ?? []))];
    dependencies.push({ ...edges[0], dependsOn });
    included.add(ref);
    pending.push(...dependsOn);
  }
  // Keep npm's actual SDK dependency graph; its workspace output still includes Root.
  sbom.metadata.component = sdk;
  sbom.components = components;
  sbom.dependencies = dependencies;
  return sbom;
}

function buildProvenancePolicy(tarballName) {
  return {
    schemaVersion: 1,
    packageName: packageJson.name,
    version: packageJson.version,
    tag: releaseTag,
    source,
    workflow: {
      path: releaseWorkflow,
      trigger: "push tags matching contracts/v*",
      requiredJobs: ["identity", "verify", "artifacts", "release"],
    },
    githubActions: {
      runner: "ubuntu-latest",
      permissions: {
        actions: "read",
        contents: "write",
        "id-token": "write",
      },
    },
    npm: {
      package: packageJson.name,
      registry: "https://registry.npmjs.org",
      publishCommand: `npm stage publish "${tarballName}" --provenance --access public`,
      authentication: "GitHub Actions OIDC trusted publishing",
      environment: "release-contracts",
      requiresProvenance: true,
    },
    attestation: {
      expected: "npm registry provenance generated by npm stage publish --provenance",
      localArtifactsDoNotProveRemoteAttestation: true,
    },
    localVerification: {
      command: `npm run release:verify --workspace contracts -- dist/release/${tarballName}`,
      checks: [
        "tarball checksum",
        "packed package identity",
        "exported package entrypoints",
        "CycloneDX SBOM and checksum",
        "provenance policy and checksum",
        "consumer matrix summary when supplied",
      ],
    },
    remoteVerification: {
      npm: `npm view "${packageJson.name}@${packageJson.version}" version dist.tarball dist.integrity --json`,
      goModule: `go list -m -json "${modulePath}@v${packageJson.version}"`,
      githubReleaseAssets: "gh release download <tag> and rerun release:verify against downloaded assets",
    },
  };
}

function optionalPathArg(name) {
  const value = valueArg(name);
  return value ? resolve(root, value) : undefined;
}

async function resolveConsumerMatrixPath(matrixPath, tarballPath) {
  return resolveManifestAssetPath(matrixPath, tarballPath);
}

async function resolveManifestAssetPath(assetPath, tarballPath) {
  const fromTarballDirectory = join(dirname(tarballPath), basename(assetPath));
  try {
    await readFile(fromTarballDirectory);
    return fromTarballDirectory;
  } catch {
    // Fall through to the manifest-relative path for local release directories.
  }

  const fromManifest = resolve(root, assetPath);
  try {
    await readFile(fromManifest);
    return fromManifest;
  } catch {
    return fromTarballDirectory;
  }
}

function verifyPackageSource(metadata, label) {
  if (metadata.repository?.type !== "git" || metadata.repository?.url !== `https://github.com/${source.repository}` || metadata.repository?.directory !== source.directory) {
    throw new Error(`${label} repository must identify ${source.repository} ${source.directory}/`);
  }
}

function checkedReleaseSource() {
  const revision = process.env.RELEASE_SHA;
  if (!/^[0-9a-f]{40}$/.test(revision) || run("git", ["rev-parse", "HEAD"]).stdout.trim() !== revision) {
    throw new Error("RELEASE_SHA must match the exact checked out source revision");
  }
  return { revision, tree: run("git", ["rev-parse", `${revision}:contracts`]).stdout.trim() };
}
