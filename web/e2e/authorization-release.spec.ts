import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("authorization release confirms full amount, preserves unknown request and queries it without another send", async ({
  page,
}) => {
  let state = "none",
    sends = 0,
    queries = 0;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "release-csrf",
        }),
      );
    const release = {
      order_id: "77",
      trade_no: "AUTH-77",
      auth_no: "AUTH-NO-77",
      channel_id: "1",
      request_no: "UA77",
      amount: "100",
      state,
      sent: state !== "none",
    };
    if (p.endsWith("/release-authorization")) {
      sends++;
      expect(r.headers()["x-csrf-token"]).toBe("release-csrf");
      expect(r.postDataJSON()).toEqual({});
      state = "unknown";
      return route.fulfill(ok({ ...release, state, sent: true }, 202));
    }
    if (p.endsWith("/query-authorization-release")) {
      queries++;
      expect(r.headers()["x-csrf-token"]).toBe("release-csrf");
      expect(r.postDataJSON()).toEqual({});
      state = "succeeded";
      return route.fulfill(ok({ ...release, state, sent: true }));
    }
    const order = { trade_no: "AUTH-77", status: "pending", amount: "100" };
    return route.fulfill(
      ok(
        p.endsWith("/AUTH-77")
          ? {
              order,
              provider: { method: "preauth", can_close: false },
              authorization: {
                auth_no: "AUTH-NO-77",
                amount: "100",
                capture_state: "none",
                release_state: state,
              },
              authorization_release: state === "none" ? undefined : release,
              can_release_authorization: state === "none",
            }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "订单详情" });
  await modal
    .getByRole("button", { name: "解除买家预授权冻结", exact: true })
    .click();
  expect(sends).toBe(0);
  await expect(modal).toContainText("确认解除买家冻结");
  await expect(modal).toContainText("AUTH-NO-77");
  await expect(modal).toContainText("尚未创建，确认后由服务端生成并持久化");
  await modal
    .getByRole("button", { name: "确认核对并解除", exact: true })
    .click();
  await expect(modal.locator(".alert.success")).toContainText("尚未核实");
  await expect(modal).toContainText("UA77");
  await expect(
    modal.getByRole("button", { name: "解除买家预授权冻结", exact: true }),
  ).toHaveCount(0);
  await expect(
    modal.getByRole("button", { name: "核对并解除原授权", exact: true }),
  ).toHaveCount(0);
  expect(sends).toBe(1);
  await modal
    .getByRole("button", { name: "查询原解除请求", exact: true })
    .click();
  await expect(modal.locator(".alert.success")).toContainText(
    "已核实买家预授权冻结已解除",
  );
  await expect(modal.locator(".alert.success")).toContainText(
    "不会增加商户余额",
  );
  expect(sends).toBe(1);
  expect(queries).toBe(1);
  await expect(
    modal.getByRole("button", { name: "关闭通道订单", exact: true }),
  ).toHaveCount(0);
  await expect(
    modal.getByRole("button", { name: "申请退款", exact: true }),
  ).toHaveCount(0);
});
test("server-approved unsent original release can be explicitly checked once; rejection is not described as released", async ({
  page,
}) => {
  let sent = false,
    attempts = 0;
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
    const release = {
      state: sent ? "failed" : "reversal_pending",
      sent,
      request_no: "UA88",
      amount: "500",
      auth_no: "AUTH88",
    };
    if (p.endsWith("/release-authorization")) {
      attempts++;
      expect(r.postDataJSON()).toEqual({});
      sent = true;
      return route.fulfill(ok({ ...release, state: "failed", sent: true }));
    }
    const order = { trade_no: "AUTH88", status: "pending", amount: "500" };
    return route.fulfill(
      ok(
        p.endsWith("/AUTH88")
          ? {
              order,
              provider: { method: "preauth", can_close: false },
              authorization: {
                amount: "500",
                auth_no: "AUTH88",
                capture_state: "failed",
                release_state: release.state,
              },
              authorization_release: release,
              can_release_authorization: !sent,
            }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "订单详情" });
  await modal
    .getByRole("button", { name: "核对并解除原授权", exact: true })
    .click();
  await expect(modal).toContainText("UA88");
  expect(attempts).toBe(0);
  await modal
    .getByRole("button", { name: "确认核对并解除", exact: true })
    .click();
  await expect(modal.locator(".alert.success")).toContainText(
    "通道已拒绝授权解除",
  );
  await expect(modal.locator(".alert.success")).not.toContainText(
    "已核实买家预授权冻结已解除",
  );
  await expect(
    modal.getByRole("button", { name: "核对并解除原授权", exact: true }),
  ).toHaveCount(0);
  expect(attempts).toBe(1);
});
test("paid orders and already-sent unknown releases never expose a new release even with contradictory permission", async ({
  page,
}) => {
  let paid = false,
    sends = 0;
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
    if (r.method() === "POST") sends++;
    const order = {
      trade_no: "AUTH99",
      status: paid ? "paid" : "pending",
      amount: "100",
    };
    return route.fulfill(
      ok(
        p.endsWith("/AUTH99")
          ? {
              order,
              provider: { method: "preauth", can_close: false },
              authorization: {
                capture_state: paid ? "succeeded" : "unknown",
                release_state: "unknown",
                auth_no: "AUTH99",
              },
              authorization_release: {
                state: "unknown",
                sent: true,
                request_no: "UA99",
              },
              can_release_authorization: true,
            }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  let modal = page.getByRole("dialog", { name: "订单详情" });
  await expect(
    modal.getByRole("button", { name: "解除买家预授权冻结", exact: true }),
  ).toHaveCount(0);
  await expect(
    modal.getByRole("button", { name: "核对并解除原授权", exact: true }),
  ).toHaveCount(0);
  await expect(
    modal.getByRole("button", { name: "查询原解除请求", exact: true }),
  ).toBeVisible();
  await modal.getByRole("button", { name: "关闭详情", exact: true }).click();
  paid = true;
  await page.getByRole("button", { name: "详情", exact: true }).click();
  modal = page.getByRole("dialog", { name: "订单详情" });
  await expect(
    modal.getByRole("button", { name: "解除买家预授权冻结", exact: true }),
  ).toHaveCount(0);
  await expect(
    modal.getByRole("button", { name: "核对并解除原授权", exact: true }),
  ).toHaveCount(0);
  expect(sends).toBe(0);
});
