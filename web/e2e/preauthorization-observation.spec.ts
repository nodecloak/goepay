import { test, expect } from "@playwright/test";

const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("preauthorization observation is explicit, unknown refresh is local GET and a fresh query has a new nonce", async ({
  page,
}) => {
  const order = {
    id: "9007199254740993123",
    merchant_id: "1000",
    trade_no: "AUTH-77",
    amount: "100",
    status: "pending",
  };
  const posts: string[] = [],
    gets: string[] = [];
  let current = "";
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname;
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "observation-only",
        }),
      );
    if (path.includes("/authorization-observations")) {
      if (req.method() === "POST") {
        expect(req.headers()["x-csrf-token"]).toBe("observation-only");
        expect(Object.keys(req.postDataJSON())).toEqual(["client_request_no"]);
        current = req.postDataJSON().client_request_no;
        expect(current).toMatch(/^[0-9a-f]{64}$/);
        posts.push(current);
      } else gets.push(path);
      const ready = posts.length > 1;
      return route.fulfill(
        ok(
          {
            request_id: (ready ? "b" : "a").repeat(64),
            client_request_no: current,
            merchant_id: order.merchant_id,
            order_id: order.id,
            attempt_id: "8",
            channel_id: "7",
            trade_no: order.trade_no,
            app_id: "APP-1",
            auth_no: "ORIGINAL-AUTH-77",
            buyer_id: "BUYER-77",
            amount: "100",
            status: ready ? "verified" : "unknown",
            rest_known: ready,
            ...(ready
              ? { rest_amount: "0", observed_at: "2026-10-24T00:00:00Z" }
              : {}),
            source_verified: ready,
            provider_status: ready ? "SUCCESS" : "",
            order_status: ready ? "AUTHORIZED" : "",
            operation_type: ready ? "FREEZE" : "",
          },
          ready ? 200 : 202,
        ),
      );
    }
    return route.fulfill(
      ok(
        path.endsWith("/AUTH-77")
          ? { order, can_observe_authorization: true }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  const panel = page.getByRole("region", { name: "预授权原冻结与余款" });
  await expect(
    panel.getByRole("button", { name: "查询原冻结与余款", exact: true }),
  ).toBeVisible();
  expect(posts).toHaveLength(0);
  await panel
    .getByRole("button", { name: "查询原冻结与余款", exact: true })
    .click();
  await expect(panel).toContainText("查询结果未知，余款未知");
  await expect(panel).not.toContainText("余款（元）0.00");
  await panel
    .getByRole("button", { name: "刷新原查询状态", exact: true })
    .click();
  await expect.poll(() => gets.length).toBe(1);
  expect(gets[0]).toBe(
    `/console/v1/merchant/orders/AUTH-77/authorization-observations/${"a".repeat(64)}`,
  );
  expect(posts).toHaveLength(1);
  await panel
    .getByRole("button", { name: "重新查询原冻结与余款", exact: true })
    .click();
  await expect(panel).toContainText("余款（元）0.00");
  await expect(panel).toContainText("来源已验证");
  await expect(panel).toContainText("不会扣款或解除冻结");
  expect(posts).toHaveLength(2);
  expect(posts[1]).not.toBe(posts[0]);
  await page.getByRole("button", { name: "关闭详情", exact: true }).click();
  await page.getByRole("button", { name: "详情", exact: true }).click();
  await expect(
    panel.getByRole("button", { name: "查询原冻结与余款", exact: true }),
  ).toBeVisible();
  await expect(panel).not.toContainText("BUYER-77");
  expect(posts).toHaveLength(2);
});

for (const gate of ["absent", "false", "delegated"] as const)
  test(`preauthorization observation cannot be launched with ${gate} permission`, async ({
    page,
  }) => {
    let sends = 0;
    await page.route("**/console/v1/**", (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith("/session"))
        return route.fulfill(
          ok({
            user: { id: "1000", name: "M", role: "merchant" },
            csrf_token: "x",
            ...(gate === "delegated"
              ? { delegated_by: { id: "1", name: "Admin" } }
              : {}),
          }),
        );
      if (path.includes("/authorization-observations")) sends++;
      const order = {
        id: "77",
        merchant_id: "1000",
        trade_no: "AUTH-77",
        status: "pending",
        amount: "100",
      };
      return route.fulfill(
        ok(
          path.endsWith("/AUTH-77")
            ? {
                order,
                ...(gate === "absent"
                  ? {}
                  : { can_observe_authorization: gate === "delegated" }),
              }
            : { items: [order], total: 1 },
        ),
      );
    });
    await page.goto("/merchant/trade/order");
    await page.getByRole("button", { name: "详情", exact: true }).click();
    await expect(page.getByRole("dialog", { name: "订单详情" })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "查询原冻结与余款", exact: true }),
    ).toHaveCount(0);
    expect(sends).toBe(0);
  });
