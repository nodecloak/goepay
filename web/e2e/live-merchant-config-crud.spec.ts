import { test, expect } from "@playwright/test";
import { localCrudFixture } from "./local-crud-fixture";

test.describe("real local merchant configuration operations", () => {
  test.skip(
    process.env.GOEPAY_LIVE_E2E !== "1" ||
      process.env.GOEPAY_LIVE_NODE_WORKFLOWS_E2E !== "1",
    "Owned loopback simulation only",
  );
  let local: Awaited<ReturnType<typeof localCrudFixture>>;
  test.beforeAll(async ({ playwright }) => {
    local = await localCrudFixture(playwright, import.meta.url);
  });
  test.beforeEach(async ({ page }) => {
    await local.attach(page, "merchant");
  });
  test.afterAll(async () => {
    await local?.dispose();
  });

  test("profile, personal bounds, clearing values and cancelling changes persist independently", async ({
    page,
  }) => {
    await page.goto("/merchant/account/settings");
    await page
      .getByLabel("商户名称", { exact: false })
      .fill(local.tag + "-merchant");
    await page
      .getByRole("button", { name: "保存账户资料", exact: true })
      .click();
    await expect(
      page.getByRole("status").filter({ hasText: "账户资料已保存" }),
    ).toBeVisible();
    await page.reload();
    await expect(page.getByLabel("商户名称", { exact: false })).toHaveValue(
      local.tag + "-merchant",
    );
    const personal = page.getByRole("region", {
      name: "个人功能设置",
      exact: true,
    });
    await personal.getByLabel("联系QQ", { exact: true }).fill("12345678");
    await personal.locator('input[name="pay_minmoney"]').fill("1.23");
    await personal.locator('input[name="pay_maxmoney"]').fill("500.01");
    await personal
      .getByRole("button", { name: "保存设置", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (await local.call("merchant", "/account/personal-settings")).qq,
      )
      .toBe("12345678");
    await page.reload();
    await expect(personal.locator('input[name="pay_minmoney"]')).toHaveValue(
      "1.23",
    );
    await expect(personal.locator('input[name="pay_maxmoney"]')).toHaveValue(
      "500.01",
    );
    await personal.getByLabel("联系QQ", { exact: true }).fill("99999999");
    await personal
      .getByRole("button", { name: "取消修改", exact: true })
      .click();
    await expect(personal.getByLabel("联系QQ", { exact: true })).toHaveValue(
      "12345678",
    );
    for (const name of ["qq", "pay_minmoney", "pay_maxmoney"])
      await personal.locator(`input[name="${name}"]`).fill("");
    await personal
      .getByRole("button", { name: "保存设置", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (await local.call("merchant", "/account/personal-settings")).qq,
      )
      .toBe("");
    const state = await local.call("merchant", "/account/personal-settings");
    expect(state.pay_minmoney).toBe("");
    expect(state.pay_maxmoney).toBe("");
    expect((await local.call("merchant", "/account/profile")).name).toBe(
      local.tag + "-merchant",
    );
  });

  test("merchant domain approval is invalidated by editing and survives disable, cancel and deletion", async ({
    page,
    browser,
  }) => {
    await page.goto("/merchant/domain");
    await page
      .getByRole("button", { name: "新增授权域名", exact: true })
      .click();
    const dialog = page.getByRole("dialog"),
      domain = local.tag + ".example.test";
    await expect(dialog.getByLabel("商户 ID", { exact: false })).toHaveCount(0);
    await dialog.getByLabel("域名", { exact: false }).fill(domain);
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    const list = await local.call("merchant", "/resources/domains?limit=100");
    const saved = list.items.find((x: any) => x.domain === domain);
    expect(saved.approved).toBe(false);
    expect(saved.merchant_id).toBe("1000");
    const admin = await browser.newContext({ baseURL: local.baseURL });
    try {
      const manager = await admin.newPage();
      await local.attach(manager, "admin");
      await manager.goto("/admin/merchant/domain");
      const row = manager.getByRole("row").filter({ hasText: domain });
      await row.getByRole("button", { name: "编辑", exact: true }).click();
      await manager
        .getByRole("dialog")
        .getByLabel("审核通过", { exact: true })
        .check();
      await manager
        .getByRole("dialog")
        .getByRole("button", { name: "保存", exact: true })
        .click();
      await expect
        .poll(
          async () =>
            (await local.call("merchant", "/resources/domains/" + saved.id))
              .approved,
        )
        .toBe(true);
    } finally {
      await admin.close();
    }
    await page.reload();
    let row = page.getByRole("row").filter({ hasText: domain });
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    const changed = "edited-" + domain;
    await dialog.getByLabel("域名", { exact: false }).fill(changed);
    await dialog.getByLabel("启用", { exact: true }).uncheck();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    const updated = await local.call(
      "merchant",
      "/resources/domains/" + saved.id,
    );
    expect(updated).toMatchObject({
      domain: changed,
      approved: false,
      enabled: false,
      merchant_id: "1000",
    });
    await page.reload();
    row = page.getByRole("row").filter({ hasText: changed });
    await row.getByRole("button", { name: "删除", exact: true }).click();
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: "删除", exact: true }).click();
    await dialog.getByRole("button", { name: "确认删除", exact: true }).click();
    await expect(row).toHaveCount(0);
    await page.reload();
    await expect(row).toHaveCount(0);
    expect(
      (
        await local.request("merchant", "/resources/domains/" + saved.id)
      ).status(),
    ).toBe(404);
  });

  test("notification thresholds save and reload without sending a notice", async ({
    page,
  }) => {
    await page.goto("/merchant/account/notice");
    const panel = page.getByRole("region", {
      name: "消息提醒设置",
      exact: true,
    });
    await panel
      .getByLabel("订单金额提醒门槛（元）", { exact: true })
      .fill("12.34");
    await panel
      .getByLabel("余额不足提醒门槛（元）", { exact: true })
      .fill("56.78");
    await panel.getByLabel("订单语音提醒", { exact: true }).check();
    const saved = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === "/console/v1/merchant/account/notice" &&
        r.request().method() === "PUT",
    );
    await panel.getByRole("button", { name: "保存设置", exact: true }).click();
    expect((await saved).ok()).toBe(true);
    await page.reload();
    await expect(
      panel.getByLabel("订单金额提醒门槛（元）", { exact: true }),
    ).toHaveValue("12.34");
    await expect(
      panel.getByLabel("余额不足提醒门槛（元）", { exact: true }),
    ).toHaveValue("56.78");
    await expect(
      panel.getByLabel("订单语音提醒", { exact: true }),
    ).toBeChecked();
    await expect(
      panel.getByRole("radio", { name: "订单支付成功 关闭提醒", exact: true }),
    ).toBeChecked();
  });

  test("settlement recipient versions, private field clearing and reserve settings survive reload", async ({
    page,
  }) => {
    await page.goto("/merchant/settle/settings");
    const recipient = page.getByRole("group", {
      name: "结算收款资料",
      exact: true,
    });
    await recipient
      .getByRole("button", { name: "配置收款资料", exact: true })
      .click();
    await recipient
      .getByLabel("收款账号", { exact: true })
      .fill("local-crud@example.invalid");
    await recipient
      .getByLabel("真实姓名", { exact: true })
      .fill("Local CRUD Fixture");
    await recipient
      .getByRole("button", { name: "保存收款版本", exact: true })
      .click();
    await expect(
      recipient.getByTestId("settlement-recipient-current"),
    ).toBeVisible();
    const before = await local.call(
      "merchant",
      "/workflows/settlement-recipient",
    );
    await page.reload();
    await expect(
      recipient.getByTestId("settlement-recipient-current"),
    ).toBeVisible();
    const reserve = page.getByRole("region", {
      name: "预留余额设置",
      exact: true,
    });
    await reserve.getByLabel("预留余额", { exact: true }).fill("10%");
    await reserve
      .getByRole("button", { name: "保存设置", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (await local.call("merchant", "/account/personal-settings"))
            .remain_money,
      )
      .toBe("10%");
    await page.reload();
    await expect(reserve.getByLabel("预留余额", { exact: true })).toHaveValue(
      "10%",
    );
    expect(
      await local.call("merchant", "/workflows/settlement-recipient"),
    ).toEqual(before);
    await expect(page.getByLabel("收款账号", { exact: true })).toHaveCount(0);
  });

  test("API preference confirmation can be cancelled, saved and read after refresh", async ({
    page,
  }) => {
    await page.goto("/merchant/account/settings");
    const panel = page.getByRole("region", {
      name: "接口与登录功能设置",
      exact: true,
    });
    const before = await local.call("merchant", "/account/api-switches");
    await panel
      .getByRole("checkbox", { name: /^开启密钥登录/ })
      .setChecked(before.keylogin !== 1);
    await panel
      .getByRole("button", { name: "保存功能开关", exact: true })
      .click();
    await panel.getByRole("button", { name: "返回修改", exact: true }).click();
    expect(
      (await local.call("merchant", "/account/api-switches")).revision,
    ).toBe(before.revision);
    await panel
      .getByRole("button", { name: "保存功能开关", exact: true })
      .click();
    await panel.getByRole("button", { name: "确认保存", exact: true }).click();
    await expect
      .poll(
        async () =>
          (await local.call("merchant", "/account/api-switches")).keylogin,
      )
      .toBe(before.keylogin === 1 ? 0 : 1);
    await page.reload();
    await expect(
      panel.getByRole("checkbox", { name: /^开启密钥登录/ }),
    ).toBeChecked({ checked: before.keylogin !== 1 });
  });

  test("printing preference confirmation persists copies without dispatching a print job", async ({
    page,
  }) => {
    await page.goto("/merchant/account/printing");
    const panel = page.getByRole("region", {
      name: "我的订单打印设置",
      exact: true,
    });
    const before = await local.call("merchant", "/account/printing");
    await panel.getByLabel("每单份数", { exact: true }).fill("2");
    await panel
      .getByRole("button", { name: "检查并保存打印设置", exact: true })
      .click();
    await panel.getByRole("button", { name: "取消", exact: true }).click();
    expect((await local.call("merchant", "/account/printing")).revision).toBe(
      before.revision,
    );
    await panel
      .getByRole("button", { name: "检查并保存打印设置", exact: true })
      .click();
    await panel.getByRole("button", { name: "确认保存", exact: true }).click();
    await expect
      .poll(
        async () => (await local.call("merchant", "/account/printing")).copies,
      )
      .toBe(2);
    await page.reload();
    await expect(panel.getByLabel("每单份数", { exact: true })).toHaveValue(
      "2",
    );
  });
});
