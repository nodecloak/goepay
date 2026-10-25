import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const plan = {
  id: "4",
  revision: "3",
  name: "每日结算",
  mode: "generate_only",
  enabled: true,
  last_authorization_id: "8",
  merchant_ids: ["2"],
  schedule: {
    frequency: "day",
    interval: "1",
    anchor_at: "2026-10-24T00:00:00Z",
    execute_hour: 9,
    execute_minute: 0,
    execute_second: 0,
  },
};
for (const width of [375, 1440])
  test(`independent automatic payout authority requires cost acknowledgement and original funds confirmation at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    let writes = 0;
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        path = new URL(req.url()).pathname;
      if (path.endsWith("/session"))
        return route.fulfill(
          ok({
            user: {
              id: "1",
              name: "Admin",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "local-auto73",
          }),
        );
      if (path.endsWith("/security/payment-password"))
        return route.fulfill(
          ok({
            configured: false,
            enforced: false,
            credential_epoch: 0,
            totp_required: false,
          }),
        );
      if (path.endsWith("/settlement-plans"))
        return route.fulfill(ok({ items: [plan], next_cursor: "" }));
      if (path.endsWith("/settlement-plans/4")) return route.fulfill(ok(plan));
      if (path.endsWith("/settlement-plans/4/runs"))
        return route.fulfill(ok({ items: [], next_cursor: "" }));
      if (path.endsWith("/settlement-plans/4/auto-authorization"))
        return route.fulfill(
          ok({
            id: "0",
            plan_id: "4",
            available: true,
            wallet_channels: [
              {
                id: "7",
                provider: "alipay",
                product: "alipay.fund.trans.toaccount.transfer",
                app_id: "local-app",
                payer_masked: "***0073",
                merchant_ids: ["2"],
              },
            ],
          }),
        );
      if (req.method() !== "GET") writes++;
      return route.fulfill(ok({}));
    });
    await page.goto("/admin/settle/plans");
    await page.getByRole("button", { name: "查看计划 4", exact: true }).click();
    const panel = page.getByRole("region", {
      name: "独立自动出款授权",
      exact: true,
    });
    // A named section is a region; no provider access occurs merely by opening it.
    await panel
      .getByRole("button", { name: "读取自动付款授权", exact: true })
      .click();
    await expect(panel.getByText(/没有可撤销/)).toHaveCount(0);
    await panel
      .getByLabel("自动出款开始 RFC3339", { exact: true })
      .fill("2026-10-24T00:00:00Z");
    await panel
      .getByLabel("自动出款截止 RFC3339", { exact: true })
      .fill("2026-10-25T00:00:00Z");
    await panel
      .getByLabel("自动出款单项毛额上限（分）", { exact: true })
      .fill("100");
    await panel
      .getByLabel("自动出款每次毛额上限（分）", { exact: true })
      .fill("200");
    await panel
      .getByLabel("自动出款每日毛额上限（分）", { exact: true })
      .fill("300");
    await panel.getByLabel("自动出款每日条数上限", { exact: true }).fill("2");
    await panel
      .getByRole("button", { name: "核对独立自动出款授权", exact: true })
      .click();
    await expect(panel.getByRole("alert")).toContainText("机构另收费不包含");
    expect(writes).toBe(0);
    await panel.getByRole("checkbox", { name: /我已确认/ }).check();
    await panel
      .getByRole("button", { name: "核对独立自动出款授权", exact: true })
      .click();
    await expect(
      page.getByRole("region", { name: "独立自动出款操作确认", exact: true }),
    ).toBeVisible();
    await expect(
      panel.getByText('"generation_authorization_id": "8"', { exact: false }),
    ).toBeVisible();
    await panel
      .getByRole("button", { name: "确认授权并验证本人资金密码", exact: true })
      .click();
    await expect(
      page.getByRole("alert").filter({ hasText: /尚未设置本人管理员资金密码/ }),
    ).toBeVisible();
    expect(writes).toBe(0);
    await page.evaluate(() => window.scrollTo(0, 0));
    await expect(page.locator(".console-sidebar")).toHaveCSS("left", "0px");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath(`settlement-auto-${width}.png`),
      fullPage: true,
      animations: "disabled",
    });
  });
