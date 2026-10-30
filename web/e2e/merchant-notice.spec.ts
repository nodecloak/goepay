import { test, expect } from "@playwright/test";
const settings = {
  id: "7",
  merchant_id: "1000",
  revision: "2",
  voice_order: false,
  email_masked: "o***@example.test",
  msgconfig: {
    order: 2,
    settle: 0,
    login: 0,
    complain: 0,
    mchrisk: 0,
    balance: 0,
    group: 0,
    domain_audit: 0,
    daily_report: 4,
    order_money: "10.00",
    balance_money: "20.00",
  },
  configured: { qqbot_appsecret: true, telegram_uid: true },
  target_masks: { qqbot_appsecret: "********" },
};
for (const area of ["admin", "merchant"] as const)
  for (const width of [375, 1280]) {
    test(`${area} notification settings preserve original fields and secrets at ${width}px`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 850 });
      let saved: unknown;
      const reads: string[] = [];
      await page.route("**/console/v1/**", async (route) => {
        const req = route.request(),
          p = new URL(req.url()).pathname;
        let data: unknown = {};
        if (p.endsWith("/session"))
          data = {
            user: {
              id: area === "admin" ? "1" : "1000",
              name: "Local",
              role: area,
              admin_class: area === "admin" ? "operator" : undefined,
            },
            csrf_token: "synthetic80",
          };
        if (p.endsWith("/notice")) {
          reads.push(p);
          if (req.method() === "PUT") {
            saved = req.postDataJSON();
            data = { ...settings, id: "8", revision: "3" };
          } else data = settings;
        }
        if (p.endsWith("/notice/events")) data = { items: [{ id: "80", scene: "order", trade_no: "LOCAL-NOTICE80", at: "2026-10-24T10:00:00Z", state: "unknown", deliveries: [{ transport: "smtp", state: "unknown" }] }], next_cursor: "" };
        await route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      await page.goto(
        area === "admin"
          ? "/admin/merchant/notice"
          : "/merchant/account/notice",
      );
      if (area === "admin") {
        await page.getByLabel("商户 ID", { exact: true }).fill("1000");
        await page.getByRole("button", { name: "读取商户设置" }).click();
      }
      await expect(page.getByLabel("订单金额提醒门槛（元）")).toHaveValue(
        "10.00",
      );
      await expect(page.getByRole("region", { name: "通知记录", exact: true })).toContainText("LOCAL-NOTICE80");
      await expect(page.getByRole("region", { name: "通知记录", exact: true })).toContainText("待核实");
      await page
        .getByLabel("订单支付成功 Telegram", { exact: true })
        .check();
      await page
        .locator('[data-target-action="telegram_uid"]')
        .selectOption("replace");
      await page
        .locator('[data-target-value="telegram_uid"]')
        .fill("-1000000000080");
      await page.getByRole("button", { name: "保存设置", exact: true }).click();
      await expect(page.getByRole("status")).toHaveText("通知设置已保存");
      expect(saved).toMatchObject({
        expected_profile_id: "7",
        msgconfig: { order: 5, order_money: "10.00", balance_money: "20.00" },
        targets: { telegram_uid: "-1000000000080" },
        clear_fields: [],
      });
      await expect(
        page.locator('[data-target-value="telegram_uid"]'),
      ).toHaveCount(0);
      expect(
        reads.every(
          (p) =>
            p ===
            (area === "admin"
              ? "/console/v1/admin/merchants/1000/notice"
              : "/console/v1/merchant/account/notice"),
        ),
      ).toBe(true);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      expect(
        await page.evaluate(() =>
          JSON.stringify({ ...localStorage, ...sessionStorage }),
        ),
      ).not.toContain("-1000000000080");
      await page.screenshot({
        path: info.outputPath(`merchant-notice-${area}-${width}.png`),
        fullPage: true,
      });
    });
  }
