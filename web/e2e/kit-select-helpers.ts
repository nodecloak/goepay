import { expect, type Locator, type Page } from "@playwright/test";

// KitSelect is a custom combobox: a <button aria-haspopup="listbox"> trigger
// that opens a role="listbox" of role="option" buttons. It is NOT a native
// <select>, so Playwright's selectOption() does not work on it. Drive it the
// way a user does: open the trigger, click the option by its visible label.
export async function chooseKitOption(
  page: Page,
  trigger: Locator,
  optionLabel: string,
) {
  await trigger.click();
  const listbox = page.getByRole("listbox");
  await listbox
    .getByRole("option", { name: optionLabel, exact: true })
    .click();
  await expect(trigger).toContainText(optionLabel);
}
