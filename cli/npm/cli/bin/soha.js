#!/usr/bin/env node

import { launch } from "../lib/launcher.js";

try {
  process.exitCode = await launch(process.argv.slice(2));
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  console.error(`soha: ${message}`);
  process.exitCode = 1;
}
