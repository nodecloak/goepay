import { test, expect } from "@playwright/test";

for (const width of [1280, 375])
  test(`original buyer statistics uses paid sources, explicit money reads and original blacklist dialogue at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const queries: URL[] = [],
      writes: string[] = [];
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        u = new URL(req.url()),
        p = u.pathname;
      if (req.method() !== "GET") writes.push(p);
      let data: unknown = { items: [], total: 0 };
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super", name: "local" },
          csrf_token: "buyer131",
        };
      else if (p.endsWith("/reports/buyers")) {
        queries.push(u);
        data =
          u.searchParams.get("method") === "2"
            ? {
                items: [],
                total: 0,
                available: false,
                unavailable_reason: "未记录可信支付手机号，暂不可按手机号统计",
              }
            : {
                items: [
                  {
                    user: "same-account",
                    order_count: "9007199254740993",
                    pay_money: "123.45",
                    trade_no: "BUYER-131",
                    is_black: false,
                  },
                ],
                total: 1,
                available: true,
              };
      } else if (p.endsWith("/orders/BUYER-131"))
        data = {
          order: { trade_no: "BUYER-131", amount: "12345", status: "paid" },
          reporting: { buyer: "same-account", client_ip: "192.0.2.10" },
        };
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/trade/stat/buyer");
    await expect
      .poll(() => queries.at(-1)?.searchParams.get("view"))
      .toBe("original");
    await expect(
      page.getByRole("button", { name: "统计方式", exact: true }),
    ).toContainText("支付账号统计");
    await expect(page.getByRole("columnheader")).toHaveText([
      "支付账号",
      "付款笔数",
      "付款金额（元）",
      "最新订单号",
      "是否拉黑",
      "操作",
    ]);
    const count = page.getByRole("link", {
      name: "9007199254740993",
      exact: true,
    });
    await expect(count).toHaveAttribute(
      "href",
      "/admin/trade/order?column=buyer&value=same-account",
    );
    await expect(
      page.getByRole("link", { name: "BUYER-131", exact: true }),
    ).toHaveAttribute(
      "href",
      "/admin/trade/order?column=trade_no&value=BUYER-131",
    );
    await expect(page.getByRole("table")).not.toContainText("123.45");
    await page
      .getByRole("button", { name: "刷新付款金额 same-account", exact: true })
      .click();
    await expect(page.getByRole("table")).toContainText("123.45");
    expect(queries.at(-1)?.searchParams.get("user")).toBe("same-account");
    await page
      .getByRole("button", { name: "拉黑 same-account", exact: true })
      .click();
    const dialog = page.getByRole("dialog", { name: /添加黑名单/ });
    await expect(dialog).toBeVisible();
    await expect(dialog.locator("input[readonly]")).toHaveValue("same-account");
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    await page.getByRole("button", { name: "统计方式", exact: true }).click();
    await page
      .getByRole("option", { name: "支付手机号统计", exact: true })
      .click();
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect(
      page.getByText("未记录可信支付手机号，暂不可按手机号统计", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "9007199254740993", exact: true }),
    ).toHaveCount(0);
    expect(writes).toHaveLength(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });

test("late original account money cannot fill the next IP result and failures clear old rows", async ({
  page,
}) => {
  let release: (() => void) | undefined;
  let refreshed = false,
    fail = false;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/console/v1/**", async (route) => {
    const u = new URL(route.request().url()),
      p = u.pathname;
    let data: unknown = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", role: "admin", admin_class: "super" },
        csrf_token: "buyer-late131",
      };
    else if (p.endsWith("/reports/buyers")) {
      if (u.searchParams.has("user")) {
        refreshed = true;
        await gate;
      }
      if (fail)
        return route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ code: -1, message: "本地统计读取失败" }),
        });
      data = {
        available: true,
        total: 1,
        items: [
          {
            user:
              u.searchParams.get("method") === "1"
                ? "192.0.2.10"
                : "old-account",
            order_count: "2",
            pay_money: u.searchParams.has("user") ? "999.99" : "1.23",
            trade_no: "BUYER-LATE131",
            is_black: false,
          },
        ],
      };
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/trade/stat/buyer");
  await page
    .getByRole("button", { name: "刷新付款金额 old-account", exact: true })
    .click();
  await expect.poll(() => refreshed).toBe(true);
  await page.getByRole("button", { name: "统计方式", exact: true }).click();
  await page.getByRole("option", { name: "支付IP统计", exact: true }).click();
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(
    page.getByRole("cell", { name: "192.0.2.10", exact: true }),
  ).toBeVisible();
  release!();
  await expect(page.getByRole("table")).not.toContainText("999.99");
  await expect(
    page.getByRole("link", { name: "2", exact: true }),
  ).toHaveAttribute("href", "/admin/trade/order?column=ip&value=192.0.2.10");
  fail = true;
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("本地统计读取失败");
  await expect(
    page.getByRole("cell", { name: "192.0.2.10", exact: true }),
  ).toHaveCount(0);
});

test("read-only administrator has original statistics without a blacklist write entry", async ({
  page,
}) => {
  let writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    if (req.method() !== "GET") writes++;
    const data = p.endsWith("/session")
      ? {
          user: { id: "1", role: "admin", admin_class: "readonly" },
          csrf_token: "buyer-read131",
        }
      : {
          available: true,
          items: [
            {
              user: "read-account",
              order_count: "1",
              pay_money: "1.00",
              trade_no: "BUYER-READ131",
              is_black: true,
            },
          ],
          total: 1,
        };
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/trade/stat/buyer");
  await expect(
    page.getByRole("cell", { name: "read-account", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "拉黑 read-account", exact: true }),
  ).toHaveCount(0);
  expect(writes).toBe(0);
});
