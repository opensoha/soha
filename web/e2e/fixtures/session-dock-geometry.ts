import { expect, type Locator } from '@playwright/test'

export async function expectSessionToolbarSpacing(dock: Locator) {
  const pane = dock.locator('.soha-realtime-session-pane:visible')
  await expect(pane.locator('.soha-terminal-toolbar')).toBeVisible()
  await expect
    .poll(async () => {
      const navigation = await dock.locator('.ant-tabs-nav:visible').boundingBox()
      const toolbar = await pane.locator('.soha-terminal-toolbar').boundingBox()
      const shell = await pane.locator('.soha-log-shell, .soha-terminal-shell').boundingBox()
      return {
        above: Math.round(toolbar!.y - navigation!.y - navigation!.height),
        below: Math.round(shell!.y - toolbar!.y - toolbar!.height),
      }
    })
    .toEqual({ above: 8, below: 8 })
}
