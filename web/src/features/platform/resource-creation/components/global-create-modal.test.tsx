import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { GlobalResourceCreateModal } from './global-create-modal'

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({ localeCode: 'zh_CN' as const }),
}))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => ({ clusterId: 'cluster-id-1', namespace: 'selected-namespace' }),
}))
vi.mock('./create-shell', () => ({
  CreateShell: ({ context }: { context: Record<string, unknown> }) => (
    <div
      data-cluster-id={String(context.clusterId)}
      data-default-namespace={String(context.defaultNamespace ?? '')}
      data-source={String(context.source)}
    />
  ),
}))

describe('GlobalResourceCreateModal', () => {
  it('leaves namespace selection to the YAML document', () => {
    const html = renderToStaticMarkup(<GlobalResourceCreateModal onClose={() => undefined} open />)

    expect(html).toContain('data-cluster-id="cluster-id-1"')
    expect(html).toContain('data-source="global_yaml"')
    expect(html).toContain('data-default-namespace=""')
    expect(html).not.toContain('selected-namespace')
  })
})
