import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("batch prepare is separate from execute and lost response recovers original number", async ({
  page,
}) => {
  let prepares = 0,
    executes = 0,
    external = "",
    lookedUp = "",
    batch: any;
  await page.route("**/console/v1/**", (route) => {
    const u = new URL(route.request().url()),
      p = u.pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Merchant", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/transfers/capabilities"))
      return route.fulfill(
        ok({
          writes_enabled: true,
          available_money: "10.00",
          ledger_available_money: "10.00",
          today_blocked_money: "0.00",
          settle_type: 0,
          channels: [{ id: "7", types: ["alipay"] }],
        }),
      );
    if (
      p.endsWith("/transfer-batches") &&
      route.request().method() === "POST"
    ) {
      prepares++;
      const body = route.request().postDataJSON();
      external = body.external_no;
      batch = {
        id: "19",
        merchant_id: "1",
        external_no: external,
        item_count: 1,
        amount: "100",
        cost_amount: "100",
        fee_amount: "0",
        state: "prepared",
        all_succeeded: false,
        summary: { unsent: 1 },
        items: [
          {
            position: 1,
            send_state: "unsent",
            transfer: {
              id: "9",
              channel_id: "7",
              amount: "100",
              status: "unsent",
            },
          },
        ],
      };
      return route.abort("failed");
    }
    if (p.endsWith("/transfer-batches")) {
      lookedUp = u.searchParams.get("external_no") ?? lookedUp;
      return route.fulfill(
        ok({
          items: batch ? [{ ...batch, items: [] }] : [],
          total: batch ? 1 : 0,
          next_before_id: "0",
        }),
      );
    }
    if (p.endsWith("/19/execute")) {
      executes++;
      batch.summary = { unknown: 1 };
      batch.items[0].send_state = "claimed";
      batch.items[0].transfer.status = "unknown";
      return route.fulfill(ok(batch));
    }
    if (p.endsWith("/19")) return route.fulfill(ok(batch));
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/transfer/batches");
  await page.getByRole("button", { name: "新建批量付款", exact: true }).click();
  await page
    .getByRole("combobox", { name: "通道", exact: true })
    .selectOption("7");
  await page.getByLabel("收款账号", { exact: true }).fill("synthetic-only");
  await page.getByLabel("付款金额（元）", { exact: true }).fill("1.00");
  await page
    .getByRole("button", { name: "核对并预留整批资金", exact: true })
    .click();
  await page
    .getByRole("button", { name: "确认预留，不发送付款", exact: true })
    .click();
  await expect(page.getByText(/预留结果未确认/)).toBeVisible();
  expect(prepares).toBe(1);
  expect(executes).toBe(0);
  expect(external.length).toBeGreaterThan(20);
  // The original request number stays in the read-only 请求号 filter; 查询
  // looks the saved batch up by exactly that number (no new batch is made).
  await expect(page.getByLabel("批次请求号", { exact: true })).toHaveValue(
    external,
  );
  await expect(
    page.getByRole("button", { name: "新建批量付款", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "查看批次 19", exact: true }),
  ).toBeVisible();
  expect(lookedUp).toBe(external);
  expect(prepares).toBe(1);
  await page.setViewportSize({ width: 375, height: 812 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.getByRole("button", { name: "查看批次 19", exact: true }).click();
  await page
    .getByRole("button", { name: "执行尚未发送项目", exact: true })
    .click();
  await page
    .getByRole("button", { name: "确认仅执行未发送项目", exact: true })
    .click();
  await expect(
    page.getByText(
      "此批次不能视为全部成功。请逐项核对状态；结果未知或已发送项只查询原记录，不重新付款。",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "执行尚未发送项目", exact: true }),
  ).toHaveCount(0);
  expect(executes).toBe(1);
  expect(prepares).toBe(1);
});
