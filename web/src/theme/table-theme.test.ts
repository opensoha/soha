// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import {
  applyAppTheme,
  getAntdTheme,
  getThemePalette,
  OVERVIEW_COMPACT_CHART_SIZE,
} from './app-theme'

afterEach(() => {
  document.documentElement.removeAttribute('style')
  delete document.documentElement.dataset.themeMode
  document.body.removeAttribute('theme-mode')
})

describe('shared theme ownership', () => {
  it('keeps CSS and Antd typography/control geometry aligned across theme switches', () => {
    for (const mode of ['light', 'dark', 'light'] as const) {
      applyAppTheme('soha', mode)
      const { token, components } = getAntdTheme(mode)
      const root = document.documentElement
      expect(root.style.getPropertyValue('--soha-font-family')).toBe(token?.fontFamily)
      expect(root.style.getPropertyValue('--soha-font-size')).toBe(`${token?.fontSize}px`)
      expect(root.style.getPropertyValue('--soha-line-height')).toBe(
        `${Number(token?.fontSize) * Number(token?.lineHeight)}px`,
      )
      expect(root.style.getPropertyValue('--soha-control-height')).toBe(`${token?.controlHeight}px`)
      expect(root.style.getPropertyValue('--soha-radius-control')).toBe(
        `${components?.Input?.borderRadius}px`,
      )
      expect(root.style.getPropertyValue('--soha-radius-lg')).toBe(`${token?.borderRadiusLG}px`)
      expect(root.style.getPropertyValue('--soha-sidebar-item-height')).toBe('31px')
      expect(components?.Menu?.itemHeight).toBe(38)
      expect(components?.Menu?.fontSize).toBe(12)
      expect(components?.Breadcrumb?.fontSize).toBe(12)
      expect(Number(components?.Menu?.fontSize)).toBeLessThan(Number(token?.fontSize))
      expect(root.style.getPropertyValue('--soha-font-size-control')).toBe(
        `${components?.Button?.contentFontSize}px`,
      )
      expect(root.style.getPropertyValue('--soha-font-size-section')).toBe(
        `${components?.Modal?.titleFontSize}px`,
      )
      expect(root.style.getPropertyValue('--soha-line-height-supporting')).toBe(
        `${Number(token?.fontSizeSM) * Number(token?.lineHeightSM)}px`,
      )
      expect(root.style.getPropertyValue('--soha-line-height-section')).toBe(
        `${Number(components?.Modal?.titleFontSize) * Number(components?.Modal?.titleLineHeight)}px`,
      )
      expect(root.style.getPropertyValue('--soha-card-padding')).toBe(
        `${components?.Card?.bodyPadding}px`,
      )
      expect(components?.Card?.headerPadding).toBe(components?.Card?.bodyPadding)
      expect(root.style.getPropertyValue('--soha-font-size-metric')).toBe('34px')
      expect(root.style.getPropertyValue('--soha-font-size-summary')).toBe('22px')
      expect(root.style.getPropertyValue('--soha-overview-chart-size')).toBe(
        `${OVERVIEW_COMPACT_CHART_SIZE}px`,
      )
      expect(root.dataset.themeMode).toBe(mode)
      expect(document.body.getAttribute('theme-mode')).toBe(mode)
      expect(root.style.getPropertyValue('--soha-bg-surface')).toBe(
        getThemePalette(mode).colorBgContainer,
      )
    }
  })
})

describe('shared table theme', () => {
  it.each(['light', 'dark'] as const)(
    'uses readable headers and distinct densities in %s',
    (mode) => {
      const table = getAntdTheme(mode).components?.Table
      const palette = getThemePalette(mode)
      expect(getAntdTheme(mode).components?.Drawer?.colorBgElevated).toBe(palette.colorBgContainer)
      expect(table?.headerColor).toBe(palette.colorText)
      expect(table?.headerSplitColor).toBe(palette.colorBorder)
      expect(table?.borderColor).toBe(palette.colorBorder)
      expect(table?.cellPaddingBlockSM).toBe(8)
      expect(table?.cellPaddingBlockMD).toBe(12)
      expect(table?.cellFontSizeSM).toBe(12)
      expect(table?.cellFontSizeMD).toBe(getAntdTheme(mode).token?.fontSize)
      expect(Number(table?.cellFontSizeSM)).toBeLessThan(Number(table?.cellFontSizeMD))
      expect(palette.colorBgLayout).not.toBe(palette.colorBgContainer)
    },
  )
})
