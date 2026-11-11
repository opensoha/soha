import { expect, type Page } from '@playwright/test'

export async function expectWorkloadDetailFrame(page: Page) {
  const header = page.locator('.soha-workload-detail-header')
  await expect(header.locator('.soha-workload-detail-namespace')).toHaveCount(1)
  await expect(header.locator('.soha-workload-detail-namespace')).toHaveClass(/ant-tag-processing/)
  for (const width of [1440, 900, 320]) {
    await page.setViewportSize({ width, height: 1000 })
    await expect
      .poll(() =>
        page.evaluate(() => {
          const header = document.querySelector('.soha-workload-detail-header')!
          const navigation = document.querySelector(
            '.soha-workload-detail-frame > .soha-workload-detail-tabs > .ant-tabs-nav',
          )!
          const holder = document.querySelector(
            '.soha-workload-detail-frame > .soha-workload-detail-tabs > :is(.ant-tabs-content-holder, .ant-tabs-body-holder)',
          )!
          const a = header.getBoundingClientRect()
          const b = navigation.getBoundingClientRect()
          const c = holder.getBoundingClientRect()
          return {
            connected: Math.abs(a.bottom - b.top) <= 1,
            leftAligned: Math.abs(a.left - c.left) <= 1,
            rightAligned: Math.abs(a.right - c.right) <= 1,
            inlinePadding: getComputedStyle(holder).paddingInlineStart,
            titleSize: getComputedStyle(header.querySelector('h1')!).fontSize,
            pageOverflow: document.documentElement.scrollWidth > innerWidth,
          }
        }),
      )
      .toEqual({
        connected: true,
        leftAligned: true,
        rightAligned: true,
        inlinePadding: '0px',
        titleSize: width <= 760 ? '18px' : '21px',
        pageOverflow: false,
      })
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
}
