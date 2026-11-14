import { readFileSync } from 'node:fs'
import {
  proxySetupContext,
  proxySetupSnippet,
  proxySetupTargets,
} from '../src/features/identity/providers/proxy-setup-model.ts'

// Node 22+: node --experimental-strip-types scripts/render-proxy-templates.mjs < setup.json
// Input uses the saved Provider and its server-provided setup response. Tokens remain placeholders.
const { provider, setup } = JSON.parse(readFileSync(0, 'utf8'))
const context = proxySetupContext(provider, setup)
if (!context) throw new Error('Provider setup is incomplete')
process.stdout.write(
  JSON.stringify(
    Object.fromEntries(
      proxySetupTargets.map((target) => [target, proxySetupSnippet(target, context)]),
    ),
  ),
)
