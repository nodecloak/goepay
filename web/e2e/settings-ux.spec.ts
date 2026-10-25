import { test, expect } from "@playwright/test";
for (const width of [375, 1280])
  test(`original settings groups, exact units and keyboard navigation at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 960 });
    const writes: { path: string; body: unknown }[] = [];
    const rows = [
      { id: "1", key: "site_name", value: "本地支付" },
      { id: "2", key: "settlement_minimum_cents", value: "123" },
      { id: "3", key: "settlement_fee_rate_bps", value: "101" },
    ];
    await page.route("**/console/v1/**", async (route) => {
      const req = route.request(),
        p = new URL(req.url()).pathname;
      let data: unknown = {};
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super", name: "Local" },
          csrf_token: "settings-csrf",
        };
      else if (p.endsWith("/security/payment-password"))
        data = {
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        };
      else if (p.endsWith("/settings/schema"))
        data = {
          title: "系统设置",
          permissions: { create: true, update: true, delete: true },
          fields: [
            {
              name: "key",
              type: "select",
              options: [
                "site_name",
                "registration_open",
                "settlement_minimum_cents",
                "settlement_fee_rate_bps",
              ].map((value) => ({ label: value, value })),
            },
            { name: "value", type: "string" },
          ],
        };
      else if (p.endsWith("/settings"))
        data = { items: rows, total: rows.length };
      else data = rows.find((x) => p.endsWith("/" + x.id)) || {};
      if (req.method() !== "GET") {
        expect(req.headers()["x-csrf-token"]).toBe("settings-csrf");
        writes.push({ path: p, body: req.postDataJSON() });
        const row = rows.find((x) => p.endsWith("/" + x.id));
        if (row) {
          Object.assign(row, req.postDataJSON());
          data = row;
        }
      }
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/system/settings");
    const tabs = page.getByRole("tablist", { name: "系统设置分类" });
    await expect(tabs.getByRole("tab")).toHaveCount(17);
    await expect(page.getByLabel("网站名称", { exact: true })).toHaveValue(
      "本地支付",
    );
    expect(writes).toHaveLength(0);
    await tabs.getByRole("tab", { name: "结算规则", exact: true }).click();
    await expect(
      page.getByLabel("最低结算金额（元）", { exact: true }),
    ).toHaveValue("1.23");
    await expect(
      page.getByLabel("结算手续费率（%）", { exact: true }),
    ).toHaveValue("1.01");
    await page.getByLabel("最低结算金额（元）", { exact: true }).fill("0.005");
    await page
      .getByRole("button", { name: "保存本页设置", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText("最多两位小数");
    expect(writes).toHaveLength(0);
    await page.getByLabel("最低结算金额（元）", { exact: true }).fill("1.24");
    await page.getByLabel("结算手续费率（%）", { exact: true }).fill("1.02");
    await page
      .getByRole("button", { name: "保存本页设置", exact: true })
      .click();
    await expect(page.getByRole("status")).toHaveText("已保存2项设置");
    expect(writes.map((x) => x.body)).toEqual([
      { value: "124" },
      { value: "102" },
    ]);
    await expect(
      page.getByRole("button", { name: "保存本页设置", exact: true }),
    ).toBeDisabled();
    await tabs.getByRole("tab", { name: "结算规则", exact: true }).focus();
    await page.keyboard.press("End");
    await expect(
      tabs.getByRole("tab", { name: "高级设置", exact: true }),
    ).toBeFocused();
    await expect(
      tabs.getByRole("tab", { name: "高级设置", exact: true }),
    ).toHaveAttribute("aria-selected", "true");
    await page.keyboard.press("Home");
    await expect(
      tabs.getByRole("tab", { name: "网站信息", exact: true }),
    ).toBeFocused();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath(`settings-${width}.png`),
      fullPage: true,
    });
  });
