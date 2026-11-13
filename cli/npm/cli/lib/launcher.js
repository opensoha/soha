import { createHash, randomBytes } from "node:crypto";
import { spawnSync } from "node:child_process";
import { chmod, mkdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { homedir, platform as hostPlatform, arch as hostArch } from "node:os";
import path from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";

const DEFAULT_RELEASE_BASE_URL = "https://github.com/opensoha/soha-cli/releases/download";
const MAX_CHECKSUM_BYTES = 1024 * 1024;
const MAX_BINARY_BYTES = 200 * 1024 * 1024;
const CACHE_LOCK_TIMEOUT_MS = 30_000;
const CACHE_LOCK_STALE_MS = 10 * 60_000;

export function resolveTarget(platform = hostPlatform(), arch = hostArch()) {
  const goos = { linux: "linux", darwin: "darwin", win32: "windows" }[platform];
  const goarch = { x64: "amd64", arm64: "arm64" }[arch];
  if (!goos || !goarch) {
    throw new Error(`unsupported platform ${platform}/${arch}; install a native soha release binary instead`);
  }
  return { goos, goarch, extension: goos === "windows" ? ".exe" : "" };
}

export function binaryAssetName(version, target = resolveTarget()) {
  return `soha_${version}_${target.goos}_${target.goarch}${target.extension}`;
}

export function parseChecksum(checksums, assetName) {
  let match = "";
  for (const line of checksums.split(/\r?\n/u)) {
    const parsed = /^([a-f0-9]{64})\s+\*?(.+)$/u.exec(line.trim());
    if (!parsed || parsed[2] !== assetName) {
      continue;
    }
    if (match) {
      throw new Error(`checksums.txt contains duplicate entries for ${assetName}`);
    }
    match = parsed[1];
  }
  if (!match) {
    throw new Error(`checksums.txt does not contain ${assetName}`);
  }
  return match;
}

export function sha256(raw) {
  return createHash("sha256").update(raw).digest("hex");
}

export function defaultCacheRoot(env = process.env) {
  if (env.SOHA_NPX_CACHE?.trim()) {
    return path.resolve(env.SOHA_NPX_CACHE.trim());
  }
  if (hostPlatform() === "win32" && env.LOCALAPPDATA?.trim()) {
    return path.join(env.LOCALAPPDATA, "OpenSoha", "cli");
  }
  if (hostPlatform() === "darwin") {
    return path.join(homedir(), "Library", "Caches", "opensoha", "cli");
  }
  return path.join(env.XDG_CACHE_HOME?.trim() || path.join(homedir(), ".cache"), "opensoha", "cli");
}

function releaseURL(baseURL, version, fileName) {
  return `${baseURL.replace(/\/+$/u, "")}/v${version}/${fileName}`;
}

function assertDownloadURL(value) {
  const parsed = new URL(value);
  const loopback = parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost" || parsed.hostname === "::1";
  if (parsed.protocol !== "https:" && !(parsed.protocol === "http:" && loopback)) {
    throw new Error(`refusing insecure download URL ${value}`);
  }
}

async function download(url, maxBytes) {
  assertDownloadURL(url);
  const response = await fetch(url, {
    redirect: "follow",
    headers: { "user-agent": "@opensoha/cli" },
  });
  assertDownloadURL(response.url);
  if (!response.ok) {
    throw new Error(`download ${url} returned HTTP ${response.status}`);
  }
  const contentLength = Number(response.headers.get("content-length") || 0);
  if (contentLength > maxBytes) {
    throw new Error(`download ${url} exceeds the ${maxBytes}-byte limit`);
  }
  const raw = Buffer.from(await response.arrayBuffer());
  if (raw.length > maxBytes) {
    throw new Error(`download ${url} exceeds the ${maxBytes}-byte limit`);
  }
  return raw;
}

async function readIfPresent(filePath) {
  try {
    return await readFile(filePath);
  } catch (error) {
    if (error?.code === "ENOENT") {
      return undefined;
    }
    throw error;
  }
}

async function acquireCacheLock(lockPath) {
  const deadline = Date.now() + CACHE_LOCK_TIMEOUT_MS;
  for (;;) {
    try {
      await mkdir(lockPath, { mode: 0o700 });
      return async () => rm(lockPath, { recursive: true, force: true });
    } catch (error) {
      if (error?.code !== "EEXIST") {
        throw error;
      }
    }

    try {
      const lock = await stat(lockPath);
      if (Date.now() - lock.mtimeMs > CACHE_LOCK_STALE_MS) {
        await rm(lockPath, { recursive: true, force: true });
        continue;
      }
    } catch (error) {
      if (error?.code === "ENOENT") {
        continue;
      }
      throw error;
    }
    if (Date.now() >= deadline) {
      throw new Error(`timed out waiting for launcher cache lock ${lockPath}`);
    }
    await delay(25);
  }
}

async function writeAtomic(filePath, raw, mode) {
  const temporary = path.join(
    path.dirname(filePath),
    `.${path.basename(filePath)}.${process.pid}.${randomBytes(6).toString("hex")}.tmp`,
  );
  try {
    await writeFile(temporary, raw, { flag: "wx", mode });
    if (mode & 0o111) {
      await chmod(temporary, mode);
    }
    try {
      await rename(temporary, filePath);
    } catch (error) {
      if (!new Set(["EACCES", "EEXIST", "EPERM"]).has(error?.code)) {
        throw error;
      }
      await rm(filePath, { force: true });
      await rename(temporary, filePath);
    }
  } finally {
    await rm(temporary, { force: true });
  }
}

export async function ensureBinary({
  version,
  cacheRoot = defaultCacheRoot(),
  releaseBaseURL = process.env.SOHA_CLI_RELEASE_BASE_URL || DEFAULT_RELEASE_BASE_URL,
  target = resolveTarget(),
} = {}) {
  if (!/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/u.test(version || "")) {
    throw new Error(`invalid launcher version ${JSON.stringify(version)}`);
  }
  const assetName = binaryAssetName(version, target);
  const versionDir = path.join(cacheRoot, version);
  const checksumPath = path.join(versionDir, "checksums.txt");
  const binaryPath = path.join(versionDir, assetName);
  await mkdir(versionDir, { recursive: true, mode: 0o755 });
  const releaseLock = await acquireCacheLock(path.join(versionDir, ".install.lock"));
  try {
    let checksumRaw = await readIfPresent(checksumPath);
    if (!checksumRaw) {
      checksumRaw = await download(releaseURL(releaseBaseURL, version, "checksums.txt"), MAX_CHECKSUM_BYTES);
      await writeAtomic(checksumPath, checksumRaw, 0o644);
    }
    const expectedSHA = parseChecksum(checksumRaw.toString("utf8"), assetName);
    const cached = await readIfPresent(binaryPath);
    if (cached && sha256(cached) === expectedSHA) {
      return binaryPath;
    }

    const downloaded = await download(releaseURL(releaseBaseURL, version, assetName), MAX_BINARY_BYTES);
    const actualSHA = sha256(downloaded);
    if (actualSHA !== expectedSHA) {
      throw new Error(`sha256 mismatch for ${assetName}: got ${actualSHA}, expected ${expectedSHA}`);
    }
    await writeAtomic(binaryPath, downloaded, target.goos === "windows" ? 0o700 : 0o755);
    return binaryPath;
  } finally {
    await releaseLock();
  }
}

export async function packageVersion() {
  const packagePath = fileURLToPath(new URL("../package.json", import.meta.url));
  const parsed = JSON.parse(await readFile(packagePath, "utf8"));
  return parsed.version;
}

export async function launch(args, options = {}) {
  const version = options.version || (await packageVersion());
  const binary = await ensureBinary({ version, ...options });
  const result = spawnSync(binary, args, { stdio: "inherit", windowsHide: true });
  if (result.error) {
    throw result.error;
  }
  if (result.signal) {
    throw new Error(`native soha process terminated by ${result.signal}`);
  }
  return result.status ?? 1;
}
