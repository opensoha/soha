import type { ConnectorManifest } from "./types.js";

const SEMVER_PATTERN = /^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/;

export function validateManifest(manifest: ConnectorManifest): string[] {
  const errors: string[] = [];

  if (!manifest.id) {
    errors.push("manifest.id is required");
  }
  if (!manifest.name) {
    errors.push(`${manifest.id || "<unknown>"}.name is required`);
  }
  if (!SEMVER_PATTERN.test(manifest.version)) {
    errors.push(`${manifest.id || "<unknown>"}.version must be semver`);
  }
  if (!["planned", "experimental", "stable"].includes(manifest.status)) {
    errors.push(`${manifest.id || "<unknown>"}.status is invalid`);
  }
  if (manifest.runtime.language !== "typescript") {
    errors.push(`${manifest.id}.runtime.language must be typescript`);
  }
  if (!manifest.runtime.entrypoint.endsWith(".ts")) {
    errors.push(`${manifest.id}.runtime.entrypoint must point at TypeScript source`);
  }
  for (const state of ["created", "configured", "started", "stopped"] as const) {
    if (!manifest.lifecycle.includes(state)) {
      errors.push(`${manifest.id}.lifecycle must include ${state}`);
    }
  }
  for (const field of manifest.config) {
    if (!field.name || !field.type || !field.description) {
      errors.push(`${manifest.id}.config contains an incomplete field`);
    }
  }
  for (const action of manifest.actions) {
    if (!action.name || !action.description || typeof action.inputSchema !== "object") {
      errors.push(`${manifest.id}.actions contains an incomplete action`);
    }
  }

  return errors;
}
