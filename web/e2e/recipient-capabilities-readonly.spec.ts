import { expect, test } from "@playwright/test";
const view = (id: string) => ({
  state: "configured",
  config_id: id,
  type: "alipay",
  app_id: "",
  account_masked: "***" + id,
  name_masked: "***",
});
const funds = {
  available_money: "10.00",
  ledger_available_money: "10.00",
  today_blocked_money: "0.00",
  settle_type: 0,
};
for (const width of [375, 1280]) {
  test(`SettleList readonly recipient survives context replacement and remains usable for fee review at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    let writes = 0,
      quoteMID = "";
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/console/v1/**", async (route) => {
      const req = route.request(),
        u = new URL(req.url()),
        p = u.pathname;
      if (req.method() !== "GET") writes++;
      let data: unknown = { items: [], total: 0 };
      if (p.endsWith("/session"))
        data = {
          user: {
            id: "1",
            name: "Settlement Operator",
            role: "admin",
            admin_class: "operator",
            permissions: ["SettleList"],
          },
          csrf_token: "readonly-recipient",
        };
      else if (p.endsWith("/security/payment-password"))
        data = {
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        };
      else if (p.endsWith("/settlement-recipient/allowed-types"))
        data = { settlement: ["alipay"], can_write: false };
      else if (p.endsWith("/settlement-recipient"))
        data = {
          ...view(u.searchParams.get("merchant_id") === "202" ? "52" : "42"),
          can_write: false,
        };
      else if (p.endsWith("/funds-availability")) data = funds;
      else if (p.endsWith("/settlement-policy")) {
        quoteMID = u.searchParams.get("merchant_id")!;
        data = {
          can_apply: true,
          external_payout: false,
          terms: {
            gross_amount: "100",
            fee_amount: "3",
            net_amount: "97",
            retained_cents: "0",
          },
        };
      }
      await route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/settle/list");
    await page.getByRole("button", { name: "申请结算", exact: true }).click();
    const dialog = page.getByRole("dialog"),
      mid = dialog.getByLabel("付款商户 ID", { exact: true });
    await mid.fill("101");
    const panel = dialog.locator("fieldset.settlement-recipient");
    await expect(
      panel.getByTestId("settlement-recipient-current"),
    ).toContainText("版本 42");
    await expect(
      panel.getByRole("button", { name: "修改收款资料", exact: true }),
    ).toBeDisabled();
    await expect(
      panel.getByRole("button", { name: "刷新收款版本", exact: true }),
    ).toBeEnabled();
    await expect(panel).toContainText("当前仅可查看收款资料");
    await mid.fill("202");
    await expect(panel).toContainText("版本 52");
    await mid.fill("101");
    await expect(panel).toContainText("版本 42");
    await expect(
      panel.getByRole("button", { name: "修改收款资料", exact: true }),
    ).toBeDisabled();
    await dialog
      .getByRole("button", { name: "查询可出账金额", exact: true })
      .click();
    await dialog.getByLabel("金额（元）", { exact: true }).fill("1.00");
    await expect(dialog.locator(".settle-live")).toContainText(
      "实际到账：0.97 元",
    );
    await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
    await expect(
      dialog.getByRole("button", { name: "按已核对费用提交", exact: true }),
    ).toBeVisible();
    expect(quoteMID).toBe("101");
    expect(writes).toBe(0);
    expect(errors).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
  for (const area of ["admin", "merchant"]) {
    test(`disabled-group ${area} caps retain actual balances and original history at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      let creates = 0,
        historyReads = 0;
      const errors: string[] = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const transfer = {
        id: "7",
        merchant_id: "1000",
        amount: "100",
        status: "unknown",
        type: "alipay",
      };
      await page.route("**/console/v1/**", async (route) => {
        const req = route.request(),
          u = new URL(req.url()),
          p = u.pathname;
        if (req.method() === "POST" && p.endsWith("/transfers")) creates++;
        let data: unknown = { items: [transfer], total: 1 };
        if (p.endsWith("/session"))
          data = {
            user: {
              id: area === "admin" ? "1" : "1000",
              name: "Local",
              role: area,
              admin_class: area === "admin" ? "super" : undefined,
            },
            csrf_token: "readonly-transfer",
          };
        else if (p.endsWith("/transfers/capabilities")) {
          expect(u.searchParams.get("merchant_id")).toBe(
            area === "admin" ? "1000" : null,
          );
          data = {
            merchant_id: "1000",
            writes_enabled: false,
            platform_enabled: true,
            effective_enabled: false,
            permission_source: "group_disabled",
            effective_group_id: "3",
            merchant_enabled: true,
            fee_policy_available: false,
            funds_policy_available: false,
            ledger_available_money: "123.45",
            reserved_money: "6.78",
            channels: [{ id: "42", types: ["alipay"] }],
            minimum_money: "1.00",
            maximum_money: "200.00",
            daily_recipient_limit: 3,
            default_remark: "",
            default_title: "",
          };
        } else if (p.endsWith("/transfers/7")) {
          historyReads++;
          data = { transfer, recipient: {} };
        }
        await route.fulfill({ json: { code: 0, data } });
      });
      await page.goto(
        area === "admin" ? "/admin/transfer/list" : "/merchant/transfer",
      );
      await page.getByRole("button", { name: "申请付款", exact: true }).click();
      const dialog = page.getByRole("dialog", {
        name: "转账付款",
        exact: true,
      });
      if (area === "admin") {
        await dialog.getByLabel("付款商户ID", { exact: true }).fill("1000");
        await dialog
          .getByRole("button", { name: "读取付款权限", exact: true })
          .click();
      }
      await expect(dialog).toContainText("当前用户组付款功能未启用");
      await expect(dialog).toContainText("账本可用余额 123.45 元");
      await expect(dialog).toContainText("预占余额 6.78 元");
      await expect(dialog).toContainText("手续费和可出账金额暂不可用");
      await expect(dialog).toContainText("单笔 1.00–200.00 元");
      await dialog.getByLabel("转账金额", { exact: true }).fill("10.00");
      await expect(
        dialog.getByLabel("需支付金额", { exact: true }),
      ).toHaveCount(0);
      await expect(dialog).not.toContainText("可出账 0.00");
      await expect(dialog).not.toContainText("undefined");
      await expect(
        dialog.getByRole("button", { name: "立即转账", exact: true }),
      ).toBeDisabled();
      await dialog.getByRole("button", { name: "关闭", exact: true }).click();
      await page.getByRole("button", { name: "付款详情", exact: true }).click();
      await expect(
        page.getByRole("dialog", { name: "付款详情", exact: true }),
      ).toBeVisible();
      expect(historyReads).toBe(1);
      expect(creates).toBe(0);
      expect(errors).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
  }
}
