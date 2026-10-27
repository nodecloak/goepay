import { openMerchantOrderDetails } from "./fixtures/merchant-order-navigation";
import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("explicit bank capability validates policy and recipient then renders pending return and verified reversal separately from historical success", async ({
  page,
}) => {
  let creates = 0,
    queries = 0,
    body: Record<string, unknown> | undefined;
  const pending = {
    id: "88",
    type: "bank",
    bank_product: "wxpay_v2",
    status: "reversal_pending",
    ledger_status: "succeeded",
    bank_lifecycle: "reversal_pending",
    bank_fee_known: false,
    bank_fee_amount: "0",
    bank_service_fee_held: "3",
    bank_returned_principal: "0",
    bank_returned_service_fee: "0",
    bank_returned_fee: "0",
    bank_reversal_journals: [],
    amount: "100",
    channel_id: "9",
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
          writes_enabled: true,
          platform_enabled: true,
          merchant_enabled: true,
          transfer_rate: "3",
          available_money: "10.00",
          ledger_available_money: "10.00",
          today_blocked_money: "0.00",
          settle_type: 0,
          channels: [
            {
              id: "9",
              types: ["bank"],
              bank_product: "wxpay_v2",
              bank_code_required: true,
            },
          ],
          minimum_money: "1.00",
          maximum_money: "2.00",
          daily_recipient_limit: 3,
          default_remark: "Local bank payout",
          default_title: "Local title",
        }),
      );
    if (p.endsWith("/query")) {
      queries++;
      return route.fulfill(
        ok({
          ...pending,
          status: "reversed",
          bank_lifecycle: "reversed",
          bank_fee_known: true,
          bank_fee_amount: "1",
          bank_returned_principal: "100",
          bank_returned_service_fee: "3",
          bank_reversal_journals: ["JR-88"],
        }),
      );
    }
    if (p.endsWith("/88"))
      return route.fulfill(
        ok({
          transfer: pending,
          recipient: {
            account: "6222020202020202020",
            name: "Test Person",
            bank_code: "1002",
          },
        }),
      );
    if (r.method() === "POST") {
      creates++;
      body = r.postDataJSON();
      return route.fulfill(ok(pending, 202));
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/transfer");
  await page.getByRole("button", { name: "申请付款", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "申请付款" });
  await expect(modal.getByLabel("收款方式")).toHaveValue("bank");
  await expect(modal.getByLabel("开户银行编号")).toHaveAttribute(
    "required",
    "",
  );
  await expect(modal.getByLabel("收款人姓名")).toHaveAttribute("required", "");
  await expect(modal.getByLabel("付款说明", { exact: true })).toHaveValue(
    "Local bank payout",
  );
  await modal.getByLabel("收款账号").fill("6222020202020202020");
  await modal.getByLabel("收款人姓名").fill("Test Person");
  await modal.getByLabel("开户银行编号").selectOption("1002");
  await modal.getByLabel("付款金额（元）").fill("0.99");
  await modal.getByRole("button", { name: "核对付款信息" }).click();
  await expect(modal.getByRole("alert")).toContainText("不得低于 1.00");
  await modal.getByLabel("付款金额（元）").fill("2.01");
  await modal.getByRole("button", { name: "核对付款信息" }).click();
  await expect(modal.getByRole("alert")).toContainText("不得高于 2.00");
  expect(creates).toBe(0);
  await modal.getByLabel("付款金额（元）").fill("1.00");
  await modal.getByRole("button", { name: "核对付款信息" }).click();
  await expect(modal).toContainText("每日同收款方次数上限");
  await expect(modal).toContainText("Local bank payout");
  await modal.getByRole("button", { name: "确认提交付款" }).click();
  const detail = page.getByRole("dialog", { name: "付款详情" });
  await expect(detail).toContainText("历史账本成功不代表当前银行最终到账");
  await expect(detail).toContainText("服务费暂存（分）");
  await expect(detail).toContainText("尚未核实");
  await expect(detail.getByRole("button", { name: /付款凭证/ })).toHaveCount(0);
  expect(body).toMatchObject({
    type: "bank",
    bank_code: "1002",
    name: "Test Person",
    amount: "1.00",
    remark: "Local bank payout",
  });
  expect(creates).toBe(1);
  await detail.getByRole("button", { name: "查询通道付款结果" }).click();
  await expect(detail).toContainText("银行退票已核实");
  await expect(detail).toContainText("JR-88");
  await expect(detail.locator(".alert.success")).not.toContainText(
    "通道已确认付款成功",
  );
  expect(queries).toBe(1);
  expect(creates).toBe(1);
});
test("preauthorization is displayed as unpaid until verified capture and offers no ordinary close or fabricated release", async ({
  page,
}) => {
  let captured = false,
    closes = 0;
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
    if (p.endsWith("/query")) {
      captured = true;
      return route.fulfill(ok({ trade_no: "AUTH-1", status: "paid" }));
    }
    if (p.endsWith("/close")) {
      closes++;
      return route.fulfill(ok(null));
    }
    const order = {
      trade_no: "AUTH-1",
      status: captured ? "paid" : "pending",
      amount: "100",
    };
    return route.fulfill(
      ok(
        p.endsWith("/AUTH-1")
          ? {
              order,
              provider: { method: "preauth", can_close: false, state: "ready" },
              authorization: {
                auth_no: "AUTH-NO",
                buyer_id: "BUYER",
                amount: "100",
                capture_state: captured ? "succeeded" : "none",
                channel_id: "1",
              },
            }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await openMerchantOrderDetails(page);
  const modal = page.getByRole("dialog", { name: "订单详情" });
  await expect(modal).toContainText("预授权已确认，尚未捕获付款");
  await expect(modal).toContainText("AUTH-NO");
  await expect(
    modal.getByRole("button", { name: "关闭通道订单", exact: true }),
  ).toHaveCount(0);
  await expect(modal.getByRole("button", { name: /解冻|撤销/ })).toHaveCount(0);
  await modal
    .getByRole("button", { name: "查询支付通道", exact: true })
    .click();
  await expect(modal).toContainText("预授权付款已捕获，并已核实订单支付");
  expect(closes).toBe(0);
});
