import { createHash } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import { existsSync, lstatSync, mkdirSync, readFileSync, readlinkSync, realpathSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const contractsRoot = resolve(fileURLToPath(new URL("..", import.meta.url)));
const workspaceRoot = dirname(contractsRoot);
const contractsTarballPlaceholder = "<contracts-tarball>";

const consumers = [
  {
    name: "soha",
    path: "soha",
    kind: "go",
    commands: [["go", ["test", "./..."]]],
  },
  {
    name: "soha-web",
    path: "soha-web",
    kind: "npm",
    commands: npmCommands(),
  },
  {
    name: "soha-cli",
    path: "soha-cli",
    kind: "go",
    commands: [["go", ["test", "./..."]]],
  },
  {
    name: "soha-app",
    path: "soha-app/frontend",
    kind: "npm",
    commands: npmCommands(),
  },
  {
    name: "soha-agent",
    path: "soha-agent",
    kind: "go",
    commands: [["go", ["test", "./..."]]],
  },
  {
    name: "soha-skills",
    path: "soha-skills",
    kind: "python",
    commands: [["python3", ["tools/validate_assets.py"]]],
  },
];

const args = process.argv.slice(2);
const requireAll = args.includes("--internal-source") || args.includes("--require-all") || process.env.SOHA_CONSUMER_REQUIRE_ALL === "1";
const dryRun = args.includes("--dry-run");
const declaredSDK = args.includes("--declared");
const internalSource = args.includes("--internal-source");
const consumerRoot = internalSource ? workspaceRoot : resolve(process.env.SOHA_CONSUMER_ROOT ?? dirname(contractsRoot));
const availableConsumers = internalSource ? [
  { name: "soha", path: ".", kind: "go", commands: [["go", ["test", "./..."]]] },
  { name: "soha-web", path: "web", kind: "npm", commands: [["npm", ["run", "build", "--workspace", "web"]]] },
] : consumers;
const listOnly = args.includes("--list");
const outputPath = valueArg("--out");
const expectedContracts = { head: valueArg("--contracts-sha"), sourceHash: valueArg("--contracts-source-hash") };
const selected = selectedConsumers(args);
if (internalSource && declaredSDK) throw new Error("--internal-source cannot be combined with --declared");

if (listOnly) {
  console.log(availableConsumers.map((consumer) => consumer.name).join("\n"));
  process.exit(0);
}

const targets = selected.length > 0 ? availableConsumers.filter((consumer) => selected.includes(consumer.name)) : availableConsumers;
const unknown = selected.filter((name) => !availableConsumers.some((consumer) => consumer.name === name));
if (unknown.length > 0) {
  throw new Error(`unknown consumer(s): ${unknown.join(", ")}`);
}

let checked = 0;
let skipped = 0;
let failed = 0;
const contractsRevision = revision(contractsRoot);
const results = [];
try {
  verifyContractsIdentity();
  if (internalSource && process.env.SOHA_CONSUMER_ROOT && realpathSync(process.env.SOHA_CONSUMER_ROOT) !== realpathSync(workspaceRoot)) {
    throw new Error("internal-source consumer root must be this workspace");
  }
} catch (error) {
  console.error(error.message);
  failed = 1;
  writeReport(error.message);
  process.exit(1);
}
for (const target of targets) {
  let env = process.env;
  let packageDir;
  let ownedGoWorkspace;
  let manifests;
  let entry;
  try {
    let consumer = declaredSDK ? { ...target, commands: target.commands.filter(([, values]) => !values.includes(contractsTarballPlaceholder)) } : target;
    if (!internalSource && !declaredSDK && target.name === "soha-skills") consumer = {
      ...consumer, commands: [["python3", ["tools/validate_assets.py",
        "--contracts-skill-schema", join(contractsRoot, "skills/skill-manifest.schema.json"),
        "--contracts-mcp-preset-schema", join(contractsRoot, "presets/mcp-preset.schema.json"),
        "--contracts-agent-profile-schema", join(contractsRoot, "profiles/agent-profile.schema.json"),
        "--contracts-permission-catalog", join(contractsRoot, "auth/permission-catalog.json"),
      ]]],
    };
    const dir = resolve(consumerRoot, consumer.path);
    if (!existsSync(dir)) {
      const message = `consumer ${consumer.name} not found at ${dir}`;
      if (requireAll || selected.includes(consumer.name)) {
        throw new Error(message);
      }
      console.warn(`warning: ${message}; skipping`);
      results.push({
        consumer: consumer.name,
        commands: consumer.commands,
        path: dir,
        reason: "missing checkout",
        status: "skipped",
      });
      skipped += 1;
      continue;
    }

    if (!internalSource && existsSync(join(workspaceRoot, "package.json"))) {
      const workspaces = JSON.parse(readFileSync(join(workspaceRoot, "package.json"), "utf8")).workspaces;
      if (Array.isArray(workspaces) && workspaces.includes("contracts") && workspaces.includes("web")) {
        const path = relative(realpathSync(workspaceRoot), realpathSync(dir));
        if (path === "" || (path !== ".." && !path.startsWith(`..${sep}`) && !isAbsolute(path))) {
          throw new Error("external consumer checkout must be outside the source npm workspace");
        }
      }
    }
    entry = { consumer: consumer.name, path: dir, commands: consumer.commands, revision: revision(dir) };
    if (dryRun) {
      console.log(`[dry-run] ${consumer.name}: ${dir}`);
      for (const [command, commandArgs] of consumer.commands) {
        console.log(`[dry-run]   ${command} ${commandArgs.join(" ")}`);
      }
      results.push({
        ...entry,
        status: "dry-run",
      });
      checked += 1;
      continue;
    }

    console.log(`checking consumer ${consumer.name} in ${dir}`);
    if (internalSource) {
      env = { ...process.env, GOWORK: join(workspaceRoot, "go.work"), GOFLAGS: "-mod=readonly" };
      run("python3", [join(workspaceRoot, "scripts/source-workspace.py"), consumer.kind === "go" ? "check-go" : "check-npm"], workspaceRoot, env);
    } else if (consumer.kind === "go") {
      if (declaredSDK) env = { ...process.env, GOWORK: "off", GOFLAGS: "-mod=readonly" };
      else {
        const temporary = await goWorkspaceEnv(dir);
        env = temporary.env;
        ownedGoWorkspace = temporary.directory;
      }
    }
    if (declaredSDK && consumer.kind === "go") verifyDeclaredGoSDK(dir, env);
    packageDir = consumer.kind === "npm" && !declaredSDK && !internalSource ? await packContracts() : undefined;
    if (packageDir) manifests = ["package.json", "package-lock.json"].map((name) => ({ path: join(dir, name), contents: readFileSync(join(dir, name)) }));
    for (const [command, commandArgs] of consumer.commands) {
      const resolvedArgs = commandArgs.map((arg) =>
        arg === contractsTarballPlaceholder ? contractsTarball(packageDir) : arg,
      );
      run(command, resolvedArgs, internalSource && consumer.kind === "npm" ? workspaceRoot : dir, env);
    }
    results.push({
      ...entry,
      status: "passed",
    });
    checked += 1;
  } catch (error) {
    console.error(error.message);
    results.push({ ...entry, consumer: target.name, status: "failed", reason: error.message });
    failed += 1;
  } finally {
    if (manifests) for (const file of manifests) writeFileSync(file.path, file.contents);
    if (process.env.OPENSOHA_KEEP_VERIFY_ARTIFACTS !== "1") {
      if (ownedGoWorkspace) await rm(ownedGoWorkspace, { recursive: true, force: true });
      if (packageDir) await rm(packageDir, { recursive: true, force: true });
    }
  }
}

if (requireAll && checked !== targets.length) {
  console.error(`consumer matrix checked ${checked}/${targets.length} required consumer(s)`);
  process.exitCode = 1;
}

console.log(`consumer matrix complete: checked ${checked}, skipped ${skipped}, root ${consumerRoot}`);

writeReport();

if (failed) process.exitCode = 1;

function writeReport(identityError) {
  if (!outputPath) return;
  const resolvedOutputPath = resolve(outputPath);
  mkdirSync(dirname(resolvedOutputPath), { recursive: true });
  writeFileSync(
    resolvedOutputPath,
    `${JSON.stringify(
      {
        schemaVersion: 1,
        dependencyMode: internalSource ? "internal-source" : declaredSDK ? "declared" : "candidate-source",
        ...(internalSource ? { workspaceRoot, workspaceInputs: internalInputs() } : {}),
        contractsVersion: packageVersion(),
        contractsRevision,
        expectedContracts,
        ...(identityError ? { identityError } : {}),
        contractsRoot,
        consumerRoot,
        requireAll,
        dryRun,
        status: failed ? "failed" : dryRun ? "dry-run" : skipped ? "incomplete" : "passed",
        checked,
        skipped,
        failed,
        results,
      },
      null,
      2,
    )}\n`,
  );
}

function verifyContractsIdentity() {
  const { head, sourceHash } = expectedContracts;
  if (head === undefined && sourceHash === undefined) return;
  if (!head || !/^[0-9a-f]{40}$/.test(head)) throw new Error("--contracts-sha must be a full lowercase 40-character commit SHA");
  if (sourceHash !== undefined && !/^[0-9a-f]{64}$/.test(sourceHash)) throw new Error("--contracts-source-hash must be a SHA-256 digest");
  if (!contractsRevision || contractsRevision.head !== head) throw new Error("contracts candidate SHA mismatch");
  if (sourceHash !== undefined ? contractsRevision.sourceHash !== sourceHash : contractsRevision.files.length > 0) {
    throw new Error(sourceHash === undefined ? "contracts candidate has uncommitted source; bind --contracts-source-hash as well" : "contracts candidate source hash mismatch");
  }
}

function npmCommands() {
  return [
    ["npm", ["install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund", contractsTarballPlaceholder]],
    ["npm", ["ci"]],
    ["npm", ["run", "build"]],
  ];
}

// Include uncommitted source: a HEAD alone does not identify a candidate worktree.
function revision(dir) {
  const head = spawnSync("git", ["-C", dir, "rev-parse", "HEAD"], { encoding: "utf8" });
  if (head.status !== 0) return null;
  const root = spawnSync("git", ["-C", dir, "rev-parse", "--show-toplevel"], { encoding: "utf8" }).stdout.trim();
  const git = (args) => spawnSync("git", ["-C", root, ...args], { encoding: "utf8" });
  const tracked = git(["-c", "core.filemode=true", "diff", "HEAD", "--name-only", "-z"]);
  const untracked = git(["ls-files", "--others", "--exclude-standard", "-z"]);
  if (tracked.status !== 0 || untracked.status !== 0) throw new Error(`cannot identify candidate source at ${dir}`);
  const paths = tracked.stdout + untracked.stdout;
  const files = [...new Set(paths.split("\0").filter(Boolean))].sort().map((path) => {
    const file = join(root, path);
    const stat = lstatSync(file, { throwIfNoEntry: false });
    if (!stat) return { path, type: "deleted", mode: null, sha256: null };
    const symlink = stat.isSymbolicLink();
    if (!symlink && !stat.isFile()) throw new Error(`unsupported candidate source entry: ${path}`);
    return {
      path,
      type: symlink ? "symlink" : "file",
      mode: symlink ? "120000" : stat.mode & 0o100 ? "100755" : "100644",
      sha256: createHash("sha256").update(symlink ? readlinkSync(file, { encoding: "buffer" }) : readFileSync(file)).digest("hex"),
    };
  });
  return { head: head.stdout.trim(), files, sourceHash: createHash("sha256").update(JSON.stringify({ head: head.stdout.trim(), files })).digest("hex") };
}

function selectedConsumers(argv) {
  const names = [];
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i] === "--consumer" && argv[i + 1]) {
      names.push(argv[i + 1]);
      i += 1;
    }
  }
  if (process.env.SOHA_CONSUMER) {
    names.push(...process.env.SOHA_CONSUMER.split(",").map((name) => name.trim()).filter(Boolean));
  }
  return [...new Set(names)];
}

function valueArg(name) {
  const prefix = `${name}=`;
  const value = args.find((arg) => arg.startsWith(prefix));
  if (value) {
    return value.slice(prefix.length);
  }
  const index = args.indexOf(name);
  if (index < 0) return undefined;
  if (!args[index + 1] || args[index + 1].startsWith("--")) throw new Error(`${name} requires a value`);
  return args[index + 1];
}

function packageVersion() {
  const packageJson = JSON.parse(readFileSync(join(contractsRoot, "package.json"), "utf8"));
  return packageJson.version;
}

async function packContracts() {
  const packageDir = await mkdtemp(join(tmpdir(), "opensoha-contracts-pack-"));
  try {
    run("npm", ["pack", "--silent", "--pack-destination", packageDir], contractsRoot, process.env);
    return packageDir;
  } catch (error) {
    if (process.env.OPENSOHA_KEEP_VERIFY_ARTIFACTS !== "1") await rm(packageDir, { recursive: true, force: true });
    throw error;
  }
}

function contractsTarball(packageDir) {
  return join(packageDir, `opensoha-contracts-${packageVersion()}.tgz`);
}

async function goWorkspaceEnv(consumerDir) {
  consumerDir = realpathSync(consumerDir);
  const workDir = await mkdtemp(join(tmpdir(), "opensoha-consumer-work-"));
  const goWork = join(workDir, "go.work");
  const env = { ...process.env, GOWORK: goWork };
  try {
    run("go", ["work", "init", consumerDir], workDir, { ...process.env, GOWORK: "off" });
    run("go", ["work", "edit", `-replace=github.com/opensoha/soha-contracts=${contractsRoot}`], workDir, env);
    return { env, directory: workDir };
  } catch (error) {
    if (process.env.OPENSOHA_KEEP_VERIFY_ARTIFACTS !== "1") await rm(workDir, { recursive: true, force: true });
    throw error;
  }
}

function run(command, args, cwd, env) {
  const label = `${basename(cwd)}$ ${command} ${args.join(" ")}`;
  console.log(label);
  const result = spawnSync(command, args, {
    cwd,
    env,
    encoding: "utf8",
    stdio: "inherit",
  });
  if (result.status !== 0) {
    throw new Error(`${label} failed with status ${result.status}`);
  }
}


function verifyDeclaredGoSDK(dir, env) {
  const result = spawnSync("go", ["list", "-m", "-json", "github.com/opensoha/soha-contracts"], { cwd: dir, env, encoding: "utf8" });
  if (result.status !== 0) throw new Error(`cannot resolve declared contracts module for ${basename(dir)}: ${result.stderr ?? result.error?.message ?? ""}`);
  const module = JSON.parse(result.stdout);
  if (module.Replace || !module.Version) throw new Error(`declared contracts module must have a version and checksum without replacement: ${basename(dir)}`);
  const downloaded = spawnSync("go", ["mod", "download", "-json", "github.com/opensoha/soha-contracts"], { cwd: dir, env, encoding: "utf8" });
  if (downloaded.status !== 0) throw new Error(`cannot download declared contracts module for ${basename(dir)}: ${downloaded.stderr ?? downloaded.error?.message ?? ""}`);
  const resolved = JSON.parse(downloaded.stdout);
  if (!resolved.Sum || resolved.Version !== module.Version || (module.Sum && resolved.Sum !== module.Sum)) throw new Error("declared SDK checksum/version mismatch");
  console.log(`declared SDK: ${resolved.Version} ${resolved.Sum}; GOWORK=off, readonly modules`);
}

function internalInputs() {
  const inputs = ["go.work", "package.json", "package-lock.json", "go.mod", "go.sum", "contracts/package.json", "contracts/go.mod", "contracts/go.sum", "web/package.json"];
  return Object.fromEntries(inputs.map((path) => [path, existsSync(join(workspaceRoot, path))
    ? createHash("sha256").update(readFileSync(join(workspaceRoot, path))).digest("hex") : null]));
}
