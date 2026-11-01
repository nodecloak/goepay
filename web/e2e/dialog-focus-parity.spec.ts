import { test, expect } from "@playwright/test";

for (const width of [1440, 375]) {
  test(`plugin and kit dialogs preserve keyboard focus at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const writes: string[] = [];
    await page.route("**/console/v1/**", async (route) => {
      const request = route.request(),
        path = new URL(request.url()).pathname;
      const ok = (data: unknown) =>
        route.fulfill({ status: 200, json: { code: 0, data } });
      if (request.method() !== "GET") {
        writes.push(path);
        throw Error("Unexpected write in keyboard-only dialog test");
      }
      if (path.endsWith("/session"))
        return ok({
          user: {
            id: "1",
            name: "Synthetic admin",
            role: "admin",
            admin_class: "super",
          },
          csrf_token: "synthetic-csrf",
        });
      if (path.endsWith("/security/payment-password"))
        return ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        });
      if (path.endsWith("/resources/blacklist/schema"))
        return ok({
          title: "黑名单管理",
          fields: [
            { name: "id", label: "ID", type: "string", read_only: true },
            {
              name: "kind",
              label: "黑名单类型",
              type: "select",
              options: [{ value: "ip", label: "IP地址" }],
            },
            { name: "value", label: "黑名单内容", type: "string" },
          ],
          permissions: { create: true, update: true, delete: true },
        });
      return ok({ items: [], total: 0 });
    });
    await page.goto("/admin/app/app-store");
    const trigger = page.getByRole("button", { name: /^应用详情 / }).first();
    await expect(trigger).toBeVisible();
    const triggerName = await trigger.getAttribute("aria-label");
    await trigger.click();
    const plugin = page.getByRole("dialog");
    await expect(plugin).toBeVisible();
    const header = plugin.locator(".drawer-head button"),
      last = plugin.locator(".drawer-foot button");
    await page.keyboard.press("Tab");
    await expect(header).toBeFocused();
    await page.keyboard.press("Shift+Tab");
    await expect(last).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(header).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(plugin).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: triggerName!, exact: true }),
    ).toBeFocused();

    await page.goto("/admin/trade/blacklist");
    const add = page.getByRole("button", { name: /^(添加|新增黑名单项)$/ });
    await expect(add).toBeVisible();
    await add.click();
    const kit = page.getByRole("dialog"),
      close = kit.getByRole("button", { name: "关闭", exact: true }),
      submit = kit.locator('button[type="submit"]');
    await expect(kit).toBeVisible();
    await submit.focus();
    await page.keyboard.press("Tab");
    await expect(close).toBeFocused();
    await page.keyboard.press("Shift+Tab");
    await expect(submit).toBeFocused();
    await page.screenshot({
      path: `E:/GOEPAY/.tools/admin-parity-20261101/dialogs/kit-focus-${width}.png`,
      fullPage: true,
      animations: "disabled",
    });
    await page.keyboard.press("Escape");
    await expect(kit).toHaveCount(0);
    await expect(add).toBeFocused();
    expect(writes).toEqual([]);
  });
}
