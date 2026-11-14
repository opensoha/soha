import { describe, expect, it } from 'vitest'
import { resourcePreflightDiagnostic } from './components/preflight-table'
import type { ResourcePreflightItem } from './types'

describe('resource preflight diagnostics', () => {
  it('surfaces list namespace mismatch as a blocking diagnostic', () => {
    const item = {
      document: { index: 0, kind: 'ConfigMap', name: 'app', contentHash: 'hash' },
      resolvedNamespace: 'ops',
      warnings: [],
      authorization: {
        allowed: false,
        allowedActions: [],
        resourceScope: { clusterIds: [], namespaces: [], resourceGroups: [], resourceKinds: [] },
      },
      capability: { key: 'resource.create', status: 'available', mode: 'direct' },
      dryRun: { status: 'skipped' },
      errors: [
        {
          code: 'namespace_mismatch',
          message: 'YAML namespace ops does not match list namespace minio',
        },
      ],
    } satisfies ResourcePreflightItem

    expect(resourcePreflightDiagnostic(item)).toContain('does not match')
  })
})
