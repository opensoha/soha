/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DecisionEvidence } from './content-dlp-policy-tools'

vi.mock('@/i18n', () => ({ useI18n: () => ({ localeCode: 'zh_CN' }) }))

describe('DLP release MFA evidence', () => {
  let container: HTMLDivElement
  let root: Root
  beforeEach(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  })
  it('guides a denied MFA match to the existing security page', async () => {
    await act(async () =>
      root.render(<DecisionEvidence decision={{ allowed: false, stepUpRequired: true }} />),
    )
    const link = container.querySelector('a')
    expect(link?.textContent).toBe('打开安全页')
    expect(link?.getAttribute('href')).toBe('/portal/security')
    expect(link?.getAttribute('target')).toBe('_blank')
    expect(link?.getAttribute('rel')).toContain('noopener')
  })

  it('keeps shadow evaluation descriptive and issues no authentication navigation', async () => {
    await act(async () =>
      root.render(<DecisionEvidence decision={{ allowed: false, stepUpRequired: true }} shadow />),
    )
    expect(container.textContent).toContain('此样本的匹配放行将需要 MFA')
    expect(container.querySelector('a')).toBeNull()
  })

  it('does not invent a missing MFA condition for historical or completed records', async () => {
    await act(async () => root.render(<DecisionEvidence decision={{ allowed: false }} />))
    expect(container.querySelector('a')).toBeNull()
    await act(async () =>
      root.render(<DecisionEvidence decision={{ allowed: true, stepUpRequired: true }} />),
    )
    expect(container.querySelector('a')).toBeNull()
  })
})
