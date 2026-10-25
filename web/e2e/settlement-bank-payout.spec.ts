import { test, expect } from "@playwright/test";

for (const product of ["wxpay_v2", "alipay_bankcard"]) {
  test(`merchant queries succeeded ${product} original settlement through incomplete then verified return`, async ({
    page,
  }) => {
    let phase = 0,
      queries = 0,
      writes = 0;
    const recipient = {
      state: "recorded",
      config_id: "42",
      type: "bank",
      app_id: "",
      account_masked: "***0000",
      name_masked: "***",
    };
    const funding = () => ({
      state: phase === 2 ? "returned" : "paid_fee_held",
      account_id: "90",
      gross_amount: "1000",
      reserved_amount: phase === 2 ? "0" : "30",
    });
    const request = () => ({
      id: "12",
      merchant_id: "1000",
      status: "approved",
      amount_cents: "1000",
      settlement: { gross_amount: "1000", fee_amount: "30", net_amount: "970" },
      funding: funding(),
      recipient,
    });
    const payout = () => ({
      request_id: "12",
      merchant_id: "1000",
      channel_id: "7",
      transfer_id: "90",
      state: ["succeeded", "reversal_pending", "reversed"][phase],
      send_state: "claimed",
      transfer: {
        id: "90",
        transfer_no: "ST12",
        amount: "970",
        status: ["succeeded", "reversal_pending", "reversed"][phase],
        type: "bank",
        bank_product: product,
      },
      funding: funding(),
      recipient,
      created_at: "2026-10-23T00:00:00Z",
    });
    const ok = (data: unknown) => ({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
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
      if (
        p.endsWith("/capabilities") ||
        p.endsWith("/prepare") ||
        p.endsWith("/execute") ||
        p.includes("/funds-authorizations")
      ) {
        writes++;
        return route.fulfill({ status: 403, body: "forbidden" });
      }
      if (p.endsWith("/query")) {
        queries++;
        phase = Math.min(2, phase + 1);
        return route.fulfill(ok(payout()));
      }
      if (p.endsWith("/settlement-payouts/12"))
        return route.fulfill(ok({ request: request(), payout: payout() }));
      return route.fulfill(
        ok({
          items: [
            {
              ...request(),
              amount: "10.00",
              settlement_funding: funding(),
              settlement_recipient: recipient,
              payout: payout(),
            },
          ],
          total: 1,
        }),
      );
    });
    await page.goto("/merchant/settle/list");
    await page
      .getByRole("button", { name: "核对结算出款", exact: true })
      .click();
    const panel = page.getByRole("region", { name: "结算出款", exact: true });
    await expect(panel).toContainText("手续费保留");
    await expect(panel).toContainText("0.30 元");
    await expect(
      panel.getByRole("button", { name: "发送原结算出款", exact: true }),
    ).toHaveCount(0);
    await panel
      .getByRole("button", { name: "只查询原出款", exact: true })
      .click();
    await expect(panel).toContainText("退票证据待核对");
    await expect(panel).toContainText("0.30 元");
    await panel
      .getByRole("button", { name: "只查询原出款", exact: true })
      .click();
    await expect(panel).toContainText("本金及保留费用已核实退回");
    await expect(
      panel.getByRole("button", { name: "只查询原出款", exact: true }),
    ).toHaveCount(0);
    expect(queries).toBe(2);
    expect(writes).toBe(0);
  });
}
