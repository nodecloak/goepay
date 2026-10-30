import { test, expect, type Page } from "@playwright/test";
const ok = (data: unknown) => ({
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
async function fixture(page: Page, area: "admin" | "merchant") {
  const writes: { path: string; body: Record<string, unknown> }[] = [];
  const configuration = {
    enabled: true,
    revision: "a".repeat(64),
    groups: [
      {
        id: "3",
        name: "专业组",
        enabled: true,
        sale_enabled: true,
        price_cents: "100",
        duration_months: 1,
        duration_days: 30,
        sort: 0,
        visible: "",
        description: "专业服务",
      },
      {
        id: "4",
        name: "永久组",
        enabled: true,
        sale_enabled: false,
        price_cents: "500",
        duration_months: 0,
        duration_days: 30,
        sort: 1,
        visible: "0",
        description: "永久服务",
      },
    ],
  };
  await page.route("**/console/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "1",
            username: area,
            name: area,
            role: area,
            admin_class: "super",
          },
          csrf_token: "csrf",
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
    if (request.method() !== "GET") {
      writes.push({ path, body: request.postDataJSON() });
      expect(request.headers()["x-csrf-token"]).toBe("csrf");
      if (path.endsWith("/membership-sales")) {
        const input = request.postDataJSON();
        expect(input.expected_revision).toBe(configuration.revision);
        configuration.enabled = input.enabled;
        for (const g of input.groups)
          Object.assign(
            configuration.groups.find((x) => x.id === g.id)!,
            g,
          );
        configuration.revision = "b".repeat(64);
        return route.fulfill(ok(configuration));
      }
      return route.fulfill(
        ok({ price: "2.00", external_no: request.postDataJSON().external_no }),
      );
    }
    if (path.endsWith("/membership-sales"))
      return route.fulfill(
        ok(
          area === "admin"
            ? configuration
            : {
                enabled: true,
                items: [
                  {
                    id: "3",
                    name: "专业组",
                    price_cents: "100",
                    duration_months: 1,
                    duration_days: 0,
                    permanent: false,
                    description: "专业服务",
                  },
                ],
              },
        ),
      );
    if (path.endsWith("/funds-availability"))
      return route.fulfill(
        ok({
          settle_type: 0,
          available_money: "1.00",
          ledger_available_money: "1.00",
          today_blocked_money: "0.00",
        }),
      );
    if (path.endsWith("/settlement-recipient"))
      return route.fulfill(ok({ state: "not_configured" }));
    if (path.endsWith("/transfers/capabilities"))
      return route.fulfill(ok({ channels: [], writes_enabled: false }));
    if (path.endsWith("/complaints-charts")) {
      return route.fulfill(
        ok({
          columns: [
            { key: "dimension", label: "商户ID" },
            { key: "today_count", label: "今日投诉总条数" },
            { key: "today_money", label: "今日投诉金额（元）" },
          ],
          items: [{ dimension: "1", today_count: "2", today_money: "123.50" }],
          summary: {
            today_count: "2",
            today_money: "123.50",
            today_alipay: "1",
            today_wxpay: "1",
            yesterday_count: "0",
            yesterday_money: "0.00",
            yesterday_alipay: "0",
            yesterday_wxpay: "0",
          },
        }),
      );
    }
    if (path.endsWith("/reports/complaints")) {
      expect(new URL(request.url()).searchParams.get("view")).toBe("overview");
      return route.fulfill(
        ok({
          columns: [
            { key: "dimension", label: "商户ID" },
            { key: "today_count", label: "今日投诉总条数" },
            { key: "today_money", label: "今日投诉金额（元）" },
          ],
          items: [{ dimension: "1", today_count: "2", today_money: "123.50" }],
          total: 1,
          source: "local_registered_complaints",
          summary: {
            today_count: "2",
            today_money: "123.50",
            today_alipay: "1",
            today_wxpay: "1",
            yesterday_count: "0",
            yesterday_money: "0.00",
            yesterday_alipay: "0",
            yesterday_wxpay: "0",
          },
        }),
      );
    }
    return route.fulfill(ok({ items: [], total: 0, next_before_id: "0" }));
  });
  return { writes, configuration };
}
for (const width of [375, 1280]) {
  test(`dedicated withdrawal form and batch entry at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 960 });
    const { writes } = await fixture(page, "merchant");
    await page.goto("/merchant/settle/apply");
    await expect(
      page.getByRole("region", { name: "申请提现", exact: true }),
    ).toBeVisible();
    await expect(page.getByLabel("金额（元）", { exact: true })).toBeVisible();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByRole("table")).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "确认提交", exact: true }),
    ).toBeDisabled();
    expect(writes).toHaveLength(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.goto("/merchant/transfer");
    await page.getByRole("link", { name: "批量转账", exact: true }).click();
    await expect(
      page.getByRole("region", { name: "批量付款", exact: true }),
    ).toBeVisible();
    expect(writes).toHaveLength(0);
  });
  test(`admin sale configuration and complaint semantics at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 960 });
    const { writes, configuration } = await fixture(page, "admin");
    await page.goto("/admin/merchant/group-buy");
    await expect(
      page.getByRole("region", { name: "用户组购买设置", exact: true }),
    ).toBeVisible();
    expect(writes).toHaveLength(0);
    await page.getByLabel("售价（元） · 专业组", { exact: true }).fill("2.99");
    await page.getByLabel("有效期（月） · 专业组", { exact: true }).fill("0");
    await page.getByRole("button", { name: "保存全部", exact: true }).click();
    await expect(page.getByRole("status")).toHaveText("保存成功");
    expect(configuration.groups[0]).toMatchObject({
      price_cents: "299",
      duration_months: 0,
    });
    expect(writes).toHaveLength(1);
    await page.getByRole("button", { name: "下架", exact: true }).click();
    await expect(page.getByRole("status")).toHaveText("已下架");
    expect(configuration.groups[0].sale_enabled).toBe(false);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.goto("/admin/complain/charts");
    await expect(
      page.getByText("投诉数据统计&排行榜", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("columnheader", { name: "今日投诉金额（元）" }),
    ).toBeVisible();
    await expect(
      page.getByRole("columnheader", { name: /平台手续费|商户净额/ }),
    ).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.goto("/admin/transfer/list");
    await page.getByRole("link", { name: "批量转账", exact: true }).click();
    await expect(
      page.getByRole("region", { name: "批量付款", exact: true }),
    ).toBeVisible();
  });
}
test("merchant purchase confirms quantity, period and exact total without a client price", async ({
  page,
}) => {
  const { writes } = await fixture(page, "merchant");
  await page.goto("/merchant/account/groupbuy");
  await page.getByRole("button", { name: "购买此用户组", exact: true }).click();
  await page.getByLabel("购买数量", { exact: true }).fill("2");
  await expect(page.getByRole("dialog")).toContainText("合计：2.00 元");
  await page.getByRole("button", { name: "确认购买会员", exact: true }).click();
  await expect(page.locator(".alert.success")).toContainText("会员购买已完成");
  expect(writes).toHaveLength(1);
  expect(writes[0].body).toMatchObject({ group_id: "3", quantity: 2 });
  expect(writes[0].body).not.toHaveProperty("price_cents");
});
