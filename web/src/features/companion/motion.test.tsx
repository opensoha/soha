import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { builtinCompanionPack } from './builtin-pack'
import {
  companionInteractionAnimation,
  companionInteractionId,
  companionStateAnimation,
} from './motion'
import { SohaOrbit } from './soha-orbit'

describe('companion motion bindings', () => {
  it('resolves manifest animations and exposes the built-in head interaction', () => {
    const manifest = builtinCompanionPack.manifest

    expect(companionStateAnimation(manifest, 'speaking')).toBe('speaking')
    expect(companionStateAnimation(manifest, 'disabled')).toBeUndefined()
    expect(companionInteractionAnimation(manifest, 'pet')).toBe('pet')
    expect(companionInteractionId(manifest, 'pet')).toBe('pet')
    expect(companionInteractionId(manifest, 'injected')).toBe('tap')

    const markup = renderToStaticMarkup(
      <SohaOrbit
        interaction={{ animation: 'pet', sequence: 1 }}
        state="idle"
        stateAnimation="idle"
      />,
    )
    expect(markup).toContain('data-interaction="pet"')
    expect(markup).toContain('data-motion="idle"')
    expect(markup).toContain('data-companion-interaction="pet"')
    expect(markup).toContain('data-hit-area="head"')
  })
})
