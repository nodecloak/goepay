import { openMerchantOrderDetails } from "./fixtures/merchant-order-navigation";
import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
for (const width of [375, 1440])
  test(`remaining authorization release preserves exact cents and original recovery at ${width}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 960 });
    let sent = false,
      done = false,
      writes = 0,
      queries = 0;
    const order = {
      id: "75",
      merchant_id: "1000",
      trade_no: "REMAIN75",
      amount: "1000",
      status: "pending",
    };
    await page.route("**/console/v1/**", (route) => {
      const r = route.request(),
        path = new URL(r.url()).pathname;
      if (path.endsWith("/session"))
        return route.fulfill(
          ok({
            user: { id: "1000", name: "M", role: "merchant" },
            csrf_token: "remaining-csrf75",
          }),
        );
      const release = {
        order_id: "75",
        trade_no: order.trade_no,
        channel_id: "7",
        auth_no: "AUTH75",
        request_no: "UA75",
        amount: "450",
        original_amount: "1000",
        source_revision: "1",
        sent: true,
        state: done ? "succeeded" : "unknown",
      };
      if (path.endsWith("/authorization-observations"))
        return route.fulfill(
          ok({
            request_id: "a".repeat(64),
            client_request_no: r.postDataJSON().client_request_no,
            merchant_id: "1000",
            order_id: "75",
            attempt_id: "8",
            channel_id: "7",
            trade_no: order.trade_no,
            app_id: "APP75",
            auth_no: "AUTH75",
            buyer_id: "BUYER75",
            amount: "1000",
            rest_amount: "450",
            status: "verified",
            rest_known: true,
            source_verified: true,
            provider_status: "SUCCESS",
            order_status: "AUTHORIZED",
            operation_type: "FREEZE",
            observed_at: "2026-10-24T06:00:00Z",
          }),
        );
      if (path.endsWith("/release-remaining-authorization")) {
        writes++;
        expect(r.headers()["x-csrf-token"]).toBe("remaining-csrf75");
        expect(r.postDataJSON()).toEqual({
          expected_observation_id: "a".repeat(64),
          expected_remaining_cents: "450",
          expected_source_revision: "0",
        });
        sent = true;
        return route.fulfill(ok(release, 202));
      }
      if (path.endsWith("/query-authorization-release")) {
        queries++;
        done = true;
        return route.fulfill(ok({ ...release, state: "succeeded" }));
      }
      if (path.endsWith("/release-authorization"))
        throw Error("remaining source entered full sender");
      return route.fulfill(
        ok(
          path.endsWith("/REMAIN75")
            ? {
                order,
                provider: { method: "preauth", can_close: false },
                authorization: {
                  auth_no: "AUTH75",
                  amount: "1000",
                  capture_state: "none",
                  release_state: sent ? "unknown" : "none",
                },
                authorization_release: sent ? release : undefined,
                can_observe_authorization: true,
                can_release_remaining_authorization: !sent,
                can_release_authorization: false,
              }
            : { items: [order], total: 1 },
        ),
      );
    });
    await page.goto("/merchant/trade/order");
    await openMerchantOrderDetails(page);
    const modal = page.getByRole("dialog", { name: "订单详情" }),
      panel = page.getByRole("region", { name: "预授权原冻结与余款" });
    await panel
      .getByRole("button", { name: "查询原冻结与余款", exact: true })
      .click();
    await panel
      .getByRole("button", { name: "解除剩余冻结", exact: true })
      .click();
    expect(writes).toBe(0);
    await expect(panel).toContainText("本次剩余解除额（元）4.50");
    await panel
      .getByRole("button", { name: "确认解除本次剩余额度", exact: true })
      .scrollIntoViewIfNeeded();
    expect(await modal.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(
      true,
    );
    await page.screenshot({
      path: testInfo.outputPath(`remaining-confirm-${width}.png`),
      fullPage: true,
    });
    await panel
      .getByRole("button", { name: "确认解除本次剩余额度", exact: true })
      .click();
    await expect(modal).toContainText("UA75");
    await expect(modal.locator(".alert.success")).toContainText("尚未核实");
    expect(writes).toBe(1);
    await expect(
      modal.getByRole("button", { name: "解除剩余冻结", exact: true }),
    ).toHaveCount(0);
    await modal
      .getByRole("button", { name: "查询原解除请求", exact: true })
      .click();
    await expect(modal.locator(".alert.success")).toContainText(
      "已核实买家预授权冻结已解除",
    );
    expect(queries).toBe(1);
    expect(writes).toBe(1);
  });
