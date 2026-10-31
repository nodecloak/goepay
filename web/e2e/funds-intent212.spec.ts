import { test, expect, type Page } from "@playwright/test";
import { openAdminAPIRefund } from "./order-action-helpers";

const ok = (data: unknown) => ({
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
async function fixture(
  page: Page,
  workflow = false,
  settlement = false,
  merchantOrders = false,
) {
  const merchant = workflow || merchantOrders;
  const state = {
    user: merchant ? "1000" : "1",
    csrf: "session-proof-212",
    posts: [] as Record<string, string>[],
    queries: 0,
    mode: "lost" as "lost" | "accepted" | "rejected",
    found: false,
    refusalStatus: 400,
    recipientVersion: "42",
    notRecorded: false,
    refusalReason: "conflict",
    fullRefundLost: false,
    refundedAmount: "0",
    changedHold: false,
    statusFailure: false,
    enforced: false,
    responseStatus: 200,
    refundResponse: {} as Record<string, unknown>,
  };
  const order = {
    id: "91",
    trade_no: "REFUND212",
    merchant_id: "1000",
    status: "paid",
    amount: "1000",
    refunded_amount: "0",
    name: "Local refund fixture",
    type: "alipay",
  };
  const orderView = () => ({
    ...order,
    realmoney: "1000",
    charge_verified104: !state.changedHold,
    refunded_amount: state.refundedAmount,
    income_status: state.changedHold ? "held" : "none",
    merchant_actions: { refund: state.refundedAmount === "0" },
  });
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      u = new URL(req.url()),
      p = u.pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: state.user,
            name: "Local",
            role: merchant ? "merchant" : "admin",
            admin_class: merchant ? undefined : "super",
          },
          csrf_token: state.csrf,
        }),
      );
    if (p.endsWith("/security/payment-password") && state.statusFailure)
      return route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ code: -1, message: "funds status unavailable" }),
      });
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: state.enforced,
          enforced: state.enforced,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (p.endsWith("/funds-authorizations/challenges"))
      return route.fulfill(
        ok({
          challenge_id: "challenge-212",
          expires_at: new Date(Date.now() + 120000).toISOString(),
          summary: { kind: "refund", amount: "2.50", recipient: "***1000" },
        }),
      );
    if (p.endsWith("/funds-authorizations/confirm"))
      return route.fulfill(
        ok({
          authorization: "ephemeral-grant-212",
          expires_at: new Date(Date.now() + 120000).toISOString(),
        }),
      );
    if (p.endsWith("/orders/REFUND212"))
      return route.fulfill(
        ok({
          order: orderView(),
          charge_verified104: !state.changedHold,
          reporting: { refunded_amount: state.refundedAmount },
          merchant_actions: { refund: state.refundedAmount === "0" },
          income_hold: state.changedHold
            ? { state: "held", source_mode: "legacy_unprepared" }
            : null,
          can_refund_held_income: false,
          admin_control: { status: "", archived: false, revision: "0" },
        }),
      );
    if (p.endsWith("/orders"))
      return route.fulfill(ok({ items: [orderView()], total: 1 }));
    if (p.endsWith("/funds-availability"))
      return route.fulfill(
        ok({
          settle_type: 0,
          available_money: "10.00",
          ledger_available_money: "10.00",
          today_blocked_money: "0.00",
        }),
      );
    if (p.endsWith("/settlement-recipient"))
      return route.fulfill(
        ok({
          state: "configured",
          config_id: state.recipientVersion,
          type: "alipay",
          app_id: "",
          account_masked: "***1234",
          name_masked: "***",
        }),
      );
    if (p.endsWith("/settlement-policy"))
      return route.fulfill(
        ok({
          can_apply: true,
          external_payout: false,
          terms: {
            gross_amount: "100",
            fee_amount: "0",
            net_amount: "100",
            retained_cents: "0",
          },
        }),
      );
    if (p.endsWith("/lookup")) {
      state.queries++;
      if (!state.found)
        return route.fulfill({
          status: 404,
          contentType: "application/json",
          body: JSON.stringify({ code: -1, message: "not found" }),
        });
      return route.fulfill(
        ok({
          id: "81",
          kind: settlement ? "settlements" : "internal-transfers",
          merchant_id: "1000",
          recipient_id: "1001",
          external_no: state.posts[0]?.external_no,
          amount: "1.00",
          amount_cents: "100",
          status: "pending",
          ...(settlement
            ? {
                settlement: { fee_amount: "0", net_amount: "100" },
                settlement_recipient: {
                  config_id: state.posts[0]?.recipient_config_id,
                },
              }
            : {}),
        }),
      );
    }
    if (p.endsWith("/refunds")) {
      state.queries++;
      return route.fulfill(
        ok({
          items: state.found
            ? [
                {
                  id: "77",
                  trade_no: "REFUND212",
                  merchant_id: "1000",
                  out_refund_no: state.posts[0]?.external_no,
                  refund_no: state.posts[0]?.external_no,
                  amount: state.fullRefundLost ? "1000" : "250",
                  status: "succeeded",
                },
              ]
            : [],
          total: state.found ? 1 : 0,
        }),
      );
    }
    if (
      req.method() === "POST" &&
      (p.endsWith("/refund") ||
        p.endsWith("/workflows/internal-transfers") ||
        p.endsWith("/workflows/settlements"))
    ) {
      const body = req.postDataJSON();
      state.posts.push(body);
      if (state.mode === "lost") {
        if (state.fullRefundLost) {
          state.refundedAmount = "1000";
          state.changedHold = true;
          state.found = true;
        }
        return route.abort("connectionreset");
      }
      if (state.mode === "rejected")
        return route.fulfill({
          status: state.refusalStatus,
          contentType: "application/json",
          body: JSON.stringify({
            code: -1,
            message: "请求明确拒绝：输入不合法",
            data: state.notRecorded
              ? {
                  request_not_recorded: true,
                  kind: settlement ? "settlements" : "internal-transfers",
                  merchant_id: state.user,
                  external_no: body.external_no,
                  reason: state.refusalReason,
                }
              : null,
          }),
        });
      return route.fulfill({
        ...ok(
          workflow
            ? {
                id: "81",
                kind: settlement ? "settlements" : "internal-transfers",
                merchant_id: "1000",
                recipient_id: body.recipient_id,
                external_no: body.external_no,
                amount: body.amount,
                amount_cents: "100",
                status: "pending",
                ...(settlement
                  ? {
                      settlement: {
                        fee_amount: body.expected_fee_cents,
                        net_amount: body.expected_net_cents,
                      },
                      settlement_recipient: {
                        config_id: body.recipient_config_id,
                      },
                    }
                  : {}),
              }
            : {
                id: "77",
                merchant_id: "1000",
                order_id: "91",
                refund_no: body.external_no,
                amount: "250",
                merchant_amount: "250",
                fee_amount: "0",
                status: "pending",
                ...state.refundResponse,
              },
        ),
        status: state.responseStatus,
      });
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  return state;
}

test("refund212 lost response survives close and reload with the immutable original refund", async ({
  page,
}) => {
  const state = await fixture(page);
  await page.goto("/admin/trade/order");
  await openAdminAPIRefund(page, "REFUND212");
  let dialog = page.getByRole("dialog", { name: "API退款确认", exact: true });
  await dialog.getByLabel("退款金额（元）").fill("2.50");
  const original = await dialog
    .getByLabel("退款请求号", { exact: true })
    .inputValue();
  await dialog
    .getByRole("button", { name: "确认申请退款", exact: true })
    .click();
  await expect(dialog.getByRole("alert")).toContainText("结果尚未核实");
  await expect(dialog.getByLabel("退款金额（元）")).not.toBeEditable();
  await expect(
    dialog.getByLabel("退款请求号", { exact: true }),
  ).not.toBeEditable();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await openAdminAPIRefund(page, "REFUND212");
  await expect(dialog.getByLabel("退款请求号", { exact: true })).toHaveValue(
    original,
  );
  await page.reload();
  await openAdminAPIRefund(page, "REFUND212");
  dialog = page.getByRole("dialog", { name: "API退款确认", exact: true });
  await expect(dialog.getByLabel("退款金额（元）")).toHaveValue("2.50");
  await expect(dialog.getByLabel("退款请求号", { exact: true })).toHaveValue(
    original,
  );
  await dialog.getByRole("button", { name: "查询原退款", exact: true }).click();
  await expect(dialog).toContainText("尚未查到");
  expect(state.posts).toHaveLength(1);
  state.mode = "accepted";
  await dialog
    .getByRole("button", { name: "重试原退款请求", exact: true })
    .click();
  await expect(dialog).toContainText("已登记");
  expect(state.posts).toEqual([
    { amount: "2.50", external_no: original },
    { amount: "2.50", external_no: original },
  ]);
  state.found = true;
  await dialog.getByRole("button", { name: "查询原退款", exact: true }).click();
  await expect(dialog).toContainText("退款成功");
});

test("workflow212 lost internal transfer restores recipient and amount and queries without a second reservation", async ({
  page,
}) => {
  const state = await fixture(page, true);
  await page.goto("/merchant/account/internal-transfer");
  await page.getByRole("button", { name: "发起站内转账", exact: true }).click();
  await page.getByLabel("收款商户 ID").fill("1001");
  await page.getByLabel("金额（元）").fill("1.00");
  const original = await page.getByLabel("业务请求号").inputValue();
  await page.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(page.getByRole("dialog").getByRole("alert")).toContainText(
    "结果尚未核实",
  );
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  await page.reload();
  await page.getByRole("button", { name: "发起站内转账", exact: true }).click();
  await expect(page.getByLabel("收款商户 ID")).toHaveValue("1001");
  await expect(page.getByLabel("金额（元）")).not.toBeEditable();
  await expect(page.getByLabel("收款商户 ID")).not.toBeEditable();
  await expect(page.getByLabel("业务请求号")).toHaveValue(original);
  state.found = true;
  await page.getByRole("button", { name: "查询原申请", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("申请已登记");
  expect(state.posts).toEqual([
    { amount: "1.00", external_no: original, recipient_id: "1001" },
  ]);
  expect(state.queries).toBe(1);
});

test("refund212 storage failure stops a new financial request before sending", async ({
  page,
}) => {
  const state = await fixture(page);
  await page.goto("/admin/trade/order");
  await openAdminAPIRefund(page, "REFUND212");
  await page.evaluate(() => {
    Storage.prototype.setItem = () => {
      throw new DOMException("quota", "QuotaExceededError");
    };
  });
  const dialog = page.getByRole("dialog", { name: "API退款确认", exact: true });
  await dialog.getByLabel("退款金额（元）").fill("2.50");
  await dialog
    .getByRole("button", { name: "确认申请退款", exact: true })
    .click();
  await expect(dialog.getByRole("alert")).toContainText("保存");
  expect(state.posts).toHaveLength(0);
});

for (const [label, change] of Object.entries({
  incomplete: { id: undefined, refund_no: "R1", amount: undefined },
  amount: { amount: "251" },
  number: { refund_no: "another-original-number" },
  merchant: { merchant_id: "1002" },
  order: { order_id: "92" },
}))
  test(`refund212 a ${label} success projection never claims verified success after closing`, async ({
    page,
  }) => {
    const state = await fixture(page);
    state.mode = "accepted";
    state.refundResponse = { status: "succeeded", ...change };
    await page.goto("/admin/trade/order");
    await openAdminAPIRefund(page, "REFUND212");
    const dialog = page.getByRole("dialog", {
      name: "API退款确认",
      exact: true,
    });
    await dialog.getByLabel("退款金额（元）").fill("2.50");
    const original = await dialog
      .getByLabel("退款请求号", { exact: true })
      .inputValue();
    await dialog
      .getByRole("button", { name: "确认申请退款", exact: true })
      .click();
    await expect(dialog.getByRole("status")).not.toContainText("退款成功");
    await expect(
      dialog.getByRole("button", { name: "已核对，结束本次退款", exact: true }),
    ).toHaveCount(0);
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "退款成功" }),
    ).toHaveCount(0);
    await openAdminAPIRefund(page, "REFUND212");
    await expect(dialog.getByLabel("退款请求号", { exact: true })).toHaveValue(
      original,
    );
    await expect(dialog.getByLabel("退款金额（元）")).not.toBeEditable();
    expect(state.posts).toHaveLength(1);
  });

for (const terminal of ["succeeded", "failed"])
  test(`refund212 HTTP202 with a complete ${terminal} snapshot remains unverified until original lookup`, async ({
    page,
  }) => {
    const state = await fixture(page);
    state.mode = "accepted";
    state.responseStatus = 202;
    state.refundResponse = { status: terminal };
    await page.goto("/admin/trade/order");
    await openAdminAPIRefund(page, "REFUND212");
    const dialog = page.getByRole("dialog", {
      name: "API退款确认",
      exact: true,
    });
    await dialog.getByLabel("退款金额（元）").fill("2.50");
    await dialog
      .getByRole("button", { name: "确认申请退款", exact: true })
      .click();
    await expect(dialog.getByRole("status")).toContainText("尚待核对");
    await expect(dialog.getByRole("status")).not.toContainText("已核实");
    await expect(
      dialog.getByRole("button", { name: "已核对，结束本次退款", exact: true }),
    ).toHaveCount(0);
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await page.reload();
    await openAdminAPIRefund(page, "REFUND212");
    await expect(
      dialog.getByRole("button", { name: "已核对，结束本次退款", exact: true }),
    ).toHaveCount(0);
    state.found = true;
    await dialog
      .getByRole("button", { name: "查询原退款", exact: true })
      .click();
    await expect(dialog.getByRole("status")).toContainText("退款成功");
    await expect(
      dialog.getByRole("button", { name: "已核对，结束本次退款", exact: true }),
    ).toBeEnabled();
    expect(state.posts).toHaveLength(1);
    expect(state.queries).toBe(1);
  });

for (const unsent of ["status503", "cancel"])
  test(`refund212 ${unsent} before business dispatch permits correction without a saved sent intent`, async ({
    page,
  }) => {
    const state = await fixture(page);
    state.statusFailure = unsent === "status503";
    state.enforced = unsent === "cancel";
    await page.goto("/admin/trade/order");
    await openAdminAPIRefund(page, "REFUND212");
    const form = page.getByRole("dialog", { name: "API退款确认", exact: true });
    await form.getByLabel("退款金额（元）").fill("2.50");
    await form
      .getByRole("button", { name: "确认申请退款", exact: true })
      .click();
    if (unsent === "cancel")
      await page
        .getByRole("dialog", { name: "管理员资金二次授权", exact: true })
        .getByRole("button", { name: "关闭资金授权", exact: true })
        .click();
    await expect(form.getByRole("alert")).toContainText(
      unsent === "cancel" ? "资金授权已取消" : "funds status unavailable",
    );
    await expect(form.getByLabel("退款金额（元）")).toBeEditable();
    expect(state.posts).toHaveLength(0);
    expect(
      await page.evaluate(() =>
        Object.keys(localStorage).filter((key) =>
          key.includes("funds-intent212"),
        ),
      ),
    ).toEqual([]);
    state.statusFailure = false;
    state.enforced = false;
    await form.getByLabel("退款金额（元）").fill("3.00");
    await form
      .getByRole("button", { name: "确认申请退款", exact: true })
      .click();
    await expect(form.getByRole("alert")).toContainText("结果尚未核实");
    expect(state.posts).toHaveLength(1);
    expect(state.posts[0].amount).toBe("3.00");
  });

test("refund212 authorized business dispatch with a lost response still persists the original intent", async ({
  page,
}) => {
  const state = await fixture(page);
  state.enforced = true;
  await page.goto("/admin/trade/order");
  await openAdminAPIRefund(page, "REFUND212");
  const form = page.getByRole("dialog", { name: "API退款确认", exact: true });
  await form.getByLabel("退款金额（元）").fill("2.50");
  const original = await form
    .getByLabel("退款请求号", { exact: true })
    .inputValue();
  await form.getByRole("button", { name: "确认申请退款", exact: true }).click();
  const auth = page.getByRole("dialog", {
    name: "管理员资金二次授权",
    exact: true,
  });
  await auth
    .getByRole("button", { name: "已核对摘要，验证资金密码", exact: true })
    .click();
  await auth
    .getByLabel("本人管理员资金密码", { exact: true })
    .fill("not-persisted-password-212");
  await auth.getByRole("button", { name: "确认资金密码", exact: true }).click();
  expect(state.posts).toHaveLength(0);
  await auth
    .getByRole("button", { name: "执行已核对的原操作", exact: true })
    .click();
  await expect(form.getByRole("alert")).toContainText("结果尚未核实");
  await page.reload();
  await openAdminAPIRefund(page, "REFUND212");
  await expect(form.getByLabel("退款请求号", { exact: true })).toHaveValue(
    original,
  );
  await expect(form.getByLabel("退款金额（元）")).not.toBeEditable();
  const storage = await page.evaluate(() =>
    JSON.stringify({ ...localStorage }),
  );
  expect(storage).not.toContain("not-persisted-password-212");
  expect(storage).not.toContain("ephemeral-grant-212");
  expect(state.posts).toEqual([{ amount: "2.50", external_no: original }]);
});

test("refund212 a definitive refusal unlocks correction and does not survive as an unknown intent", async ({
  page,
}) => {
  const state = await fixture(page);
  state.mode = "rejected";
  await page.goto("/admin/trade/order");
  await openAdminAPIRefund(page, "REFUND212");
  const dialog = page.getByRole("dialog", { name: "API退款确认", exact: true });
  await dialog.getByLabel("退款金额（元）").fill("2.50");
  await dialog
    .getByRole("button", { name: "确认申请退款", exact: true })
    .click();
  await expect(dialog.getByRole("alert")).toContainText("明确拒绝");
  await expect(dialog.getByLabel("退款金额（元）")).toBeEditable();
  await page.reload();
  await openAdminAPIRefund(page, "REFUND212");
  await expect(
    dialog.getByRole("button", { name: "重试原退款请求", exact: true }),
  ).toHaveCount(0);
});

test("workflow212 a changed principal cannot restore another merchant's pending transfer", async ({
  page,
}) => {
  const state = await fixture(page, true);
  await page.goto("/merchant/account/internal-transfer");
  await page.getByRole("button", { name: "发起站内转账", exact: true }).click();
  await page.getByLabel("收款商户 ID").fill("1001");
  await page.getByLabel("金额（元）").fill("1.00");
  const original = await page.getByLabel("业务请求号").inputValue();
  await page.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(page.getByRole("dialog").getByRole("alert")).toBeVisible();
  state.user = "1002";
  state.csrf = "other-session-212";
  await page.reload();
  await page.getByRole("button", { name: "发起站内转账", exact: true }).click();
  await expect(page.getByLabel("金额（元）")).toHaveValue("");
  await expect(page.getByLabel("业务请求号")).not.toHaveValue(original);
  expect(state.queries).toBe(0);
});

test("workflow212 the same merchant can recover after reauthentication without resending automatically", async ({
  page,
}) => {
  const state = await fixture(page, true);
  await page.goto("/merchant/account/internal-transfer");
  await page.getByRole("button", { name: "发起站内转账", exact: true }).click();
  await page.getByLabel("收款商户 ID").fill("1001");
  await page.getByLabel("金额（元）").fill("1.00");
  const original = await page.getByLabel("业务请求号").inputValue();
  await page.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(page.getByRole("dialog").getByRole("alert")).toContainText(
    "结果尚未核实",
  );
  state.csrf = "new-authenticated-session-proof-212";
  await page.reload();
  await page.getByRole("button", { name: "发起站内转账", exact: true }).click();
  await expect(page.getByLabel("金额（元）")).toHaveValue("1.00");
  await expect(page.getByLabel("金额（元）")).not.toBeEditable();
  await expect(page.getByLabel("业务请求号")).toHaveValue(original);
  state.found = true;
  await page.getByRole("button", { name: "查询原申请", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("申请已登记");
  expect(state.posts).toHaveLength(1);
});

test("refund212 a fully refunded merchant order keeps an independent original-refund recovery entry after reload", async ({
  page,
}) => {
  const state = await fixture(page, false, false, true);
  state.fullRefundLost = true;
  await page.goto("/merchant/trade/order");
  await page
    .getByRole("button", { name: "订单操作 REFUND212", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "退款", exact: true }).click();
  const create = page.getByRole("dialog", { name: "申请退款", exact: true });
  await create.getByLabel("退款金额（元）").fill("10.00");
  const original = await create
    .getByLabel("退款请求号", { exact: true })
    .inputValue();
  await create
    .getByRole("button", { name: "确认申请退款", exact: true })
    .click();
  await expect(create.getByRole("alert")).toContainText("结果尚未核实");
  await create.getByRole("button", { name: "取消", exact: true }).click();
  await page.reload();
  await page
    .getByRole("button", { name: "订单操作 REFUND212", exact: true })
    .click();
  await expect(
    page.getByRole("menuitem", { name: "退款", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("menuitem", { name: "核对原退款", exact: true }).click();
  const recover = page.getByRole("dialog", { name: "核对原退款", exact: true });
  await expect(recover.getByLabel("退款请求号", { exact: true })).toHaveValue(
    original,
  );
  await expect(recover.getByLabel("退款金额（元）")).toHaveValue("10.00");
  await expect(recover.getByLabel("退款金额（元）")).not.toBeEditable();
  await expect(
    recover.getByRole("button", { name: "确认申请退款", exact: true }),
  ).toHaveCount(0);
  await recover
    .getByRole("button", { name: "查询原退款", exact: true })
    .click();
  await expect(recover).toContainText("退款成功");
  await recover
    .getByRole("button", { name: "已核对，结束本次退款", exact: true })
    .click();
  await page
    .getByRole("button", { name: "订单操作 REFUND212", exact: true })
    .click();
  await expect(
    page.getByRole("menuitem", { name: "核对原退款", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("menuitem", { name: "退款", exact: true }),
  ).toHaveCount(0);
  expect(state.posts).toEqual([{ amount: "10.00", external_no: original }]);
});

test("refund212 an administrator can recover after lost response and changed charge or held-funding eligibility", async ({
  page,
}) => {
  const state = await fixture(page);
  state.fullRefundLost = true;
  await page.goto("/admin/trade/order");
  await openAdminAPIRefund(page, "REFUND212");
  const create = page.getByRole("dialog", { name: "API退款确认", exact: true });
  await create.getByLabel("退款金额（元）").fill("10.00");
  const original = await create
    .getByLabel("退款请求号", { exact: true })
    .inputValue();
  await create
    .getByRole("button", { name: "确认申请退款", exact: true })
    .click();
  await expect(create.getByRole("alert")).toContainText("结果尚未核实");
  await create.getByRole("button", { name: "取消", exact: true }).click();
  await page.reload();
  state.user = "2";
  state.csrf = "other-admin-session-212";
  await page.reload();
  await page
    .getByRole("button", { name: "订单操作 REFUND212", exact: true })
    .click();
  await expect(
    page.getByRole("menuitem", { name: "核对原退款", exact: true }),
  ).toHaveCount(0);
  await page.keyboard.press("Escape");
  state.user = "1";
  state.csrf = "same-admin-new-session-212";
  await page.reload();
  await page
    .getByRole("button", { name: "订单操作 REFUND212", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "API退款", exact: true }).click();
  const blocked = page.getByRole("dialog", {
    name: "API退款确认",
    exact: true,
  });
  await expect(blocked.getByRole("alert")).toContainText(
    "实际支付金额无法核对",
  );
  await expect(blocked.getByLabel("退款金额（元）")).toHaveCount(0);
  await blocked
    .getByRole("button", { name: "关闭", exact: true })
    .first()
    .click();
  await page
    .getByRole("button", { name: "订单操作 REFUND212", exact: true })
    .click();
  await expect(
    page.getByRole("menuitem", { name: "核对原退款", exact: true }),
  ).toBeVisible();
  await page.getByRole("menuitem", { name: "核对原退款", exact: true }).click();
  const recover = page.getByRole("dialog", { name: "核对原退款", exact: true });
  await expect(recover.getByLabel("退款请求号", { exact: true })).toHaveValue(
    original,
  );
  await expect(recover.getByLabel("退款金额（元）")).toHaveValue("10.00");
  await expect(recover.getByLabel("退款金额（元）")).not.toBeEditable();
  await expect(
    recover.getByRole("button", { name: "确认申请退款", exact: true }),
  ).toHaveCount(0);
  await recover
    .getByRole("button", { name: "查询原退款", exact: true })
    .click();
  await expect(recover).toContainText("退款成功");
  await recover
    .getByRole("button", { name: "已核对，结束本次退款", exact: true })
    .click();
  await page
    .getByRole("button", { name: "订单操作 REFUND212", exact: true })
    .click();
  await expect(
    page.getByRole("menuitem", { name: "核对原退款", exact: true }),
  ).toHaveCount(0);
  expect(state.queries).toBe(1);
  expect(state.posts).toEqual([{ amount: "10.00", external_no: original }]);
});

for (const role of ["merchant", "admin"])
  for (const storageState of ["missing", "corrupt"])
    test(`refund212 a stale ${role} recovery entry fails closed when original storage is ${storageState}`, async ({
      page,
    }) => {
      const state = await fixture(page, false, false, role === "merchant");
      state.fullRefundLost = true;
      await page.goto(`/${role}/trade/order`);
      if (role === "admin") await openAdminAPIRefund(page, "REFUND212");
      else {
        await page
          .getByRole("button", { name: "订单操作 REFUND212", exact: true })
          .click();
        await page.getByRole("menuitem", { name: "退款", exact: true }).click();
      }
      const create = page.getByRole("dialog", {
        name: role === "admin" ? "API退款确认" : "申请退款",
        exact: true,
      });
      await create.getByLabel("退款金额（元）").fill("10.00");
      await create
        .getByRole("button", { name: "确认申请退款", exact: true })
        .click();
      await expect(create.getByRole("alert")).toContainText("结果尚未核实");
      await create.getByRole("button", { name: "取消", exact: true }).click();
      await page.reload();
      await page
        .getByRole("button", { name: "订单操作 REFUND212", exact: true })
        .click();
      const recover = page.getByRole("menuitem", {
        name: "核对原退款",
        exact: true,
      });
      await expect(recover).toBeVisible();
      await page.evaluate((value) => {
        const key = Object.keys(localStorage).find((candidate) =>
          candidate.endsWith(".refund.REFUND212"),
        );
        if (!key) throw new Error("original refund fixture must exist");
        if (value === "missing") localStorage.removeItem(key);
        else localStorage.setItem(key, "{invalid");
      }, storageState);
      await recover.click();
      await expect(page.getByRole("alert")).toContainText(
        storageState === "missing" ? "未找到" : "无法安全读取或保存原资金请求",
      );
      await expect(page.getByRole("dialog")).toHaveCount(0);
      await page
        .getByRole("button", { name: "订单操作 REFUND212", exact: true })
        .click();
      await expect(recover).toHaveCount(0);
      if (role === "merchant")
        await expect(
          page.getByRole("menuitem", { name: "退款", exact: true }),
        ).toHaveCount(0);
      await page.keyboard.press("Escape");
      await page.reload();
      await page
        .getByRole("button", { name: "订单操作 REFUND212", exact: true })
        .click();
      await expect(recover).toHaveCount(0);
      expect(state.posts).toHaveLength(1);
      expect(state.queries).toBe(0);
    });

for (const status of [401, 403, 429])
  test(`refund212 a retry refusal ${status} cannot erase an earlier unknown refund`, async ({
    page,
  }) => {
    const state = await fixture(page);
    await page.goto("/admin/trade/order");
    await openAdminAPIRefund(page, "REFUND212");
    let dialog = page.getByRole("dialog", { name: "API退款确认", exact: true });
    await dialog.getByLabel("退款金额（元）").fill("2.50");
    const original = await dialog
      .getByLabel("退款请求号", { exact: true })
      .inputValue();
    await dialog
      .getByRole("button", { name: "确认申请退款", exact: true })
      .click();
    await expect(dialog.getByRole("alert")).toContainText("结果尚未核实");
    state.mode = "rejected";
    state.refusalStatus = status;
    await dialog
      .getByRole("button", { name: "重试原退款请求", exact: true })
      .click();
    if (status === 401) {
      await expect(
        page.getByRole("button", { name: "登录工作台" }),
      ).toBeVisible();
      await page.reload();
      await openAdminAPIRefund(page, "REFUND212");
      dialog = page.getByRole("dialog", { name: "API退款确认", exact: true });
    }
    await expect(dialog.getByLabel("退款金额（元）")).not.toBeEditable();
    await expect(dialog.getByLabel("退款请求号", { exact: true })).toHaveValue(
      original,
    );
    expect(state.posts[1]).toEqual(state.posts[0]);
  });

test("settlement212 reload and retry preserve the original fee and recipient version after configuration changes", async ({
  page,
}) => {
  const state = await fixture(page, true, true);
  await page.goto("/merchant/settle/apply");
  const form = page.getByRole("region", { name: "申请提现", exact: true });
  await form.getByLabel("金额（元）", { exact: true }).fill("1.00");
  await form.getByRole("button", { name: "确认提交", exact: true }).click();
  await form
    .getByRole("button", { name: "按已核对费用提交", exact: true })
    .click();
  await expect(form.getByRole("alert")).toContainText("结果尚未核实");
  const original = state.posts[0];
  state.recipientVersion = "99";
  await page.reload();
  await expect(form.getByLabel("金额（元）", { exact: true })).toHaveValue(
    "1.00",
  );
  await expect(
    form.getByLabel("金额（元）", { exact: true }),
  ).not.toBeEditable();
  await expect(form).toContainText("原收款版本：42");
  state.mode = "rejected";
  state.refusalStatus = 429;
  await form
    .getByRole("button", { name: "重试原申请请求", exact: true })
    .click();
  await expect(
    form.getByLabel("金额（元）", { exact: true }),
  ).not.toBeEditable();
  expect(state.posts[1]).toEqual(original);
  state.refusalStatus = 409;
  state.notRecorded = true;
  await form
    .getByRole("button", { name: "重试原申请请求", exact: true })
    .click();
  await expect(
    form.getByLabel("金额（元）", { exact: true }),
  ).not.toBeEditable();
  expect(state.posts[2]).toEqual(original);
  state.mode = "accepted";
  state.found = true;
  await form.getByRole("button", { name: "查询原申请", exact: true }).click();
  await expect(form).toContainText("申请已登记");
  expect(original.recipient_config_id).toBe("42");
  expect(state.posts).toHaveLength(3);
});

for (const reason of ["conflict", "insufficient"])
  test(`workflow212 first confirmed not-recorded ${reason} permits corrected submission`, async ({
    page,
  }) => {
    const state = await fixture(page, true, reason === "conflict");
    state.mode = "rejected";
    state.refusalStatus = 409;
    state.notRecorded = true;
    state.refusalReason = reason;
    await page.goto(
      reason === "conflict"
        ? "/merchant/settle/apply"
        : "/merchant/account/internal-transfer",
    );
    if (reason !== "conflict") {
      await page
        .getByRole("button", { name: "发起站内转账", exact: true })
        .click();
      await page.getByLabel("收款商户 ID").fill("1001");
    }
    const form =
      reason === "conflict"
        ? page.getByRole("region", { name: "申请提现", exact: true })
        : page.getByRole("dialog");
    await form.getByLabel("金额（元）", { exact: true }).fill("1.00");
    await form.getByRole("button", { name: "确认提交", exact: true }).click();
    if (reason === "conflict")
      await form
        .getByRole("button", { name: "按已核对费用提交", exact: true })
        .click();
    await expect(form.getByRole("alert")).toContainText("明确拒绝");
    await expect(form.getByLabel("金额（元）", { exact: true })).toBeEditable();
    state.mode = "accepted";
    await form.getByLabel("金额（元）", { exact: true }).fill("1.00");
    await form.getByRole("button", { name: "确认提交", exact: true }).click();
    if (reason === "conflict")
      await form
        .getByRole("button", { name: "按已核对费用提交", exact: true })
        .click();
    await expect(page.locator(".alert.success")).toContainText("申请已提交");
    expect(state.posts).toHaveLength(2);
  });
