/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import { usePreferencesStore } from '@/stores/preferences-store'
import { I18nProvider } from './index'

describe('I18nProvider', () => {
  beforeAll(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  })

  afterEach(() => {
    usePreferencesStore.setState({ localeCode: 'zh_CN' })
    document.documentElement.lang = 'zh-CN'
    document.body.replaceChildren()
  })

  it('keeps the document language synchronized with the selected locale', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<I18nProvider>content</I18nProvider>)
    })
    expect(document.documentElement.lang).toBe('zh-CN')

    await act(async () => {
      usePreferencesStore.getState().setLocaleCode('en_US')
    })
    expect(document.documentElement.lang).toBe('en-US')

    await act(async () => root.unmount())
  })
})
