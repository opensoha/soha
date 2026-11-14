export function assertBaselineVersion(baselineVersion, currentVersion) {
  const baseline = parseVersion(baselineVersion, "baseline");
  const current = parseVersion(currentVersion, "current");
  if (compareVersions(baseline, current) > 0) {
    throw new Error(`baseline version (${baselineVersion}) must not be newer than current version (${currentVersion})`);
  }
}

function parseVersion(value, label) {
  const match = typeof value === "string" && value.match(/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/);
  const prerelease = match?.[4]?.split(".");
  if (!match || match[0] !== value || prerelease?.some((part) => /^\d+$/.test(part) && part.length > 1 && part.startsWith("0"))) {
    throw new Error(`${label} version must be valid SemVer: ${String(value)}`);
  }
  return { core: match.slice(1, 4).map((part) => BigInt(part)), prerelease };
}

function compareVersions(before, after) {
  for (let index = 0; index < 3; index += 1) {
    if (before.core[index] !== after.core[index]) return before.core[index] < after.core[index] ? -1 : 1;
  }
  const a = before.prerelease;
  const b = after.prerelease;
  if (!a || !b) return a ? -1 : b ? 1 : 0;
  for (let index = 0; index < Math.max(a.length, b.length); index += 1) {
    if (a[index] === b[index]) continue;
    if (a[index] === undefined) return -1;
    if (b[index] === undefined) return 1;
    const aNumeric = /^\d+$/.test(a[index]);
    const bNumeric = /^\d+$/.test(b[index]);
    if (aNumeric && bNumeric) return BigInt(a[index]) < BigInt(b[index]) ? -1 : 1;
    if (aNumeric !== bNumeric) return aNumeric ? -1 : 1;
    return a[index] < b[index] ? -1 : 1;
  }
  return 0;
}
