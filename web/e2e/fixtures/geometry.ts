import { expect, type Locator, type Page } from '@playwright/test'

export async function verifyGeometry(page: Page, target: Locator) {
  await expect(target).toBeVisible()
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth))
    .toBeLessThanOrEqual(1)
  await expect
    .poll(
      () =>
        target.evaluate((element) => {
          const box = element.getBoundingClientRect()
          const hit = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2)
          return (
            box.width > 0 && box.height > 0 && !!hit && (hit === element || element.contains(hit))
          )
        }),
      { timeout: 2_500 },
    )
    .toBe(true)
}
