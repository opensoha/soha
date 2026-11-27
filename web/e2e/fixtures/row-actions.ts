import { expect, type Locator, type Page } from '@playwright/test'

export async function chooseRowAction(
  scope: Page | Locator,
  targetLabel: string,
  actionName: string | RegExp,
) {
  const page = 'page' in scope ? scope.page() : scope
  await scope.getByRole('button', { name: `更多操作：${targetLabel}`, exact: true }).click()
  const menu = page.locator('.ant-dropdown:not(.ant-dropdown-hidden)').last()
  await menu
    .getByRole('menuitem', {
      name: actionName,
      exact: typeof actionName === 'string' ? true : undefined,
    })
    .click()
}

export async function expectRowActionDisabled(
  scope: Page | Locator,
  targetLabel: string,
  actionName: string | RegExp,
) {
  const page = 'page' in scope ? scope.page() : scope
  const trigger = scope.getByRole('button', { name: `更多操作：${targetLabel}`, exact: true })
  await trigger.click()
  const menu = page.locator('.ant-dropdown:not(.ant-dropdown-hidden)').last()
  const item = menu.getByRole('menuitem', {
    name: actionName,
    exact: typeof actionName === 'string' ? true : undefined,
  })
  await expect(item).toHaveAttribute('aria-disabled', 'true')
  await page.keyboard.press('Escape')
}
