import type { Page } from "@playwright/test";

/** Reload the actual page, then reopen the previously visible settings category. */
export async function pageReload248(page: Page) {
  const tab = page.locator(
    '.settings-tabs > [id^="settings-tab-"][aria-selected="true"]',
  );
  const category = (await tab.count()) ? await tab.getAttribute("id") : null;
  await page.reload();
  if (category) await page.locator(`#${category}`).click();
}
