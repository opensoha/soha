/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest'
import { theme } from 'antd'
import { applyAppTheme, getAntdTheme, watchSystemMotion } from './app-theme'

afterEach(() => {
  vi.unstubAllGlobals()
  document.documentElement.removeAttribute('style')
})

it.each(['light', 'dark'] as const)('uses the same %s motion tokens for CSS and Antd', (mode) => {
  vi.stubGlobal('matchMedia', () => ({ matches: false }))
  applyAppTheme('soha', mode)
  const token = theme.getDesignToken(getAntdTheme(mode))
  expect(document.documentElement.style.getPropertyValue('--soha-motion-duration-mid')).toBe(
    token.motionDurationMid,
  )
  expect(document.documentElement.style.getPropertyValue('--soha-motion-ease')).toBe(
    token.motionEaseInOut,
  )
})

it('updates CSS and component motion when the system setting changes and removes the listener', () => {
  const media = new EventTarget() as EventTarget & { matches: boolean }
  media.matches = false
  vi.stubGlobal('matchMedia', () => media)
  applyAppTheme('soha', 'dark')
  const canvas = document.documentElement.style.getPropertyValue('--soha-bg-layout')
  const change = vi.fn(() => applyAppTheme('soha', 'dark'))
  const unsubscribe = watchSystemMotion(change)
  media.matches = true
  media.dispatchEvent(new Event('change'))
  expect(getAntdTheme('dark').token?.motion).toBe(false)
  expect(document.documentElement.style.getPropertyValue('--soha-motion-duration-mid')).toBe('0s')
  expect(document.documentElement.style.getPropertyValue('--soha-bg-layout')).toBe(canvas)
  unsubscribe()
  media.dispatchEvent(new Event('change'))
  expect(change).toHaveBeenCalledOnce()
})
