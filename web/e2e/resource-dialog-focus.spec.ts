import { expect, test } from "@playwright/test";

for (const width of [1280, 375]) {
  test(`funds request drawers support keyboard cancel without mutations at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const writes: string[] = [];
    await page.route("**/console/v1/**", (route) => {
      const request = route.request();
      if (request.method() !== "GET") {
        writes.push(request.url());
        return route.abort();
      }
      const data = new URL(request.url()).pathname.endsWith("/session")
        ? {
            user: { id: "1", role: "admin", admin_class: "super" },
            csrf_token: "synthetic-focus",
          }
        : { items: [], total: 0 };
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/merchant/internal-transfer");
    const opener = page.getByRole("button", {
      name: "发起站内转账",
      exact: true,
    });
    await opener.click();
    const dialog = page.getByRole("dialog", {
      name: "新增账务申请",
      exact: true,
    });
    await expect(dialog.locator("input").first()).toBeFocused();
    const first = dialog.getByRole("button", { name: "关闭", exact: true });
    const last = dialog.getByRole("button", { name: "取消", exact: true });
    await last.focus();
    await page.keyboard.press("Tab");
    await expect(first).toBeFocused();
    await page.keyboard.press("Shift+Tab");
    await expect(last).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();
    expect(writes).toEqual([]);
  });
  test(`generic resource drawers retain keyboard focus without writes at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const writes: string[] = [];
    await page.route("**/console/v1/**", (route) => {
      const request = route.request(),
        path = new URL(request.url()).pathname;
      if (request.method() !== "GET") {
        writes.push(path);
        return route.abort();
      }
      let data: unknown = { items: [], total: 0 };
      if (path.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "synthetic-focus",
        };
      else if (path.endsWith("/resources/domains/schema"))
        data = {
          title: "授权域名",
          permissions: { create: true, update: true, delete: true },
          fields: [
            {
              name: "merchant_id",
              label: "商户 ID",
              type: "string",
              required: true,
            },
            { name: "domain", label: "域名", type: "string", required: true },
            { name: "enabled", label: "启用", type: "boolean" },
          ],
        };
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/merchant/domain");
    const opener = page.getByRole("button", {
      name: "新增授权域名",
      exact: true,
    });
    await opener.click();
    const dialog = page.getByRole("dialog", {
      name: "新增授权域名",
      exact: true,
    });
    await expect(dialog.locator("#field-merchant_id")).toBeFocused();
    const first = dialog.getByRole("button", { name: "关闭", exact: true });
    const last = dialog.getByRole("button", { name: "保存", exact: true });
    await last.focus();
    await page.keyboard.press("Tab");
    await expect(first).toBeFocused();
    await page.keyboard.press("Shift+Tab");
    await expect(last).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();
    await opener.click();
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();
    expect(writes).toEqual([]);
  });
}
