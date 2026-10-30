import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
for (const width of [375, 1440])
  test(`runtime channel restoration requires original confirmation and funds grant at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    let writes = 0;
    const stopped = {
      channel_id: "7",
      event_id: "31",
      revision: "1",
      blocked: true,
      provider: "alipay",
      code: "40004:PAYER_BALANCE_NOT_ENOUGH",
      changed_at: "2026-10-24T00:00:00Z",
      notice_state: "unknown",
      notice_transport: "telegram",
    };
    await page.route("**/console/v1/**", async (route) => {
      const r = route.request(),
        path = new URL(r.url()).pathname;
      if (path.endsWith("/session"))
        return route.fulfill(
          ok({
            user: {
              id: "1",
              name: "Local Admin",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "local-channel76",
          }),
        );
      if (path.endsWith("/security/payment-password"))
        return route.fulfill(
          ok({
            configured: true,
            enforced: false,
            credential_epoch: 1,
            totp_required: false,
          }),
        );
      if (path.endsWith("/channel-stops") && r.method() === "GET")
        return route.fulfill(ok({ items: [stopped] }));
      if (path.endsWith("/security/funds-authorizations/challenges")) {
        expect(r.postDataJSON()).toEqual({
          target: "/console/v1/admin/channel-stops/7/resume",
          body: { expected_event_id: "31", expected_revision: "1" },
        });
        return route.fulfill(
          ok(
            {
              challenge_id: "c".repeat(64),
              expires_at: new Date(Date.now() + 60000).toISOString(),
              summary: {
                channel_id: "7",
                expected_event_id: "31",
                expected_revision: "1",
                channel_stop_code: stopped.code,
              },
            },
            201,
          ),
        );
      }
      if (path.endsWith("/security/funds-authorizations/confirm")) {
        expect(r.postDataJSON().payment_password).toBe(
          "local-ui76-funds-password",
        );
        return route.fulfill(
          ok({
            authorization: "g".repeat(43),
            expires_at: new Date(Date.now() + 60000).toISOString(),
          }),
        );
      }
      if (path.endsWith("/channel-stops/7/resume") && r.method() === "POST") {
        writes++;
        expect(r.headers()["x-csrf-token"]).toBe("local-channel76");
        expect(r.headers()["x-payment-authorization"]).toBe("g".repeat(43));
        expect(r.postDataJSON()).toEqual({
          expected_event_id: "31",
          expected_revision: "1",
        });
        return route.fulfill(
          ok({ ...stopped, blocked: false, event_id: "32", revision: "2" }),
        );
      }
      if (path.endsWith("/schema"))
        return route.fulfill(
          ok({
            title: "支付通道",
            description: "本地通道资料",
            fields: [
              { name: "name", label: "通道名称", type: "string" },
              { name: "enabled", label: "启用", type: "boolean" },
            ],
            permissions: { create: false, update: false, delete: false },
          }),
        );
      expect(r.method()).toBe("GET");
      return route.fulfill(ok({ items: [], total: 0 }));
    });
    await page.goto("/admin/payapi/pay-channel");
    await page.waitForLoadState("networkidle");
    const panel = page.getByRole("region", {
      name: "通道运行状态",
      exact: true,
    });
    await expect(panel.getByText("已停用", { exact: true })).toBeVisible();
    await expect(panel.getByText("管理员通知：发送结果待核对")).toBeVisible();
    expect(writes).toBe(0);
    await panel.getByRole("button", { name: "恢复通道", exact: true }).click();
    await expect(
      panel.getByRole("button", { name: "确认恢复", exact: true }),
    ).toBeDisabled();
    await panel
      .getByRole("checkbox", { name: "我已核对原转账和通道配置，确认恢复" })
      .check();
    await panel.getByRole("button", { name: "确认恢复", exact: true }).click();
    await expect(panel.getByText("通知方式：Telegram")).toBeVisible();
    const dialog = page.getByRole("dialog", { name: "管理员资金二次授权" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(stopped.code, { exact: true })).toBeVisible();
    expect(writes).toBe(0);
    await dialog
      .getByRole("button", { name: "已核对摘要，验证资金密码" })
      .click();
    await dialog
      .getByLabel("本人管理员资金密码")
      .fill("local-ui76-funds-password");
    await dialog
      .getByRole("button", { name: "确认资金密码", exact: true })
      .click();
    await expect(
      dialog.getByRole("button", { name: "执行已核对的原操作" }),
    ).toBeVisible();
    expect(writes).toBe(0);
    await dialog.getByRole("button", { name: "执行已核对的原操作" }).click();
    await expect(panel.getByText("通道已恢复，原转账不会重发。")).toBeVisible();
    // Restored channel: no stopped channels remain, so the panel header reports 运行正常.
    await expect(panel.getByText("运行正常", { exact: true })).toBeVisible();
    expect(writes).toBe(1);
    await page.evaluate(() => window.scrollTo(0, 0));
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath(`channel-stop-${width}.png`),
      fullPage: true,
      animations: "disabled",
    });
  });
