import { test, expect } from "@playwright/test";

const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const session = {
  user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
  csrf_token: "mock-only",
};

test("order cells preserve string IDs and exact yuan while notification failure stays independent", async ({
  page,
}) => {
  const order = {
    trade_no: "9007199254740993123",
    out_trade_no: "MERCHANT-9007199254740993123",
    api_trade_no: "PROVIDER-9007199254740993123",
    merchant_id: "1000",
    name: "演示商品",
    amount: "9007199254740993",
    merchant_amount: "9007199254740992",
    platform_fee: "1",
    refunded_amount: "23",
    type: "wxpay",
    channel_id: "9007199254740993124",
    status: "paid",
    notification_state: "dead",
    api_key: "must-never-display",
    created_at: "2026-10-24T00:00:00Z",
  };
  let providerCalls = 0;
  await page.route("**/console/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (/\/(query|close|refund)$/.test(path)) providerCalls++;
    return route.fulfill(
      ok(
        path.endsWith("/session")
          ? session
          : path.endsWith(order.trade_no)
            ? {
                order,
                reporting: {
                  api_trade_no: order.api_trade_no,
                  channel_id: order.channel_id,
                  notification_state: "dead",
                  refunded_amount: "23",
                },
              }
            : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/admin/trade/order");
  expect(
    (await page.getByLabel("搜索内容", { exact: true }).boundingBox())!.width,
  ).toBeGreaterThanOrEqual(160);
  const row = page.getByRole("row").filter({ hasText: order.trade_no });
  // epay_pro two-line cells: the row shows the exact order amount (grouped yuan)
  // and the refunded amount; merchant入账 / 通道 ID / 接口订单号 moved to the detail.
  await expect(row).toContainText("¥90,071,992,547,409.93");
  await expect(row).toContainText("已退款 ¥0.23");
  await expect(row).toContainText(order.out_trade_no);
  await expect(row.getByText("已支付", { exact: true })).toBeVisible();
  await expect(row.getByText("通知失败", { exact: true })).toBeVisible();
  await expect(page.getByText(order.api_key, { exact: true })).toHaveCount(0);
  await page.screenshot({
    path: test.info().outputPath("order-fields.png"),
    fullPage: true,
  });
  await row.getByRole("button", { name: "详情", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "订单详情" });
  await expect(dialog).toContainText("订单金额");
  await expect(dialog).toContainText("90071992547409.93");
  await expect(dialog).toContainText("商户入账额");
  await expect(dialog).toContainText("90071992547409.92");
  await expect(dialog).toContainText("已退款金额");
  await expect(dialog).toContainText("0.23");
  await expect(dialog).toContainText(order.api_trade_no);
  await expect(dialog).toContainText("9007199254740993124");
  await expect(dialog).toContainText("通知失败");
  await expect(dialog.getByText(order.api_key, { exact: true })).toHaveCount(0);
  expect(providerCalls).toBe(0);
});

test("merchant list is compact and permission driven without copying secret fields", async ({
  page,
}) => {
  const merchant = {
    id: "9007199254740993123",
    name: "测试商户",
    email: "masked@example.invalid",
    enabled: true,
    base_group_id: "3",
    api_key: "must-never-display",
    available_minor: "9007199254740993",
    frozen_minor: "123",
    income_held_minor: "456",
    created_at: "2026-10-24T00:00:00Z",
    effective_group_id: "3",
    effective_group_name: "专业组",
    membership_expires_at: "2026-11-01T00:00:00Z",
    group_membership_active: true,
    approved_domains: ["shop.example.com", "pay.example.com"],
    settlement_recipient: {
      state: "configured",
      config_id: "1",
      type: "alipay",
      app_id: "",
      account_masked: "***1234",
      name_masked: "***",
    },
    account: "legacy-plaintext-account",
  };
  const fields = [
    { name: "id", label: "商户 ID", type: "string" },
    { name: "name", label: "名称", type: "string" },
    { name: "email", label: "电子邮箱", type: "email" },
    { name: "enabled", label: "启用", type: "boolean" },
    { name: "base_group_id", label: "基础组账务快照", type: "string" },
    { name: "api_key", label: "API密钥", type: "string" },
    ...[
      "effective_group_id",
      "effective_group_name",
      "membership_expires_at",
      "group_membership_active",
      "approved_domains",
    ].map((name) => ({
      name,
      label: name,
      type:
        name === "group_membership_active"
          ? "boolean"
          : name === "approved_domains"
            ? "json"
            : "string",
      read_only: true,
    })),
    {
      name: "settlement_recipient",
      label: "结算账号 / 姓名（脱敏）",
      type: "json",
      read_only: true,
    },
    {
      name: "available_minor",
      label: "可用余额（分）",
      type: "string",
      read_only: true,
    },
    {
      name: "frozen_minor",
      label: "冻结预留（分）",
      type: "string",
      read_only: true,
    },
    {
      name: "income_held_minor",
      label: "待解冻收入（分）",
      type: "string",
      read_only: true,
    },
    { name: "created_at", label: "注册时间", type: "string", read_only: true },
  ];
  await page.route("**/console/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    return route.fulfill(
      ok(
        path.endsWith("/session")
          ? session
          : path.endsWith("/schema")
            ? {
                fields,
                permissions: { create: false, update: false, delete: false },
              }
            : path.endsWith(merchant.id)
              ? merchant
              : { items: [merchant], total: 1 },
      ),
    );
  });
  await page.goto("/admin/merchant/merchant-list");
  await expect(page.getByRole("columnheader")).toHaveText([
    "商户号 / 用户组",
    "名称",
    "可用余额",
    "结算账号 / 姓名（脱敏）",
    "电子邮箱",
    "注册时间",
    "启用",
    "操作",
  ]);
  await expect(
    page.getByRole("cell").filter({ hasText: "9007199254740993123" }),
  ).toBeVisible();
  await expect(
    page.getByRole("cell", { name: "正常", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "编辑", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("cell").filter({ hasText: "9007199254740993123" }),
  ).toContainText("专业组（3）");
  await expect(
    page.getByRole("cell", { name: "支付宝 · ***1234 / ***", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("cell").filter({ hasText: "2026-10-24 08:00:00" }),
  ).toContainText("shop.example.com");
  await expect(
    page.getByRole("link", { name: "shop.example.com", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByText("legacy-plaintext-account", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByText("must-never-display", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("cell").filter({ hasText: "冻结预留：1.23" }),
  ).toContainText("90071992547409.93");
  await expect(
    page.getByRole("cell").filter({ hasText: "冻结预留：1.23" }),
  ).toContainText("待解冻收入：4.56");
  await page.screenshot({
    path: test.info().outputPath("merchant-fields.png"),
    fullPage: true,
  });
  // 详情 now lives in the row's 商户关联记录 overflow menu (read-only row).
  await page
    .getByRole("button", { name: `商户关联记录 ${merchant.id}`, exact: true })
    .click();
  await page.getByRole("menuitem", { name: "详情", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("冻结预留1.23");
  await expect(dialog).toContainText("待解冻收入4.56");
  await expect(dialog).not.toContainText("must-never-display");
  await expect(dialog).not.toContainText("legacy-plaintext-account");
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  merchant.settlement_recipient.state = "unavailable";
  await page.getByRole("button", { name: "刷新", exact: true }).click();
  await expect(
    page.getByRole("cell", { name: "资料不可用", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("cell", { name: "支付宝 · ***1234 / ***", exact: true }),
  ).toHaveCount(0);
});

for (const area of ["admin", "merchant"] as const)
  test(`ledger ${area} shows original per-account before and after balances with exact units`, async ({
    page,
  }) => {
    const entry = {
      id: "11",
      merchant_id: "1000",
      account_id: "9007199254740993123",
      description: "订单退款",
      amount: "-1",
      balance_before: "9223372036854775807",
      balance_after: "9223372036854775806",
      kind: "available",
      created_at: "2026-10-24",
      event_key: "REFUND-11",
    };
    await page.route("**/console/v1/**", (route) =>
      route.fulfill(
        ok(
          new URL(route.request().url()).pathname.endsWith("/session")
            ? {
                user: {
                  id: area === "admin" ? "1" : "1000",
                  name: "Test",
                  role: area,
                  admin_class: "super",
                },
                csrf_token: "x",
              }
            : { items: [entry], total: 100 },
        ),
      ),
    );
    await page.goto(
      area === "admin" ? "/admin/merchant/record" : "/merchant/record",
    );
    await expect(
      page.getByRole("columnheader", { name: "账户变更前", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "92233720368547758.07", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "92233720368547758.06", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "¥-0.01", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: entry.account_id, exact: true }),
    ).toHaveCount(0);
    await page
      .getByRole("button", { name: "查看资金明细 11", exact: true })
      .click();
    const ledgerDetail = page.getByRole("dialog", {
      name: "资金明细详情",
      exact: true,
    });
    await expect(
      ledgerDetail.getByText(entry.account_id, { exact: true }),
    ).toBeVisible();
    await expect(
      ledgerDetail.getByText("可用余额", { exact: true }),
    ).toBeVisible();
    if (area === "merchant")
      await expect(
        page.getByRole("columnheader", { name: "商户号", exact: true }),
      ).toHaveCount(0);
  });

test("merchant original daily preserves yuan fields and weighted summary without date controls", async ({
  page,
}) => {
  await page.clock.setFixedTime(new Date("2026-10-23T16:30:00Z"));
  const queries: URL[] = [];
  const row = {
    day: "2026-10-24",
    realmoney: "1.23",
    getmoney: "1.11",
    refundmoney: "0.10",
    orderCount: "3",
    successCount: "2",
    successRate: "66.67",
  };
  await page.route("**/console/v1/**", (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/session"))
      return route.fulfill(
        ok({ ...session, user: { ...session.user, role: "merchant" } }),
      );
    if (url.pathname.endsWith("/reports/daily")) {
      queries.push(url);
      return route.fulfill(
        ok({ items: [row], summary: { ...row, day: "合计" }, total: 1 }),
      );
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/trade/daily-stat");
  await page.getByLabel("支付方式", { exact: true }).click();
  await page.getByRole("option", { name: "微信支付", exact: true }).click();
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect
    .poll(() => queries.at(-1)?.searchParams.get("type"))
    .toBe("wxpay");
  expect(queries.at(-1)?.searchParams.get("view")).toBe("original");
  expect(queries.at(-1)?.searchParams.has("from")).toBe(false);
  expect(queries.at(-1)?.searchParams.has("to")).toBe(false);
  await expect(page.getByRole("columnheader")).toHaveText([
    "日期",
    "订单金额",
    "分成金额",
    "退款金额",
    "总订单数",
    "成功订单数",
    "支付成功率",
  ]);
  await expect(
    page.locator("tbody").getByRole("cell", { name: "1.23", exact: true }),
  ).toBeVisible();
  await expect(
    page.locator("tbody").getByRole("cell", { name: "1.11", exact: true }),
  ).toBeVisible();
  await expect(page.locator("tfoot")).toContainText("66.67%");
  await expect(
    page.getByLabel("统计日期快捷范围", { exact: true }),
  ).toHaveCount(0);
  await page.screenshot({
    path: test.info().outputPath("report-fields.png"),
    fullPage: true,
  });
});
