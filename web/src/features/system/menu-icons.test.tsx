import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { AnimatedIcon } from '@/components/animated-icon'
import {
  MENU_ICON_OPTIONS,
  isKnownMenuIcon,
  resolveMenuIcon,
  resolveWorkbenchIcon,
} from './menu-icons'

describe('shared animated navigation icons', () => {
  it('renders every persisted menu icon key through the shared component', () => {
    expect(new Set(MENU_ICON_OPTIONS.map((option) => option.value)).size).toBe(
      MENU_ICON_OPTIONS.length,
    )
    for (const option of MENU_ICON_OPTIONS) {
      expect(isKnownMenuIcon(option.value)).toBe(true)
      const markup = renderToStaticMarkup(option.preview)
      expect(markup).toContain('soha-animated-icon')
      expect(markup).toContain(`data-icon="${option.value}"`)
      expect(markup).toMatch(/<(path|rect|circle|ellipse|line|polyline|polygon)[ >]/)
      expect(markup).toContain('aria-hidden="true"')
    }
  })

  it('keeps trimmed custom keys and provides a safe unknown-key fallback', () => {
    expect(renderToStaticMarkup(resolveMenuIcon(' network '))).toContain('data-icon="network"')
    expect(isKnownMenuIcon('removed-key')).toBe(false)
    expect(renderToStaticMarkup(resolveMenuIcon('removed-key'))).toContain(
      'data-icon="menu-square"',
    )
    expect(renderToStaticMarkup(resolveMenuIcon())).toContain('data-icon="menu-square"')
  })

  it('uses workbench semantics instead of borrowing the first child menu icon', () => {
    for (const [workbench, icon] of Object.entries({
      platform: 'cluster',
      delivery: 'activity',
      monitoring: 'radio-tower',
      compute: 'server',
      ai: 'bot',
      security: 'shield',
      settings: 'settings',
    })) {
      expect(renderToStaticMarkup(resolveWorkbenchIcon(workbench))).toContain(`data-icon="${icon}"`)
    }
  })

  it('retains all Heya icon shapes in the same shared component', () => {
    for (const name of [
      'chat',
      'search',
      'tasks',
      'results',
      'calendar',
      'identity',
      'settings',
      'workbench',
    ] as const) {
      expect(renderToStaticMarkup(<AnimatedIcon name={name} />)).toContain(`data-icon="${name}"`)
    }
  })
})
