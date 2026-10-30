import { test, expect } from "@playwright/test";
for (const width of [375, 1280])
  test(`original administrator business notice modes and read-only history at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 850 });
    const methods: string[] = [];
    await page.route("**/console/v1/**", async (route) => {
      const req = route.request(),
        p = new URL(req.url()).pathname;
      let data: unknown = {};
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", name: "Local", role: "admin", admin_class: "super" },
          csrf_token: "synthetic83",
        };
      if (p.endsWith("/resources/settings/schema"))
        data = {
          name: "settings",
          title: "系统设置",
          fields: [
            {
              name: "key",
              label: "配置项",
              type: "select",
              required: true,
              options: [
                { label: "域名与站内转账提醒", value: "msgconfig_domain" },
                { label: "提现提醒", value: "msgconfig_apply" },
                { label: "注册审核提醒", value: "msgconfig_regaudit" },
              ],
            },
            { name: "value", label: "配置值", type: "string", required: true },
          ],
          permissions: { create: true, update: true, delete: true },
        };
      if (p.endsWith("/resources/settings")) data = { items: [], total: 0 };
      if (p.endsWith("/business-notices")) {
        methods.push(req.method());
        data = {
          items: [
            {
              id: "83",
              merchant_id: "1000",
              source_id: "8",
              job_id: "9",
              scene: "internal_transfer",
              mode: "4",
              transport: "telegram",
              state: "unknown",
              at: "2026-10-25T10:00:00Z",
            },
          ],
          next_cursor: "",
        };
      }
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/system/settings");
    await page.getByRole("tab", { name: "消息提醒", exact: true }).click();
    const records = page.getByRole("region", {
      name: "管理员业务提醒记录",
      exact: true,
    });
    await expect(records).toContainText("站内转账待审核");
    await expect(records).toContainText("待核实");
    const mode = page.getByRole("button", {
      name: "域名与站内转账待审核提醒",
      exact: true,
    });
    await expect(mode).toBeVisible();
    await expect(mode).toHaveText("未配置（使用系统默认）");
    await mode.click();
    const listbox = page.getByRole("listbox");
    await expect(listbox.getByRole("option")).toHaveText([
      "关闭提醒",
      "电子邮件",
      "群机器人",
      "Telegram",
      "QQ Bot",
    ]);
    await listbox.getByRole("option", { name: "Telegram", exact: true }).click();
    await expect(mode).toHaveText("Telegram");
    expect(methods).toEqual(["GET"]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath(`business-notice-${width}.png`),
      fullPage: true,
    });
  });
