import { test, expect } from "@playwright/test";
import { openAdminAPIRefund } from "./order-action-helpers";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("login errors, scope navigation, and mobile layout", async ({ page }) => {
  await page.route("**/console/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (path.endsWith("/session"))
      return route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({ code: 401, message: "请登录" }),
      });
    return route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({ code: 401, message: "用户名或密码错误" }),
    });
  });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto("/admin/dashboard");
  await page.getByLabel("用户名").fill("admin");
  await page.getByLabel("密码", { exact: true }).fill("invalid");
  await page.getByRole("button", { name: "登录工作台" }).click();
  await expect(page.getByRole("alert")).toHaveText("用户名或密码错误");
  await page.getByRole("link", { name: "商户后台", exact: true }).click();
  await expect(page.getByRole("heading", { name: "商户登录" })).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
test("authorized order refund uses trade number and exact decimal, blocked pages stay honest", async ({
  page,
}) => {
  let posted: unknown;
  await page.route("**/console/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "1",
            name: "操作员",
            role: "admin",
            admin_class: "super",
          },
          csrf_token: "csrf-e2e",
        }),
      );
    if (path.endsWith("/dashboard"))
      return route.fulfill(
        ok({
          orders: 1,
          paid_orders: 1,
          available_money: "12.00",
          frozen_money: "0.00",
        }),
      );
    if (
      path.endsWith("/orders/TRADE-001") &&
      route.request().method() === "GET"
    )
      return route.fulfill(
        ok({
          order: { trade_no: "TRADE-001", status: "paid", amount: "1" },
          reporting: { refunded_amount: "0" },
          admin_control: { status: "", archived: false, revision: "0" },
        }),
      );
    if (route.request().method() === "POST") {
      expect(path).toBe("/console/v1/admin/orders/TRADE-001/refund");
      expect(route.request().headers()["x-csrf-token"]).toBe("csrf-e2e");
      posted = route.request().postDataJSON();
      return route.fulfill(ok({ refund_no: "R1", status: "succeeded" }));
    }
    return route.fulfill(
      ok({
        items: [
          {
            id: "internal-id",
            trade_no: "TRADE-001",
            money: "0.01",
            status: "paid",
          },
        ],
        total: 1,
      }),
    );
  });
  await page.goto("/admin/trade/order");
  await expect(
    page.getByRole("cell", { name: "TRADE-001", exact: true }),
  ).toBeVisible();
  await openAdminAPIRefund(page, "TRADE-001");
  await page.getByLabel("退款金额（元）").fill("0.01");
  await page.getByRole("button", { name: "确认申请退款" }).click();
  await expect(page.locator(".alert.success")).toHaveText(
    "退款成功（R1），请核对退款记录和资金明细。",
  );
  expect(posted).toMatchObject({ amount: "0.01" });
  await page.goto("/admin/app/app-store");
  await expect(
    page.getByRole("heading", { name: "此功能尚未接入" }),
  ).toBeVisible();
  await page.screenshot({
    path: "test-results/unavailable-desktop.png",
    fullPage: true,
  });
  await page.goto("/admin/other/get-token");
  await expect(
    page.getByRole("heading", { name: "获取支付用户标识" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "创建授权请求" }),
  ).toBeDisabled();
});
test("R_ADMIN cannot access super-admin-only pages or hidden links", async ({
  page,
}) => {
  await page.route("**/console/v1/**", (route) =>
    route.fulfill(
      ok({
        user: {
          id: "2",
          name: "普通管理员",
          role: "admin",
          admin_class: "operator",
        },
        csrf_token: "csrf",
      }),
    ),
  );
  await page.goto("/admin/app/app-store");
  await expect(
    page.getByRole("heading", { name: "您没有访问此页面的权限" }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "应用市场", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "商户进件表单", exact: true }),
  ).toHaveCount(0);
});
test("dashboard request failure never renders invented zero counts", async ({
  page,
}) => {
  await page.route("**/console/v1/**", (route) =>
    new URL(route.request().url()).pathname.endsWith("/session")
      ? route.fulfill(
          ok({
            user: { id: "1", name: "管理员", role: "R_SUPER" },
            csrf_token: "csrf",
          }),
        )
      : route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ code: 503, message: "数据库暂不可用" }),
        }),
  );
  await page.goto("/admin/dashboard");
  await expect(page.getByRole("alert")).toHaveText("数据库暂不可用");
  await expect(page.locator(".stat")).toHaveCount(0);
});
