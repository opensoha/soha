import { createHash } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import { existsSync, mkdirSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const contractsRoot = resolve(fileURLToPath(new URL("..", import.meta.url)));
const consumerRoot = resolve(process.env.SOHA_CONSUMER_ROOT ?? dirname(contractsRoot));
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
const requireAll = args.includes("--require-all") || process.env.SOHA_CONSUMER_REQUIRE_ALL === "1";
const dryRun = args.includes("--dry-run");
const declaredSDK = args.includes("--declared");
const listOnly = args.includes("--list");
const outputPath = valueArg("--out");
const selected = selectedConsumers(args);

if (listOnly) {
  console.log(consumers.map((consumer) => consumer.name).join("\n"));
  process.exit(0);
}

const targets = selected.length > 0 ? consumers.filter((consumer) => selected.includes(consumer.name)) : consumers;
const unknown = selected.filter((name) => !consumers.some((consumer) => consumer.name === name));
if (unknown.length > 0) {
  throw new Error(`unknown consumer(s): ${unknown.join(", ")}`);
}

let checked = 0;
let skipped = 0;
let failed = 0;
const contractsRevision = revision(contractsRoot);
const results = [];
for (const target of targets) {
  let env = process.env;
  let packageDir;
  let manifests;
  let entry;
  try {
    const consumer = declaredSDK ? { ...target, commands: target.commands.filter(([, values]) => !values.includes(contractsTarballPlaceholder)) } : target;
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
    env = consumer.kind === "go" ? (declaredSDK ? { ...process.env, GOWORK: "off", GOFLAGS: "-mod=readonly" } : await goWorkspaceEnv(dir)) : process.env;
    if (declaredSDK && consumer.kind === "go") verifyDeclaredGoSDK(dir, env);
    packageDir = consumer.kind === "npm" && !declaredSDK ? await packContracts() : undefined;
    if (packageDir) manifests = ["package.json", "package-lock.json"].map((name) => ({ path: join(dir, name), contents: readFileSync(join(dir, name)) }));
    for (const [command, commandArgs] of consumer.commands) {
      const resolvedArgs = commandArgs.map((arg) =>
        arg === contractsTarballPlaceholder ? contractsTarball(packageDir) : arg,
      );
      run(command, resolvedArgs, dir, env);
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
      if (env !== process.env && env.GOWORK && env.GOWORK !== "off") await rm(dirname(env.GOWORK), { recursive: true, force: true });
      if (packageDir) await rm(packageDir, { recursive: true, force: true });
    }
  }
}

if (requireAll && checked !== targets.length) {
  console.error(`consumer matrix checked ${checked}/${targets.length} required consumer(s)`);
  process.exitCode = 1;
}

console.log(`consumer matrix complete: checked ${checked}, skipped ${skipped}, root ${consumerRoot}`);

if (outputPath) {
  const resolvedOutputPath = resolve(outputPath);
  mkdirSync(dirname(resolvedOutputPath), { recursive: true });
  writeFileSync(
    resolvedOutputPath,
    `${JSON.stringify(
      {
        schemaVersion: 1,
        dependencyMode: declaredSDK ? "declared" : "candidate-source",
        contractsVersion: packageVersion(),
        contractsRevision,
        contractsRoot,
        consumerRoot,
        requireAll,
        dryRun,
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

if (failed) process.exitCode = 1;

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
  const tracked = git(["diff", "HEAD", "--name-only", "-z"]);
  const untracked = git(["ls-files", "--others", "--exclude-standard", "-z"]);
  if (tracked.status !== 0 || untracked.status !== 0) throw new Error(`cannot identify candidate source at ${dir}`);
  const paths = tracked.stdout + untracked.stdout;
  const files = [...new Set(paths.split("\0").filter(Boolean))].sort().map((path) => {
    const file = join(root, path);
    return { path, sha256: existsSync(file) ? createHash("sha256").update(readFileSync(file)).digest("hex") : null };
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
  return index >= 0 ? args[index + 1] : undefined;
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
    return env;
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
