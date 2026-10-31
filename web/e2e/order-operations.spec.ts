import { openMerchantOrderDetails } from "./fixtures/merchant-order-navigation";
import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("order detail queries only after a click and unknown close remains unverified", async ({
  page,
}) => {
  let queries = 0,
    closes = 0;
  await page.route("**/console/v1/**", (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "test",
        }),
      );
    if (path.endsWith("/query")) {
      queries++;
      return route.fulfill(ok({ trade_no: "ORDER-1", status: "pending" }));
    }
    if (path.endsWith("/close")) {
      closes++;
      return route.fulfill(ok(null, 202));
    }
    const order = {
      trade_no: "ORDER-1",
      status: "pending",
      amount: "100",
      merchant_amount: "99",
      platform_fee: "1",
    };
    return route.fulfill(
      ok(
        path.endsWith("/ORDER-1")
          ? {
              order,
              provider: { channel_id: "1", method: "native", state: "pending" },
            }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await openMerchantOrderDetails(page);
  const dialog = page.getByRole("dialog", { name: "订单详情" });
  await expect(dialog).toContainText("商户入账额0.99");
  expect(queries).toBe(0);
  await dialog
    .getByRole("button", { name: "查询支付通道", exact: true })
    .click();
  await expect(dialog.locator(".alert.success")).toContainText(
    "当前订单状态：pending",
  );
  expect(queries).toBe(1);
  await dialog
    .getByRole("button", { name: "关闭通道订单", exact: true })
    .click();
  expect(closes).toBe(0);
  await dialog
    .getByRole("button", { name: "确认关闭通道订单", exact: true })
    .click();
  await expect(dialog.locator(".alert.success")).toContainText("结果尚未核实");
  await expect(dialog.locator(".alert.success")).not.toContainText(
    "关单已确认",
  );
  expect(closes).toBe(1);
});
test("a 202 refund is described as unknown even when its snapshot has a terminal label", async ({
  page,
}) => {
  let refunds = 0;
  await page.route("**/console/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "test",
        }),
      );
    if (path.endsWith("/refund")) {
      refunds++;
      return route.fulfill(
        ok(
          {
            id: "77",
            merchant_id: "1000",
            order_id: "91",
            refund_no: route.request().postDataJSON().external_no,
            amount: "1",
            merchant_amount: "1",
            fee_amount: "0",
            status: "succeeded",
          },
          202,
        ),
      );
    }
    const order = {
      id: "91",
      merchant_id: "1000",
      trade_no: "ORDER-1",
      status: "paid",
      amount: "100",
      refunded_amount: "0",
      merchant_actions: { refund: true },
    };
    return route.fulfill(
      ok(
        path.endsWith("/orders/ORDER-1")
          ? {
              order: {
                id: order.id,
                merchant_id: order.merchant_id,
                trade_no: order.trade_no,
                status: order.status,
                amount: order.amount,
              },
              reporting: { refunded_amount: "0" },
              merchant_actions: { refund: true },
            }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/merchant/trade/order");
  await page
    .getByRole("button", { name: "订单操作 ORDER-1", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "退款", exact: true }).click();
  await page.getByLabel("退款金额（元）").fill("0.01");
  await page.getByRole("button", { name: "确认申请退款", exact: true }).click();
  const refund = page.getByRole("dialog", { name: "申请退款", exact: true });
  await expect(refund.getByRole("status")).toContainText("结果尚待核对");
  await expect(refund.getByRole("status")).not.toContainText("退款成功");
  await expect(
    refund.getByRole("button", { name: "已核对，结束本次退款", exact: true }),
  ).toHaveCount(0);
  await refund.getByRole("button", { name: "取消", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("结果尚未核实");
  await expect(page.getByRole("status")).not.toContainText("退款成功");
  expect(refunds).toBe(1);
});
test("merchant console credentials are optional and existing login names are immutable during password reset", async ({
  page,
}) => {
  const row: Record<string, unknown> = {
    id: "1000",
    name: "Test merchant",
    login_username: "existing-login",
  };
  let posted: Record<string, unknown> | undefined;
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "test",
        }),
      );
    if (p.endsWith("/schema"))
      return route.fulfill(
        ok({
          title: "商户管理",
          fields: [
            { name: "name", label: "名称", type: "string", required: true },
            { name: "login_username", label: "后台登录账号", type: "string" },
            {
              name: "login_password",
              label: "后台登录密码",
              type: "password",
              write_only: true,
            },
          ],
          permissions: { create: true, update: true, delete: false },
        }),
      );
    if (req.method() === "POST" || req.method() === "PATCH") {
      posted = req.postDataJSON();
      return route.fulfill(ok(row));
    }
    return route.fulfill(
      ok(p.endsWith("/1000") ? row : { items: [row], total: 1 }),
    );
  });
  await page.goto("/admin/merchant/merchant-list");
  await page.getByRole("button", { name: "新增商户", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("名称", { exact: false }).fill("API-only");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator(".alert.success")).toContainText("新增已保存");
  expect(posted).toEqual({ name: "API-only" });
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(dialog.getByLabel("后台登录账号", { exact: false })).toHaveCount(
    0,
  );
  await expect(dialog).toContainText("已绑定登录账号：existing-login");
  await dialog
    .getByLabel("后台登录密码", { exact: false })
    .fill("new-strong-password");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator(".alert.success")).toContainText("修改已保存");
  expect(posted).toMatchObject({ login_password: "new-strong-password" });
  expect(posted).not.toHaveProperty("login_username");
});
test("administrator updates omit the immutable username and blank password", async ({
  page,
}) => {
  let payload: Record<string, unknown> | undefined;
  const item = {
    id: "3",
    username: "operator-test",
    display_name: "Operator",
    admin_class: "operator",
    enabled: true,
  };
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/schema"))
      return route.fulfill(
        ok({
          title: "管理员列表",
          fields: [
            { name: "username", label: "账号", type: "string", required: true },
            {
              name: "display_name",
              label: "名称",
              type: "string",
              required: true,
            },
            {
              name: "password",
              label: "密码",
              type: "password",
              write_only: true,
            },
            { name: "enabled", label: "启用", type: "boolean" },
          ],
          permissions: { create: true, update: true, delete: false },
        }),
      );
    if (r.method() === "PATCH") {
      payload = r.postDataJSON();
      return route.fulfill(ok(item));
    }
    return route.fulfill(
      ok(p.endsWith("/3") ? item : { items: [item], total: 1 }),
    );
  });
  await page.goto("/admin/system/admin-user");
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByLabel("账号", { exact: false })).toHaveCount(0);
  await dialog.getByLabel("启用", { exact: true }).uncheck();
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator(".alert.success")).toContainText("修改已保存");
  expect(payload).toEqual({ display_name: "Operator", enabled: false });
});
