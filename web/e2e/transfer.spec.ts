import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
for (const width of [1280, 375])
  test(`group permission does not misreport global or execution state at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    let writes = 0;
    await page.route("**/console/v1/**", (route) => {
      const r = route.request(),
        p = new URL(r.url()).pathname;
      if (r.method() !== "GET") writes++;
      if (p.endsWith("/session"))
        return route.fulfill(
          ok({
            user: { id: "1000", name: "Merchant", role: "merchant" },
            csrf_token: "local144",
          }),
        );
      if (p.endsWith("/capabilities"))
        return route.fulfill(
          ok({
            writes_enabled: false,
            platform_enabled: false,
            effective_enabled: true,
            permission_source: "group",
            effective_group_id: "9007199254740993",
            merchant_enabled: true,
            transfer_rate: "0",
            available_money: "10.00",
            ledger_available_money: "10.00",
            today_blocked_money: "0.00",
            settle_type: 0,
            channels: [{ id: "42", types: ["alipay"] }],
          }),
        );
      return route.fulfill(ok({ items: [], total: 0 }));
    });
    await page.goto("/merchant/transfer");
    await page.getByRole("button", { name: "申请付款", exact: true }).click();
    const modal = page.getByRole("dialog", { name: "申请付款" });
    await expect(modal).not.toContainText("平台付款功能未启用");
    await expect(modal).toContainText("当前运行环境或通道未启用付款");
    await expect(
      modal.getByRole("button", { name: "核对付款信息" }),
    ).toBeDisabled();
    expect(writes).toBe(0);
  });
test("disabled transfer capability blocks submission without creating a payment", async ({
  page,
}) => {
  let writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Merchant", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/capabilities"))
      return route.fulfill(
        ok({
          writes_enabled: false,
          platform_enabled: false,
          merchant_enabled: true,
          transfer_rate: "3",
          available_money: "10.00",
          ledger_available_money: "10.00",
          today_blocked_money: "0.00",
          settle_type: 0,
          channels: [{ id: "42", types: ["alipay"] }],
        }),
      );
    if (route.request().method() === "POST") writes++;
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/transfer");
  await page.getByRole("button", { name: "申请付款", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "申请付款" });
  await expect(modal).toContainText("平台付款功能未启用");
  await expect(
    modal.getByRole("button", { name: "核对付款信息" }),
  ).toBeDisabled();
  expect(writes).toBe(0);
});
for (const width of [1280, 375])
  test(`disabled group is distinguished from global and runtime permission at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    let writes = 0;
    await page.route("**/console/v1/**", (route) => {
      const r = route.request(),
        p = new URL(r.url()).pathname;
      if (r.method() !== "GET") writes++;
      if (p.endsWith("/session"))
        return route.fulfill(
          ok({
            user: { id: "1000", role: "merchant" },
            csrf_token: "local145",
          }),
        );
      if (p.endsWith("/capabilities"))
        return route.fulfill(
          ok({
            writes_enabled: false,
            platform_enabled: true,
            effective_enabled: false,
            permission_source: "group_disabled",
            merchant_enabled: true,
            transfer_rate: "0",
            available_money: "10.00",
            ledger_available_money: "10.00",
            today_blocked_money: "0.00",
            settle_type: 0,
            channels: [{ id: "42", types: ["alipay"] }],
          }),
        );
      return route.fulfill(ok({ items: [], total: 0 }));
    });
    await page.goto("/merchant/transfer");
    await page.getByRole("button", { name: "申请付款", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "申请付款" });
    await expect(dialog).toContainText("当前用户组付款功能未启用");
    await expect(
      dialog.getByRole("button", { name: "核对付款信息" }),
    ).toBeDisabled();
    expect(writes).toBe(0);
  });
test("wallet transfer keeps uncertain reservation and only queries original payment; proof requires confirmation", async ({
  page,
}) => {
  let submitted: Record<string, unknown> | undefined,
    creates = 0,
    queries = 0,
    proofs = 0;
  const transfer = {
    id: "7",
    transfer_no: "TF-7",
    type: "alipay",
    amount: "100",
    fee_amount: "3",
    cost_amount: "103",
    status: "unknown",
    channel_id: "42",
  };
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Merchant", role: "merchant" },
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
          ledger_available_money: "12.00",
          today_blocked_money: "2.00",
          settle_type: 1,
          channels: [{ id: "42", types: ["alipay", "wxpay", "future-bank"] }],
        }),
      );
    if (p.endsWith("/query")) {
      queries++;
      return route.fulfill(ok({ ...transfer, status: "succeeded" }));
    }
    if (p.endsWith("/proof")) {
      proofs++;
      return route.fulfill(
        ok({
          file_id: "FILE-7",
          state: "pending",
          download_url: "javascript:alert(1)",
        }),
      );
    }
    if (p.endsWith("/transfers/7"))
      return route.fulfill(
        ok({
          transfer,
          recipient: { account: "account@example.test", name: "Test" },
        }),
      );
    if (r.method() === "POST") {
      creates++;
      submitted = r.postDataJSON();
      expect(r.headers()["x-csrf-token"]).toBe("csrf");
      return route.fulfill(ok(transfer, 202));
    }
    return route.fulfill(
      ok({ items: creates ? [transfer] : [], total: creates ? 1 : 0 }),
    );
  });
  await page.goto("/merchant/transfer");
  await page.getByRole("button", { name: "申请付款", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "申请付款" });
  await expect(modal.getByLabel("收款方式").locator("option")).toHaveCount(2);
  await expect(modal).toContainText("T+1");
  await expect(modal).toContainText("账面可用余额（元）");
  await expect(modal).toContainText("12.00");
  await expect(modal).toContainText("可出账金额（元）");
  await expect(modal).toContainText("10.00");
  await modal.getByLabel("收款账号").fill("account@example.test");
  await modal.getByLabel("付款金额（元）").fill("1.00");
  await modal.getByLabel("业务请求号").fill("stable-request-7");
  await modal.getByRole("button", { name: "核对付款信息" }).click();
  await expect(modal).toContainText("当日实际受限金额（元）");
  await expect(modal).toContainText("2.00");
  expect(creates).toBe(0);
  await modal.getByRole("button", { name: "确认提交付款" }).click();
  const detail = page.getByRole("dialog", { name: "付款详情" });
  await expect(detail).toContainText("付款结果尚未核实，预占资金将保留");
  expect(submitted).toMatchObject({
    amount: "1.00",
    external_no: "stable-request-7",
    channel_id: "42",
    type: "alipay",
    account: "account@example.test",
  });
  expect(submitted).not.toHaveProperty("merchant_id");
  expect(creates).toBe(1);
  expect(queries).toBe(0);
  await detail.getByRole("button", { name: "查询通道付款结果" }).click();
  await expect(detail).toContainText("通道已确认付款成功");
  expect(queries).toBe(1);
  expect(creates).toBe(1);
  await detail
    .getByRole("button", { name: "申请付款凭证", exact: true })
    .click();
  expect(proofs).toBe(0);
  await detail.getByRole("button", { name: "确认申请凭证" }).click();
  await expect(detail).toContainText("FILE-7");
  expect(proofs).toBe(1);
  await expect(detail.getByRole("link", { name: "下载付款凭证" })).toHaveCount(
    0,
  );
  await detail.getByRole("button", { name: "查询付款凭证" }).click();
  await expect.poll(() => proofs).toBe(2);
  expect(creates).toBe(1);
});
test("WeChat requires a payment remark and admin merchant changes invalidate capabilities", async ({
  page,
}) => {
  let writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/capabilities")) {
      expect(
        new URL(route.request().url()).searchParams.get("merchant_id"),
      ).toBe("1000");
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
          channels: [{ id: "42", types: ["wxpay"] }],
        }),
      );
    }
    if (route.request().method() === "POST") writes++;
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/admin/transfer/list");
  await page.getByRole("button", { name: "申请付款", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "申请付款" });
  await modal.getByLabel("付款商户 ID").fill("1000");
  await modal.getByRole("button", { name: "读取付款权限" }).click();
  await modal.getByLabel("收款账号").fill("OPENID");
  await modal.getByLabel("付款金额（元）").fill("1.00");
  await expect(modal.getByLabel("付款说明（必填）")).toHaveAttribute(
    "required",
    "",
  );
  await modal.getByRole("button", { name: "核对付款信息" }).click();
  await expect(modal.getByRole("button", { name: "确认提交付款" })).toHaveCount(
    0,
  );
  await modal.getByLabel("付款说明（必填）").fill("订单退款补偿");
  await modal.getByLabel("付款商户 ID").fill("1001");
  await expect(
    modal.getByRole("button", { name: "核对付款信息" }),
  ).toBeDisabled();
  expect(writes).toBe(0);
});
