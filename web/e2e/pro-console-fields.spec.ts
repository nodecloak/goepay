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
    (await page.getByLabel("搜索记录", { exact: true }).boundingBox())!.width,
  ).toBeGreaterThanOrEqual(160);
  const row = page.getByRole("row").filter({ hasText: order.trade_no });
  await expect(row).toContainText("90071992547409.93");
  await expect(row).toContainText("商户入账：90071992547409.92");
  await expect(row).toContainText("已退款：0.23");
  await expect(row).toContainText(order.api_trade_no);
  await expect(row).toContainText("通道 ID：9007199254740993124");
  await expect(row.getByText("已支付", { exact: true })).toBeVisible();
  await expect(row.getByText("通知失败", { exact: true })).toBeVisible();
  await expect(page.getByText(order.api_key, { exact: true })).toHaveCount(0);
  await page.screenshot({
    path: test.info().outputPath("order-fields.png"),
    fullPage: true,
  });
  await row.getByRole("button", { name: "详情", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "订单详情" });
  await expect(dialog).toContainText("订单金额（元）");
  await expect(dialog).toContainText("90071992547409.93");
  await expect(dialog).toContainText("通知失败");
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
    "可用余额（元）",
    "结算账号 / 姓名（脱敏）",
    "电子邮箱",
    "注册时间（北京时间）",
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
  await page.getByRole("button", { name: "详情", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("冻结预留（元）1.23");
  await expect(dialog).toContainText("待解冻收入（元）4.56");
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
      page.getByRole("columnheader", { name: "账户变更前（元）", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "92233720368547758.07", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "92233720368547758.06", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "-0.01", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: entry.account_id, exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "available", exact: true }),
    ).toBeVisible();
    if (area === "merchant")
      await expect(
        page.getByRole("columnheader", { name: "商户号", exact: true }),
      ).toHaveCount(0);
  });

test("merchant daily report uses Beijing shortcuts and already-yuan values with derived success rate", async ({
  page,
}) => {
  await page.clock.setFixedTime(new Date("2026-10-23T16:30:00Z"));
  const queries: URL[] = [];
  await page.route("**/console/v1/**", (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/session"))
      return route.fulfill(
        ok({ ...session, user: { ...session.user, role: "merchant" } }),
      );
    queries.push(url);
    return route.fulfill(
      ok({
        columns: [
          { key: "dimension", label: "日期" },
          { key: "orders", label: "订单数" },
          { key: "paid_orders", label: "成功订单数" },
          { key: "paid_money", label: "成功金额（元）" },
        ],
        items: [
          {
            dimension: "2026-10-24",
            orders: "3",
            paid_orders: "2",
            paid_money: "1.23",
          },
        ],
        total: 1,
      }),
    );
  });
  await page.goto("/merchant/trade/daily-stat");
  await page
    .getByLabel("统计日期快捷范围", { exact: true })
    .selectOption("today");
  await expect
    .poll(() => queries.at(-1)?.searchParams.get("from"))
    .toBe("2026-10-24");
  await expect(page.getByRole("columnheader")).toHaveText([
    "日期",
    "成功金额（元）",
    "订单数",
    "成功订单数",
    "支付成功率",
  ]);
  await expect(
    page.getByRole("cell", { name: "1.23", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("cell", { name: "66.67%", exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: test.info().outputPath("report-fields.png"),
    fullPage: true,
  });
});
