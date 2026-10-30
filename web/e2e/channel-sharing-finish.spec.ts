import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("merchant independent finish shows original auto-finish as read-only and waits for verified PF query", async ({
  page,
}) => {
  let sends = 0,
    queries = 0;
  const eligible = {
    id: "0",
    order_id: "15",
    merchant_id: "1000",
    trade_no: "LOCAL_FINISH",
    external_no: "",
    out_no: "",
    state: "eligible",
    source: "finish",
    gross_amount: "100",
    merchant_net: "90",
    shared_amount: "0",
    expected_unsplit: "100",
    release_amount: "90",
    funds_released: false,
  };
  const pending = {
    ...eligible,
    id: "16",
    external_no: "stable-local-finish",
    out_no: "PF" + "c".repeat(30),
    state: "pending",
  };
  // The 渠道独立完结 panel lives inside the 操作台 overlay (ChannelSharingPage),
  // which is opened from an order-list row. Give the Pro list one row so the
  // 操作台 button renders.
  const listRow = {
    id: "5",
    external_no: "rec-row-5",
    out_no: "",
    state: "succeeded",
    share_amount: "2000",
    order_amount: "10000",
    provider: "wxpay",
    method: "native",
    rule_id: "15",
    channel_id: "7",
    created_at: "2026-10-01T00:00:00Z",
  };
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      u = new URL(r.url()),
      p = u.pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Local Merchant", role: "merchant" },
          csrf_token: "local-csrf",
        }),
      );
    if (p.endsWith("/channels"))
      return route.fulfill(
        ok({
          merchant_id: "1000",
          available: true,
          reason: "",
          channels: [
            {
              channel_id: "7",
              provider: "wxpay",
              product: "wxpay_v3_direct",
              methods: ["native"],
              receiver_types: ["MERCHANT_ID"],
              return_supported: true,
              finish_supported: true,
            },
          ],
        }),
      );
    if (p.includes("finish-preview/AUTO_FINISHED"))
      return route.fulfill(
        ok({
          ...eligible,
          trade_no: "AUTO_FINISHED",
          source: "sharing",
          state: "succeeded",
          funds_released: true,
          external_no: "original-ps",
          shared_amount: "20",
          expected_unsplit: "80",
          release_amount: "70",
        }),
      );
    if (p.includes("finish-preview/LOCAL_FINISH"))
      return route.fulfill(ok(eligible));
    if (r.method() === "POST") {
      expect(r.headers()["x-csrf-token"]).toBe("local-csrf");
      if (p.endsWith("/query")) {
        queries++;
        expect(p).toContain("stable-local-finish");
        expect(r.postDataJSON()).toEqual({});
        return route.fulfill(
          ok({ ...pending, state: "succeeded", funds_released: true }),
        );
      }
      sends++;
      expect(r.postDataJSON()).toEqual({
        trade_no: "LOCAL_FINISH",
        external_no: "stable-local-finish",
      });
      return route.fulfill(ok(pending));
    }
    if (p.endsWith("/orders") && r.method() === "GET") {
      if (u.searchParams.has("offset"))
        return route.fulfill(ok({ items: [listRow], total: "1", limit: 20 }));
      return route.fulfill(ok({ items: [], next_cursor: "0" }));
    }
    return route.fulfill(ok({ items: [], next_cursor: "0" }));
  });
  await page.goto("/merchant/profitsharing/order");
  // Open the 操作台 overlay that hosts the 渠道独立完结 panel.
  await page
    .getByRole("region", { name: "分账记录", exact: true })
    .getByRole("button", { name: "操作台" })
    .first()
    .click();
  const panel = page.getByRole("region", { name: "渠道独立完结", exact: true });
  await panel.getByLabel("完结平台订单号").fill("AUTO_FINISHED");
  await panel.getByRole("button", { name: "核对原订单完结状态" }).click();
  await expect(panel).toContainText("原分账已可信完结，无须新建完结请求");
  await expect(
    panel.getByRole("button", { name: "核对并申请独立完结" }),
  ).toHaveCount(0);
  expect(sends).toBe(0);
  await panel.getByLabel("完结平台订单号").fill("LOCAL_FINISH");
  await panel.getByRole("button", { name: "核对原订单完结状态" }).click();
  await expect(panel).toContainText("应解冻 1.00 元");
  await expect(panel).toContainText("可释放原商户净额 0.90 元");
  await panel.getByLabel("完结业务请求号").fill("stable-local-finish");
  await panel.getByRole("button", { name: "核对并申请独立完结" }).click();
  await panel
    .getByRole("dialog")
    .getByRole("button", { name: "确认申请完结" })
    .click();
  const records = panel.getByRole("region", {
    name: "独立完结记录",
    exact: true,
  });
  await expect(records).toContainText("原资金仍保留");
  await panel.getByRole("button", { name: "查询原完结请求" }).click();
  await expect(records).toContainText("原余款已释放");
  expect(sends).toBe(1);
  expect(queries).toBe(1);
});
