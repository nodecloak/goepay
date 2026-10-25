import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("capture requires clear amount confirmation, excludes release while confirming, and unknown only queries the original order", async ({
  page,
}) => {
  let captureState = "none",
    paid = false,
    captures = 0,
    queries = 0;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "capture-csrf",
        }),
      );
    if (p.endsWith("/capture-authorization")) {
      captures++;
      expect(r.postDataJSON()).toEqual({});
      expect(r.headers()["x-csrf-token"]).toBe("capture-csrf");
      captureState = "unknown";
      return route.fulfill(
        ok({ capture_state: captureState, release_state: "none" }, 202),
      );
    }
    if (p.endsWith("/query")) {
      queries++;
      paid = true;
      captureState = "succeeded";
      return route.fulfill(ok({ trade_no: "AUTH-C", status: "paid" }));
    }
    const order = {
      trade_no: "AUTH-C",
      status: paid ? "paid" : "pending",
      amount: "100",
    };
    return route.fulfill(
      ok(
        p.endsWith("/AUTH-C")
          ? {
              order,
              provider: { method: "preauth", can_close: false },
              authorization: {
                auth_no: "AUTH-C-NO",
                amount: "100",
                capture_state: captureState,
                release_state: "none",
              },
              can_capture_authorization: true,
              can_release_authorization: !paid,
            }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "订单详情" });
  await modal
    .getByRole("button", { name: "扣取买家授权金额", exact: true })
    .click();
  await expect(modal).toContainText("AUTH-C-NO");
  await expect(modal).toContainText("100");
  await expect(
    modal.getByRole("button", { name: "解除买家预授权冻结", exact: true }),
  ).toBeDisabled();
  expect(captures).toBe(0);
  await modal
    .getByRole("button", { name: "确认扣取授权金额", exact: true })
    .click();
  await expect(modal.locator(".alert.success")).toContainText(
    "扣款结果尚未核实",
  );
  await expect(
    modal.getByRole("button", { name: "扣取买家授权金额", exact: true }),
  ).toHaveCount(0);
  await modal
    .getByRole("button", { name: "查询支付通道", exact: true })
    .click();
  await expect(modal).toContainText("预授权付款已捕获，并已核实订单支付");
  expect(captures).toBe(1);
  expect(queries).toBe(1);
  await expect(
    modal.getByRole("button", { name: "扣取买家授权金额", exact: true }),
  ).toHaveCount(0);
});
test("released or already-consumed authorization cannot expose capture with contradictory flags", async ({
  page,
}) => {
  let released = true,
    captures = 0;
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
    if (r.method() === "POST") captures++;
    const order = { trade_no: "AUTH-X", status: "pending", amount: "100" };
    return route.fulfill(
      ok(
        p.endsWith("/AUTH-X")
          ? {
              order,
              provider: { method: "preauth", can_close: false },
              authorization: {
                capture_state: released ? "none" : "succeeded",
                release_state: released ? "succeeded" : "none",
              },
              can_capture_authorization: true,
            }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "订单详情" });
  await expect(
    modal.getByRole("button", { name: "扣取买家授权金额", exact: true }),
  ).toHaveCount(0);
  await modal.getByRole("button", { name: "关闭详情", exact: true }).click();
  released = false;
  await page.getByRole("button", { name: "详情", exact: true }).click();
  await expect(
    modal.getByRole("button", { name: "扣取买家授权金额", exact: true }),
  ).toHaveCount(0);
  expect(captures).toBe(0);
});
