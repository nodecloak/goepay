import { openMerchantOrderDetails } from "./fixtures/merchant-order-navigation";
import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("notification retries preserve UUID after network error, distinguish queue from receipt, and show new-operation cooldown", async ({
  page,
}) => {
  const ids: string[] = [];
  let notification: Record<string, unknown> | undefined;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/resend-notification")) {
      expect(r.headers()["x-csrf-token"]).toBe("csrf");
      const body = r.postDataJSON();
      expect(Object.keys(body)).toEqual(["external_no"]);
      ids.push(body.external_no);
      if (ids.length === 1) return route.abort("failed");
      if (ids.length === 4)
        return route.fulfill({
          status: 429,
          contentType: "application/json",
          headers: { "Retry-After": "60" },
          body: JSON.stringify({
            code: -1,
            message: "notification resend cooldown",
          }),
        });
      notification = {
        job_id: "71",
        state: ids.length === 2 ? "pending" : "succeeded",
        attempts: 1,
        run_at: "2026-10-23T00:00:00Z",
        created_at: "2026-10-23T00:00:00Z",
      };
      return route.fulfill(ok(notification, ids.length === 2 ? 202 : 200));
    }
    const order = { trade_no: "ORDER-N", status: "paid", amount: "100" };
    return route.fulfill(
      ok(
        p.endsWith("/ORDER-N")
          ? { order, can_resend_notification: true, notification }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await openMerchantOrderDetails(page);
  const modal = page.getByRole("dialog", { name: "订单详情" });
  await modal
    .getByRole("button", { name: "重发商户通知", exact: true })
    .click();
  expect(ids).toHaveLength(0);
  await modal
    .getByRole("button", { name: "确认通知请求", exact: true })
    .click();
  await expect(modal).toContainText("保留原请求号");
  expect(ids[0]).toMatch(/^[0-9a-f-]{36}$/);
  await modal
    .getByRole("button", { name: "重试原通知请求", exact: true })
    .click();
  await modal
    .getByRole("button", { name: "确认通知请求", exact: true })
    .click();
  await expect(modal).toContainText("尚未确认商户收到");
  expect(ids[1]).toBe(ids[0]);
  await modal
    .getByRole("button", { name: "核对原通知请求", exact: true })
    .click();
  await modal
    .getByRole("button", { name: "确认通知请求", exact: true })
    .click();
  await expect(modal).toContainText("商户通知已收到成功响应");
  expect(ids[2]).toBe(ids[0]);
  await modal
    .getByRole("button", { name: "发起新的通知重发", exact: true })
    .click();
  await modal
    .getByRole("button", { name: "确认通知请求", exact: true })
    .click();
  await expect(modal).toContainText("冷却");
  await expect(modal).toContainText("60");
  expect(ids[3]).not.toBe(ids[0]);
  await expect(
    modal.getByRole("button", { name: "重试原通知请求", exact: true }),
  ).toBeDisabled();
  expect(ids).toHaveLength(4);
});
test("unpaid or unpermitted orders do not expose notification send even if other fields contradict", async ({
  page,
}) => {
  let paid = false,
    writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (r.method() === "POST") writes++;
    const order = { trade_no: "ORDER-D", status: paid ? "paid" : "pending" };
    return route.fulfill(
      ok(
        p.endsWith("/ORDER-D")
          ? {
              order,
              can_resend_notification: !paid,
              notification: { job_id: "72", state: "dead", attempts: 4 },
            }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await openMerchantOrderDetails(page);
  const modal = page.getByRole("dialog", { name: "订单详情" });
  await expect(
    modal.getByRole("button", { name: "重发商户通知", exact: true }),
  ).toHaveCount(0);
  await modal.getByRole("button", { name: "关闭详情", exact: true }).click();
  paid = true;
  await openMerchantOrderDetails(page);
  await expect(modal).toContainText("dead");
  await expect(
    modal.getByRole("button", { name: "重发商户通知", exact: true }),
  ).toHaveCount(0);
  expect(writes).toBe(0);
});
test("HTTP200 terminal notification failure does not claim merchant receipt", async ({
  page,
}) => {
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/resend-notification"))
      return route.fulfill(ok({ job_id: "DEAD", state: "dead" }));
    const order = { trade_no: "NOTIFY-DEAD", status: "paid" };
    return route.fulfill(
      ok(
        p.endsWith("/NOTIFY-DEAD")
          ? { order, can_resend_notification: true }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await openMerchantOrderDetails(page);
  const modal = page.getByRole("dialog", { name: "订单详情" });
  await modal
    .getByRole("button", { name: "重发商户通知", exact: true })
    .click();
  await modal
    .getByRole("button", { name: "确认通知请求", exact: true })
    .click();
  await expect(
    modal.locator(".notification-panel .alert.success"),
  ).toContainText("尚未确认商户收到");
  await expect(
    modal.locator(".notification-panel .alert.success"),
  ).not.toContainText("商户通知已收到成功响应");
});
