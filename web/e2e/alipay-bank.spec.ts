import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const funds = {
  settle_type: 0,
  available_money: "10.00",
  ledger_available_money: "10.00",
  today_blocked_money: "0.00",
};
test("Ali bank transfer requires private account name without Wx code and keeps estimate separate from actual fees and return evidence", async ({
  page,
}) => {
  let creates = 0,
    queries = 0,
    body: Record<string, unknown> | undefined;
  const transfer = {
    id: "31",
    type: "bank",
    bank_product: "alipay_bankcard",
    amount: "100",
    channel_id: "3",
    status: "reversal_pending",
    ledger_status: "succeeded",
    bank_lifecycle: "reversal_pending",
    bank_estimated_fee_amount: "17",
    bank_estimated_fee_known: true,
    bank_fee_amount: "0",
    bank_fee_known: false,
    bank_returned_principal: "0",
  };
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/capabilities"))
      return route.fulfill(
        ok({
          ...funds,
          writes_enabled: true,
          platform_enabled: true,
          merchant_enabled: true,
          transfer_rate: "1",
          channels: [
            {
              id: "3",
              types: ["alipay", "bank"],
              bank_product: "alipay_bankcard",
              bank_code_required: false,
            },
            {
              id: "4",
              types: ["bank"],
              bank_product: "wxpay_v2",
              bank_code_required: true,
            },
          ],
        }),
      );
    if (p.endsWith("/query")) {
      queries++;
      return route.fulfill(ok(transfer, 202));
    }
    if (p.endsWith("/31"))
      return route.fulfill(
        ok({
          transfer,
          recipient: {
            account: "6222020202020202020",
            name: "Synthetic Person",
            bank_code: "",
          },
        }),
      );
    if (r.method() === "POST") {
      creates++;
      body = r.postDataJSON();
      return route.fulfill(ok(transfer, 202));
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/transfer");
  await page.getByRole("button", { name: "申请付款", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "转账付款" });
  const pick = async (select: string, option: string | RegExp) => {
    await modal.getByRole("button", { name: select }).click();
    await modal.getByRole("option", { name: option, exact: true }).click();
  };
  await expect(modal.getByRole("tab", { name: "支付宝" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(modal).not.toContainText("支付宝银行卡（对私）");
  await modal.getByRole("tab", { name: "银行卡" }).click();
  await pick("通道选择", "通道 4");
  await pick("开户银行编号", /（1002）$/);
  await pick("通道选择", "通道 3");
  await expect(
    modal.locator(".pa-label--req", { hasText: /^姓名$/ }),
  ).toHaveCount(1);
  await expect(modal.getByRole("button", { name: "开户银行编号" })).toHaveCount(
    0,
  );
  await expect(modal).toContainText("支付宝银行卡（对私）");
  await modal.getByLabel("银行卡号").fill("6222020202020202020");
  await modal.getByLabel("姓名", { exact: true }).fill("Synthetic Person");
  await modal.getByLabel("转账金额").fill("1.00");
  await modal.getByRole("button", { name: "立即转账" }).click();
  const check = page.getByRole("dialog", { name: "核对付款" });
  await expect(check).toContainText("无需微信银行编号");
  expect(creates).toBe(0);
  await check.getByRole("button", { name: "确认提交付款" }).click();
  const detail = page.getByRole("dialog", { name: "付款详情" });
  await expect(detail).toContainText("预计银行手续费（分，仅估算）");
  await expect(detail).toContainText("17");
  await expect(detail).toContainText("尚未核实");
  await expect(detail).toContainText("估算不代表实际扣费或退费");
  await expect(detail.locator(".alert.success")).toContainText(
    "不能据此再次付款或假定资金已返还",
  );
  expect(body).toMatchObject({
    channel_id: "3",
    type: "bank",
    account: "6222020202020202020",
    name: "Synthetic Person",
  });
  expect(body).not.toHaveProperty("bank_code");
  await expect(detail.getByRole("button", { name: /付款凭证/ })).toHaveCount(0);
  await detail.getByRole("button", { name: "查询通道付款结果" }).click();
  await expect(detail.locator(".alert.success")).not.toContainText(
    "银行退票已核实",
  );
  expect(queries).toBe(1);
  expect(creates).toBe(1);
});
test("unknown or inconsistent bank product capability is not usable", async ({
  page,
}) => {
  let writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/capabilities"))
      return route.fulfill(
        ok({
          ...funds,
          writes_enabled: true,
          platform_enabled: true,
          merchant_enabled: true,
          transfer_rate: "1",
          channels: [
            {
              id: "3",
              types: ["bank"],
              bank_product: "alipay_bankcard",
              bank_code_required: true,
            },
            {
              id: "4",
              types: ["bank"],
              bank_product: "unknown",
              bank_code_required: false,
            },
          ],
        }),
      );
    if (r.method() === "POST") writes++;
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/transfer");
  await page.getByRole("button", { name: "申请付款", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "转账付款" });
  await expect(modal.getByRole("button", { name: "立即转账" })).toBeDisabled();
  await expect(modal).toContainText("银行卡产品能力尚未核实");
  await expect(modal.getByRole("tab", { name: "银行卡" })).toBeDisabled();
  expect(writes).toBe(0);
});
